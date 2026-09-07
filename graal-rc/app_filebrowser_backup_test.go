package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalBackupFilePathStaysInsideSnapshot(t *testing.T) {
	root := t.TempDir()
	path, err := localBackupFilePath(root, `levels\main.nw`)
	if err != nil {
		t.Fatalf("localBackupFilePath() error = %v", err)
	}
	want := filepath.Join(root, "levels", "main.nw")
	if path != want {
		t.Fatalf("local backup path = %q, want %q", path, want)
	}

	for _, remotePath := range []string{"../outside.txt", "levels/../../outside.txt", "C:/outside.txt"} {
		t.Run(remotePath, func(t *testing.T) {
			if _, err := localBackupFilePath(root, remotePath); err == nil {
				t.Fatalf("localBackupFilePath(%q) accepted an unsafe path", remotePath)
			}
		})
	}
}

func TestWriteBackupFileAtomicCreatesParentAndReplacesDestination(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "levels", "main.nw")
	if err := writeBackupFileAtomic(destination, []byte("first")); err != nil {
		t.Fatalf("first writeBackupFileAtomic() error = %v", err)
	}
	if err := writeBackupFileAtomic(destination, []byte("second")); err != nil {
		t.Fatalf("second writeBackupFileAtomic() error = %v", err)
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if string(content) != "second" {
		t.Fatalf("backup content = %q, want %q", content, "second")
	}

	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".graal-rc-backup-") {
			t.Fatalf("temporary backup file %q was left behind", entry.Name())
		}
	}
}
