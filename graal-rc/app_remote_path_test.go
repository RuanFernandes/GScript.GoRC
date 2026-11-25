package main

import (
	"strings"
	"testing"
)

// The window passes an entry as the listing names it, which can be the file name
// alone. Writes need the folder, or they would go to the root.
func TestQualifyRemotePath(t *testing.T) {
	tests := []struct{ folder, listed, want string }{
		{"levels/", "town.nw", "levels/town.nw"},
		{"levels", "levels/town.nw", "levels/town.nw"},
		{"", "town.nw", "town.nw"},
		{"a/b", `town.nw`, "a/b/town.nw"},
		{"levels", "other/town.nw", "other/town.nw"},
	}
	for _, tt := range tests {
		if got := qualifyRemotePath(tt.folder, tt.listed); got != tt.want {
			t.Errorf("qualifyRemotePath(%q, %q) = %q, want %q", tt.folder, tt.listed, got, tt.want)
		}
	}
}

// Two databases that share a name must not share a local copy: committing one
// would upload the other's contents into it.
func TestSqliteCacheNameIsPerRemotePath(t *testing.T) {
	first, second := remoteCacheName("a/data.db"), remoteCacheName("b/data.db")
	if first == second {
		t.Fatalf("both use %q", first)
	}
	if !strings.HasSuffix(first, "data.db") || strings.ContainsAny(first, `/\:`) {
		t.Fatalf("unexpected cache name %q", first)
	}
}

// Older backups can name a file without its folder. Restoring one could replace
// an unrelated file at the root.
func TestRollbackRefusesABackupWithoutItsFolder(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"a.txt": "unrelated root file"})
	meta, err := a.backups.SaveLimited(DeploymentBackup{Resource: "textfile", Target: "a.txt"}, []byte("older"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RollbackDeployment(meta.ID); err == nil {
		t.Fatal("restored a backup that does not record its folder")
	}
	wantCalls(t, remote)
}

func TestRollbackAcceptsANewBackupOfARootFile(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"a.txt": "current"})
	meta, _, err := a.saveDeploymentBackup("textfile", "a.txt", []byte("older"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RollbackDeployment(meta.ID); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, remote, "download a.txt", "upload a.txt")
}

// The diff shown before a restore must read the same file the restore replaces.
func TestBackupDiffReadsTheTargetFolder(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"config/a.txt": "current"})
	content, err := a.readCurrentDeploymentContent(DeploymentBackup{Resource: "textfile", Target: "config/a.txt", FullPath: true})
	if err != nil || string(content) != "current" {
		t.Fatalf("content = %q, err = %v", content, err)
	}
	wantCalls(t, remote, "download config/a.txt")
}
