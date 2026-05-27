package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	SessionStatusRunning = "running"
	SessionStatusIdle    = "idle"
	SessionStatusError   = "error"
)

const (
	SessionSourceEMT      = "emt"
	SessionSourceImported = "imported"
)

type Session struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	CodexSessionID   string    `json:"codex_session_id"`
	CodexSessionPath string    `json:"codex_session_path"`
	WorkingDir       string    `json:"working_dir"`
	Source           string    `json:"source"`
	CreatedAt        time.Time `json:"created_at"`
	LastActiveAt     time.Time `json:"last_active_at"`
	Status           string    `json:"status"`
}

type sessionFile struct {
	Sessions []Session `json:"sessions"`
}

type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
}

type SessionManager struct {
	path       string
	workingDir string
	sessions   []Session
}

func NewSessionManager(path string, workingDir string) *SessionManager {
	return &SessionManager{path: path, workingDir: workingDir}
}

func (m *SessionManager) LoadSessions() ([]Session, error) {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		m.sessions = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var file sessionFile
	if err := json.Unmarshal(data, &file); err != nil {
		backup := fmt.Sprintf("%s.bak.%d", m.path, time.Now().Unix())
		_ = os.Rename(m.path, backup)
		m.sessions = nil
		return nil, nil
	}

	for i := range file.Sessions {
		normalizeSession(&file.Sessions[i])
	}

	m.sessions = file.Sessions
	return append([]Session(nil), m.sessions...), nil
}

func (m *SessionManager) SaveSessions(sessions []Session) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(sessionFile{Sessions: sessions}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.path, data, 0o600); err != nil {
		return err
	}

	m.sessions = append([]Session(nil), sessions...)
	return nil
}

func normalizeSession(session *Session) {
	if session.Source == "" {
		session.Source = SessionSourceEMT
	}
	if session.Status == SessionStatusRunning {
		session.Status = SessionStatusIdle
	}
}

func (m *SessionManager) RenameSession(id string, name string) (Session, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Session{}, errors.New("session name cannot be empty")
	}

	for i := range m.sessions {
		if m.sessions[i].ID != id {
			continue
		}
		sessions := append([]Session(nil), m.sessions...)
		sessions[i].Name = name
		if err := m.SaveSessions(sessions); err != nil {
			return Session{}, err
		}
		return m.sessions[i], nil
	}

	return Session{}, fmt.Errorf("session %q not found", id)
}

func (m *SessionManager) DeleteSession(id string) (Session, error) {
	for i := range m.sessions {
		if m.sessions[i].ID != id {
			continue
		}
		deleted := m.sessions[i]
		sessions := append([]Session(nil), m.sessions[:i]...)
		sessions = append(sessions, m.sessions[i+1:]...)
		if err := m.SaveSessions(sessions); err != nil {
			return Session{}, err
		}
		return deleted, nil
	}

	return Session{}, fmt.Errorf("session %q not found", id)
}

func (m *SessionManager) ImportCodexSessions(root string) (ImportResult, error) {
	var result ImportResult

	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if !info.IsDir() {
		return result, nil
	}

	existingCodexIDs := make(map[string]bool, len(m.sessions))
	existingSessionIDs := make(map[string]bool, len(m.sessions))
	for _, session := range m.sessions {
		if session.CodexSessionID != "" {
			existingCodexIDs[session.CodexSessionID] = true
		}
		existingSessionIDs[session.ID] = true
	}

	sessions := append([]Session(nil), m.sessions...)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Failed++
			return nil
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		meta, err := ParseCodexSessionMeta(path)
		if err != nil {
			result.Failed++
			return nil
		}
		if existingCodexIDs[meta.ID] {
			result.Skipped++
			return nil
		}

		session := Session{
			ID:               importedSessionID(meta.ID, existingSessionIDs),
			Name:             importedSessionName(meta),
			CodexSessionID:   meta.ID,
			CodexSessionPath: meta.Path,
			WorkingDir:       meta.CWD,
			Source:           SessionSourceImported,
			CreatedAt:        meta.Timestamp,
			LastActiveAt:     meta.ModTime,
			Status:           SessionStatusIdle,
		}
		sessions = append(sessions, session)
		existingCodexIDs[meta.ID] = true
		existingSessionIDs[session.ID] = true
		result.Imported++
		return nil
	}); err != nil {
		return result, err
	}

	if result.Imported == 0 {
		return result, nil
	}
	if err := m.SaveSessions(sessions); err != nil {
		return result, err
	}
	return result, nil
}

func importedSessionID(codexSessionID string, existing map[string]bool) string {
	base := "imported-" + codexSessionID
	id := base
	for suffix := 2; existing[id]; suffix++ {
		id = fmt.Sprintf("%s-%d", base, suffix)
	}
	return id
}

func importedSessionName(meta CodexSessionMeta) string {
	timestamp := meta.Timestamp
	if timestamp.IsZero() {
		timestamp = meta.ModTime
	}
	suffix := timestamp.Format("2006-01-02 15:04")
	if meta.CWD == "" {
		return "Imported " + suffix
	}
	return fmt.Sprintf("%s %s", filepath.Base(meta.CWD), suffix)
}

type CodexSessionMeta struct {
	ID        string
	CWD       string
	Path      string
	Timestamp time.Time
	ModTime   time.Time
}

func ParseCodexSessionMeta(path string) (CodexSessionMeta, error) {
	info, err := os.Stat(path)
	if err != nil {
		return CodexSessionMeta{}, err
	}

	file, err := os.Open(path)
	if err != nil {
		return CodexSessionMeta{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line struct {
			Timestamp string `json:"timestamp"`
			Type      string `json:"type"`
			Payload   struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type == "session_meta" && line.Payload.ID != "" {
			timestamp, _ := time.Parse(time.RFC3339Nano, line.Timestamp)
			if timestamp.IsZero() {
				timestamp = info.ModTime()
			}
			return CodexSessionMeta{
				ID:        line.Payload.ID,
				CWD:       line.Payload.CWD,
				Path:      path,
				Timestamp: timestamp,
				ModTime:   info.ModTime(),
			}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return CodexSessionMeta{}, err
	}
	return CodexSessionMeta{}, errors.New("codex session_meta not found")
}

func FindCodexSessionMetaAfter(root string, after time.Time, cwd string) (CodexSessionMeta, error) {
	var newest CodexSessionMeta
	var newestModTime time.Time

	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		info, err := entry.Info()
		if err != nil || info.ModTime().Before(after) {
			return nil
		}

		meta, err := ParseCodexSessionMeta(path)
		if err != nil {
			return nil
		}
		if cwd != "" && meta.CWD != cwd {
			return nil
		}
		if newest.ID == "" || info.ModTime().After(newestModTime) {
			newest = meta
			newestModTime = info.ModTime()
		}
		return nil
	}); err != nil {
		return CodexSessionMeta{}, err
	}

	if newest.ID == "" {
		return CodexSessionMeta{}, errors.New("codex session_meta not found")
	}
	return newest, nil
}
