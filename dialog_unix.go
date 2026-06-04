//go:build !windows

package main

import (
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func choosePlatformWorkingDir(app *App, defaultDir string) (string, error) {
	options := runtime.OpenDialogOptions{}
	defaultDir = strings.TrimSpace(defaultDir)
	if defaultDir != "" {
		if info, err := os.Stat(defaultDir); err == nil && info.IsDir() {
			options.DefaultDirectory = defaultDir
		}
	}
	return runtime.OpenDirectoryDialog(app.ctx, options)
}
