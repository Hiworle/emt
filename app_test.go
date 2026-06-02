package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeRunningSessionsMarksRunningIdle(t *testing.T) {
	sessions := []Session{
		{ID: "emt-1", Status: SessionStatusRunning},
		{ID: "emt-2", Status: SessionStatusIdle},
	}

	changed := normalizeRunningSessions(sessions)

	if !changed {
		t.Fatal("expected sessions to change")
	}
	if sessions[0].Status != SessionStatusIdle {
		t.Fatalf("expected running session to become idle, got %q", sessions[0].Status)
	}
	if sessions[1].Status != SessionStatusIdle {
		t.Fatalf("expected idle session to stay idle, got %q", sessions[1].Status)
	}
}

func TestNextSessionName(t *testing.T) {
	sessions := []Session{
		{Name: "Session 1"},
		{Name: "Session 2"},
	}

	if got := nextSessionName(sessions); got != "Session 3" {
		t.Fatalf("got %q, want %q", got, "Session 3")
	}
}

func TestResolveWorkingDirUsesProvidedDir(t *testing.T) {
	app := &App{workDir: "/startup"}
	got, err := app.resolveWorkingDir(t.TempDir())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got == "/startup" {
		t.Fatalf("expected provided dir, got startup dir")
	}
}

func TestResolveWorkingDirFallsBackToStartupDir(t *testing.T) {
	dir := t.TempDir()
	app := &App{workDir: dir}
	got, err := app.resolveWorkingDir("")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
}

func TestResolveWorkingDirRejectsMissingDir(t *testing.T) {
	app := &App{workDir: t.TempDir()}
	if _, err := app.resolveWorkingDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatalf("expected missing dir error")
	}
}

func TestAppListSessionsReturnsNonNilEmptySlice(t *testing.T) {
	app := &App{sessions: NewSessionManager(filepath.Join(t.TempDir(), "sessions.json"), "/tmp/work")}

	sessions, err := app.ListSessions()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if sessions == nil {
		t.Fatal("expected non-nil empty sessions")
	}
	if len(sessions) != 0 {
		t.Fatalf("expected empty sessions, got %d", len(sessions))
	}
}

func TestAppPreviewCodexSessionsReturnsNonNilEmptySessions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	app := &App{sessions: NewSessionManager(filepath.Join(t.TempDir(), "sessions.json"), "/tmp/work")}

	result, err := app.PreviewCodexSessions()
	if err != nil {
		t.Fatalf("preview sessions: %v", err)
	}
	if result.Sessions == nil {
		t.Fatal("expected non-nil empty preview sessions")
	}
	if len(result.Sessions) != 0 {
		t.Fatalf("expected empty preview sessions, got %d", len(result.Sessions))
	}
}

func TestAppClearImportedSessionsRemovesImportedSessions(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	manager := NewSessionManager(storePath, "/tmp/work")
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := manager.SaveSessions([]Session{
		{ID: "emt-1", Name: "EMT", Source: SessionSourceEMT, CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle},
		{ID: "imported-1", Name: "Imported", Source: SessionSourceImported, CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	app := &App{sessions: manager}
	result, err := app.ClearImportedSessions()
	if err != nil {
		t.Fatalf("clear imported: %v", err)
	}
	if result.Cleared != 1 {
		t.Fatalf("expected 1 cleared, got %+v", result)
	}
	if len(manager.sessions) != 1 || manager.sessions[0].ID != "emt-1" {
		t.Fatalf("unexpected sessions: %+v", manager.sessions)
	}
}

func TestAppClearImportedSessionsKeepsPTYWhenSaveFails(t *testing.T) {
	storePath := t.TempDir()
	manager := NewSessionManager(storePath, "/tmp/work")
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	manager.sessions = []Session{
		{ID: "imported-1", Name: "Imported", Source: SessionSourceImported, CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle},
	}

	tempFile, err := os.CreateTemp(t.TempDir(), "pty")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer tempFile.Close()

	ptyManager := NewPTYManager(nil, nil)
	ptyManager.terms["imported-1"] = &ptySession{file: tempFile}
	app := &App{sessions: manager, pty: ptyManager}

	if _, err := app.ClearImportedSessions(); err == nil {
		t.Fatalf("expected clear imported error")
	}
	if _, ok := ptyManager.terms["imported-1"]; !ok {
		t.Fatalf("expected imported PTY to remain after save failure")
	}
}
