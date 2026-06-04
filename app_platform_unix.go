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

func defaultCodexSessionRoot() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".codex", "sessions")
}
