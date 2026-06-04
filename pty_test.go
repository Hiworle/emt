package main

import (
	"context"
	"os"
	"reflect"
	"strings"
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

func TestPTYReadLoopBuffersOutputAndEmitsLiveData(t *testing.T) {
	var events []string
	manager := NewPTYManager(func(sessionID string, data string) {
		if sessionID != "session-1" {
			t.Fatalf("got session %q, want session-1", sessionID)
		}
		events = append(events, data)
	}, nil)

	readPTYTestInput(t, manager, "session-1", "hello\nworld")

	if got := manager.Buffer("session-1"); got != "hello\nworld" {
		t.Fatalf("got buffer %q, want %q", got, "hello\nworld")
	}
	if strings.Join(events, "") != "hello\nworld" {
		t.Fatalf("got events %q, want %q", strings.Join(events, ""), "hello\nworld")
	}
}

func TestPTYBufferKeepsLatestBytes(t *testing.T) {
	manager := NewPTYManager(nil, nil)
	data := strings.Repeat("a", terminalBufferLimit+32)

	readPTYTestInput(t, manager, "session-1", data)

	got := manager.Buffer("session-1")
	if len(got) != terminalBufferLimit {
		t.Fatalf("got buffer length %d, want %d", len(got), terminalBufferLimit)
	}
	if want := data[len(data)-terminalBufferLimit:]; got != want {
		t.Fatal("expected terminal buffer to keep latest bytes")
	}
}

func readPTYTestInput(t *testing.T, manager *PTYManager, sessionID string, data string) {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "pty-input")
	if err != nil {
		t.Fatalf("create pty input: %v", err)
	}
	defer file.Close()

	if _, err := file.WriteString(data); err != nil {
		t.Fatalf("write pty input: %v", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatalf("seek pty input: %v", err)
	}

	manager.readLoop(sessionID, file)
}

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
