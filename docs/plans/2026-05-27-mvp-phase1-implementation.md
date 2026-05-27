# MVP Phase 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the first EMT MVP loop for creating, switching, closing, persisting, and resuming Codex CLI sessions inside a Wails desktop app.

**Architecture:** Keep one business entity, `Session`, persisted in `~/.emt/sessions.json`. The Go backend owns session persistence and PTY processes; the Vue frontend owns only the selected session and xterm.js rendering.

**Tech Stack:** Wails v2, Go 1.23, Vue 3, TypeScript, `github.com/creack/pty`, `@xterm/xterm`, `@xterm/addon-fit`.

---

### Task 1: Add Runtime Dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `frontend/package.json`
- Create or modify: `frontend/package-lock.json`

**Step 1: Add Go PTY dependency**

Run:

```bash
go get github.com/creack/pty@latest
go mod tidy
```

Expected: `go.mod` contains `github.com/creack/pty`, and `go.sum` is updated.

**Step 2: Add frontend terminal dependencies**

Run:

```bash
cd frontend
npm install @xterm/xterm @xterm/addon-fit
```

Expected: `frontend/package.json` contains both packages and `frontend/package-lock.json` is created or updated.

**Step 3: Verify dependency-only build still works**

Run:

```bash
go test ./...
cd frontend && npm run build
```

Expected: Go tests pass. Frontend build still compiles the scaffold.

**Step 4: Commit**

```bash
git add go.mod go.sum frontend/package.json frontend/package-lock.json
git commit -m "chore: add terminal runtime dependencies"
```

---

### Task 2: Implement Session Persistence and Codex Metadata Parsing

**Files:**
- Create: `session.go`
- Create: `session_test.go`

**Step 1: Write failing tests for session store**

Create `session_test.go` with focused tests:

```go
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
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./... -run 'TestSessionStore|TestParseCodexSessionMeta'
```

Expected: FAIL because `Session`, `NewSessionManager`, and `ParseCodexSessionMeta` do not exist.

**Step 3: Implement minimal session code**

Create `session.go`:

```go
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
```

**Step 4: Run tests to verify they pass**

Run:

```bash
gofmt -w session.go session_test.go
go test ./... -run 'TestSessionStore|TestParseCodexSessionMeta'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go
git commit -m "feat: add session persistence"
```

---

### Task 3: Implement Minimal PTY Manager

**Files:**
- Create: `pty.go`
- Create: `pty_test.go`

**Step 1: Write failing tests for Codex command args**

Create `pty_test.go`:

```go
package main

import (
	"reflect"
	"testing"
)

func TestCodexNewArgs(t *testing.T) {
	got := codexNewArgs("/tmp/work")
	want := []string{"-C", "/tmp/work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCodexResumeArgs(t *testing.T) {
	got := codexResumeArgs("019d-meta", "/tmp/work")
	want := []string{"resume", "019d-meta", "-C", "/tmp/work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
```

**Step 2: Run tests to verify they fail**

Run:

```bash
go test ./... -run 'TestCodex.*Args'
```

Expected: FAIL because arg helpers do not exist.

**Step 3: Implement PTY manager**

Create `pty.go`:

```go
package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

type TerminalDataHandler func(sessionID string, data string)
type TerminalExitHandler func(sessionID string, err error)

type PTYManager struct {
	mu       sync.Mutex
	terms    map[string]*ptySession
	onData   TerminalDataHandler
	onExit   TerminalExitHandler
}

type ptySession struct {
	cmd  *exec.Cmd
	file *os.File
}

func NewPTYManager(onData TerminalDataHandler, onExit TerminalExitHandler) *PTYManager {
	return &PTYManager{
		terms:  make(map[string]*ptySession),
		onData: onData,
		onExit: onExit,
	}
}

func codexNewArgs(workingDir string) []string {
	return []string{"-C", workingDir}
}

func codexResumeArgs(codexSessionID string, workingDir string) []string {
	return []string{"resume", codexSessionID, "-C", workingDir}
}

func (m *PTYManager) StartNew(ctx context.Context, sessionID string, workingDir string) error {
	return m.start(ctx, sessionID, codexNewArgs(workingDir))
}

func (m *PTYManager) Resume(ctx context.Context, session Session) error {
	if session.CodexSessionID == "" {
		return errors.New("codex session id is empty")
	}
	return m.start(ctx, session.ID, codexResumeArgs(session.CodexSessionID, session.WorkingDir))
}

func (m *PTYManager) start(ctx context.Context, sessionID string, args []string) error {
	m.mu.Lock()
	if _, ok := m.terms[sessionID]; ok {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	cmd := exec.CommandContext(ctx, "codex", args...)
	file, err := pty.Start(cmd)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.terms[sessionID] = &ptySession{cmd: cmd, file: file}
	m.mu.Unlock()

	go m.readLoop(sessionID, file)
	go m.waitLoop(sessionID, cmd)
	return nil
}

func (m *PTYManager) Write(sessionID string, data string) error {
	m.mu.Lock()
	term := m.terms[sessionID]
	m.mu.Unlock()
	if term == nil {
		return errors.New("terminal is not running")
	}
	_, err := io.WriteString(term.file, data)
	return err
}

func (m *PTYManager) Resize(sessionID string, rows int, cols int) error {
	m.mu.Lock()
	term := m.terms[sessionID]
	m.mu.Unlock()
	if term == nil {
		return errors.New("terminal is not running")
	}
	return pty.Setsize(term.file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

func (m *PTYManager) Close(sessionID string) error {
	m.mu.Lock()
	term := m.terms[sessionID]
	delete(m.terms, sessionID)
	m.mu.Unlock()
	if term == nil {
		return nil
	}
	return term.file.Close()
}

func (m *PTYManager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.terms))
	for id := range m.terms {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Close(id)
	}
}

func (m *PTYManager) readLoop(sessionID string, file *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := file.Read(buf)
		if n > 0 && m.onData != nil {
			m.onData(sessionID, string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

func (m *PTYManager) waitLoop(sessionID string, cmd *exec.Cmd) {
	err := cmd.Wait()
	m.mu.Lock()
	delete(m.terms, sessionID)
	m.mu.Unlock()
	if m.onExit != nil {
		m.onExit(sessionID, err)
	}
}
```

Do not add support for shells, non-Codex commands, or multiple agent types in this task.

**Step 4: Fix imports and run tests**

Run:

```bash
gofmt -w pty.go pty_test.go
go test ./... -run 'TestCodex.*Args'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add pty.go pty_test.go
git commit -m "feat: add codex pty manager"
```

---

### Task 4: Wire Wails App Methods

**Files:**
- Modify: `app.go`
- Modify: `main.go`
- Modify: `frontend/wailsjs/go/main/App.d.ts`
- Modify: `frontend/wailsjs/go/main/App.js`
- Create or modify: `frontend/wailsjs/go/models.ts`

**Step 1: Add App state and startup loading**

Replace the demo `Greet` flow in `app.go` with:

```go
type App struct {
	ctx      context.Context
	workDir  string
	sessions *SessionManager
	pty      *PTYManager
}
```

`NewApp()` should resolve:

- `workDir` using `os.Getwd()`
- store path using `os.UserHomeDir()` + `.emt/sessions.json`

`startup(ctx)` should:

- store the context
- create `PTYManager`
- call `LoadSessions()`
- mark any persisted `running` sessions as `idle`
- save the normalized list

**Step 2: Implement Wails methods**

Add:

```go
func (a *App) ListSessions() ([]Session, error)
func (a *App) CreateSession(name string) (Session, error)
func (a *App) ResumeSession(id string) error
func (a *App) CloseSession(id string) error
func (a *App) SendInput(id string, data string) error
func (a *App) ResizeTerminal(id string, rows int, cols int) error
```

Implementation rules:

- Default name format: `Session N`.
- Create session status starts as `running` only after PTY start succeeds.
- Resume returns an error if `CodexSessionID` is empty.
- Close stops only the PTY and marks the session `idle`; it does not delete the session from the list.
- Each status change saves `sessions.json`.
- Emit `terminal:data`, `terminal:exit`, and `session:updated` using Wails runtime events.

**Step 3: Add shutdown cleanup**

Modify `main.go`:

```go
OnShutdown: app.shutdown,
```

Add `shutdown(ctx context.Context)` in `app.go`:

```go
func (a *App) shutdown(ctx context.Context) {
	if a.pty != nil {
		a.pty.CloseAll()
	}
}
```

**Step 4: Regenerate Wails bindings**

Run:

```bash
wails generate module
```

Expected: `frontend/wailsjs/go/main/App.*` exposes the new methods and `frontend/wailsjs/go/models.ts` contains `main.Session`.

**Step 5: Verify backend compiles**

Run:

```bash
gofmt -w app.go main.go
go test ./...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add app.go main.go frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/main/App.js frontend/wailsjs/go/models.ts
git commit -m "feat: expose session terminal bindings"
```

---

### Task 5: Build Minimal Vue Terminal UI

**Files:**
- Modify: `frontend/src/App.vue`
- Modify: `frontend/src/main.ts`
- Modify: `frontend/src/style.css`
- Create: `frontend/src/components/Sidebar.vue`
- Create: `frontend/src/components/TerminalPanel.vue`
- Delete: `frontend/src/components/HelloWorld.vue`

**Step 1: Replace app state**

In `frontend/src/App.vue`, keep state local:

```ts
import * as models from '../wailsjs/go/models'

type Session = models.main.Session

const sessions = ref<Session[]>([])
const selectedId = ref("")
const error = ref("")
```

On mount:

- call `ListSessions()`
- register `EventsOn("session:updated", ...)`
- register `EventsOn("terminal:exit", ...)`

On unmount:

- call `EventsOff("session:updated")`
- call `EventsOff("terminal:exit")`
- call `EventsOff("terminal:data")` from `TerminalPanel.vue`

Render `TerminalPanel` with `:key="selectedId"` so xterm input handlers are recreated when the selected session changes.

**Step 2: Create Sidebar component**

`Sidebar.vue` props:

```ts
defineProps<{
  sessions: Session[]
  selectedId: string
}>()
```

Emits:

```ts
defineEmits<{
  (event: "new-session"): void
  (event: "select-session", id: string): void
  (event: "close-session", id: string): void
}>()
```

UI:

- Header `EMT`
- Icon/text button `+`
- List session name, status, and close button

**Step 3: Create TerminalPanel component**

`TerminalPanel.vue` props:

```ts
defineProps<{
  sessionId: string
}>()
```

Use:

```ts
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
```

Behavior:

- Create xterm on mount.
- Attach `onData(data => SendInput(sessionId, data))`.
- Listen to `terminal:data`; write only events matching the current `sessionId`.
- Fit on mount and window resize.
- Call `ResizeTerminal(sessionId, rows, cols)` after fit.

**Step 4: Apply focused CSS**

Replace scaffold CSS with a terminal-first layout:

- Full-height app shell.
- Left sidebar width around `260px`.
- Right terminal panel fills remaining space.
- Avoid cards inside cards.
- Use compact, readable controls.

**Step 5: Verify frontend build**

Run:

```bash
cd frontend
npm run build
```

Expected: TypeScript and Vite build pass.

**Step 6: Commit**

```bash
git add frontend/src/App.vue frontend/src/main.ts frontend/src/style.css frontend/src/components/Sidebar.vue frontend/src/components/TerminalPanel.vue
git rm frontend/src/components/HelloWorld.vue
git commit -m "feat: add session terminal interface"
```

---

### Task 6: Add Codex Session ID Discovery

**Files:**
- Modify: `session.go`
- Modify: `session_test.go`
- Modify: `app.go`

**Step 1: Write failing test for discovery**

Add a test that creates a fake Codex sessions tree:

```go
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
```

**Step 2: Run test to verify it fails**

Run:

```bash
go test ./... -run TestFindCodexSessionMetaAfter
```

Expected: FAIL because `FindCodexSessionMetaAfter` does not exist.

**Step 3: Implement minimal discovery**

Add `FindCodexSessionMetaAfter(root string, after time.Time, cwd string)`.

Rules:

- Walk `root` recursively.
- Only inspect `.jsonl` files.
- Ignore files with mod time before `after`.
- Parse `session_meta`.
- If `cwd` is non-empty, require meta cwd to match.
- Return the newest matching meta.

In `CreateSession`, record `startedAt := time.Now()` before PTY start. After PTY start succeeds, launch one goroutine that polls `FindCodexSessionMetaAfter(defaultCodexSessionRoot(), startedAt, a.workDir)` for up to 10 seconds. When found, update `CodexSessionID`, save sessions, and emit `session:updated`.

**Step 4: Run tests**

Run:

```bash
gofmt -w session.go session_test.go app.go
go test ./...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_test.go app.go
git commit -m "feat: discover codex session ids"
```

---

### Task 7: End-to-End Verification

**Files:**
- No planned source changes unless verification exposes defects.

**Step 1: Run full backend tests**

Run:

```bash
go test ./...
```

Expected: PASS.

**Step 2: Run frontend build**

Run:

```bash
cd frontend
npm run build
```

Expected: PASS.

**Step 3: Run Wails dev manually**

Run:

```bash
wails dev
```

Expected:

- App opens with sidebar and terminal area.
- New session starts Codex in the embedded terminal.
- Typing in xterm reaches Codex.
- `~/.emt/sessions.json` is created.
- After restarting `wails dev`, the session list reloads.
- Selecting the session resumes with `codex resume <session-id>`.

**Step 4: Fix any verification defects**

If verification fails, write the smallest failing test possible for backend defects. For frontend-only integration defects, make the smallest scoped code change and rerun the relevant build or manual check.

**Step 5: Final commit if fixes were needed**

```bash
git add <changed-files>
git commit -m "fix: stabilize mvp session flow"
```
