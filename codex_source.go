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
