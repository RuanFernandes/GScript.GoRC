package main

import (
	"os"
	"path/filepath"
	"testing"

	"graal-rc/rclib"
)

func TestFileBrowserUploadRemotePath(t *testing.T) {
	t.Parallel()

	got, err := fileBrowserUploadRemotePath("levels\\forest/", `C:\Users\RuanF\Pictures\ChatGPT Image 28.png`)
	if err != nil {
		t.Fatalf("fileBrowserUploadRemotePath() error = %v", err)
	}
	if want := "levels/forest/ChatGPT Image 28.png"; got != want {
		t.Fatalf("fileBrowserUploadRemotePath() = %q, want %q", got, want)
	}

	got, err = fileBrowserUploadRemotePath("/", "/tmp/guard.png")
	if err != nil {
		t.Fatalf("root fileBrowserUploadRemotePath() error = %v", err)
	}
	if want := "guard.png"; got != want {
		t.Fatalf("root fileBrowserUploadRemotePath() = %q, want %q", got, want)
	}
}

func TestFileBrowserUploadRemotePathRejectsTraversalAndMissingFile(t *testing.T) {
	t.Parallel()

	for _, input := range []struct {
		folder string
		local  string
	}{
		{folder: "levels/../scripts", local: `C:\temp\script.txt`},
		{folder: "levels//scripts", local: `C:\temp\script.txt`},
		{folder: "levels", local: ""},
	} {
		if _, err := fileBrowserUploadRemotePath(input.folder, input.local); err == nil {
			t.Errorf("fileBrowserUploadRemotePath(%q, %q) succeeded; want error", input.folder, input.local)
		}
	}
}

func TestFileBrowserUploadRemoteFileExists(t *testing.T) {
	t.Parallel()

	entries := []rclib.FileBrowserEntry{
		{Path: "photo.png", IsDirectory: false},
		{Path: "folder/", IsDirectory: true},
	}
	if !fileBrowserUploadRemoteFileExists(entries, "levels/photo.png") {
		t.Fatal("existing file was not detected")
	}
	if fileBrowserUploadRemoteFileExists(entries, "levels/folder") {
		t.Fatal("directory was incorrectly treated as a file")
	}
	if fileBrowserUploadRemoteFileExists(entries, "levels/new.png") {
		t.Fatal("missing file was treated as existing")
	}
}

func TestReadFileBrowserUploadValidatesFileAndSize(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filePath := filepath.Join(root, "upload.bin")
	if err := os.WriteFile(filePath, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := readFileBrowserUpload(filePath, 4)
	if err != nil {
		t.Fatalf("readFileBrowserUpload() error = %v", err)
	}
	if string(got) != "data" {
		t.Fatalf("readFileBrowserUpload() = %q, want %q", got, "data")
	}
	if _, err := readFileBrowserUpload(filePath, 3); err == nil {
		t.Fatal("readFileBrowserUpload() succeeded above the configured limit")
	}
	if _, err := readFileBrowserUpload("relative-upload.bin", 0); err == nil {
		t.Fatal("readFileBrowserUpload() accepted a relative path")
	}
	if _, err := readFileBrowserUpload(root, 0); err == nil {
		t.Fatal("readFileBrowserUpload() accepted a directory")
	}
}
