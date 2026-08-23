package main

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestExtractLibraryAcceptsWindowsX64AssetName(t *testing.T) {
	var archiveBytes bytes.Buffer
	archive := zip.NewWriter(&archiveBytes)
	entry, err := archive.Create("grclib.dll")
	if err != nil {
		t.Fatalf("create archive entry: %v", err)
	}
	if _, err := entry.Write([]byte("patched-library")); err != nil {
		t.Fatalf("write archive entry: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}

	got, err := extractLibrary(archiveBytes.Bytes(), "grclib.dll", "grclib64.dll")
	if err != nil {
		t.Fatalf("extractLibrary returned error: %v", err)
	}
	if string(got) != "patched-library" {
		t.Fatalf("extracted library = %q, want patched-library", got)
	}
}
