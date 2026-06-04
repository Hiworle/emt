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
