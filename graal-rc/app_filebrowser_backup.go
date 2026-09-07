package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"graal-rc/internal/connection"
	"graal-rc/rclib"
)

// FileBrowserBackupResult is returned after every selected remote file has
// been persisted locally. Destination is the unique subfolder created inside
// the directory chosen by the user.
type FileBrowserBackupResult struct {
	Destination string `json:"destination"`
	Files       int    `json:"files"`
	Bytes       int64  `json:"bytes"`
}

type fileBrowserBackupProgressEvent struct {
	Phase        string `json:"phase"`
	Folder       string `json:"folder,omitempty"`
	File         string `json:"file,omitempty"`
	FoldersDone  int    `json:"foldersDone"`
	FoldersTotal int    `json:"foldersTotal"`
	FilesDone    int    `json:"filesDone"`
	FilesTotal   int    `json:"filesTotal"`
	BytesDone    int64  `json:"bytesDone"`
	BytesTotal   int64  `json:"bytesTotal"`
	Error        string `json:"error,omitempty"`
}

func (a *App) OpenFileBrowserBackup() {
	a.fileBrowserBackupMu.Lock()
	defer a.fileBrowserBackupMu.Unlock()
	if a.fileBrowserBackupWindow != nil {
		a.fileBrowserBackupWindow.Show()
		a.fileBrowserBackupWindow.Focus()
		return
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "file-browser-backup",
		Title:            serverWindowTitle(a.sessions.Status().ServerName, "File Browser Backup"),
		URL:              "/#file-backup",
		Width:            760,
		Height:           700,
		MinWidth:         600,
		MinHeight:        520,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.fileBrowserBackupWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.fileBrowserBackupMu.Lock()
		if a.fileBrowserBackupWindow == w {
			a.fileBrowserBackupWindow = nil
			if a.fileBrowserBackupCancel != nil {
				a.fileBrowserBackupCancel()
			}
		}
		a.fileBrowserBackupMu.Unlock()
	})
}

// ChooseBackupDirectory opens a native folder picker for the local backup
// parent directory. It never touches the remote server.
func (a *App) ChooseBackupDirectory() (string, error) {
	return a.app.Dialog.OpenFile().
		AttachToWindow(a.dialogParentWindow()).
		SetTitle("Select backup destination").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
}

// CancelFileBrowserBackup asks the current backup to stop after the in-flight
// read finishes. The protocol has no remote transfer-abort primitive, so the
// active request is allowed to release its native transfer slot safely.
func (a *App) CancelFileBrowserBackup() {
	a.fileBrowserBackupMu.Lock()
	cancel := a.fileBrowserBackupCancel
	a.fileBrowserBackupMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// BackupFileBrowser reads the selected remote folders and writes their files to
// a new local snapshot directory. The remote side is strictly read-only: the
// only session operations invoked by the connection layer are folder listings
// and file downloads.
func (a *App) BackupFileBrowser(folders []string, destination string) (FileBrowserBackupResult, error) {
	parent, err := validateBackupParentDirectory(destination)
	if err != nil {
		return FileBrowserBackupResult{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.fileBrowserBackupMu.Lock()
	if a.fileBrowserBackupCancel != nil {
		a.fileBrowserBackupMu.Unlock()
		cancel()
		return FileBrowserBackupResult{}, errors.New("a file-browser backup is already running")
	}
	a.fileBrowserBackupCancel = cancel
	a.fileBrowserBackupMu.Unlock()
	defer func() {
		a.fileBrowserBackupMu.Lock()
		a.fileBrowserBackupCancel = nil
		a.fileBrowserBackupMu.Unlock()
		cancel()
	}()

	backupDir, err := createBackupSnapshotDirectory(parent)
	if err != nil {
		return FileBrowserBackupResult{}, err
	}
	lastProgress := connection.FileBrowserBackupProgress{}
	a.emitFileBrowserBackupProgress(lastProgress, "scanning", "", "", "")

	remoteResult, err := a.sessions.BackupFileBrowser(
		ctx,
		folders,
		func(remotePath string, _ rclib.FileBrowserEntry, content []byte) error {
			localPath, err := localBackupFilePath(backupDir, remotePath)
			if err != nil {
				return err
			}
			return writeBackupFileAtomic(localPath, content)
		},
		func(progress connection.FileBrowserBackupProgress) {
			lastProgress = progress
			a.emitFileBrowserBackupProgress(progress, progress.Phase, progress.Folder, progress.File, "")
		},
	)
	if err != nil {
		phase := "failed"
		if errors.Is(err, context.Canceled) {
			phase = "cancelled"
		}
		a.emitFileBrowserBackupProgress(lastProgress, phase, lastProgress.Folder, lastProgress.File, err.Error())
		return FileBrowserBackupResult{}, err
	}

	completed := connection.FileBrowserBackupProgress{
		Phase:        "completed",
		FoldersDone:  lastProgress.FoldersTotal,
		FoldersTotal: lastProgress.FoldersTotal,
		FilesDone:    remoteResult.Files,
		FilesTotal:   remoteResult.Files,
		BytesDone:    remoteResult.Bytes,
		BytesTotal:   remoteResult.Bytes,
	}
	a.emitFileBrowserBackupProgress(completed, "completed", "", "", "")
	return FileBrowserBackupResult{
		Destination: backupDir,
		Files:       remoteResult.Files,
		Bytes:       remoteResult.Bytes,
	}, nil
}

func (a *App) emitFileBrowserBackupProgress(
	progress connection.FileBrowserBackupProgress,
	phase, folder, file, errorMessage string,
) {
	if a.app == nil {
		return
	}
	payload := fileBrowserBackupProgressEvent{
		Phase:        phase,
		Folder:       folder,
		File:         file,
		FoldersDone:  progress.FoldersDone,
		FoldersTotal: progress.FoldersTotal,
		FilesDone:    progress.FilesDone,
		FilesTotal:   progress.FilesTotal,
		BytesDone:    progress.BytesDone,
		BytesTotal:   progress.BytesTotal,
		Error:        errorMessage,
	}
	b, err := json.Marshal(payload)
	if err == nil {
		a.app.Event.Emit("rc:fileBrowserBackup", string(b))
	}
}

func validateBackupParentDirectory(destination string) (string, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return "", errors.New("backup destination is required")
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return "", fmt.Errorf("resolve backup destination: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("read backup destination: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("backup destination is not a directory")
	}
	return filepath.Clean(abs), nil
}

func createBackupSnapshotDirectory(parent string) (string, error) {
	base := "graal-rc-backup-" + time.Now().Format("20060102-150405")
	for index := 0; index < 1000; index++ {
		name := base
		if index > 0 {
			name = fmt.Sprintf("%s-%d", base, index+1)
		}
		candidate := filepath.Join(parent, name)
		err := os.Mkdir(candidate, 0o755)
		if err == nil {
			return candidate, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	return "", errors.New("could not create a unique backup directory")
}

func normalizeLocalBackupRemotePath(remotePath string) (string, error) {
	remotePath = strings.TrimSpace(strings.ReplaceAll(remotePath, "\\", "/"))
	remotePath = strings.Trim(remotePath, "/")
	if remotePath == "" || strings.Contains(remotePath, "\x00") {
		return "", errors.New("invalid remote backup path")
	}
	segments := strings.Split(remotePath, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(segment, ":") {
			return "", errors.New("remote backup path contains an invalid segment")
		}
	}
	cleaned := pathpkg.Clean(remotePath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("remote backup path cannot escape its root")
	}
	return cleaned, nil
}

func localBackupFilePath(root, remotePath string) (string, error) {
	relative, err := normalizeLocalBackupRemotePath(remotePath)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	destination := filepath.Join(root, filepath.FromSlash(relative))
	relativeToRoot, err := filepath.Rel(root, destination)
	if err != nil {
		return "", err
	}
	if relativeToRoot == ".." || strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relativeToRoot) {
		return "", errors.New("remote backup path escapes destination")
	}
	return destination, nil
}

func writeBackupFileAtomic(destination string, content []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".graal-rc-backup-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if _, err = temporary.Write(content); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporaryName, destination); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}
