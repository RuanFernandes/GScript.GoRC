package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"graal-rc/internal/appid"
	"graal-rc/internal/mcp"
)

// mcpRemoteFilePath accepts only a path that names one file. A trailing slash
// is refused before cleaning, which would turn the folder into a file name.
func mcpRemoteFilePath(value string) (string, error) {
	if isFileBrowserDirectoryPath(value) {
		return "", errors.New("path must name a file, not a folder")
	}
	cleaned, err := normalizePluginRemotePath(value)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(path.Base(cleaned), `<>:"|?*`) {
		return "", fmt.Errorf("invalid remote file name: %q", path.Base(cleaned))
	}
	return cleaned, nil
}

// mcpDownloadPath is where the local copy of a remote file goes. Downloading it
// again replaces that copy.
func mcpDownloadPath(remotePath string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appid.ID(), "mcp-downloads", remoteCacheName(remotePath)), nil
}

func checkUploadSize(size, limit int64) error {
	if limit > 0 && size > limit {
		return fmt.Errorf("file exceeds upload limit of %d bytes", limit)
	}
	return nil
}

func (b appMCPBackend) DownloadFile(ctx context.Context, remotePath string) (mcp.DownloadedFile, error) {
	remotePath, err := mcpRemoteFilePath(remotePath)
	if err != nil {
		return mcp.DownloadedFile{}, err
	}
	if err := ctx.Err(); err != nil {
		return mcp.DownloadedFile{}, err
	}
	content, err := b.app.readRemoteFile(remotePath)
	if err != nil {
		return mcp.DownloadedFile{}, err
	}
	localPath, err := mcpDownloadPath(remotePath)
	if err == nil {
		err = writeBackupFileAtomic(localPath, content)
	}
	if err != nil {
		return mcp.DownloadedFile{}, fmt.Errorf("save the downloaded file: %w", err)
	}
	return mcp.DownloadedFile{Path: remotePath, LocalPath: localPath, Content: content}, nil
}

func (b appMCPBackend) UploadFile(ctx context.Context, upload mcp.UploadRequest) (mcp.UploadResult, error) {
	remotePath, err := mcpRemoteFilePath(upload.Path)
	if err != nil {
		return mcp.UploadResult{}, err
	}
	limit := b.app.sessions.MaxUploadFileSize()
	var content []byte
	if upload.Content != nil {
		content = []byte(*upload.Content)
		err = checkUploadSize(int64(len(content)), limit)
	} else {
		content, err = readFileBrowserUpload(upload.LocalPath, limit)
	}
	if err != nil {
		return mcp.UploadResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return mcp.UploadResult{}, err
	}
	replaced, err := b.app.uploadRemoteFile(remotePath, content)
	if err != nil {
		return mcp.UploadResult{}, err
	}
	return mcp.UploadResult{Path: remotePath, Size: len(content), Replaced: replaced}, nil
}
