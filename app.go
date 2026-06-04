package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx         context.Context
	workDir     string
	sessions    *SessionManager
	codexSource CodexSessionSource
	pty         *PTYManager
	mu          sync.Mutex
}

// NewApp creates a new App application struct
func NewApp() *App {
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	return &App{
		workDir:     workDir,
		sessions:    newDefaultSessionManager(workDir),
		codexSource: defaultCodexSource(),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.pty = NewPTYManager(a.emitTerminalData, a.handleTerminalExit)

	sessions, err := a.sessions.LoadSessions()
	if err != nil {
		return
	}
	if normalizeRunningSessions(sessions) {
		_ = a.sessions.SaveSessions(sessions)
	}
}

func (a *App) shutdown(ctx context.Context) {
	if a.pty != nil {
		a.pty.CloseAll()
	}
}

func (a *App) ListSessions() ([]Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Session{}, a.sessions.sessions...), nil
}

func (a *App) CreateSession(name string, workingDir string) (Session, error) {
	startedAt := time.Now()
	now := startedAt.UTC()
	resolvedWorkingDir, err := a.resolveWorkingDir(workingDir)
	if err != nil {
		return Session{}, err
	}

	a.mu.Lock()
	if strings.TrimSpace(name) == "" {
		name = nextSessionName(a.sessions.sessions)
	} else {
		name = strings.TrimSpace(name)
	}
	session := Session{
		ID:           fmt.Sprintf("emt-%d", now.UnixNano()),
		Name:         name,
		WorkingDir:   resolvedWorkingDir,
		Source:       SessionSourceEMT,
		CreatedAt:    now,
		LastActiveAt: now,
		Status:       SessionStatusIdle,
	}
	ptyManager := a.ensurePTYLocked()
	ctx := a.ctx
	a.mu.Unlock()

	if err := ptyManager.StartNew(contextOrBackground(ctx), session.ID, session.WorkingDir); err != nil {
		return Session{}, err
	}

	session.Status = SessionStatusRunning
	session.LastActiveAt = time.Now().UTC()

	a.mu.Lock()
	updated := append(append([]Session(nil), a.sessions.sessions...), session)
	err = a.sessions.SaveSessions(updated)
	a.mu.Unlock()
	if err != nil {
		_ = ptyManager.Close(session.ID)
		return Session{}, err
	}

	a.emitSessionUpdated(session)
	go a.discoverCodexSessionID(session.ID, startedAt, session.WorkingDir)
	return session, nil
}

func (a *App) ResumeSession(id string) error {
	a.mu.Lock()
	index := a.sessionIndexLocked(id)
	if index < 0 {
		a.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	session := a.sessions.sessions[index]
	ptyManager := a.ensurePTYLocked()
	ctx := a.ctx
	a.mu.Unlock()

	if session.CodexSessionID == "" {
		return errors.New("codex session id is empty")
	}
	if err := ptyManager.Resume(contextOrBackground(ctx), session); err != nil {
		return err
	}

	a.mu.Lock()
	index = a.sessionIndexLocked(id)
	if index < 0 {
		a.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	a.sessions.sessions[index].Status = SessionStatusRunning
	a.sessions.sessions[index].LastActiveAt = time.Now().UTC()
	session = a.sessions.sessions[index]
	err := a.sessions.SaveSessions(a.sessions.sessions)
	a.mu.Unlock()
	if err != nil {
		return err
	}

	a.emitSessionUpdated(session)
	return nil
}

func (a *App) CloseSession(id string) error {
	a.mu.Lock()
	index := a.sessionIndexLocked(id)
	if index < 0 {
		a.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	ptyManager := a.pty
	a.mu.Unlock()

	if ptyManager != nil {
		if err := ptyManager.Close(id); err != nil {
			return err
		}
	}

	a.mu.Lock()
	index = a.sessionIndexLocked(id)
	if index < 0 {
		a.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	a.sessions.sessions[index].Status = SessionStatusIdle
	a.sessions.sessions[index].LastActiveAt = time.Now().UTC()
	session := a.sessions.sessions[index]
	err := a.sessions.SaveSessions(a.sessions.sessions)
	a.mu.Unlock()
	if err != nil {
		return err
	}

	a.emitSessionUpdated(session)
	return nil
}

func (a *App) ImportCodexSessions() (ImportResult, error) {
	root := defaultCodexSessionRoot()
	if root == "" {
		return ImportResult{}, nil
	}
	metas, failed, err := a.codexSessionSource().Scan(root)
	if err != nil {
		return ImportResult{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions.ImportCodexMetas(metas, failed)
}

func (a *App) PreviewCodexSessions() (ImportPreviewResult, error) {
	root := defaultCodexSessionRoot()
	if root == "" {
		return ImportPreviewResult{Sessions: []ImportPreviewSession{}}, nil
	}
	metas, failed, err := a.codexSessionSource().Scan(root)
	if err != nil {
		return ImportPreviewResult{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions.PreviewCodexMetas(metas, failed), nil
}

func (a *App) ImportSelectedCodexSessions(codexSessionIDs []string) (ImportResult, error) {
	root := defaultCodexSessionRoot()
	if root == "" {
		return ImportResult{}, nil
	}
	metas, failed, err := a.codexSessionSource().Scan(root)
	if err != nil {
		return ImportResult{}, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions.ImportSelectedCodexMetas(metas, failed, codexSessionIDs)
}

func (a *App) ClearImportedSessions() (ClearImportedResult, error) {
	a.mu.Lock()
	importedIDs := make([]string, 0)
	for _, session := range a.sessions.sessions {
		if session.Source == SessionSourceImported {
			importedIDs = append(importedIDs, session.ID)
		}
	}
	ptyManager := a.pty
	result, err := a.sessions.ClearImportedSessions()
	if err != nil {
		a.mu.Unlock()
		return result, err
	}

	if ptyManager != nil {
		for _, id := range importedIDs {
			_ = ptyManager.Close(id)
		}
	}
	a.mu.Unlock()
	return result, nil
}

func (a *App) RenameSession(id string, name string) (Session, error) {
	a.mu.Lock()
	session, err := a.sessions.RenameSession(id, name)
	a.mu.Unlock()
	if err != nil {
		return Session{}, err
	}

	a.emitSessionUpdated(session)
	return session, nil
}

func (a *App) DeleteSession(id string) (Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions.DeleteSession(id)
}

func (a *App) ChooseWorkingDir(defaultDir string) (string, error) {
	return choosePlatformWorkingDir(a, defaultDir)
}

func (a *App) resolveWorkingDir(workingDir string) (string, error) {
	return resolvePlatformWorkingDir(workingDir, a.workDir)
}

func resolveLocalWorkingDir(workingDir string, fallback string) (string, error) {
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		workingDir = strings.TrimSpace(fallback)
	}
	info, err := os.Stat(workingDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%q is not a directory", workingDir)
	}
	return workingDir, nil
}

func resolveWSLWorkingDir(workingDir string, fallback string) (string, error) {
	workingDir = strings.TrimSpace(workingDir)
	if workingDir == "" {
		workingDir = strings.TrimSpace(fallback)
	}
	if err := validateWSLWorkingDir(workingDir); err != nil {
		return "", err
	}
	return workingDir, nil
}

func (a *App) SendInput(id string, data string) error {
	a.mu.Lock()
	ptyManager := a.pty
	a.mu.Unlock()
	if ptyManager == nil {
		return errors.New("terminal is not running")
	}
	return ptyManager.Write(id, data)
}

func (a *App) ResizeTerminal(id string, rows int, cols int) error {
	a.mu.Lock()
	ptyManager := a.pty
	a.mu.Unlock()
	if ptyManager == nil {
		return errors.New("terminal is not running")
	}
	return ptyManager.Resize(id, rows, cols)
}

func (a *App) TerminalBuffer(id string) string {
	a.mu.Lock()
	ptyManager := a.pty
	a.mu.Unlock()
	if ptyManager == nil {
		return ""
	}
	return ptyManager.Buffer(id)
}

func (a *App) ensurePTYLocked() *PTYManager {
	if a.pty == nil {
		a.pty = NewPTYManager(a.emitTerminalData, a.handleTerminalExit)
	}
	return a.pty
}

func (a *App) codexSessionSource() CodexSessionSource {
	if a.codexSource != nil {
		return a.codexSource
	}
	return localCodexSessionSource{}
}

func (a *App) sessionIndexLocked(id string) int {
	for i := range a.sessions.sessions {
		if a.sessions.sessions[i].ID == id {
			return i
		}
	}
	return -1
}

func (a *App) emitTerminalData(sessionID string, data string) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "terminal:data", terminalDataEvent{
		SessionID: sessionID,
		Data:      data,
	})
}

func (a *App) handleTerminalExit(sessionID string, err error) {
	if a.ctx != nil {
		message := ""
		if err != nil {
			message = err.Error()
		}
		runtime.EventsEmit(a.ctx, "terminal:exit", terminalExitEvent{
			SessionID: sessionID,
			Error:     message,
		})
	}

	a.mu.Lock()
	index := a.sessionIndexLocked(sessionID)
	if index < 0 {
		a.mu.Unlock()
		return
	}
	a.sessions.sessions[index].Status = SessionStatusIdle
	a.sessions.sessions[index].LastActiveAt = time.Now().UTC()
	session := a.sessions.sessions[index]
	saveErr := a.sessions.SaveSessions(a.sessions.sessions)
	a.mu.Unlock()
	if saveErr == nil {
		a.emitSessionUpdated(session)
	}
}

func (a *App) emitSessionUpdated(session Session) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, "session:updated", session)
}

func (a *App) discoverCodexSessionID(sessionID string, startedAt time.Time, workingDir string) {
	root := defaultCodexSessionRoot()
	if root == "" {
		return
	}

	deadline := time.Now().Add(10 * time.Second)
	source := a.codexSessionSource()
	for time.Now().Before(deadline) {
		meta, err := source.FindAfter(root, startedAt, workingDir)
		if err == nil && meta.ID != "" {
			a.saveCodexSessionMeta(sessionID, meta)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (a *App) saveCodexSessionMeta(sessionID string, meta CodexSessionMeta) {
	a.mu.Lock()
	index := a.sessionIndexLocked(sessionID)
	if index < 0 {
		a.mu.Unlock()
		return
	}
	a.sessions.sessions[index].CodexSessionID = meta.ID
	a.sessions.sessions[index].CodexSessionPath = meta.Path
	a.sessions.sessions[index].LastActiveAt = time.Now().UTC()
	session := a.sessions.sessions[index]
	err := a.sessions.SaveSessions(a.sessions.sessions)
	a.mu.Unlock()
	if err == nil {
		a.emitSessionUpdated(session)
	}
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func normalizeRunningSessions(sessions []Session) bool {
	changed := false
	for i := range sessions {
		if sessions[i].Status == SessionStatusRunning {
			sessions[i].Status = SessionStatusIdle
			changed = true
		}
	}
	return changed
}

func nextSessionName(sessions []Session) string {
	return fmt.Sprintf("Session %d", len(sessions)+1)
}

type terminalDataEvent struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
}

type terminalExitEvent struct {
	SessionID string `json:"sessionId"`
	Error     string `json:"error"`
}
