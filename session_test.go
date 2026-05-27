package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	manager := NewSessionManager(path, "/tmp/work")

	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	session := Session{
		ID:             "emt-1",
		Name:           "Session 1",
		CodexSessionID: "019d-test",
		WorkingDir:     "/tmp/work",
		CreatedAt:      now,
		LastActiveAt:   now,
		Status:         SessionStatusIdle,
	}

	if err := manager.SaveSessions([]Session{session}); err != nil {
		t.Fatalf("save sessions: %v", err)
	}

	loaded, err := manager.LoadSessions()
	if err != nil {
		t.Fatalf("load sessions: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 session, got %d", len(loaded))
	}
	if loaded[0].CodexSessionID != "019d-test" {
		t.Fatalf("expected codex id to round trip, got %q", loaded[0].CodexSessionID)
	}
}

func TestLoadSessionsNormalizesPhase1Records(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	data := []byte(`{
	  "sessions": [{
	    "id": "emt-1",
	    "name": "Session 1",
	    "codex_session_id": "019d-old",
	    "working_dir": "/tmp/work",
	    "created_at": "2026-05-27T01:00:00Z",
	    "last_active_at": "2026-05-27T01:10:00Z",
	    "status": "running"
	  }]
	}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write store: %v", err)
	}

	manager := NewSessionManager(path, "/tmp/work")
	sessions, err := manager.LoadSessions()
	if err != nil {
		t.Fatalf("load sessions: %v", err)
	}
	if sessions[0].Source != SessionSourceEMT {
		t.Fatalf("expected source %q, got %q", SessionSourceEMT, sessions[0].Source)
	}
	if sessions[0].Status != SessionStatusIdle {
		t.Fatalf("expected status %q, got %q", SessionStatusIdle, sessions[0].Status)
	}
}

func TestSessionStoreBacksUpCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(path, []byte("{bad json"), 0o600); err != nil {
		t.Fatalf("write corrupt json: %v", err)
	}

	manager := NewSessionManager(path, "/tmp/work")
	sessions, err := manager.LoadSessions()
	if err != nil {
		t.Fatalf("load corrupt store: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected empty sessions, got %d", len(sessions))
	}

	matches, err := filepath.Glob(path + ".bak.*")
	if err != nil {
		t.Fatalf("glob backup: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one backup, got %d", len(matches))
	}
}

func TestParseCodexSessionMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	line := `{"timestamp":"2026-05-27T01:00:00Z","type":"session_meta","payload":{"id":"019d-meta","cwd":"/tmp/work"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	meta, err := ParseCodexSessionMeta(path)
	if err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	if meta.ID != "019d-meta" || meta.CWD != "/tmp/work" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}

func TestParseCodexSessionMetaIncludesTimestampAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	line := `{"timestamp":"2026-05-27T01:00:00Z","type":"session_meta","payload":{"id":"019d-meta","cwd":"/tmp/work"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	meta, err := ParseCodexSessionMeta(path)
	if err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	if meta.ID != "019d-meta" || meta.CWD != "/tmp/work" || meta.Path != path {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if meta.Timestamp.IsZero() {
		t.Fatalf("expected timestamp")
	}
	if meta.ModTime.IsZero() {
		t.Fatalf("expected mod time")
	}
}

func TestParseCodexSessionMetaFallsBackToModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	line := `{"type":"session_meta","payload":{"id":"019d-meta","cwd":"/tmp/work"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	meta, err := ParseCodexSessionMeta(path)
	if err != nil {
		t.Fatalf("parse meta: %v", err)
	}
	if meta.Timestamp.IsZero() {
		t.Fatalf("expected fallback timestamp")
	}
	if !meta.Timestamp.Equal(meta.ModTime) {
		t.Fatalf("expected timestamp %v to equal mod time %v", meta.Timestamp, meta.ModTime)
	}
}

func TestFindCodexSessionMetaAfter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "2026", "05", "27", "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	line := `{"type":"session_meta","payload":{"id":"019d-new","cwd":"/tmp/work"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	meta, err := FindCodexSessionMetaAfter(root, time.Now().Add(-time.Minute), "/tmp/work")
	if err != nil {
		t.Fatalf("find meta: %v", err)
	}
	if meta.ID != "019d-new" {
		t.Fatalf("got %q", meta.ID)
	}
}
