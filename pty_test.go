package main

import (
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
