package main

import (
	"bytes"
	"testing"

	deploylib "graal-rc/internal/deploy"
)

func TestNewDeploymentBackupDiffText(t *testing.T) {
	meta := deploylib.Backup{Resource: "script", Target: "weapon:Staff"}
	got := newDeploymentBackupDiff(meta, []byte("old\n"), []byte("new\n"))

	if !got.Diffable {
		t.Fatal("text backup should be diffable")
	}
	if got.Language != "graalscript" {
		t.Fatalf("language = %q, want graalscript", got.Language)
	}
	if got.BackupContent != "old\n" || got.CurrentContent != "new\n" {
		t.Fatalf("diff content = %q / %q", got.BackupContent, got.CurrentContent)
	}
	if got.CurrentSize != 4 || got.CurrentSHA256 == "" {
		t.Fatalf("current metadata = size %d, sha256 %q", got.CurrentSize, got.CurrentSHA256)
	}
}

func TestNewDeploymentBackupDiffBinary(t *testing.T) {
	meta := deploylib.Backup{Resource: "file", Target: "levels/world.gmap"}
	got := newDeploymentBackupDiff(meta, []byte{0x00, 0xff}, []byte{0x00, 0xfe})

	if got.Diffable {
		t.Fatal("binary backup should not be rendered as text")
	}
	if got.DiffReason != "binary" {
		t.Fatalf("diff reason = %q, want binary", got.DiffReason)
	}
	if got.BackupContent != "" || got.CurrentContent != "" {
		t.Fatal("binary payload should not be exposed as text")
	}
}

func TestNewDeploymentBackupDiffTooLarge(t *testing.T) {
	meta := deploylib.Backup{Resource: "textfile", Target: "logs/server.log"}
	content := bytes.Repeat([]byte("x"), maxDeploymentDiffTextBytes+1)
	got := newDeploymentBackupDiff(meta, content, []byte("current"))

	if got.Diffable {
		t.Fatal("oversized backup should not be rendered in Monaco")
	}
	if got.DiffReason != "too_large" {
		t.Fatalf("diff reason = %q, want too_large", got.DiffReason)
	}
}
