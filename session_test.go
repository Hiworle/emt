package main

import (
	"fmt"
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

func TestLoadSessionsMissingStoreReturnsNonNilEmptySlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	manager := NewSessionManager(path, "/tmp/work")

	sessions, err := manager.LoadSessions()
	if err != nil {
		t.Fatalf("load missing store: %v", err)
	}
	if sessions == nil {
		t.Fatal("expected non-nil empty sessions")
	}
	if len(sessions) != 0 {
		t.Fatalf("expected empty sessions, got %d", len(sessions))
	}
	if manager.sessions == nil {
		t.Fatal("expected manager sessions to be non-nil empty slice")
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
	if sessions == nil {
		t.Fatal("expected non-nil empty sessions")
	}
	if len(sessions) != 0 {
		t.Fatalf("expected empty sessions, got %d", len(sessions))
	}
	if manager.sessions == nil {
		t.Fatal("expected manager sessions to be non-nil empty slice")
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

func TestImportCodexSessionsAddsNewSessions(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	writeCodexMeta(t, root, "a.jsonl", "019d-a", "/tmp/project-a", "2026-05-27T01:00:00Z")

	manager := NewSessionManager(storePath, "/tmp/work")
	if _, err := manager.LoadSessions(); err != nil {
		t.Fatalf("load: %v", err)
	}
	result, err := manager.ImportCodexSessions(root)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(manager.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(manager.sessions))
	}
	if manager.sessions[0].Source != SessionSourceImported {
		t.Fatalf("expected imported source, got %q", manager.sessions[0].Source)
	}
	if manager.sessions[0].CodexSessionPath == "" {
		t.Fatalf("expected codex session path")
	}
}

func TestImportCodexSessionsIsIdempotent(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	writeCodexMeta(t, root, "a.jsonl", "019d-a", "/tmp/project-a", "2026-05-27T01:00:00Z")

	manager := NewSessionManager(storePath, "/tmp/work")
	_, _ = manager.LoadSessions()
	if _, err := manager.ImportCodexSessions(root); err != nil {
		t.Fatalf("first import: %v", err)
	}
	result, err := manager.ImportCodexSessions(root)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if result.Imported != 0 || result.Skipped != 1 || len(manager.sessions) != 1 {
		t.Fatalf("unexpected idempotent result: %+v len=%d", result, len(manager.sessions))
	}
}

func TestImportCodexSessionsCountsFailures(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bad.jsonl"), []byte("{bad"), 0o600); err != nil {
		t.Fatalf("write bad jsonl: %v", err)
	}

	manager := NewSessionManager(storePath, "/tmp/work")
	_, _ = manager.LoadSessions()
	result, err := manager.ImportCodexSessions(root)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Failed != 1 {
		t.Fatalf("expected 1 failed, got %+v", result)
	}
}

func TestImportSelectedCodexSessionsImportsOnlyRequestedIDs(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	writeCodexMeta(t, root, "a.jsonl", "019d-a", "/tmp/project-a", "2026-05-29T01:00:00Z")
	writeCodexMeta(t, root, "b.jsonl", "019d-b", "/tmp/project-b", "2026-05-29T02:00:00Z")

	manager := NewSessionManager(storePath, "/tmp/work")
	_, _ = manager.LoadSessions()
	result, err := manager.ImportSelectedCodexSessions(root, []string{"019d-b"})
	if err != nil {
		t.Fatalf("import selected: %v", err)
	}
	if result.Imported != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(manager.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(manager.sessions))
	}
	if manager.sessions[0].CodexSessionID != "019d-b" {
		t.Fatalf("imported wrong session: %+v", manager.sessions[0])
	}
}

func TestImportSelectedCodexSessionsEmptyInputIsNoop(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	writeCodexMeta(t, root, "a.jsonl", "019d-a", "/tmp/project-a", "2026-05-29T01:00:00Z")

	manager := NewSessionManager(storePath, "/tmp/work")
	_, _ = manager.LoadSessions()
	result, err := manager.ImportSelectedCodexSessions(root, nil)
	if err != nil {
		t.Fatalf("import selected: %v", err)
	}
	if result.Imported != 0 || result.Skipped != 0 || result.Failed != 0 || len(manager.sessions) != 0 {
		t.Fatalf("unexpected noop result: %+v len=%d", result, len(manager.sessions))
	}
}

func TestImportSelectedCodexSessionsSkipsExistingAndCountsMissing(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	writeCodexMeta(t, root, "a.jsonl", "019d-a", "/tmp/project-a", "2026-05-29T01:00:00Z")

	manager := NewSessionManager(storePath, "/tmp/work")
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := manager.SaveSessions([]Session{{
		ID: "imported-a", Name: "A", CodexSessionID: "019d-a", WorkingDir: "/tmp/project-a",
		Source: SessionSourceImported, CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := manager.ImportSelectedCodexSessions(root, []string{"019d-a", "019d-missing"})
	if err != nil {
		t.Fatalf("import selected: %v", err)
	}
	if result.Imported != 0 || result.Skipped != 1 || result.Failed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(manager.sessions) != 1 {
		t.Fatalf("expected no duplicate session, got %d", len(manager.sessions))
	}
}

func TestImportSelectedCodexSessionsDedupesScanByNewestLastActive(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	oldPath := writeCodexMeta(t, root, "duplicate-old.jsonl", "019d-duplicate", "/tmp/old", "2026-05-27T01:00:00Z")
	newPath := writeCodexMeta(t, root, "duplicate-new.jsonl", "019d-duplicate", "/tmp/new", "2026-05-29T01:00:00Z")
	oldTime := time.Date(2026, 5, 27, 1, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	manager := NewSessionManager(storePath, "/tmp/work")
	_, _ = manager.LoadSessions()
	result, err := manager.ImportSelectedCodexSessions(root, []string{"019d-duplicate"})
	if err != nil {
		t.Fatalf("import selected: %v", err)
	}
	if result.Imported != 1 || result.Skipped != 0 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(manager.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(manager.sessions))
	}
	if manager.sessions[0].CodexSessionPath != newPath {
		t.Fatalf("expected newest duplicate path %q, got %q", newPath, manager.sessions[0].CodexSessionPath)
	}
	if !manager.sessions[0].LastActiveAt.Equal(newTime) {
		t.Fatalf("expected newest duplicate mod time %v, got %v", newTime, manager.sessions[0].LastActiveAt)
	}
}

func TestPreviewCodexSessionsDoesNotMutateStore(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "sessions.json")
	root := t.TempDir()
	writeCodexMeta(t, root, "a.jsonl", "019d-a", "/tmp/project-a", "2026-05-29T01:00:00Z")

	manager := NewSessionManager(storePath, "/tmp/work")
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := manager.SaveSessions([]Session{{
		ID: "emt-1", Name: "EMT", WorkingDir: "/tmp/work", Source: SessionSourceEMT,
		CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	before, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	result, err := manager.PreviewCodexSessions(root)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 preview session, got %d", len(result.Sessions))
	}

	after, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("preview mutated store\nbefore=%s\nafter=%s", before, after)
	}
}

func TestPreviewCodexSessionsEmptyReturnsNonNilSessions(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	manager := NewSessionManager(storePath, "/tmp/work")
	_, _ = manager.LoadSessions()

	result, err := manager.PreviewCodexSessions(root)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if result.Sessions == nil {
		t.Fatal("expected non-nil empty preview sessions")
	}
	if len(result.Sessions) != 0 {
		t.Fatalf("expected empty preview sessions, got %d", len(result.Sessions))
	}
}

func TestPreviewCodexSessionsMarksExistingAndSortsByLastActive(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	oldPath := writeCodexMeta(t, root, "old.jsonl", "019d-old", "/tmp/old", "2026-05-27T01:00:00Z")
	newPath := writeCodexMeta(t, root, "new.jsonl", "019d-new", "/tmp/new", "2026-05-29T01:00:00Z")
	oldTime := time.Date(2026, 5, 27, 1, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	manager := NewSessionManager(storePath, "/tmp/work")
	if err := manager.SaveSessions([]Session{{
		ID: "imported-old", Name: "Old", CodexSessionID: "019d-old", WorkingDir: "/tmp/old",
		Source: SessionSourceImported, CreatedAt: oldTime, LastActiveAt: oldTime, Status: SessionStatusIdle,
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := manager.PreviewCodexSessions(root)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(result.Sessions) != 2 {
		t.Fatalf("expected 2 preview sessions, got %d", len(result.Sessions))
	}
	if result.Sessions[0].CodexSessionID != "019d-new" {
		t.Fatalf("expected newest first, got %+v", result.Sessions)
	}
	if result.Sessions[0].Status != ImportPreviewStatusNew {
		t.Fatalf("expected new status, got %q", result.Sessions[0].Status)
	}
	if result.Sessions[1].Status != ImportPreviewStatusExisting {
		t.Fatalf("expected existing status, got %q", result.Sessions[1].Status)
	}
}

func TestPreviewCodexSessionsDedupesScanByNewestLastActive(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	oldPath := writeCodexMeta(t, root, "duplicate-old.jsonl", "019d-duplicate", "/tmp/old", "2026-05-27T01:00:00Z")
	newPath := writeCodexMeta(t, root, "duplicate-new.jsonl", "019d-duplicate", "/tmp/new", "2026-05-29T01:00:00Z")
	oldTime := time.Date(2026, 5, 27, 1, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, newTime, newTime); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	manager := NewSessionManager(storePath, "/tmp/work")
	_, _ = manager.LoadSessions()
	result, err := manager.PreviewCodexSessions(root)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if result.Failed != 0 {
		t.Fatalf("expected no failures, got %+v", result)
	}
	if len(result.Sessions) != 1 {
		t.Fatalf("expected 1 deduped preview session, got %d: %+v", len(result.Sessions), result.Sessions)
	}
	if result.Sessions[0].CodexSessionPath != newPath {
		t.Fatalf("expected newest duplicate path %q, got %q", newPath, result.Sessions[0].CodexSessionPath)
	}
	if !result.Sessions[0].LastActiveAt.Equal(newTime) {
		t.Fatalf("expected newest duplicate mod time %v, got %v", newTime, result.Sessions[0].LastActiveAt)
	}
	if result.Sessions[0].Status != ImportPreviewStatusNew {
		t.Fatalf("expected duplicate to remain new, got %q", result.Sessions[0].Status)
	}
}

func TestPreviewCodexSessionsCountsFailures(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "bad.jsonl"), []byte("{bad"), 0o600); err != nil {
		t.Fatalf("write bad jsonl: %v", err)
	}

	manager := NewSessionManager(storePath, "/tmp/work")
	_, _ = manager.LoadSessions()
	result, err := manager.PreviewCodexSessions(root)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if result.Failed != 1 {
		t.Fatalf("expected 1 failed, got %+v", result)
	}
}

func TestClearImportedSessionsRemovesOnlyImported(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	manager := NewSessionManager(storePath, "/tmp/work")
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := manager.SaveSessions([]Session{
		{ID: "emt-1", Name: "EMT", Source: SessionSourceEMT, CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle},
		{ID: "imported-1", Name: "Imported", Source: SessionSourceImported, CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle},
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := manager.ClearImportedSessions()
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

func TestClearImportedSessionsLeavesCodexJSONL(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "sessions.json")
	jsonlPath := filepath.Join(dir, "rollout.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	manager := NewSessionManager(storePath, "/tmp/work")
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := manager.SaveSessions([]Session{{
		ID: "imported-1", Name: "Imported", CodexSessionPath: jsonlPath,
		Source: SessionSourceImported, CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := manager.ClearImportedSessions(); err != nil {
		t.Fatalf("clear imported: %v", err)
	}
	if _, err := os.Stat(jsonlPath); err != nil {
		t.Fatalf("expected jsonl to remain: %v", err)
	}
}

func TestClearImportedSessionsNoopWhenNone(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "sessions.json")
	manager := NewSessionManager(storePath, "/tmp/work")
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	if err := manager.SaveSessions([]Session{{
		ID: "emt-1", Name: "EMT", Source: SessionSourceEMT,
		CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	result, err := manager.ClearImportedSessions()
	if err != nil {
		t.Fatalf("clear imported: %v", err)
	}
	if result.Cleared != 0 || len(manager.sessions) != 1 {
		t.Fatalf("unexpected noop result: %+v len=%d", result, len(manager.sessions))
	}
}

func TestRenameSessionRejectsEmptyName(t *testing.T) {
	manager := NewSessionManager(filepath.Join(t.TempDir(), "sessions.json"), "/tmp/work")
	now := time.Now().UTC()
	if err := manager.SaveSessions([]Session{{
		ID: "emt-1", Name: "Old", WorkingDir: "/tmp/work", Source: SessionSourceEMT,
		CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := manager.RenameSession("emt-1", " "); err == nil {
		t.Fatalf("expected empty name error")
	}
}

func TestRenameSessionPersistsName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	manager := NewSessionManager(path, "/tmp/work")
	now := time.Now().UTC()
	_ = manager.SaveSessions([]Session{{
		ID: "emt-1", Name: "Old", WorkingDir: "/tmp/work", Source: SessionSourceEMT,
		CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}})

	session, err := manager.RenameSession("emt-1", "New")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if session.Name != "New" {
		t.Fatalf("expected renamed session, got %q", session.Name)
	}

	reloaded := NewSessionManager(path, "/tmp/work")
	sessions, err := reloaded.LoadSessions()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if sessions[0].Name != "New" {
		t.Fatalf("expected persisted name, got %q", sessions[0].Name)
	}
}

func TestDeleteSessionRemovesOnlyIndex(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "sessions.json")
	jsonlPath := filepath.Join(dir, "rollout.jsonl")
	if err := os.WriteFile(jsonlPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	manager := NewSessionManager(storePath, "/tmp/work")
	now := time.Now().UTC()
	_ = manager.SaveSessions([]Session{{
		ID: "emt-1", Name: "Old", CodexSessionPath: jsonlPath, WorkingDir: "/tmp/work",
		Source: SessionSourceImported, CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}})

	if _, err := manager.DeleteSession("emt-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(manager.sessions) != 0 {
		t.Fatalf("expected empty sessions, got %d", len(manager.sessions))
	}
	if _, err := os.Stat(jsonlPath); err != nil {
		t.Fatalf("expected jsonl to remain: %v", err)
	}
}

func writeCodexMeta(t *testing.T, root string, name string, id string, cwd string, timestamp string) string {
	t.Helper()
	path := filepath.Join(root, "2026", "05", "27", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	line := fmt.Sprintf(`{"timestamp":%q,"type":"session_meta","payload":{"id":%q,"cwd":%q}}`, timestamp, id, cwd) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
	return path
}
