// Package fileutil contains small, platform-neutral filesystem primitives used
// by app-owned state. The helpers are intentionally conservative: a failed
// write must never silently replace a valid configuration with a truncated
// file.
package fileutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var pathLocks sync.Map

func lockForPath(path string) *sync.Mutex {
	key, err := filepath.Abs(path)
	if err != nil {
		key = path
	}
	candidate := &sync.Mutex{}
	actual, _ := pathLocks.LoadOrStore(key, candidate)
	return actual.(*sync.Mutex)
}

// AtomicWriteFile writes data to path through a same-directory temporary file
// and keeps the previous contents in path+.bak when a previous file exists.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return AtomicWriteFileWithValidator(path, data, perm, nil)
}

// AtomicReplaceFile replaces path through a same-directory temporary file
// without creating a path+.bak copy. It is useful for compacting append-only
// stores whose retention policy must not leave a second copy of expired data.
func AtomicReplaceFile(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return errors.New("file path is required")
	}
	lock := lockForPath(path)
	lock.Lock()
	defer lock.Unlock()
	return atomicReplaceLocked(path, data, perm)
}

// AtomicWriteFileWithValidator is AtomicWriteFile with a validator for the
// current payload. Invalid current data is not copied into the backup, which
// preserves the last known-good backup during recovery from corruption.
func AtomicWriteFileWithValidator(path string, data []byte, perm os.FileMode, validCurrent func([]byte) error) error {
	if path == "" {
		return errors.New("file path is required")
	}
	lock := lockForPath(path)
	lock.Lock()
	defer lock.Unlock()
	return atomicWriteLocked(path, data, perm, validCurrent)
}

// ReadAndRecover reads path and validates it. If the primary payload is
// missing or invalid, a valid path+.bak payload is restored atomically and
// returned. ok is false only when neither file exists.
func ReadAndRecover(path string, perm os.FileMode, validate func([]byte) error) ([]byte, bool, error) {
	if path == "" {
		return nil, false, errors.New("file path is required")
	}
	lock := lockForPath(path)
	lock.Lock()
	defer lock.Unlock()

	primary, primaryReadErr := os.ReadFile(path)
	if primaryReadErr == nil {
		if err := validatePayload(primary, validate); err == nil {
			return primary, true, nil
		} else {
			primaryReadErr = fmt.Errorf("invalid primary payload: %w", err)
		}
	}

	backupPath := path + ".bak"
	backup, backupReadErr := os.ReadFile(backupPath)
	if backupReadErr == nil {
		if err := validatePayload(backup, validate); err == nil {
			if err := atomicReplaceLocked(path, backup, perm); err != nil {
				return backup, true, fmt.Errorf("restore %q from backup: %w", path, err)
			}
			return backup, true, nil
		} else {
			backupReadErr = fmt.Errorf("invalid backup payload: %w", err)
		}
	}

	if os.IsNotExist(primaryReadErr) && os.IsNotExist(backupReadErr) {
		return nil, false, nil
	}
	if backupReadErr != nil && !os.IsNotExist(backupReadErr) {
		return nil, false, errors.Join(primaryReadErr, backupReadErr)
	}
	return nil, false, primaryReadErr
}

// RestoreAtomicFile replaces path with a known-good payload without changing
// path+.bak. This is used after backup recovery so a corrupt primary cannot
// overwrite the good backup.
func RestoreAtomicFile(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return errors.New("file path is required")
	}
	lock := lockForPath(path)
	lock.Lock()
	defer lock.Unlock()
	return atomicReplaceLocked(path, data, perm)
}

func validatePayload(data []byte, validate func([]byte) error) error {
	if validate == nil {
		return nil
	}
	return validate(data)
}

func atomicWriteLocked(path string, data []byte, perm os.FileMode, validCurrent func([]byte) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create persistence directory: %w", err)
	}

	previous, previousErr := os.ReadFile(path)
	if previousErr != nil && !os.IsNotExist(previousErr) {
		return fmt.Errorf("read existing %q: %w", path, previousErr)
	}
	if previousErr == nil && (validCurrent == nil || validCurrent(previous) == nil) {
		if err := atomicReplaceLocked(path+".bak", previous, perm); err != nil {
			return fmt.Errorf("backup %q: %w", path, err)
		}
	}
	return atomicReplaceLocked(path, data, perm)
}

func atomicReplaceLocked(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create persistence directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	if _, err := io.Copy(tmp, bytes.NewReader(data)); err != nil {
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Rename(tmpPath, path); err != nil {
			return fmt.Errorf("replace %q: %w", path, err)
		}
	} else {
		if err := replaceWindows(tmpPath, path); err != nil {
			return fmt.Errorf("replace %q: %w", path, err)
		}
	}
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("set file permissions: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("sync persistence directory: %w", err)
	}
	return nil
}
