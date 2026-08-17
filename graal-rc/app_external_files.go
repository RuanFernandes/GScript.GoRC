package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"graal-rc/rclib"
)

const (
	externalFileWatcherInterval = 2500 * time.Millisecond
	externalFileRetryInterval   = 5 * time.Second
)

type externalFileSignature struct {
	modified int64
	size     int64
}

type externalFileSession struct {
	id            uint64
	remotePath    string
	localPath     string
	writable      bool
	lastSignature externalFileSignature
	lastAttempt   time.Time
}

func isExternalRemoteFile(remotePath string) bool {
	normalized := strings.TrimSpace(strings.ReplaceAll(remotePath, "\\", "/"))
	if normalized == "" || strings.HasSuffix(normalized, "/") {
		return false
	}
	switch strings.ToLower(strings.TrimPrefix(path.Ext(normalized), ".")) {
	case "nw", "gmap":
		return true
	default:
		return false
	}
}

func externalFileLocalPath(downloadDir, remotePath string) (string, error) {
	downloadDir = strings.TrimSpace(downloadDir)
	if downloadDir == "" {
		return "", fmt.Errorf("no downloads folder set — configure it in Settings → Files")
	}
	normalized := strings.TrimSpace(strings.ReplaceAll(remotePath, "\\", "/"))
	if normalized == "" || strings.HasSuffix(normalized, "/") {
		return "", fmt.Errorf("invalid remote file path: %q", remotePath)
	}
	name := path.Base(normalized)
	if name == "" || name == "." || name == ".." || name == "/" {
		return "", fmt.Errorf("invalid remote file path: %q", remotePath)
	}
	absDir, err := filepath.Abs(downloadDir)
	if err != nil {
		return "", fmt.Errorf("resolve downloads folder: %w", err)
	}
	return filepath.Join(absDir, name), nil
}

func externalFileSignatureFor(localPath string) (externalFileSignature, error) {
	info, err := os.Lstat(localPath)
	if err != nil {
		return externalFileSignature{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return externalFileSignature{}, fmt.Errorf("local external file is a symbolic link: %s", localPath)
	}
	if !info.Mode().IsRegular() {
		return externalFileSignature{}, fmt.Errorf("local external file is not regular: %s", localPath)
	}
	return externalFileSignature{modified: info.ModTime().UnixNano(), size: info.Size()}, nil
}

func externalFileChanged(previous, current externalFileSignature) bool {
	return previous != current
}

func externalFilePathsMatch(requested, listed string) bool {
	requested = normalizeFileBrowserPath(requested)
	listed = normalizeFileBrowserPath(listed)
	if requested == "" || listed == "" {
		return false
	}
	return requested == listed ||
		strings.HasSuffix(requested, "/"+listed) ||
		strings.HasSuffix(listed, "/"+requested)
}

func externalFileHasWriteAccess(entries []rclib.FileBrowserEntry, remotePath string) bool {
	for _, entry := range entries {
		if entry.IsDirectory || !externalFilePathsMatch(remotePath, entry.Path) {
			continue
		}
		return strings.Contains(strings.ToLower(entry.Rights), "w")
	}
	return false
}

func (a *App) remoteExternalFileWriteAccess(remotePath string) bool {
	if a == nil || a.sessions == nil {
		return false
	}
	entries, err := a.sessions.GetFileBrowserFiles()
	if err != nil {
		log.Printf("[external-file] could not inspect write rights for %q: %v", remotePath, err)
		return false
	}
	return externalFileHasWriteAccess(entries, remotePath)
}

func (a *App) openExternalRemoteFile(remotePath string, content []byte) (string, error) {
	if a == nil || a.sessions == nil {
		return "", fmt.Errorf("connection service is unavailable")
	}
	if !isExternalRemoteFile(remotePath) {
		return "", fmt.Errorf("unsupported external file type: %s", remotePath)
	}

	localPath, err := externalFileLocalPath(a.GetFileBrowserConfig().DownloadDir, remotePath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return "", fmt.Errorf("create downloads folder: %w", err)
	}
	if err := os.WriteFile(localPath, content, 0o644); err != nil {
		return "", fmt.Errorf("save external file copy: %w", err)
	}
	signature, err := externalFileSignatureFor(localPath)
	if err != nil {
		return "", err
	}
	writable := a.remoteExternalFileWriteAccess(remotePath)

	key := normalizeFileBrowserPath(remotePath)
	a.externalFilesMu.Lock()
	if a.externalFiles == nil {
		a.externalFiles = make(map[string]externalFileSession)
	}
	a.externalFileSequence++
	session := externalFileSession{
		id:            a.externalFileSequence,
		remotePath:    remotePath,
		localPath:     localPath,
		writable:      writable,
		lastSignature: signature,
	}
	a.externalFiles[key] = session
	a.externalFilesMu.Unlock()

	if err := osOpenPath(localPath); err != nil {
		a.removeExternalFileSession(key, session.id)
		return "", fmt.Errorf("open external file: %w", err)
	}
	a.ensureExternalFileWatcher()
	log.Printf("[external-file] opened remote=%q local=%q writable=%t", remotePath, localPath, session.writable)
	return localPath, nil
}

func (a *App) removeExternalFileSession(key string, id uint64) {
	a.externalFilesMu.Lock()
	defer a.externalFilesMu.Unlock()
	if current, ok := a.externalFiles[key]; ok && current.id == id {
		delete(a.externalFiles, key)
	}
}

// clearExternalFileSessions detaches local editor copies from the active
// server before logout or server switching. It deliberately leaves the local
// files on disk because they belong to the user and may still be open in the
// external editor.
func (a *App) clearExternalFileSessions() {
	if a == nil {
		return
	}
	a.externalFilesMu.Lock()
	a.externalFiles = make(map[string]externalFileSession)
	a.externalFilesMu.Unlock()
}

func (a *App) ensureExternalFileWatcher() {
	if a == nil {
		return
	}
	a.externalFilesMu.Lock()
	if a.externalFileWatcherStarted {
		a.externalFilesMu.Unlock()
		return
	}
	a.externalFileWatcherStarted = true
	a.externalFilesMu.Unlock()

	lifecycleFor(a).startBackground(func(stop <-chan struct{}) {
		ticker := time.NewTicker(externalFileWatcherInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !appIsShuttingDown(a) {
					a.pollExternalFiles()
				}
			}
		}
	})
}

func (a *App) pollExternalFiles() {
	a.externalFilesMu.Lock()
	defer a.externalFilesMu.Unlock()
	for key, session := range a.externalFiles {
		a.pollExternalFileLocked(&session)
		a.externalFiles[key] = session
	}
}

func (a *App) pollExternalFileLocked(session *externalFileSession) {
	current, err := externalFileSignatureFor(session.localPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[external-file] could not inspect %q: %v", session.localPath, err)
		}
		return
	}
	if !externalFileChanged(session.lastSignature, current) {
		return
	}
	if !session.writable {
		session.lastSignature = current
		log.Printf("[external-file] local change ignored without write rights remote=%q", session.remotePath)
		return
	}
	if !session.lastAttempt.IsZero() && time.Since(session.lastAttempt) < externalFileRetryInterval {
		return
	}
	session.lastAttempt = time.Now()
	content, err := os.ReadFile(session.localPath)
	if err != nil {
		log.Printf("[external-file] could not read changed file %q: %v", session.localPath, err)
		return
	}
	if err := a.sessions.UploadFile(session.remotePath, content); err != nil {
		log.Printf("[external-file] upload failed remote=%q: %v", session.remotePath, err)
		return
	}
	if uploaded, statErr := externalFileSignatureFor(session.localPath); statErr == nil {
		session.lastSignature = uploaded
	} else {
		session.lastSignature = current
	}
	session.lastAttempt = time.Time{}
	log.Printf("[external-file] uploaded local changes remote=%q bytes=%d", session.remotePath, len(content))
}
