//go:build windows

package main

import "errors"

func choosePlatformWorkingDir(app *App, defaultDir string) (string, error) {
	return "", errors.New("Windows directory picker returns Windows paths; enter a WSL absolute path manually")
}
