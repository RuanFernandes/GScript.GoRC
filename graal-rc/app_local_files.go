package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	synclib "graal-rc/internal/sync"
)

// revealLocalPath asks the platform's native file manager to show a file.
// Linux file managers do not have a portable "select this file" interface, so
// the containing directory is opened there instead.
func revealLocalPath(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve local script path: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", "/select,"+absPath).Start()
	case "darwin":
		return exec.Command("open", "-R", absPath).Start()
	default:
		return exec.Command("xdg-open", filepath.Dir(absPath)).Start()
	}
}

// OpenLocalScriptInFileBrowser reveals a synced script in the native local
// file manager. It deliberately uses the Sync output directory rather than a
// path supplied by the frontend, keeping the file-system boundary server-side.
func (a *App) OpenLocalScriptInFileBrowser(kind, name string) error {
	cfg, err := a.getSyncConfig()
	if err != nil {
		return fmt.Errorf("load sync configuration: %w", err)
	}

	path, err := synclib.LocalScriptPath(cfg.OutputDir, kind, name)
	if err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("local script is not available at %s; run Sync first", path)
		}
		return fmt.Errorf("inspect local script: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("local script path is a directory: %s", path)
	}

	if err := revealLocalPath(path); err != nil {
		return fmt.Errorf("open local file browser: %w", err)
	}
	return nil
}
