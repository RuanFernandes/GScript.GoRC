//go:build windows

package fileutil

import (
	"errors"
	"fmt"
	"os"
)

func replaceWindows(tmpPath, path string) error {
	oldPath := path + ".replace-old"
	if err := os.Remove(oldPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale replacement: %w", err)
	}
	hadOld := false
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, oldPath); err != nil {
			return err
		}
		hadOld = true
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if hadOld {
			if restoreErr := os.Rename(oldPath, path); restoreErr != nil {
				return errors.Join(err, fmt.Errorf("restore previous file: %w", restoreErr))
			}
		}
		return err
	}
	if hadOld {
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("remove replaced file: %w", err)
		}
	}
	return nil
}

func syncDirectory(string) error { return nil }
