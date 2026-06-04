# Windows WSL Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run EMT as a native Windows Wails app that uses the Windows IME while executing Codex sessions inside the default WSL distribution.

**Architecture:** Keep `Session` as the only persisted business entity. Refactor the backend behind small `TerminalBackend`, `SessionStore`, and `CodexSessionSource` interfaces, preserve the existing Unix behavior, then add Windows implementations backed by `wsl.exe` and ConPTY.

**Tech Stack:** Go 1.23, Wails v2, Vue 3, xterm.js, `github.com/creack/pty` for Unix PTY, `golang.org/x/sys/windows` ConPTY APIs for Windows.

---

## Constraints

- Default WSL distribution only.
- WSL absolute paths only, for example `/home/hope/proj/emt`.
- No Windows path conversion.
- No distro selector or settings page.
- Windows EMT reads and writes WSL `~/.emt/sessions.json`.
- Windows Codex history import reads WSL `~/.codex/sessions`.
- Preserve existing Linux/macOS behavior and current terminal buffer replay.

## Task 1: Add Codex Command and WSL Path Helpers

**Files:**
- Modify: `pty.go`
- Test: `pty_test.go`

**Step 1: Write failing command construction tests**

Add tests beside `TestCodexNewArgs`:

```go
func TestCodexNewCommandLocal(t *testing.T) {
	got := codexNewCommand(codexRuntimeLocal, "/tmp/work")
	want := terminalCommand{Name: "codex", Args: []string{"-C", "/tmp/work"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCodexResumeCommandLocal(t *testing.T) {
	got := codexResumeCommand(codexRuntimeLocal, "019d-meta", "/tmp/work")
	want := terminalCommand{Name: "codex", Args: []string{"resume", "019d-meta", "-C", "/tmp/work"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCodexNewCommandWSL(t *testing.T) {
	got := codexNewCommand(codexRuntimeWSL, "/home/hope/proj/emt")
	want := terminalCommand{
		Name: "wsl.exe",
		Args: []string{"--cd", "/home/hope/proj/emt", "codex", "-C", "/home/hope/proj/emt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCodexResumeCommandWSL(t *testing.T) {
	got := codexResumeCommand(codexRuntimeWSL, "019d-meta", "/home/hope/proj/emt")
	want := terminalCommand{
		Name: "wsl.exe",
		Args: []string{"--cd", "/home/hope/proj/emt", "codex", "resume", "019d-meta", "-C", "/home/hope/proj/emt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestValidateWSLWorkingDir(t *testing.T) {
	if err := validateWSLWorkingDir("/home/hope/proj/emt"); err != nil {
		t.Fatalf("expected valid WSL path: %v", err)
	}
	for _, path := range []string{"", "relative/path", `C:\Users\hope`, `\\wsl$\Debian\home\hope`} {
		if err := validateWSLWorkingDir(path); err == nil {
			t.Fatalf("expected %q to be rejected", path)
		}
	}
}
```

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./... -run 'TestCodex.*Command|TestValidateWSLWorkingDir'
```

Expected: FAIL because `terminalCommand`, `codexRuntime*`, and `validateWSLWorkingDir` do not exist.

**Step 3: Add minimal helpers**

Add to `pty.go` near the existing Codex arg helpers:

```go
type codexRuntime string

const (
	codexRuntimeLocal codexRuntime = "local"
	codexRuntimeWSL   codexRuntime = "wsl"
)

type terminalCommand struct {
	Name string
	Args []string
}

func codexNewCommand(runtime codexRuntime, workingDir string) terminalCommand {
	args := codexNewArgs(workingDir)
	if runtime == codexRuntimeWSL {
		return terminalCommand{
			Name: "wsl.exe",
			Args: append([]string{"--cd", workingDir, "codex"}, args...),
		}
	}
	return terminalCommand{Name: "codex", Args: args}
}

func codexResumeCommand(runtime codexRuntime, codexSessionID string, workingDir string) terminalCommand {
	args := codexResumeArgs(codexSessionID, workingDir)
	if runtime == codexRuntimeWSL {
		return terminalCommand{
			Name: "wsl.exe",
			Args: append([]string{"--cd", workingDir, "codex"}, args...),
		}
	}
	return terminalCommand{Name: "codex", Args: args}
}

func validateWSLWorkingDir(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("working directory must be a WSL absolute path")
	}
	return nil
}
```

Add `strings` to `pty.go` imports.

**Step 4: Run tests**

Run:

```bash
go test ./... -run 'TestCodex.*Command|TestValidateWSLWorkingDir'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add pty.go pty_test.go
git commit -m "test: define codex runtime commands"
```

## Task 2: Introduce TerminalBackend Without Changing Unix Behavior

**Files:**
- Modify: `pty.go`
- Test: `pty_test.go`

**Step 1: Write fake backend tests**

Add tests that prove `PTYManager` delegates start/write/resize/close through a backend and still buffers output:

```go
func TestPTYManagerStartsTerminalBackendCommand(t *testing.T) {
	backend := &fakeTerminalBackend{}
	manager := NewPTYManagerWithBackend(backend, codexRuntimeLocal, nil, nil)

	if err := manager.StartNew(context.Background(), "session-1", "/tmp/work"); err != nil {
		t.Fatalf("start: %v", err)
	}

	want := terminalCommand{Name: "codex", Args: []string{"-C", "/tmp/work"}}
	if !reflect.DeepEqual(backend.commands[0], want) {
		t.Fatalf("got %#v, want %#v", backend.commands[0], want)
	}
}

func TestPTYManagerWritesAndResizesBackendProcess(t *testing.T) {
	process := &fakeTerminalProcess{}
	backend := &fakeTerminalBackend{next: process}
	manager := NewPTYManagerWithBackend(backend, codexRuntimeLocal, nil, nil)

	if err := manager.StartNew(context.Background(), "session-1", "/tmp/work"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := manager.Write("session-1", "hello"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := manager.Resize("session-1", 24, 80); err != nil {
		t.Fatalf("resize: %v", err)
	}

	if process.writes.String() != "hello" {
		t.Fatalf("got writes %q", process.writes.String())
	}
	if process.rows != 24 || process.cols != 80 {
		t.Fatalf("got size %dx%d", process.rows, process.cols)
	}
}
```

Add test fakes at the bottom of `pty_test.go`:

```go
type fakeTerminalBackend struct {
	commands []terminalCommand
	next     *fakeTerminalProcess
}

func (b *fakeTerminalBackend) Start(ctx context.Context, command terminalCommand) (terminalProcess, error) {
	b.commands = append(b.commands, command)
	if b.next != nil {
		return b.next, nil
	}
	return &fakeTerminalProcess{}, nil
}

type fakeTerminalProcess struct {
	reads  strings.Reader
	writes strings.Builder
	rows   int
	cols   int
	closed bool
}

func (p *fakeTerminalProcess) Read(buf []byte) (int, error) {
	return p.reads.Read(buf)
}

func (p *fakeTerminalProcess) Write(data []byte) (int, error) {
	return p.writes.Write(data)
}

func (p *fakeTerminalProcess) Resize(rows int, cols int) error {
	p.rows = rows
	p.cols = cols
	return nil
}

func (p *fakeTerminalProcess) Close() error {
	p.closed = true
	return nil
}

func (p *fakeTerminalProcess) Wait() error {
	return nil
}
```

Add `context` to `pty_test.go` imports.

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./... -run 'TestPTYManagerStartsTerminalBackendCommand|TestPTYManagerWritesAndResizesBackendProcess'
```

Expected: FAIL because backend interfaces and constructor do not exist.

**Step 3: Refactor `PTYManager`**

In `pty.go`, add:

```go
type terminalBackend interface {
	Start(ctx context.Context, command terminalCommand) (terminalProcess, error)
}

type terminalProcess interface {
	io.Reader
	io.Writer
	Resize(rows int, cols int) error
	Close() error
	Wait() error
}
```

Change `PTYManager` fields:

```go
backend terminalBackend
runtime codexRuntime
terms   map[string]terminalProcess
```

Keep `buffers`, `onData`, and `onExit` unchanged.

Add:

```go
func NewPTYManagerWithBackend(backend terminalBackend, runtime codexRuntime, onData TerminalDataHandler, onExit TerminalExitHandler) *PTYManager {
	return &PTYManager{
		backend: backend,
		runtime: runtime,
		terms:   make(map[string]terminalProcess),
		buffers: make(map[string][]byte),
		onData:  onData,
		onExit:  onExit,
	}
}
```

Update `StartNew` and `Resume` to call `codexNewCommand` / `codexResumeCommand`, then `start(ctx, sessionID, command)`.

Update `Write` to use `term.Write([]byte(data))`.

Update `Resize` to call `term.Resize(rows, cols)`.

Update `readLoop` to accept `io.Reader`.

Update `waitLoop` to accept `terminalProcess`.

**Step 4: Add Unix backend adapter in `pty.go` temporarily**

Still in `pty.go`, add:

```go
type localPTYBackend struct{}

func (localPTYBackend) Start(ctx context.Context, command terminalCommand) (terminalProcess, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	file, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &localPTYProcess{cmd: cmd, file: file}, nil
}

type localPTYProcess struct {
	cmd  *exec.Cmd
	file *os.File
}

func (p *localPTYProcess) Read(buf []byte) (int, error) { return p.file.Read(buf) }
func (p *localPTYProcess) Write(data []byte) (int, error) { return p.file.Write(data) }
func (p *localPTYProcess) Resize(rows int, cols int) error {
	return pty.Setsize(p.file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}
func (p *localPTYProcess) Close() error { return p.file.Close() }
func (p *localPTYProcess) Wait() error { return p.cmd.Wait() }
```

Update `NewPTYManager`:

```go
func NewPTYManager(onData TerminalDataHandler, onExit TerminalExitHandler) *PTYManager {
	return NewPTYManagerWithBackend(localPTYBackend{}, codexRuntimeLocal, onData, onExit)
}
```

**Step 5: Run tests**

Run:

```bash
go test ./...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add pty.go pty_test.go
git commit -m "refactor: introduce terminal backend"
```

## Task 3: Move Unix PTY Implementation Behind Build Tags

**Files:**
- Modify: `pty.go`
- Create: `terminal_backend_unix.go`
- Create: `terminal_backend_windows.go`
- Test: `pty_test.go`

**Step 1: Move Unix-only imports and code**

Move `github.com/creack/pty`, `os`, and `os/exec` usage out of `pty.go`.

Create `terminal_backend_unix.go`:

```go
//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func defaultTerminalBackend() terminalBackend {
	return localPTYBackend{}
}

func defaultCodexRuntime() codexRuntime {
	return codexRuntimeLocal
}

type localPTYBackend struct{}

func (localPTYBackend) Start(ctx context.Context, command terminalCommand) (terminalProcess, error) {
	cmd := exec.CommandContext(ctx, command.Name, command.Args...)
	file, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &localPTYProcess{cmd: cmd, file: file}, nil
}

type localPTYProcess struct {
	cmd  *exec.Cmd
	file *os.File
}

func (p *localPTYProcess) Read(buf []byte) (int, error) { return p.file.Read(buf) }
func (p *localPTYProcess) Write(data []byte) (int, error) { return p.file.Write(data) }
func (p *localPTYProcess) Resize(rows int, cols int) error {
	return pty.Setsize(p.file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}
func (p *localPTYProcess) Close() error { return p.file.Close() }
func (p *localPTYProcess) Wait() error { return p.cmd.Wait() }
```

Create `terminal_backend_windows.go` as a stub:

```go
//go:build windows

package main

func defaultTerminalBackend() terminalBackend {
	return windowsConPTYBackend{}
}

func defaultCodexRuntime() codexRuntime {
	return codexRuntimeWSL
}
```

This intentionally will not compile on Windows until Task 9 adds `windowsConPTYBackend`.

Update `NewPTYManager`:

```go
func NewPTYManager(onData TerminalDataHandler, onExit TerminalExitHandler) *PTYManager {
	return NewPTYManagerWithBackend(defaultTerminalBackend(), defaultCodexRuntime(), onData, onExit)
}
```

**Step 2: Run tests**

Run:

```bash
go test ./...
```

Expected: PASS on Linux.

**Step 3: Commit**

```bash
git add pty.go terminal_backend_unix.go terminal_backend_windows.go
git commit -m "refactor: isolate unix terminal backend"
```

## Task 4: Add SessionStore Abstraction

**Files:**
- Modify: `session.go`
- Create: `session_store.go`
- Test: `session_test.go`

**Step 1: Write fake store tests**

Add tests:

```go
func TestSessionManagerLoadsFromStore(t *testing.T) {
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	data, err := json.Marshal(sessionFile{Sessions: []Session{{
		ID: "emt-1", Name: "Stored", Source: SessionSourceEMT,
		CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	manager := NewSessionManagerWithStore(&memorySessionStore{data: data}, "/tmp/work")
	sessions, err := manager.LoadSessions()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Name != "Stored" {
		t.Fatalf("unexpected sessions: %+v", sessions)
	}
}

func TestSessionManagerSavesToStore(t *testing.T) {
	store := &memorySessionStore{}
	manager := NewSessionManagerWithStore(store, "/tmp/work")
	now := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)

	if err := manager.SaveSessions([]Session{{
		ID: "emt-1", Name: "Saved", Source: SessionSourceEMT,
		CreatedAt: now, LastActiveAt: now, Status: SessionStatusIdle,
	}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.Contains(string(store.data), `"name": "Saved"`) {
		t.Fatalf("expected saved JSON, got %s", store.data)
	}
}
```

Add fake store:

```go
type memorySessionStore struct {
	data []byte
	err  error
}

func (s *memorySessionStore) Load() ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte(nil), s.data...), nil
}

func (s *memorySessionStore) Save(data []byte) error {
	if s.err != nil {
		return s.err
	}
	s.data = append([]byte(nil), data...)
	return nil
}
```

Add `encoding/json` and `strings` to `session_test.go` imports if missing.

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./... -run 'TestSessionManager.*Store'
```

Expected: FAIL because store constructors do not exist.

**Step 3: Implement store abstraction**

Create `session_store.go`:

```go
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type sessionStore interface {
	Load() ([]byte, error)
	Save(data []byte) error
}

type localSessionStore struct {
	path string
}

func newLocalSessionStore(path string) *localSessionStore {
	return &localSessionStore{path: path}
}

func (s *localSessionStore) Load() ([]byte, error) {
	return os.ReadFile(s.path)
}

func (s *localSessionStore) Save(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

func backupCorruptLocalSessionStore(path string) {
	backup := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
	_ = os.Rename(path, backup)
}

func isStoreNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
```

Modify `SessionManager`:

```go
type SessionManager struct {
	path       string
	store      sessionStore
	workingDir string
	sessions   []Session
}
```

Update constructors:

```go
func NewSessionManager(path string, workingDir string) *SessionManager {
	return &SessionManager{path: path, store: newLocalSessionStore(path), workingDir: workingDir}
}

func NewSessionManagerWithStore(store sessionStore, workingDir string) *SessionManager {
	return &SessionManager{store: store, workingDir: workingDir}
}
```

Update `LoadSessions` to call `m.store.Load()` and treat missing or empty data as an empty session list:

```go
data, err := m.store.Load()
if isStoreNotExist(err) || len(strings.TrimSpace(string(data))) == 0 {
	m.sessions = []Session{}
	return []Session{}, nil
}
```

When JSON unmarshal fails, only call `backupCorruptLocalSessionStore(m.path)` if `m.path != ""`.

Update `SaveSessions` to marshal JSON and call `m.store.Save(data)`.

**Step 4: Run tests**

Run:

```bash
go test ./...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add session.go session_store.go session_test.go
git commit -m "refactor: abstract session storage"
```

## Task 5: Add WSL Command Runner and WSL Session Store

**Files:**
- Create: `wsl_runner.go`
- Create: `wsl_session_store.go`
- Test: `wsl_session_store_test.go`

**Step 1: Write WSL store tests**

Create `wsl_session_store_test.go`:

```go
package main

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func TestWSLSessionStoreLoadUsesDefaultDistro(t *testing.T) {
	runner := &fakeCommandRunner{stdout: []byte(`{"sessions":[]}`)}
	store := newWSLSessionStore(runner)

	data, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if string(data) != `{"sessions":[]}` {
		t.Fatalf("got %s", data)
	}

	want := commandCall{Name: "wsl.exe", Args: []string{"sh", "-lc", "test -f ~/.emt/sessions.json && cat ~/.emt/sessions.json || true"}}
	if !reflect.DeepEqual(runner.calls[0].withoutStdin(), want) {
		t.Fatalf("got %#v, want %#v", runner.calls[0].withoutStdin(), want)
	}
}

func TestWSLSessionStoreSaveStreamsJSONToStdin(t *testing.T) {
	runner := &fakeCommandRunner{}
	store := newWSLSessionStore(runner)

	data := []byte(`{"sessions":[]}`)
	if err := store.Save(data); err != nil {
		t.Fatalf("save: %v", err)
	}
	if !bytes.Equal(runner.calls[0].Stdin, data) {
		t.Fatalf("stdin mismatch: %s", runner.calls[0].Stdin)
	}
	if runner.calls[0].Name != "wsl.exe" {
		t.Fatalf("expected wsl.exe, got %q", runner.calls[0].Name)
	}
}
```

Add fake runner:

```go
type commandCall struct {
	Name  string
	Args  []string
	Stdin []byte
}

func (c commandCall) withoutStdin() commandCall {
	c.Stdin = nil
	return c
}

type fakeCommandRunner struct {
	calls  []commandCall
	stdout []byte
	err    error
}

func (r *fakeCommandRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	r.calls = append(r.calls, commandCall{Name: name, Args: append([]string(nil), args...), Stdin: append([]byte(nil), stdin...)})
	if r.err != nil {
		return nil, r.err
	}
	return append([]byte(nil), r.stdout...), nil
}
```

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./... -run 'TestWSLSessionStore'
```

Expected: FAIL because WSL store does not exist.

**Step 3: Implement command runner**

Create `wsl_runner.go`:

```go
package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

type commandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, err
	}
	return out, nil
}
```

Add `strings` to imports.

**Step 4: Implement WSL session store**

Create `wsl_session_store.go`:

```go
package main

import "context"

const wslSessionFile = "~/.emt/sessions.json"

type wslSessionStore struct {
	runner commandRunner
}

func newWSLSessionStore(runner commandRunner) *wslSessionStore {
	return &wslSessionStore{runner: runner}
}

func (s *wslSessionStore) Load() ([]byte, error) {
	return s.runner.Run(context.Background(), "wsl.exe", []string{
		"sh", "-lc", "test -f " + wslSessionFile + " && cat " + wslSessionFile + " || true",
	}, nil)
}

func (s *wslSessionStore) Save(data []byte) error {
	_, err := s.runner.Run(context.Background(), "wsl.exe", []string{
		"sh", "-lc", "mkdir -p ~/.emt && tmp=$(mktemp ~/.emt/sessions.json.tmp.XXXXXX) && cat > \"$tmp\" && mv \"$tmp\" " + wslSessionFile,
	}, data)
	return err
}
```

**Step 5: Run tests**

Run:

```bash
go test ./... -run 'TestWSLSessionStore'
go test ./...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add wsl_runner.go wsl_session_store.go wsl_session_store_test.go
git commit -m "feat: add wsl session store"
```

## Task 6: Add CodexSessionSource Abstraction

**Files:**
- Modify: `session.go`
- Create: `codex_source.go`
- Test: `session_test.go`

**Step 1: Write reader parser test**

Add:

```go
func TestParseCodexSessionMetaFromReader(t *testing.T) {
	modTime := time.Date(2026, 5, 29, 1, 0, 0, 0, time.UTC)
	reader := strings.NewReader(`{"timestamp":"2026-05-27T01:00:00Z","type":"session_meta","payload":{"id":"019d-meta","cwd":"/tmp/work"}}` + "\n")

	meta, err := ParseCodexSessionMetaFromReader("/wsl/rollout.jsonl", modTime, reader)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if meta.ID != "019d-meta" || meta.CWD != "/tmp/work" || meta.Path != "/wsl/rollout.jsonl" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if !meta.ModTime.Equal(modTime) {
		t.Fatalf("expected mod time %v, got %v", modTime, meta.ModTime)
	}
}
```

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./... -run TestParseCodexSessionMetaFromReader
```

Expected: FAIL because parser does not exist.

**Step 3: Extract parser and source**

Create `codex_source.go`:

```go
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"time"
)

type CodexSessionSource interface {
	Scan(root string) ([]CodexSessionMeta, int, error)
	FindAfter(root string, after time.Time, cwd string) (CodexSessionMeta, error)
}

type localCodexSessionSource struct{}

func (localCodexSessionSource) Scan(root string) ([]CodexSessionMeta, int, error) {
	return scanCodexSessionCandidates(root)
}

func (localCodexSessionSource) FindAfter(root string, after time.Time, cwd string) (CodexSessionMeta, error) {
	return FindCodexSessionMetaAfter(root, after, cwd)
}

func ParseCodexSessionMetaFromReader(path string, modTime time.Time, reader io.Reader) (CodexSessionMeta, error) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var line struct {
			Timestamp string `json:"timestamp"`
			Type      string `json:"type"`
			Payload   struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type == "session_meta" && line.Payload.ID != "" {
			timestamp, _ := time.Parse(time.RFC3339Nano, line.Timestamp)
			if timestamp.IsZero() {
				timestamp = modTime
			}
			return CodexSessionMeta{
				ID:        line.Payload.ID,
				CWD:       line.Payload.CWD,
				Path:      path,
				Timestamp: timestamp,
				ModTime:   modTime,
			}, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return CodexSessionMeta{}, err
	}
	return CodexSessionMeta{}, errors.New("codex session_meta not found")
}
```

Update `ParseCodexSessionMeta` to open the file and call `ParseCodexSessionMetaFromReader(path, info.ModTime(), file)`.

Remove duplicated parser imports from `session.go`.

**Step 4: Thread source through App**

Add to `App`:

```go
codexSource CodexSessionSource
```

Set in `NewApp`:

```go
codexSource: localCodexSessionSource{},
```

Update `ImportCodexSessions`, `PreviewCodexSessions`, `ImportSelectedCodexSessions`, and `discoverCodexSessionID` to use `a.codexSource`.

Minimal internal changes:

```go
metas, failed, err := a.codexSource.Scan(root)
```

Then add `SessionManager` methods that consume already-scanned metas:

```go
func (m *SessionManager) ImportCodexMetas(metas []CodexSessionMeta, failed int) (ImportResult, error)
func (m *SessionManager) PreviewCodexMetas(metas []CodexSessionMeta, failed int) ImportPreviewResult
func (m *SessionManager) ImportSelectedCodexMetas(metas []CodexSessionMeta, failed int, codexSessionIDs []string) (ImportResult, error)
```

Keep existing `ImportCodexSessions(root)`, `PreviewCodexSessions(root)`, and `ImportSelectedCodexSessions(root, ids)` as compatibility wrappers that call `localCodexSessionSource{}.Scan(root)`.

**Step 5: Run tests**

Run:

```bash
go test ./...
```

Expected: PASS.

**Step 6: Commit**

```bash
git add app.go session.go codex_source.go session_test.go
git commit -m "refactor: abstract codex history source"
```

## Task 7: Add WSL Codex History Source

**Files:**
- Create: `wsl_codex_source.go`
- Test: `wsl_codex_source_test.go`

**Step 1: Write WSL source tests**

Create `wsl_codex_source_test.go`:

```go
package main

import (
	"context"
	"testing"
)

func TestWSLCodexSessionSourceScansMetas(t *testing.T) {
	listOutput := "/home/hope/.codex/sessions/a.jsonl\t1780016400\n"
	fileOutput := `{"timestamp":"2026-05-29T00:30:00Z","type":"session_meta","payload":{"id":"019d-a","cwd":"/home/hope/proj/emt"}}` + "\n"
	runner := &fakeCommandRunnerSequence{outputs: [][]byte{[]byte(listOutput), []byte(fileOutput)}}
	source := newWSLCodexSessionSource(runner)

	metas, failed, err := source.Scan("~/.codex/sessions")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if failed != 0 || len(metas) != 1 {
		t.Fatalf("unexpected scan failed=%d metas=%+v", failed, metas)
	}
	if metas[0].ID != "019d-a" || metas[0].CWD != "/home/hope/proj/emt" {
		t.Fatalf("unexpected meta: %+v", metas[0])
	}
}
```

Add sequence fake:

```go
type fakeCommandRunnerSequence struct {
	outputs [][]byte
	calls   []commandCall
}

func (r *fakeCommandRunnerSequence) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, error) {
	r.calls = append(r.calls, commandCall{Name: name, Args: append([]string(nil), args...), Stdin: append([]byte(nil), stdin...)})
	out := r.outputs[0]
	r.outputs = r.outputs[1:]
	return out, nil
}
```

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./... -run TestWSLCodexSessionSourceScansMetas
```

Expected: FAIL because WSL source does not exist.

**Step 3: Implement WSL source**

Create `wsl_codex_source.go`:

```go
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type wslCodexSessionSource struct {
	runner commandRunner
}

func newWSLCodexSessionSource(runner commandRunner) *wslCodexSessionSource {
	return &wslCodexSessionSource{runner: runner}
}

func (s *wslCodexSessionSource) Scan(root string) ([]CodexSessionMeta, int, error) {
	listScript := fmt.Sprintf(`test -d %s || exit 0; find %s -type f -name '*.jsonl' -printf '%%p\t%%T@\n'`, root, root)
	out, err := s.runner.Run(context.Background(), "wsl.exe", []string{"sh", "-lc", listScript}, nil)
	if err != nil {
		return nil, 0, err
	}

	var metas []CodexSessionMeta
	var failed int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 {
			failed++
			continue
		}
		path := fields[0]
		seconds, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			failed++
			continue
		}
		wholeSeconds := int64(seconds)
		nanos := int64((seconds - float64(wholeSeconds)) * 1e9)
		modTime := time.Unix(wholeSeconds, nanos).UTC()
		data, err := s.runner.Run(context.Background(), "wsl.exe", []string{"cat", path}, nil)
		if err != nil {
			failed++
			continue
		}
		meta, err := ParseCodexSessionMetaFromReader(path, modTime, bytes.NewReader(data))
		if err != nil {
			failed++
			continue
		}
		metas = append(metas, meta)
	}
	return metas, failed, nil
}

func (s *wslCodexSessionSource) FindAfter(root string, after time.Time, cwd string) (CodexSessionMeta, error) {
	metas, _, err := s.Scan(root)
	if err != nil {
		return CodexSessionMeta{}, err
	}
	var newest CodexSessionMeta
	for _, meta := range metas {
		if meta.ModTime.Before(after) {
			continue
		}
		if cwd != "" && meta.CWD != cwd {
			continue
		}
		if newest.ID == "" || meta.ModTime.After(newest.ModTime) {
			newest = meta
		}
	}
	if newest.ID == "" {
		return CodexSessionMeta{}, errors.New("codex session_meta not found")
	}
	return newest, nil
}
```

If shell path quoting becomes necessary, add a small `shellQuote` helper and tests before using it. The first version only passes constant roots and WSL paths already controlled by EMT.

**Step 4: Run tests**

Run:

```bash
go test ./... -run 'TestWSLCodexSessionSource|TestParseCodexSessionMetaFromReader'
go test ./...
```

Expected: PASS.

**Step 5: Commit**

```bash
git add wsl_codex_source.go wsl_codex_source_test.go
git commit -m "feat: add wsl codex history source"
```

## Task 8: Wire Platform Defaults

**Files:**
- Modify: `app.go`
- Create: `app_platform_unix.go`
- Create: `app_platform_windows.go`
- Test: `app_test.go`

**Step 1: Write app environment tests**

Add tests using explicit constructors instead of build-tag-dependent defaults:

```go
func TestResolveWSLWorkingDirRejectsWindowsPaths(t *testing.T) {
	for _, path := range []string{`C:\Users\hope`, `\\wsl$\Debian\home\hope`, "relative"} {
		if _, err := resolveWSLWorkingDir(path, ""); err == nil {
			t.Fatalf("expected %q to be rejected", path)
		}
	}
}

func TestResolveWSLWorkingDirAcceptsAbsoluteWSLPath(t *testing.T) {
	got, err := resolveWSLWorkingDir("/home/hope/proj/emt", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got != "/home/hope/proj/emt" {
		t.Fatalf("got %q", got)
	}
}
```

**Step 2: Run tests and verify failure**

Run:

```bash
go test ./... -run TestResolveWSLWorkingDir
```

Expected: FAIL because `resolveWSLWorkingDir` does not exist.

**Step 3: Add platform environment helpers**

Create `app_platform_unix.go`:

```go
//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func newDefaultSessionManager(workDir string) *SessionManager {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = workDir
	}
	return NewSessionManager(filepath.Join(homeDir, ".emt", "sessions.json"), workDir)
}

func defaultCodexSource() CodexSessionSource {
	return localCodexSessionSource{}
}

func resolvePlatformWorkingDir(workingDir string, fallback string) (string, error) {
	return resolveLocalWorkingDir(workingDir, fallback)
}
```

Create `app_platform_windows.go`:

```go
//go:build windows

package main

func newDefaultSessionManager(workDir string) *SessionManager {
	return NewSessionManagerWithStore(newWSLSessionStore(execCommandRunner{}), workDir)
}

func defaultCodexSource() CodexSessionSource {
	return newWSLCodexSessionSource(execCommandRunner{})
}

func resolvePlatformWorkingDir(workingDir string, fallback string) (string, error) {
	return resolveWSLWorkingDir(workingDir, fallback)
}
```

Move the current `resolveWorkingDir` logic into:

```go
func resolveLocalWorkingDir(workingDir string, fallback string) (string, error)
```

Add:

```go
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
```

Update `App.resolveWorkingDir` to call `resolvePlatformWorkingDir(workingDir, a.workDir)`.

Update `NewApp` to use:

```go
return &App{
	workDir:     workDir,
	sessions:   newDefaultSessionManager(workDir),
	codexSource: defaultCodexSource(),
}
```

**Step 4: Move `defaultCodexSessionRoot`**

Move current Unix implementation to `app_platform_unix.go`.

Add Windows implementation to `app_platform_windows.go`:

```go
func defaultCodexSessionRoot() string {
	return "~/.codex/sessions"
}
```

**Step 5: Run tests**

Run:

```bash
go test ./...
```

Expected: PASS on Linux.

**Step 6: Commit**

```bash
git add app.go app_test.go app_platform_unix.go app_platform_windows.go
git commit -m "feat: wire platform defaults"
```

## Task 9: Add Windows ConPTY Backend

**Files:**
- Modify: `terminal_backend_windows.go`

**Step 1: Implement ConPTY process**

Replace the Windows stub with an implementation using `golang.org/x/sys/windows`.

Implementation outline:

```go
//go:build windows

package main

import (
	"context"
	"os"
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsConPTYBackend struct{}

func defaultTerminalBackend() terminalBackend {
	return windowsConPTYBackend{}
}

func defaultCodexRuntime() codexRuntime {
	return codexRuntimeWSL
}

func (windowsConPTYBackend) Start(ctx context.Context, command terminalCommand) (terminalProcess, error) {
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		return nil, err
	}

	var pc windows.Handle
	size := windows.Coord{X: 80, Y: 24}
	if err := windows.CreatePseudoConsole(size, windows.Handle(inputReader.Fd()), windows.Handle(outputWriter.Fd()), 0, &pc); err != nil {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		return nil, err
	}

	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(pc)
		return nil, err
	}
	if err := attrList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(pc), unsafe.Sizeof(pc)); err != nil {
		attrList.Delete()
		windows.ClosePseudoConsole(pc)
		return nil, err
	}

	args := append([]string{command.Name}, command.Args...)
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		attrList.Delete()
		windows.ClosePseudoConsole(pc)
		return nil, err
	}

	startupInfo := &windows.StartupInfoEx{}
	startupInfo.StartupInfo.Cb = uint32(unsafe.Sizeof(*startupInfo))
	startupInfo.ProcThreadAttributeList = attrList.List()
	var processInfo windows.ProcessInformation
	if err := windows.CreateProcess(
		nil,
		commandLine,
		nil,
		nil,
		false,
		windows.EXTENDED_STARTUPINFO_PRESENT,
		nil,
		nil,
		&startupInfo.StartupInfo,
		&processInfo,
	); err != nil {
		attrList.Delete()
		windows.ClosePseudoConsole(pc)
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
		return nil, err
	}

	_ = inputReader.Close()
	_ = outputWriter.Close()

	process := &windowsConPTYProcess{
		process:      processInfo.Process,
		thread:       processInfo.Thread,
		pconsole:     pc,
		attrs:        attrList,
		inputWriter:  inputWriter,
		outputReader: outputReader,
	}
	go func() {
		<-ctx.Done()
		_ = process.Close()
	}()
	return process, nil
}

type windowsConPTYProcess struct {
	process      windows.Handle
	thread       windows.Handle
	pconsole     windows.Handle
	attrs        *windows.ProcThreadAttributeListContainer
	inputWriter  *os.File
	outputReader *os.File
	closeOnce    sync.Once
}

func (p *windowsConPTYProcess) Read(buf []byte) (int, error) { return p.outputReader.Read(buf) }
func (p *windowsConPTYProcess) Write(data []byte) (int, error) { return p.inputWriter.Write(data) }
func (p *windowsConPTYProcess) Resize(rows int, cols int) error {
	return windows.ResizePseudoConsole(p.pconsole, windows.Coord{X: int16(cols), Y: int16(rows)})
}
func (p *windowsConPTYProcess) Close() error {
	_ = p.inputWriter.Close()
	_ = p.outputReader.Close()
	_ = windows.TerminateProcess(p.process, 1)
	return nil
}
func (p *windowsConPTYProcess) Wait() error {
	_, err := windows.WaitForSingleObject(p.process, windows.INFINITE)
	if err != nil {
		p.cleanup()
		return err
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(p.process, &exitCode); err != nil {
		p.cleanup()
		return err
	}
	p.cleanup()
	if exitCode != 0 {
		return fmt.Errorf("process exited with code %d", exitCode)
	}
	return nil
}

func (p *windowsConPTYProcess) cleanup() {
	p.closeOnce.Do(func() {
		_ = p.inputWriter.Close()
		_ = p.outputReader.Close()
		windows.ClosePseudoConsole(p.pconsole)
		p.attrs.Delete()
		_ = windows.CloseHandle(p.thread)
		_ = windows.CloseHandle(p.process)
	})
}
```

This code must use `windows.CreateProcess` directly. Do not use `exec.Command`, because Go's `syscall.SysProcAttr` does not expose the pseudo console process-thread attribute list.

Adjust names and types to match the exact `golang.org/x/sys/windows` version in `go.mod`.

**Step 2: Compile Linux tests**

Run:

```bash
go test ./...
```

Expected: PASS on Linux. This does not compile Windows-only files.

**Step 3: Cross-compile Windows Go package if possible**

Run:

```bash
GOOS=windows GOARCH=amd64 go test ./... -run '^$'
```

Expected: PASS if Wails and Windows dependencies cross-compile cleanly. If Wails cross-compile fails for environment reasons, run a narrower compile:

```bash
GOOS=windows GOARCH=amd64 go test -c
```

Expected: package compiles or reports concrete Windows API field errors to fix.

**Step 4: Commit**

```bash
git add terminal_backend_windows.go
git commit -m "feat: add windows conpty backend"
```

## Task 10: Disable Windows Directory Picker and Improve Errors

**Files:**
- Modify: `app.go`
- Create: `dialog_windows.go`
- Create: `dialog_unix.go`
- Test: `app_test.go`

**Step 1: Add platform dialog function**

Create `dialog_unix.go`:

```go
//go:build !windows

package main

import (
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func choosePlatformWorkingDir(app *App, defaultDir string) (string, error) {
	options := runtime.OpenDialogOptions{}
	defaultDir = strings.TrimSpace(defaultDir)
	if defaultDir != "" {
		if info, err := os.Stat(defaultDir); err == nil && info.IsDir() {
			options.DefaultDirectory = defaultDir
		}
	}
	return runtime.OpenDirectoryDialog(app.ctx, options)
}
```

Create `dialog_windows.go`:

```go
//go:build windows

package main

import "errors"

func choosePlatformWorkingDir(app *App, defaultDir string) (string, error) {
	return "", errors.New("Windows directory picker returns Windows paths; enter a WSL absolute path manually")
}
```

Update `App.ChooseWorkingDir`:

```go
func (a *App) ChooseWorkingDir(defaultDir string) (string, error) {
	return choosePlatformWorkingDir(a, defaultDir)
}
```

Remove `runtime` import from `app.go` only if it becomes unused. `app.go` still needs Wails runtime for events, so keep it.

**Step 2: Run tests**

Run:

```bash
go test ./...
```

Expected: PASS.

**Step 3: Commit**

```bash
git add app.go dialog_unix.go dialog_windows.go app_test.go
git commit -m "feat: handle windows working directory selection"
```

## Task 11: Regenerate Wails Bindings If Needed

**Files:**
- Modify if generated changes occur: `frontend/wailsjs/go/main/App.d.ts`
- Modify if generated changes occur: `frontend/wailsjs/go/main/App.js`
- Modify if generated changes occur: `frontend/wailsjs/go/models.ts`

**Step 1: Generate bindings**

Run:

```bash
wails generate module
```

Expected: No changes if public Wails method signatures stayed the same.

**Step 2: Check status**

Run:

```bash
git status --short
```

If generated files changed, inspect them and commit:

```bash
git add frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/main/App.js frontend/wailsjs/go/models.ts
git commit -m "chore: regenerate wails bindings"
```

If no generated changes, do not commit.

## Task 12: Verification

**Files:**
- No expected file changes.

**Step 1: Run Go tests**

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

**Step 3: Run Linux Wails smoke test**

Run:

```bash
wails dev
```

Expected:

- App opens under current WSL/Linux environment.
- Existing Linux terminal behavior still works.
- Session switching still replays buffered terminal output.

Stop the dev server after verifying.

**Step 4: Manual Windows verification**

On Windows, run the Windows build/dev flow:

```powershell
wails dev
```

Expected:

- EMT opens as a native Windows window.
- Windows IME can input Chinese into xterm.
- Creating a session with a WSL path starts Codex in WSL.
- `~/.emt/sessions.json` inside default WSL is updated.
- Restarting EMT reloads the WSL session list.
- Resume starts `codex resume <id> -C <wsl-path>` in WSL.
- Import preview reads WSL `~/.codex/sessions`.

**Step 5: Final status**

Run:

```bash
git status --short
```

Expected: only unrelated pre-existing files such as `.claude/` remain untracked.
