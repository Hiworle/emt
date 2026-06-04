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
