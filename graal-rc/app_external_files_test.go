package main

import (
	"os"
	"path/filepath"
	"testing"

	"graal-rc/rclib"
)

func TestIsExternalRemoteFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "levels/main.nw", want: true},
		{path: `levels\main.GMAP`, want: true},
		{path: "levels/main.nw.bak", want: false},
		{path: "levels/main.txt", want: false},
		{path: "levels/main.nw/", want: false},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := isExternalRemoteFile(test.path); got != test.want {
				t.Fatalf("isExternalRemoteFile(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestExternalFileLocalPathUsesRemoteBasename(t *testing.T) {
	dir := t.TempDir()
	got, err := externalFileLocalPath(dir, `levels\users\main.NW`)
	if err != nil {
		t.Fatalf("externalFileLocalPath returned error: %v", err)
	}
	want := filepath.Join(dir, "main.NW")
	if got != want {
		t.Fatalf("externalFileLocalPath = %q, want %q", got, want)
	}

	if _, err := externalFileLocalPath("", "main.nw"); err == nil {
		t.Fatal("externalFileLocalPath accepted an empty downloads folder")
	}
}

func TestExternalFileSignatureDetectsChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.nw")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	initial, err := externalFileSignatureFor(path)
	if err != nil {
		t.Fatalf("stat initial file: %v", err)
	}
	unchanged, err := externalFileSignatureFor(path)
	if err != nil {
		t.Fatalf("stat unchanged file: %v", err)
	}
	if externalFileChanged(initial, unchanged) {
		t.Fatal("unchanged file was reported as changed")
	}

	if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
		t.Fatalf("write changed file: %v", err)
	}
	changed, err := externalFileSignatureFor(path)
	if err != nil {
		t.Fatalf("stat changed file: %v", err)
	}
	if !externalFileChanged(initial, changed) {
		t.Fatal("changed file was not reported as changed")
	}
}

func TestExternalFileHasWriteAccess(t *testing.T) {
	entries := []rclib.FileBrowserEntry{
		{Path: "levels/users/main.nw", Rights: "rw-"},
	}
	if !externalFileHasWriteAccess(entries, `levels\users\main.nw`) {
		t.Fatal("write permission was not detected for a matching remote path")
	}
	if externalFileHasWriteAccess(entries, "levels/users/other.nw") {
		t.Fatal("write permission leaked to a different remote path")
	}

	entries[0].Rights = "r--"
	if externalFileHasWriteAccess(entries, "levels/users/main.nw") {
		t.Fatal("read-only rights were reported as writable")
	}

	entries = append(entries, rclib.FileBrowserEntry{
		Path:        "levels/users/",
		Rights:      "rw-",
		IsDirectory: true,
	})
	if externalFileHasWriteAccess(entries, "levels/users/") {
		t.Fatal("directory rights were reported as file write access")
	}
}
