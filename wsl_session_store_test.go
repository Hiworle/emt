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
