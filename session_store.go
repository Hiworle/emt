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
