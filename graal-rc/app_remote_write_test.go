package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"graal-rc/internal/connection"
	deploylib "graal-rc/internal/deploy"
	"graal-rc/internal/sqlite"
	"graal-rc/rclib"
)

// fakeRemote stands in for the server's folders and records what was done in
// which one.
type fakeRemote struct {
	files       map[string]string // full path -> content
	calls       []string
	downloadErr error
}

type fakeRemoteDir struct {
	remote *fakeRemote
	folder string
}

func (d fakeRemoteDir) path(name string) string {
	if d.folder == "" {
		return name
	}
	return d.folder + "/" + name
}

func (d fakeRemoteDir) Download(name string) ([]byte, error) {
	d.remote.calls = append(d.remote.calls, "download "+d.path(name))
	if d.remote.downloadErr != nil {
		return nil, d.remote.downloadErr
	}
	return []byte(d.remote.files[d.path(name)]), nil
}

func (d fakeRemoteDir) Upload(remotePath string, content []byte) error {
	// Like the server, only the base name counts: the open folder decides.
	target := d.path(filepath.Base(remotePath))
	d.remote.calls = append(d.remote.calls, "upload "+target)
	d.remote.files[target] = string(content)
	return nil
}

func remoteApp(t *testing.T, files map[string]string) (*App, *fakeRemote) {
	t.Helper()
	remote := &fakeRemote{files: files}
	backups, err := deploylib.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{sessions: connection.NewService(), backups: backups,
		textCache: map[string][]byte{}, textOriginalCache: map[string][]byte{}}
	a.openRemoteFolder = func(_ context.Context, folder string, use func(fileBrowserUploadFolder, []rclib.FileBrowserEntry) error) error {
		var entries []rclib.FileBrowserEntry
		for full := range remote.files {
			if fileBrowserUploadParent(full) == folder {
				entries = append(entries, rclib.FileBrowserEntry{Path: filepath.Base(full)})
			}
		}
		return use(fakeRemoteDir{remote, folder}, entries)
	}
	return a, remote
}

func wantCalls(t *testing.T, remote *fakeRemote, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(remote.calls, want) {
		t.Fatalf("calls = %v, want %v", remote.calls, want)
	}
}

func TestSaveTextFileWritesIntoItsOwnFolder(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"config/a.txt": "old"})
	if err := a.SaveTextFile("config/a.txt", "new"); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, remote, "download config/a.txt", "upload config/a.txt")
}

// Creating it would hide that the open file was moved or deleted.
func TestSaveTextFileRefusesAFileNoLongerThere(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"config/other.txt": "x"})
	if err := a.SaveTextFile("config/a.txt", "new"); err == nil {
		t.Fatal("saved a file that is not in the folder")
	}
	wantCalls(t, remote)
}

func TestSaveTextFileStillWarnsAboutAServerChange(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"config/a.txt": "theirs"})
	a.textOriginalCache["config/a.txt"] = []byte("opened")
	if err := a.SaveTextFile("config/a.txt", "mine"); !errors.Is(err, errServerChanged) {
		t.Fatalf("error = %v, want server changed", err)
	}
	wantCalls(t, remote, "download config/a.txt")
}

func TestExternalFileUploadsIntoItsOwnFolder(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"levels/town.nw": "old"})
	local := filepath.Join(t.TempDir(), "town.nw")
	if err := os.WriteFile(local, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := externalFileSession{remotePath: "levels/town.nw", localPath: local, writable: true}
	a.pollExternalFileSession(&session)
	wantCalls(t, remote, "download levels/town.nw", "upload levels/town.nw")
	if remote.files["levels/town.nw"] != "edited" || session.lastSignature == (externalFileSignature{}) {
		t.Fatalf("upload was not recorded: %q", remote.files["levels/town.nw"])
	}
}

func TestRollbackRestoresIntoItsOwnFolderAndBacksUpFirst(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"config/a.txt": "current"})
	meta, ok, err := a.saveDeploymentBackup("textfile", "config/a.txt", []byte("older"), true)
	if err != nil || !ok {
		t.Fatalf("seed backup: %v", err)
	}
	if err := a.RollbackDeployment(meta.ID); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, remote, "download config/a.txt", "upload config/a.txt")
	if remote.files["config/a.txt"] != "older" {
		t.Fatalf("content = %q", remote.files["config/a.txt"])
	}
}

func TestRollbackCanRecreateADeletedFile(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{})
	meta, _, err := a.saveDeploymentBackup("file", "config/a.txt", []byte("older"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RollbackDeployment(meta.ID); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, remote, "upload config/a.txt")
}

func TestPluginWriteUsesTheTargetFolderForItsRevisionCheck(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"scripts/a.txt": "current"})
	err := a.writeRemotePluginFile("scripts/a.txt", []byte("new"), pluginFileRevision([]byte("stale")))
	if err == nil {
		t.Fatal("a stale revision was accepted")
	}
	wantCalls(t, remote, "download scripts/a.txt")
	remote.calls = nil
	if err := a.writeRemotePluginFile("scripts/a.txt", []byte("new"), pluginFileRevision([]byte("current"))); err != nil {
		t.Fatal(err)
	}
	if got := remote.calls[len(remote.calls)-1]; got != "upload scripts/a.txt" {
		t.Fatalf("calls = %v", remote.calls)
	}
}

func TestSqliteCommitWritesIntoItsOwnFolder(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"db/data.db": "old"})
	local := filepath.Join(t.TempDir(), "data.db")
	db, err := sqlite.Open(local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlite.Close(local) })
	if _, err := db.Exec("CREATE TABLE items (name TEXT)"); err != nil {
		t.Fatal(err)
	}
	a.dbFiles = map[string]string{"db/data.db": local}
	changes := sqlite.Changes{Table: "items", Inserts: []map[string]any{{"name": "sword"}}}
	if err := a.CommitSqlite("db/data.db", changes); err != nil {
		t.Fatal(err)
	}
	wantCalls(t, remote, "download db/data.db", "upload db/data.db")
}

func TestRemoteWriteStopsWhenTheBackupFails(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"config/a.txt": "old"})
	remote.downloadErr = errors.New("timed out")
	if err := a.SaveTextFile("config/a.txt", "new"); err == nil {
		t.Fatal("saved without a backup")
	}
	wantCalls(t, remote, "download config/a.txt")
}
