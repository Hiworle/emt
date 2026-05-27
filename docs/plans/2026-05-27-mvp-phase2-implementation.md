# MVP Phase 2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add working-directory based session management, Codex history import, and basic session maintenance while keeping `Session` as the only business entity.

**Architecture:** Extend the existing Wails app in place. `SessionManager` owns the flat JSON store and import logic, `App` exposes small Wails bindings, `PTYManager` remains Codex-only, and Vue keeps state locally in `App.vue`.

**Tech Stack:** Wails v2, Go 1.23, Vue 3, TypeScript, xterm.js, `github.com/creack/pty`.

---

### Task 1: Extend Session Model and Backward Compatibility

**Files:**
- Modify: `session.go`
- Modify: `session_test.go`

**Step 1: Write failing compatibility test**

Add a test that loads a Phase 1-style store with no `source` or `codex_session_path`.

```go
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
```

**Step 2: Run test to verify it fails**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run TestLoadSessionsNormalizesPhase1Records
```

Expected: FAIL because `Session.Source`, `SessionSourceEMT`, and normalization do not exist.

**Step 3: Implement model fields and normalization**

In `session.go`:

- Add `CodexSessionPath string`.
- Add `Source string`.
- Add `SessionSourceEMT` and `SessionSourceImported` constants.
- Add `normalizeSession(session *Session)` helper.
- Call normalization from `LoadSessions`.

Rules:

- Empty source becomes `emt`.
- Persisted `running` becomes `idle`.

**Step 4: Run tests**

Run:

```bash
gofmt -w session.go session_test.go
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run 'TestLoadSessionsNormalizesPhase1Records|TestSessionStoreRoundTrip'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: extend session metadata"
```

---

### Task 2: Extend Codex JSONL Metadata Parsing

**Files:**
- Modify: `session.go`
- Modify: `session_test.go`

**Step 1: Write failing parser tests**

Add tests:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run 'TestParseCodexSessionMetaIncludesTimestampAndPath|TestParseCodexSessionMetaFallsBackToModTime'
```

Expected: FAIL because `CodexSessionMeta` lacks `Timestamp`, `Path`, and `ModTime`.

**Step 3: Implement parser extension**

Update `CodexSessionMeta` and `ParseCodexSessionMeta`:

- Read file info at start for mod time.
- Parse top-level `timestamp`.
- Set `Path`.
- If parsed timestamp is zero, use mod time.
- Keep existing behavior of ignoring malformed non-meta lines.

**Step 4: Run tests**

Run:

```bash
gofmt -w session.go session_test.go
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run 'TestParseCodexSessionMeta|TestFindCodexSessionMetaAfter'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: parse codex session metadata"
```

---

### Task 3: Implement Codex History Import

**Files:**
- Modify: `session.go`
- Modify: `session_test.go`

**Step 1: Write failing import tests**

Add tests:

```go
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
```

Add helper:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run 'TestImportCodexSessions'
```

Expected: FAIL because `ImportCodexSessions` and `ImportResult` do not exist.

**Step 3: Implement import**

In `session.go`:

- Add `ImportResult`.
- Add `ImportCodexSessions(root string)`.
- Add helper to generate imported id.
- Add helper to generate imported name.
- Add duplicate index by `CodexSessionID`.
- Save once at end.

Do not create `Project` or import preview structures.

**Step 4: Run tests**

Run:

```bash
gofmt -w session.go session_test.go
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run 'TestImportCodexSessions|TestSessionStoreRoundTrip'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: import codex sessions"
```

---

### Task 4: Add Rename and Delete Session Operations

**Files:**
- Modify: `session.go`
- Modify: `session_test.go`

**Step 1: Write failing tests**

Add tests:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run 'TestRenameSession|TestDeleteSession'
```

Expected: FAIL because methods do not exist.

**Step 3: Implement methods**

In `session.go`:

- Add `RenameSession`.
- Add `DeleteSession`.
- Trim names and reject empty rename.
- Save after mutation.
- Return updated/deleted session for App event use.

**Step 4: Run tests**

Run:

```bash
gofmt -w session.go session_test.go
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run 'TestRenameSession|TestDeleteSession'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: add session maintenance operations"
```

---

### Task 5: Expose Phase 2 Wails Methods

**Files:**
- Modify: `app.go`
- Modify: `app_test.go`
- Modify: `main.go` if needed
- Modify: `frontend/wailsjs/go/main/App.d.ts`
- Modify: `frontend/wailsjs/go/main/App.js`
- Modify: `frontend/wailsjs/go/models.ts`

**Step 1: Write failing App tests for working directory selection**

Add tests in `app_test.go` for helper-level behavior:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase2 go test ./... -run TestResolveWorkingDir
```

Expected: FAIL because `resolveWorkingDir` does not exist.

**Step 3: Update App methods**

In `app.go`:

- Change `CreateSession(name string)` to `CreateSession(name string, workingDir string)`.
- Add `resolveWorkingDir`.
- Set `Source=SessionSourceEMT` on created sessions.
- Pass resolved working dir to PTY.
- Change discovery to `discoverCodexSessionID(session.ID, startedAt, session.WorkingDir)`.
- Save `CodexSessionPath` from discovery metadata.
- Add `ImportCodexSessions()`.
- Add `RenameSession(id, name)`.
- Add `DeleteSession(id)`.
- Add `ChooseWorkingDir(defaultDir string)` with `runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{DefaultDirectory: defaultDir})`.

If `defaultDir` is empty or does not exist, pass an empty `DefaultDirectory` so the dialog can still open.

**Step 4: Regenerate Wails bindings**

Run:

```bash
wails generate module
```

Expected: frontend bindings expose new/changed methods and models include `ImportResult` plus extended `Session`.

**Step 5: Run backend tests**

Run:

```bash
gofmt -w app.go app_test.go
GOCACHE=/tmp/go-build-emt-phase2 go test ./...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add app.go app_test.go main.go frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/main/App.js frontend/wailsjs/go/models.ts
git commit -m "feat: expose phase two session APIs"
```

---

### Task 6: Add Grouped Sidebar and Import Interaction

**Files:**
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/components/Sidebar.vue`
- Modify: `frontend/src/style.css`

**Step 1: Add grouped state in App.vue**

Add:

```ts
const search = ref('')
const notice = ref('')
const importing = ref(false)
```

Add computed `groupedSessions`:

- filter by lowercased `name`, `working_dir`, `codex_session_id`
- group by `working_dir || '(unknown directory)'`
- sort groups and sessions by `last_active_at`

**Step 2: Wire import**

Add `importSessions()`:

```ts
async function importSessions() {
  importing.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await ImportCodexSessions()
    notice.value = `Imported ${result.imported}, skipped ${result.skipped}, failed ${result.failed}`
    await loadSessions()
  } catch (err) {
    error.value = String(err)
  } finally {
    importing.value = false
  }
}
```

**Step 3: Update Sidebar props and template**

Change Sidebar to receive groups, search, and importing.

Add:

- search input
- `+ New` button
- `Import` button
- grouped directory headings
- session source label
- action buttons for rename/close/delete

Keep actions simple. Avoid adding a generic dropdown component unless needed.

**Step 4: Run frontend build**

Run:

```bash
cd frontend
npm run build
```

Expected: FAIL at first if imports/types are incomplete; fix minimal issues until PASS.

**Step 5: Commit**

```bash
git add frontend/src/App.vue frontend/src/components/Sidebar.vue frontend/src/style.css
git commit -m "feat: group and import sessions in sidebar"
```

---

### Task 7: Add New Session Dialog with Working Directory

**Files:**
- Create: `frontend/src/components/NewSessionDialog.vue`
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/style.css`

**Step 1: Create dialog component**

Props:

```ts
defineProps<{
  open: boolean
  defaultWorkingDir: string
}>()
```

Emits:

```ts
defineEmits<{
  (event: 'close'): void
  (event: 'create', payload: { name: string; workingDir: string }): void
  (event: 'browse', currentDir: string): void
}>()
```

Fields:

- session name
- working directory
- Browse
- Cancel
- Create

**Step 2: Wire dialog in App.vue**

Add:

- `newDialogOpen`
- `defaultWorkingDir`
- `createSession(payload)`
- `browseWorkingDir(currentDir)`

Update `CreateSession` call to `CreateSession(name, workingDir)`.

**Step 3: Add styles**

Add minimal modal/dialog styles. Keep it utilitarian and compact.

**Step 4: Run frontend build**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend/src/App.vue frontend/src/components/NewSessionDialog.vue frontend/src/style.css
git commit -m "feat: choose working directory for sessions"
```

---

### Task 8: Add Rename and Delete UI

**Files:**
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/components/Sidebar.vue`
- Modify: `frontend/src/style.css`

**Step 1: Wire rename**

Add `renameSession(id)` in `App.vue`.

Use a minimal inline edit or small prompt-like dialog. If using `window.prompt`, document that it is a temporary MVP compromise in code comments only if necessary. Prefer a small custom dialog if quick.

Call:

```ts
RenameSession(id, name)
```

Upsert returned session.

**Step 2: Wire delete**

Add `deleteSession(id)`:

- confirm deletion
- call `DeleteSession(id)`
- remove local session
- if selected session was deleted, select the next available session or clear selection

Confirmation copy:

```text
Remove this session from EMT? Codex history files will not be deleted.
```

**Step 3: Wire Sidebar actions**

Ensure row action clicks do not select the session accidentally.

**Step 4: Run frontend build**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS.

**Step 5: Commit**

```bash
git add frontend/src/App.vue frontend/src/components/Sidebar.vue frontend/src/style.css
git commit -m "feat: manage session names and index entries"
```

---

### Task 9: Final Verification

**Files:**
- No planned source changes unless verification exposes defects.

**Step 1: Run Go tests**

Run:

```bash
GOCACHE=/tmp/go-build-emt-phase2 go test ./...
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

1. Create a session in a non-startup directory.
2. Confirm it appears under that directory.
3. Import Codex sessions.
4. Repeat import and confirm no duplicates.
5. Search by name/path/id.
6. Rename a session.
7. Delete a session and confirm JSONL remains.
8. Resume an imported session.

**Step 4: Fix defects with the smallest scoped changes**

For backend defects, add or update a focused test first.

**Step 5: Commit fixes if needed**

```bash
git add <changed-files>
git commit -m "fix: stabilize phase two session management"
```
