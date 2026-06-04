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
