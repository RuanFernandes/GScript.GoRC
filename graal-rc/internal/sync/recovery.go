package sync

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Sync never deletes a script file. One the server no longer lists is moved to
// <output dir>/.sync-removed/<time>/<kind folder>/<name>.gs2.removed. Nothing
// prunes that folder. The extra extension keeps the copies out of script
// indexing; removing it restores the file.
const (
	recoveryDirName = ".sync-removed"
	recoverySuffix  = ".removed"
)

// moveToRecovery moves a script file into the recovery folder without ever
// replacing an earlier copy, and returns where it went.
func (e *Engine) moveToRecovery(path string) (string, error) {
	root, relative := scriptRoot(path)
	stamp := e.now().Format("20060102-150405")
	for attempt := 1; attempt <= 1000; attempt++ {
		folder := stamp
		if attempt > 1 {
			folder = fmt.Sprintf("%s-%d", stamp, attempt)
		}
		destination := filepath.Join(root, recoveryDirName, folder, filepath.FromSlash(relative)+recoverySuffix)
		if _, err := os.Lstat(destination); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return "", err
		}
		if err := os.Rename(path, destination); err != nil {
			return "", err
		}
		return destination, nil
	}
	return "", fmt.Errorf("no free recovery name for %q", path)
}

// retireLocalFile takes the file of a script the server no longer has out of
// the sync folder and forgets everything tracked about it. If it cannot be
// moved it stays where it is. It reports whether the path was retired.
func (e *Engine) retireLocalFile(path string) bool {
	e.mu.RLock()
	ref := e.refs[path]
	e.mu.RUnlock()
	if ref.kind == "" {
		name := decodeName(strings.TrimSuffix(filepath.Base(path), scriptExt))
		ref = scriptRef{kind: kindFromDir(filepath.Dir(path)), key: name, name: name}
	}
	destination, err := e.moveToRecovery(path)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("sync could not move server-missing local script %q aside; leaving it in place: %v", path, err)
		return false
	}
	if err == nil {
		log.Printf("sync: the server does not list %q; local copy moved to %q", path, destination)
		e.auditPull(ref, "success", "the server does not list this script; local copy moved to "+destination)
	}
	e.mu.Lock()
	delete(e.hashes, path)
	delete(e.refs, path)
	e.mu.Unlock()
	e.dropReview(path)
	e.dropBaseline(path)
	return true
}
