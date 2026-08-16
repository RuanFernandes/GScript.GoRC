package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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

func syncedLocalScriptPath(a *App, kind, name string) (string, error) {
	cfg, err := a.getSyncConfig()
	if err != nil {
		return "", fmt.Errorf("load sync configuration: %w", err)
	}

	path, err := synclib.LocalScriptPath(cfg.OutputDir, kind, name)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("local script is not available at %s; run Sync first", path)
		}
		return "", fmt.Errorf("inspect local script: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("local script path is a directory: %s", path)
	}
	return path, nil
}

// OpenLocalScriptInFileBrowser reveals a synced script in the native local
// file manager. It deliberately uses the Sync output directory rather than a
// path supplied by the frontend, keeping the file-system boundary server-side.
func (a *App) OpenLocalScriptInFileBrowser(kind, name string) error {
	path, err := syncedLocalScriptPath(a, kind, name)
	if err != nil {
		return err
	}

	if err := revealLocalPath(path); err != nil {
		return fmt.Errorf("open local file browser: %w", err)
	}
	return nil
}

// OpenLocalScriptInExternalEditor opens the synced script with the editor
// preset selected in Settings. Only fixed, supported editor commands are ever
// launched; the path comes from the validated Sync output directory and is
// passed as an argument without a shell.
func (a *App) OpenLocalScriptInExternalEditor(kind, name string) error {
	path, err := syncedLocalScriptPath(a, kind, name)
	if err != nil {
		return err
	}

	if err := launchExternalEditor(a.GetCodingSettings().ExternalEditor, path); err != nil {
		return fmt.Errorf("open local script with external editor: %w", err)
	}
	return nil
}

func launchExternalEditor(editor, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve local script path: %w", err)
	}

	cmd, err := externalEditorCommand(editor, absPath)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start editor: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func externalEditorCommand(editor, path string) (*exec.Cmd, error) {
	editor = strings.ToLower(strings.TrimSpace(editor))
	if !isSupportedExternalEditor(editor) {
		return nil, fmt.Errorf("unsupported external editor: %s", editor)
	}

	if executable := findExternalEditorExecutable(editor); executable != "" {
		return exec.Command(executable, path), nil
	}

	if runtime.GOOS == "darwin" {
		appName, appPaths := macExternalEditorApp(editor)
		for _, appPath := range appPaths {
			if info, err := os.Stat(appPath); err == nil && info.IsDir() {
				return exec.Command("open", "-a", appName, path), nil
			}
		}
	}

	return nil, fmt.Errorf("%s is not installed or available on PATH", externalEditorDisplayName(editor))
}

func findExternalEditorExecutable(editor string) string {
	for _, candidate := range externalEditorCandidates(editor) {
		if candidate == "" {
			continue
		}
		if filepath.IsAbs(candidate) || strings.ContainsAny(candidate, `/\`) {
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
			continue
		}
		if executable, err := exec.LookPath(candidate); err == nil {
			if runtime.GOOS == "windows" && strings.EqualFold(filepath.Ext(executable), ".cmd") {
				continue
			}
			return executable
		}
	}
	return ""
}

func externalEditorCandidates(editor string) []string {
	switch editor {
	case externalEditorVSCode:
		candidates := []string{"code", "code.exe"}
		if runtime.GOOS == "windows" {
			candidates = append(candidates,
				filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Microsoft VS Code", "Code.exe"),
				filepath.Join(os.Getenv("ProgramFiles"), "Microsoft VS Code", "Code.exe"),
				filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft VS Code", "Code.exe"),
			)
		}
		if runtime.GOOS == "darwin" {
			candidates = append(candidates,
				"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
				filepath.Join(os.Getenv("HOME"), "Applications", "Visual Studio Code.app", "Contents", "Resources", "app", "bin", "code"),
			)
		}
		return candidates
	case externalEditorSublime:
		candidates := []string{"subl", "sublime_text", "sublime_text.exe"}
		if runtime.GOOS == "windows" {
			candidates = append(candidates,
				filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Sublime Text", "sublime_text.exe"),
				filepath.Join(os.Getenv("ProgramFiles"), "Sublime Text", "sublime_text.exe"),
				filepath.Join(os.Getenv("ProgramFiles(x86)"), "Sublime Text", "sublime_text.exe"),
			)
		}
		if runtime.GOOS == "darwin" {
			candidates = append(candidates,
				"/Applications/Sublime Text.app/Contents/SharedSupport/bin/subl",
				filepath.Join(os.Getenv("HOME"), "Applications", "Sublime Text.app", "Contents", "SharedSupport", "bin", "subl"),
			)
		}
		return candidates
	case externalEditorNotepadPlus:
		candidates := []string{"notepad++", "notepad-plus-plus", "notepad++.exe"}
		if runtime.GOOS == "windows" {
			candidates = append(candidates,
				filepath.Join(os.Getenv("ProgramFiles"), "Notepad++", "notepad++.exe"),
				filepath.Join(os.Getenv("ProgramFiles(x86)"), "Notepad++", "notepad++.exe"),
			)
		}
		return candidates
	default:
		return nil
	}
}

func macExternalEditorApp(editor string) (string, []string) {
	switch editor {
	case externalEditorVSCode:
		return "Visual Studio Code", []string{
			"/Applications/Visual Studio Code.app",
			filepath.Join(os.Getenv("HOME"), "Applications", "Visual Studio Code.app"),
		}
	case externalEditorSublime:
		return "Sublime Text", []string{
			"/Applications/Sublime Text.app",
			filepath.Join(os.Getenv("HOME"), "Applications", "Sublime Text.app"),
		}
	default:
		return "", nil
	}
}

func externalEditorDisplayName(editor string) string {
	switch editor {
	case externalEditorVSCode:
		return "Visual Studio Code"
	case externalEditorSublime:
		return "Sublime Text"
	case externalEditorNotepadPlus:
		return "Notepad++"
	default:
		return "Selected external editor"
	}
}
