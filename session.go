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

type Session struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	CodexSessionID string    `json:"codex_session_id"`
	WorkingDir     string    `json:"working_dir"`
	CreatedAt      time.Time `json:"created_at"`
	LastActiveAt   time.Time `json:"last_active_at"`
	Status         string    `json:"status"`
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
