package main

import (
	"errors"
	"reflect"
	"testing"

	"graal-rc/internal/connection"
	"graal-rc/rclib"
)

type fakeUploadFolder struct {
	calls       []string
	downloadErr error
}

func (f *fakeUploadFolder) Download(name string) ([]byte, error) {
	f.calls = append(f.calls, "download "+name)
	return []byte("old"), f.downloadErr
}

func (f *fakeUploadFolder) Upload(path string, _ []byte) error {
	f.calls = append(f.calls, "upload "+path)
	return nil
}

func TestUploadBacksUpAFileOfTheTargetFolderFirst(t *testing.T) {
	a := &App{sessions: connection.NewService()}
	folder := &fakeUploadFolder{}
	entries := []rclib.FileBrowserEntry{{Path: "town.nw"}}
	if err := a.uploadIntoFolder(folder, entries, "levels/town.nw", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if want := []string{"download town.nw", "upload levels/town.nw"}; !reflect.DeepEqual(folder.calls, want) {
		t.Fatalf("calls = %v, want %v", folder.calls, want)
	}
}

func TestUploadOfAnExistingFileStopsWhenTheBackupFails(t *testing.T) {
	a := &App{sessions: connection.NewService()}
	folder := &fakeUploadFolder{downloadErr: errors.New("timed out")}
	if err := a.uploadIntoFolder(folder, []rclib.FileBrowserEntry{{Path: "town.nw"}}, "levels/town.nw", []byte("new")); err == nil {
		t.Fatal("uploaded without a backup")
	}
	if want := []string{"download town.nw"}; !reflect.DeepEqual(folder.calls, want) {
		t.Fatalf("calls = %v, want %v", folder.calls, want)
	}
}

func TestUploadOfANewFileDownloadsNothing(t *testing.T) {
	a := &App{sessions: connection.NewService()}
	folder := &fakeUploadFolder{}
	if err := a.uploadIntoFolder(folder, []rclib.FileBrowserEntry{{Path: "other.nw"}}, "levels/town.nw", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if want := []string{"upload levels/town.nw"}; !reflect.DeepEqual(folder.calls, want) {
		t.Fatalf("calls = %v, want %v", folder.calls, want)
	}
}

func TestFileBrowserUploadParent(t *testing.T) {
	for path, want := range map[string]string{"town.nw": "", "levels/town.nw": "levels", `a\b\c.txt`: "a/b"} {
		if got := fileBrowserUploadParent(path); got != want {
			t.Errorf("fileBrowserUploadParent(%q) = %q, want %q", path, got, want)
		}
	}
}
