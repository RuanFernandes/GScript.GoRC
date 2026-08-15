package main

import (
	"testing"

	"graal-rc/rclib"
)

func TestFileBrowserPathHelpersRejectFolderRenames(t *testing.T) {
	for _, path := range []string{"folder/", `folder\`, " folder/ "} {
		if !isFileBrowserDirectoryPath(path) {
			t.Fatalf("isFileBrowserDirectoryPath(%q) = false", path)
		}
	}
	for _, path := range []string{"file.txt", `folder\file.txt`, " folder.txt "} {
		if isFileBrowserDirectoryPath(path) {
			t.Fatalf("isFileBrowserDirectoryPath(%q) = true", path)
		}
	}

	entries := []rclib.FileBrowserEntry{
		{Path: "folder/", IsDirectory: true},
		{Path: `folder\file.txt`, IsDirectory: false},
	}
	if !fileBrowserEntryIsDirectory(entries, `folder\`) {
		t.Fatal("directory entry with slash normalization was not detected")
	}
	if fileBrowserEntryIsDirectory(entries, "folder/file.txt") {
		t.Fatal("regular file was detected as a directory")
	}
}

func TestCodingSettingsAcceptOnlySupportedTabSizes(t *testing.T) {
	if DefaultCodingSettings.TabSize != 2 {
		t.Fatalf("default tab size = %d, want 2", DefaultCodingSettings.TabSize)
	}
	for _, size := range []int{1, 2, 4} {
		if !isSupportedTabSize(size) {
			t.Fatalf("tab size %d was rejected", size)
		}
	}
	for _, size := range []int{0, 3, 5, -1} {
		if isSupportedTabSize(size) {
			t.Fatalf("tab size %d was accepted", size)
		}
	}
}
