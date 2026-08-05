//go:build !windows

package fileutil

import "os"

func replaceWindows(tmpPath, path string) error {
	return os.Rename(tmpPath, path)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
