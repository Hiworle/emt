package main

import (
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
