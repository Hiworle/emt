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

func defaultCodexSessionRoot() string {
	return "~/.codex/sessions"
}
