package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path"
	"strings"

	"graal-rc/internal/connection"
	"graal-rc/rclib"
)

// The server writes an upload into the connection's current File Browser folder,
// whatever path the caller meant. Every upload therefore goes through
// withRemoteFile, which opens the target's folder and keeps it open while its
// current version is read, backed up and replaced.

// remoteFolderOpener runs use while folder is the server's current one.
type remoteFolderOpener func(ctx context.Context, folder string, use func(fileBrowserUploadFolder, []rclib.FileBrowserEntry) error) error

// remoteFile is a file of the open folder. Its methods are the only File Browser
// calls allowed inside withRemoteFile: any other would wait for it to finish.
type remoteFile struct {
	Exists bool
	folder fileBrowserUploadFolder
	path   string
}

func (f remoteFile) Read() ([]byte, error) {
	name := fileBrowserUploadBackupDownloadPath(f.path)
	content, err := f.folder.Download(name)
	if err != nil {
		return nil, fmt.Errorf("download the current version of %q: %w", name, err)
	}
	return content, nil
}

func (f remoteFile) Write(content []byte) error { return f.folder.Upload(f.path, content) }

// requireExisting refuses to create a file that an open editor expects to be
// there: it was moved, renamed or deleted.
func (f remoteFile) requireExisting() error {
	if !f.Exists {
		return fmt.Errorf("%q is no longer in its remote folder; nothing was uploaded", f.path)
	}
	return nil
}

func (a *App) withRemoteFile(remotePath string, use func(remoteFile) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), uploadQueueTimeout)
	defer cancel()
	open := a.openRemoteFolder
	if open == nil {
		open = func(ctx context.Context, folder string, use func(fileBrowserUploadFolder, []rclib.FileBrowserEntry) error) error {
			return a.sessions.WithFileBrowserFolder(ctx, folder, func(directory connection.FileBrowserDirectory) error {
				return use(directory, directory.Entries)
			})
		}
	}
	return open(ctx, fileBrowserUploadParent(remotePath), func(folder fileBrowserUploadFolder, entries []rclib.FileBrowserEntry) error {
		return use(remoteFile{Exists: fileBrowserUploadRemoteFileExists(entries, remotePath), folder: folder, path: remotePath})
	})
}

// backupBeforeWrite saves the current version of an existing file and returns
// the audit detail. Any failure must stop the write.
func (a *App) backupBeforeWrite(resource string, file remoteFile, previous []byte) (string, error) {
	backup, hasBackup, err := a.saveDeploymentBackup(resource, file.path, previous, true)
	if err != nil {
		return "", fmt.Errorf("create safety backup: %w", err)
	}
	if !hasBackup {
		return "replaced existing file without a backup", nil
	}
	return "backup " + backup.ID, nil
}

// qualifyRemotePath completes an entry of the shown listing, which can be the
// file name alone. A path that already has a folder is kept.
func qualifyRemotePath(shownFolder, listed string) string {
	normalized := strings.ReplaceAll(listed, "\\", "/")
	folder := strings.Trim(strings.ReplaceAll(shownFolder, "\\", "/"), "/")
	if folder == "" || strings.Contains(normalized, "/") {
		return normalized
	}
	return folder + "/" + normalized
}

// remoteCacheName keeps the local copies of same-named remote files apart.
func remoteCacheName(remotePath string) string {
	digest := sha256.Sum256([]byte(remotePath))
	return fmt.Sprintf("%x-%s", digest[:8], path.Base(strings.ReplaceAll(remotePath, "\\", "/")))
}

func isFileBackupResource(resource string) bool {
	return resource == "file" || resource == "textfile" || resource == "sqlite"
}

// requireBackupFolder refuses a file backup whose folder is unknown: restoring
// it could replace an unrelated file at the root.
func requireBackupFolder(meta DeploymentBackup) error {
	if isFileBackupResource(meta.Resource) && !meta.FullPath && !strings.ContainsAny(meta.Target, `/\`) {
		return fmt.Errorf("this older backup does not record the folder of %q, so it cannot be compared or restored automatically", meta.Target)
	}
	return nil
}

// readRemoteFile reads a file from its own folder.
func (a *App) readRemoteFile(remotePath string) ([]byte, error) {
	var content []byte
	err := a.withRemoteFile(remotePath, func(file remoteFile) error {
		if !file.Exists {
			return fmt.Errorf("%q is not in its remote folder", remotePath)
		}
		var err error
		content, err = file.Read()
		return err
	})
	return content, err
}
