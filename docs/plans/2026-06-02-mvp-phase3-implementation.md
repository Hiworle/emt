# MVP Phase 3 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace one-click full Codex history import with a preview-first selected import flow plus index-only clearing of imported sessions.

**Architecture:** Extend the existing Wails app in place. `SessionManager` owns Codex JSONL scanning, preview DTOs, selected import, and imported-session removal; `App` exposes Wails bindings and coordinates best-effort PTY cleanup; Vue keeps state local in `App.vue` and adds one dense `ImportDialog.vue`.

**Tech Stack:** Wails v2, Go 1.23, Vue 3, TypeScript, xterm.js, `github.com/creack/pty`.

---

### Task 1: Add Codex Import Preview Backend

**Files:**
- Modify: `session.go`
- Modify: `session_test.go`

**Step 1: Write failing preview tests**

Add these tests to `session_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase3 go test ./... -run 'TestPreviewCodexSessions'
```

Expected: FAIL because `PreviewCodexSessions`, preview DTOs, and preview status constants do not exist.

**Step 3: Add preview DTOs and shared scan helper**

In `session.go`, add `sort` to imports.

Add DTOs and status constants near `ImportResult`:

```go
const (
	ImportPreviewStatusNew      = "new"
	ImportPreviewStatusExisting = "existing"
)

type ImportPreviewSession struct {
	CodexSessionID   string    `json:"codex_session_id"`
	CodexSessionPath string    `json:"codex_session_path"`
	Name             string    `json:"name"`
	WorkingDir       string    `json:"working_dir"`
	CreatedAt        time.Time `json:"created_at"`
	LastActiveAt     time.Time `json:"last_active_at"`
	Status           string    `json:"status"`
}

type ImportPreviewResult struct {
	Sessions []ImportPreviewSession `json:"sessions"`
	Failed   int                    `json:"failed"`
}
```

Add the shared scanner:

```go
func scanCodexSessionCandidates(root string) ([]CodexSessionMeta, int, error) {
	var metas []CodexSessionMeta
	var failed int

	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if !info.IsDir() {
		return nil, 0, nil
	}

	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			failed++
			return nil
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		meta, err := ParseCodexSessionMeta(path)
		if err != nil {
			failed++
			return nil
		}
		metas = append(metas, meta)
		return nil
	}); err != nil {
		return nil, failed, err
	}

	return metas, failed, nil
}
```

Add `PreviewCodexSessions`:

```go
func (m *SessionManager) PreviewCodexSessions(root string) (ImportPreviewResult, error) {
	metas, failed, err := scanCodexSessionCandidates(root)
	if err != nil {
		return ImportPreviewResult{}, err
	}

	existingCodexIDs := make(map[string]bool, len(m.sessions))
	for _, session := range m.sessions {
		if session.CodexSessionID != "" {
			existingCodexIDs[session.CodexSessionID] = true
		}
	}

	result := ImportPreviewResult{Failed: failed}
	for _, meta := range metas {
		status := ImportPreviewStatusNew
		if existingCodexIDs[meta.ID] {
			status = ImportPreviewStatusExisting
		}
		result.Sessions = append(result.Sessions, ImportPreviewSession{
			CodexSessionID:   meta.ID,
			CodexSessionPath: meta.Path,
			Name:             importedSessionName(meta),
			WorkingDir:       meta.CWD,
			CreatedAt:        meta.Timestamp,
			LastActiveAt:     meta.ModTime,
			Status:           status,
		})
	}

	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].LastActiveAt.After(result.Sessions[j].LastActiveAt)
	})
	return result, nil
}
```

Do not modify `ImportCodexSessions` in this task except for imports needed by new code.

**Step 4: Run tests**

Run:

```bash
gofmt -w session.go session_test.go
GOCACHE=/tmp/go-build-emt-phase3 go test ./... -run 'TestPreviewCodexSessions|TestImportCodexSessions'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: preview codex sessions"
```

---

### Task 2: Add Selected Codex Import Backend

**Files:**
- Modify: `session.go`
- Modify: `session_test.go`

**Step 1: Write failing selected-import tests**

Add these tests to `session_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase3 go test ./... -run 'TestImportSelectedCodexSessions'
```

Expected: FAIL because `ImportSelectedCodexSessions` does not exist.

**Step 3: Implement selected import**

In `session.go`, add:

```go
func (m *SessionManager) ImportSelectedCodexSessions(root string, codexSessionIDs []string) (ImportResult, error) {
	var result ImportResult

	requested := make(map[string]bool, len(codexSessionIDs))
	for _, id := range codexSessionIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			requested[id] = true
		}
	}
	if len(requested) == 0 {
		return result, nil
	}

	metas, failed, err := scanCodexSessionCandidates(root)
	if err != nil {
		return result, err
	}
	result.Failed += failed

	existingCodexIDs := make(map[string]bool, len(m.sessions))
	existingSessionIDs := make(map[string]bool, len(m.sessions))
	for _, session := range m.sessions {
		if session.CodexSessionID != "" {
			existingCodexIDs[session.CodexSessionID] = true
		}
		existingSessionIDs[session.ID] = true
	}

	found := make(map[string]bool, len(requested))
	sessions := append([]Session(nil), m.sessions...)
	for _, meta := range metas {
		if !requested[meta.ID] {
			continue
		}
		found[meta.ID] = true
		if existingCodexIDs[meta.ID] {
			result.Skipped++
			continue
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
	}

	for id := range requested {
		if !found[id] {
			result.Failed++
		}
	}

	if result.Imported == 0 {
		return result, nil
	}
	if err := m.SaveSessions(sessions); err != nil {
		return result, err
	}
	return result, nil
}
```

Keep `ImportCodexSessions` available for compatibility. Do not change frontend usage yet.

**Step 4: Run tests**

Run:

```bash
gofmt -w session.go session_test.go
GOCACHE=/tmp/go-build-emt-phase3 go test ./... -run 'TestImportSelectedCodexSessions|TestImportCodexSessions'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: import selected codex sessions"
```

---

### Task 3: Add Clear Imported Backend

**Files:**
- Modify: `session.go`
- Modify: `session_test.go`

**Step 1: Write failing clear-imported tests**

Add these tests to `session_test.go`:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase3 go test ./... -run 'TestClearImportedSessions'
```

Expected: FAIL because `ClearImportedSessions` and `ClearImportedResult` do not exist.

**Step 3: Implement clear imported**

In `session.go`, add the DTO:

```go
type ClearImportedResult struct {
	Cleared int `json:"cleared"`
}
```

Add the method:

```go
func (m *SessionManager) ClearImportedSessions() (ClearImportedResult, error) {
	var result ClearImportedResult
	sessions := make([]Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		if session.Source == SessionSourceImported {
			result.Cleared++
			continue
		}
		sessions = append(sessions, session)
	}

	if result.Cleared == 0 {
		return result, nil
	}
	if err := m.SaveSessions(sessions); err != nil {
		return result, err
	}
	return result, nil
}
```

**Step 4: Run tests**

Run:

```bash
gofmt -w session.go session_test.go
GOCACHE=/tmp/go-build-emt-phase3 go test ./... -run 'TestClearImportedSessions|TestDeleteSessionRemovesOnlyIndex'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: clear imported sessions"
```

---

### Task 4: Expose Phase 3 Wails Methods

**Files:**
- Modify: `app.go`
- Modify: `app_test.go`
- Modify: `frontend/wailsjs/go/main/App.d.ts`
- Modify: `frontend/wailsjs/go/main/App.js`
- Modify: `frontend/wailsjs/go/models.ts`

**Step 1: Write failing App-level clear test**

Add this test to `app_test.go`:

```go
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
```

Add `time` to `app_test.go` imports.

**Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase3 go test ./... -run 'TestAppClearImportedSessionsRemovesImportedSessions'
```

Expected: FAIL because `App.ClearImportedSessions` does not exist.

**Step 3: Add App methods**

In `app.go`, add:

```go
func (a *App) PreviewCodexSessions() (ImportPreviewResult, error) {
	root := defaultCodexSessionRoot()
	if root == "" {
		return ImportPreviewResult{}, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions.PreviewCodexSessions(root)
}

func (a *App) ImportSelectedCodexSessions(codexSessionIDs []string) (ImportResult, error) {
	root := defaultCodexSessionRoot()
	if root == "" {
		return ImportResult{}, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions.ImportSelectedCodexSessions(root, codexSessionIDs)
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
	a.mu.Unlock()

	if ptyManager != nil {
		for _, id := range importedIDs {
			_ = ptyManager.Close(id)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions.ClearImportedSessions()
}
```

Keep `ImportCodexSessions()` for compatibility. The frontend will stop using it in a later task.

**Step 4: Regenerate Wails bindings**

Run:

```bash
wails generate module
```

Expected: generated files expose `PreviewCodexSessions`, `ImportSelectedCodexSessions`, `ClearImportedSessions`, `ImportPreviewSession`, `ImportPreviewResult`, and `ClearImportedResult`.

**Step 5: Run backend tests**

Run:

```bash
gofmt -w app.go app_test.go
GOCACHE=/tmp/go-build-emt-phase3 go test ./...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add app.go app_test.go frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/main/App.js frontend/wailsjs/go/models.ts
git commit -m "feat: expose phase three import APIs"
```

---

### Task 5: Add Import Preview Dialog

**Files:**
- Create: `frontend/src/components/ImportDialog.vue`
- Modify: `frontend/src/style.css`

**Step 1: Create ImportDialog component**

Create `frontend/src/components/ImportDialog.vue`:

```vue
<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import {
  ImportSelectedCodexSessions,
  PreviewCodexSessions,
} from '../../wailsjs/go/main/App'
import * as models from '../../wailsjs/go/models'

type ImportPreviewSession = models.main.ImportPreviewSession

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'imported', result: models.main.ImportResult): void
  (event: 'error', message: string): void
}>()

const loading = ref(false)
const importing = ref(false)
const sessions = ref<ImportPreviewSession[]>([])
const failed = ref(0)
const selectedIds = ref<Set<string>>(new Set())
const search = ref('')
const timeFilter = ref<'7d' | '30d' | 'all'>('30d')
const workingDirFilter = ref('')
const showExisting = ref(false)

const directoryOptions = computed(() => {
  const values = new Set<string>()
  for (const session of sessions.value) {
    if (session.working_dir) {
      values.add(session.working_dir)
    }
  }
  return Array.from(values).sort((a, b) => a.localeCompare(b))
})

const visibleSessions = computed(() => {
  const query = search.value.trim().toLowerCase()
  const cutoff = timeCutoff(timeFilter.value)
  return sessions.value.filter((session) => {
    if (!showExisting.value && session.status === 'existing') {
      return false
    }
    if (workingDirFilter.value && session.working_dir !== workingDirFilter.value) {
      return false
    }
    if (cutoff > 0 && timestampValue(session.last_active_at) < cutoff) {
      return false
    }
    if (!query) {
      return true
    }
    return [session.name, session.working_dir, session.codex_session_id]
      .filter(Boolean)
      .join(' ')
      .toLowerCase()
      .includes(query)
  })
})

const selectedCount = computed(() => selectedIds.value.size)

watch(
  () => props.open,
  async (open) => {
    if (!open) {
      return
    }
    resetFilters()
    await loadPreview()
  },
)

function resetFilters() {
  search.value = ''
  timeFilter.value = '30d'
  workingDirFilter.value = ''
  showExisting.value = false
  selectedIds.value = new Set()
}

async function loadPreview() {
  loading.value = true
  try {
    const result = await PreviewCodexSessions()
    sessions.value = result.sessions.map((session) =>
      models.main.ImportPreviewSession.createFrom(session),
    )
    failed.value = result.failed || 0
  } catch (err) {
    emit('error', String(err))
  } finally {
    loading.value = false
  }
}

function timestampValue(value: unknown): number {
  if (value instanceof Date) {
    return value.getTime()
  }
  if (typeof value === 'string') {
    const parsed = Date.parse(value)
    return Number.isNaN(parsed) ? 0 : parsed
  }
  if (typeof value === 'number') {
    return value
  }
  return 0
}

function timeCutoff(filter: '7d' | '30d' | 'all'): number {
  if (filter === 'all') {
    return 0
  }
  const days = filter === '7d' ? 7 : 30
  return Date.now() - days * 24 * 60 * 60 * 1000
}

function formatDate(value: unknown): string {
  const timestamp = timestampValue(value)
  if (!timestamp) {
    return ''
  }
  return new Date(timestamp).toLocaleString()
}

function isSelected(id: string): boolean {
  return selectedIds.value.has(id)
}

function toggleSelection(session: ImportPreviewSession) {
  if (session.status !== 'new') {
    return
  }
  const next = new Set(selectedIds.value)
  if (next.has(session.codex_session_id)) {
    next.delete(session.codex_session_id)
  } else {
    next.add(session.codex_session_id)
  }
  selectedIds.value = next
}

function selectVisible() {
  const next = new Set(selectedIds.value)
  for (const session of visibleSessions.value) {
    if (session.status === 'new') {
      next.add(session.codex_session_id)
    }
  }
  selectedIds.value = next
}

function clearSelection() {
  selectedIds.value = new Set()
}

async function importSelected() {
  if (selectedIds.value.size === 0) {
    return
  }
  importing.value = true
  try {
    const result = await ImportSelectedCodexSessions(Array.from(selectedIds.value))
    emit('imported', result)
  } catch (err) {
    emit('error', String(err))
  } finally {
    importing.value = false
  }
}
</script>

<template>
  <div v-if="open" class="dialog-backdrop" @click.self="$emit('close')">
    <section class="session-dialog import-dialog">
      <header class="dialog-header">
        <div class="dialog-title">Import Codex sessions</div>
        <button class="dialog-close" type="button" title="Close" @click="$emit('close')">x</button>
      </header>

      <div class="import-filters">
        <input
          v-model="search"
          class="dialog-input"
          type="search"
          placeholder="Search name, directory, or Codex id"
        />
        <select v-model="workingDirFilter" class="dialog-select">
          <option value="">All directories</option>
          <option v-for="directory in directoryOptions" :key="directory" :value="directory">
            {{ directory }}
          </option>
        </select>
        <select v-model="timeFilter" class="dialog-select">
          <option value="7d">Recent 7 days</option>
          <option value="30d">Recent 30 days</option>
          <option value="all">All</option>
        </select>
        <label class="dialog-checkbox">
          <input v-model="showExisting" type="checkbox" />
          <span>Show existing</span>
        </label>
      </div>

      <div class="import-summary">
        <span>{{ visibleSessions.length }} visible</span>
        <span>{{ selectedCount }} selected</span>
        <span v-if="failed > 0">Failed to parse: {{ failed }}</span>
      </div>

      <div class="import-table">
        <div class="import-row import-row-header">
          <span></span>
          <span>Name</span>
          <span>Directory</span>
          <span>Last active</span>
          <span>Status</span>
        </div>
        <button
          v-for="session in visibleSessions"
          :key="session.codex_session_id + session.codex_session_path"
          class="import-row"
          type="button"
          :class="{ existing: session.status === 'existing' }"
          @click="toggleSelection(session)"
        >
          <input
            type="checkbox"
            :checked="isSelected(session.codex_session_id)"
            :disabled="session.status !== 'new'"
            @click.stop="toggleSelection(session)"
          />
          <span class="import-name" :title="session.name">{{ session.name }}</span>
          <span class="import-path" :title="session.working_dir">{{ session.working_dir }}</span>
          <span>{{ formatDate(session.last_active_at) }}</span>
          <span class="import-status" :class="session.status">{{ session.status }}</span>
        </button>
        <div v-if="!loading && visibleSessions.length === 0" class="import-empty">No sessions</div>
        <div v-if="loading" class="import-empty">Loading sessions</div>
      </div>

      <footer class="dialog-actions import-actions">
        <button class="secondary-button" type="button" @click="selectVisible">Select visible</button>
        <button class="secondary-button" type="button" @click="clearSelection">Clear selection</button>
        <button
          class="primary-button"
          type="button"
          :disabled="selectedCount === 0 || importing"
          @click="importSelected"
        >
          {{ importing ? 'Importing' : 'Import selected' }}
        </button>
      </footer>
    </section>
  </div>
</template>
```

**Step 2: Add dialog styles**

In `frontend/src/style.css`, add compact styles for the dialog:

```css
.import-dialog {
  width: min(960px, 100%);
  max-height: calc(100vh - 48px);
}

.import-filters {
  display: grid;
  grid-template-columns: minmax(220px, 1fr) minmax(160px, 220px) 150px auto;
  gap: 8px;
  align-items: center;
}

.dialog-select {
  min-width: 0;
  height: 34px;
  border: 1px solid #343c46;
  border-radius: 6px;
  padding: 0 8px;
  color: #e7edf3;
  background: #111418;
  font: inherit;
  font-size: 13px;
}

.dialog-checkbox {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: #aeb8c4;
  font-size: 12px;
  white-space: nowrap;
}

.import-summary {
  display: flex;
  gap: 12px;
  color: #8b97a5;
  font-size: 12px;
}

.import-table {
  min-height: 220px;
  max-height: 56vh;
  overflow: auto;
  border: 1px solid #2a2f36;
  border-radius: 6px;
}

.import-row {
  display: grid;
  grid-template-columns: 28px minmax(160px, 1.1fr) minmax(180px, 1.3fr) 150px 82px;
  align-items: center;
  width: 100%;
  min-height: 36px;
  border: 0;
  border-bottom: 1px solid #242a31;
  padding: 0 8px;
  color: #cbd5df;
  background: #111418;
  font-size: 12px;
  text-align: left;
}

.import-row:not(.import-row-header):hover {
  background: #20252c;
}

.import-row-header {
  position: sticky;
  top: 0;
  z-index: 1;
  color: #8b97a5;
  background: #171a1f;
  font-weight: 700;
}

.import-row.existing {
  color: #7e8996;
}

.import-name,
.import-path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.import-path {
  font-family: "SFMono-Regular", Consolas, monospace;
}

.import-status {
  width: fit-content;
  border: 1px solid #3a434d;
  border-radius: 4px;
  padding: 1px 6px;
  color: #aeb8c4;
  background: #15191f;
}

.import-status.new {
  border-color: #3f6a5d;
  color: #b8ead9;
}

.import-empty {
  padding: 22px;
  color: #7f8994;
  font-size: 13px;
  text-align: center;
}

.import-actions {
  align-items: center;
}
```

Add a responsive fallback near the existing media query:

```css
@media (max-width: 860px) {
  .import-filters {
    grid-template-columns: 1fr;
  }

  .import-row {
    grid-template-columns: 28px minmax(140px, 1fr) minmax(140px, 1fr) 120px 74px;
  }
}
```

**Step 3: Run frontend build**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS if generated bindings from Task 4 are present. If TypeScript reports model constructor or time conversion mismatches, make the smallest local adjustment in `ImportDialog.vue`.

**Step 4: Commit**

```bash
git add frontend/src/components/ImportDialog.vue frontend/src/style.css
git commit -m "feat: add codex import preview dialog"
```

---

### Task 6: Wire Import Dialog and Clear Imported UI

**Files:**
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/components/Sidebar.vue`
- Modify: `frontend/src/style.css`

**Step 1: Update App imports and state**

In `frontend/src/App.vue`:

- Remove `ImportCodexSessions` import.
- Add `ClearImportedSessions`.
- Add `ImportDialog` component import.
- Remove `importing`.
- Add:

```ts
const importDialogOpen = ref(false)
```

**Step 2: Replace one-click import handler**

Remove `importSessions()`.

Add:

```ts
function openImportDialog() {
  error.value = ''
  notice.value = ''
  importDialogOpen.value = true
}

async function handleImported(result: models.main.ImportResult) {
  notice.value = `Imported ${result.imported}, skipped ${result.skipped}, failed ${result.failed}`
  importDialogOpen.value = false
  await loadSessions()
}

function handleImportError(message: string) {
  error.value = message
}
```

Add clear imported:

```ts
async function clearImportedSessions() {
  if (
    !window.confirm(
      'Remove all imported sessions from EMT? Codex history files and EMT-created sessions will not be deleted.',
    )
  ) {
    return
  }

  error.value = ''
  notice.value = ''
  try {
    const result = await ClearImportedSessions()
    await loadSessions()
    notice.value =
      result.cleared === 0
        ? 'No imported sessions to clear'
        : `Cleared ${result.cleared} imported sessions`
  } catch (err) {
    error.value = String(err)
  }
}
```

`loadSessions()` already clears invalid `selectedId` and selects the first remaining session, so no extra selected-session logic is needed.

**Step 3: Update App template**

Update `Sidebar` usage:

```vue
<Sidebar
  v-model:search="search"
  :groups="groupedSessions"
  :selected-id="selectedId"
  @import-sessions="openImportDialog"
  @clear-imported="clearImportedSessions"
  @new-session="openNewSessionDialog"
  @select-session="selectSession"
  @close-session="closeSession"
  @rename-session="openRenameDialog"
  @delete-session="deleteSession"
/>
```

Add the dialog near existing dialogs:

```vue
<ImportDialog
  :open="importDialogOpen"
  @close="importDialogOpen = false"
  @imported="handleImported"
  @error="handleImportError"
/>
```

**Step 4: Update Sidebar props, emits, and template**

In `frontend/src/components/Sidebar.vue`:

- Remove `importing` prop.
- Add emit:

```ts
(event: 'clear-imported'): void
```

Use an action row so button text fits:

```vue
<div class="sidebar-tools">
  <input
    class="search-input"
    type="search"
    placeholder="Search"
    :value="search"
    @input="handleSearchInput"
  />
  <div class="sidebar-action-row">
    <button class="primary-button" title="New session" @click="$emit('new-session')">+ New</button>
    <button class="secondary-button" @click="$emit('import-sessions')">Import</button>
    <button class="secondary-button" @click="$emit('clear-imported')">Clear Imported</button>
  </div>
</div>
```

Keep the brand in the header and remove the old header `+ New` button to avoid duplicate controls.

**Step 5: Update sidebar styles**

In `frontend/src/style.css`, update sidebar tools:

```css
.sidebar-header {
  justify-content: flex-start;
}

.sidebar-tools {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  border-bottom: 1px solid #242a31;
}

.sidebar-action-row {
  display: grid;
  grid-template-columns: auto auto minmax(0, 1fr);
  gap: 8px;
}

.sidebar-action-row .secondary-button:last-child {
  min-width: 0;
}
```

Remove or replace the previous `.sidebar-tools` rule that laid out search and import in one row.

**Step 6: Run frontend build**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS.

**Step 7: Commit**

```bash
git add frontend/src/App.vue frontend/src/components/Sidebar.vue frontend/src/style.css
git commit -m "feat: wire preview import controls"
```

---

### Task 7: Final Verification

**Files:**
- No planned source changes unless verification exposes defects.

**Step 1: Run all Go tests**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase3 go test ./...
```

Expected: PASS.

**Step 2: Run frontend build**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS.

**Step 3: Run Wails dev smoke test**

Run:

```bash
wails dev
```

Manual checks:

1. Click `Import`; confirm the dialog opens and the main session list does not change.
2. Confirm default filter is recent 30 days and existing sessions are hidden.
3. Toggle `Show existing` and confirm existing imported sessions are visible and disabled.
4. Change the time filter to `All`.
5. Filter by text and exact working directory.
6. Click `Select visible`; confirm only visible `new` rows are selected.
7. Click `Import selected`; confirm notice shows `Imported X, skipped Y, failed Z` and the main list refreshes.
8. Reopen import; confirm imported sessions are now `existing`.
9. Delete one imported session and reopen import; confirm it can appear as `new`.
10. Click `Clear Imported`; confirm only imported sessions disappear and EMT-created sessions remain.
11. Confirm original Codex JSONL files still exist under `~/.codex/sessions`.

**Step 4: Fix defects with minimal scoped changes**

For backend defects, add or update a focused test first. For frontend display defects, keep changes inside `App.vue`, `Sidebar.vue`, `ImportDialog.vue`, or `style.css`.

**Step 5: Commit fixes if needed**

```bash
git add <changed-files>
git commit -m "fix: stabilize phase three import flow"
```
