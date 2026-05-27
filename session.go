package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

type CodexSessionMeta struct {
	ID  string
	CWD string
}

func ParseCodexSessionMeta(path string) (CodexSessionMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return CodexSessionMeta{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line struct {
			Type    string `json:"type"`
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type == "session_meta" && line.Payload.ID != "" {
			return CodexSessionMeta{ID: line.Payload.ID, CWD: line.Payload.CWD}, nil
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
