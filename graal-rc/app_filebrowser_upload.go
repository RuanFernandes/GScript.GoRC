package main

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"graal-rc/rclib"
)

func fileBrowserUploadRemotePath(remoteFolder, localPath string) (string, error) {
	if strings.TrimSpace(localPath) == "" {
		return "", fmt.Errorf("invalid local file path")
	}

	folder := strings.Trim(strings.ReplaceAll(remoteFolder, "\\", "/"), "/")
	if folder != "" {
		for _, segment := range strings.Split(folder, "/") {
			if segment == "" || segment == "." || segment == ".." {
				return "", fmt.Errorf("invalid remote folder path: %q", remoteFolder)
			}
		}
	}

	name := path.Base(strings.ReplaceAll(localPath, "\\", "/"))
	if name == "" || name == "." || name == ".." || name == "/" {
		return "", fmt.Errorf("invalid local file name: %q", localPath)
	}
	if folder == "" {
		return name, nil
	}
	return path.Join(folder, name), nil
}

func fileBrowserUploadRemoteFileExists(entries []rclib.FileBrowserEntry, remotePath string) bool {
	name := path.Base(strings.ReplaceAll(remotePath, "\\", "/"))
	for _, entry := range entries {
		if entry.IsDirectory {
			continue
		}
		entryName := path.Base(strings.ReplaceAll(entry.Path, "\\", "/"))
		if entryName == name {
			return true
		}
	}
	return false
}

func readFileBrowserUpload(localPath string, maxBytes int64) ([]byte, error) {
	if strings.TrimSpace(localPath) == "" {
		return nil, fmt.Errorf("invalid local file path")
	}
	if !filepath.IsAbs(localPath) {
		return nil, fmt.Errorf("local file path must be absolute")
	}

	pathInfo, err := os.Lstat(localPath)
	if err != nil {
		return nil, fmt.Errorf("inspect local file: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("cannot upload a symbolic link")
	}
	if !pathInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("only regular files can be uploaded")
	}
	if maxBytes > 0 && pathInfo.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds upload limit of %d bytes", maxBytes)
	}

	file, err := os.Open(localPath)
	if err != nil {
		return nil, fmt.Errorf("open local file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened file: %w", err)
	}
	if !os.SameFile(pathInfo, fileInfo) {
		return nil, fmt.Errorf("local file changed before upload")
	}

	var reader io.Reader = file
	if maxBytes > 0 && maxBytes < 1<<63-1 {
		reader = io.LimitReader(file, maxBytes+1)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read local file: %w", err)
	}
	if maxBytes > 0 && int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("file exceeds upload limit of %d bytes", maxBytes)
	}
	return content, nil
}

// UploadDroppedFile reads an OS-dropped file in the Go process and uploads it
// to the selected remote folder without copying its contents through WebView IPC.
func (a *App) UploadDroppedFile(localPath, remoteFolder string) error {
	if a == nil || a.sessions == nil {
		return fmt.Errorf("file browser is unavailable")
	}
	remotePath, err := fileBrowserUploadRemotePath(remoteFolder, localPath)
	if err != nil {
		return err
	}
	content, err := readFileBrowserUpload(localPath, a.sessions.MaxUploadFileSize())
	if err != nil {
		return err
	}
	entries, err := a.sessions.GetFileBrowserFiles()
	if err != nil {
		return fmt.Errorf("inspect current remote folder: %w", err)
	}
	return a.uploadFileContent(remotePath, content, fileBrowserUploadRemoteFileExists(entries, remotePath))
}
