package connection

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeFileBrowserBackupFolders(t *testing.T) {
	got, err := normalizeFileBrowserBackupFolders([]string{"scripts\\", "levels", "levels/"})
	if err != nil {
		t.Fatalf("normalizeFileBrowserBackupFolders() error = %v", err)
	}
	if want := []string{"levels", "scripts"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized folders = %#v, want %#v", got, want)
	}
}

func TestNormalizeFileBrowserBackupFoldersAllowsRoot(t *testing.T) {
	got, err := normalizeFileBrowserBackupFolders([]string{"/"})
	if err != nil {
		t.Fatalf("normalizeFileBrowserBackupFolders() error = %v", err)
	}
	if want := []string{""}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized root = %#v, want %#v", got, want)
	}
}

func TestNormalizeFileBrowserBackupFoldersRejectsTraversal(t *testing.T) {
	for _, folder := range []string{"../outside", "levels/../outside", "C:/outside"} {
		t.Run(folder, func(t *testing.T) {
			if _, err := normalizeFileBrowserBackupFolders([]string{folder}); err == nil {
				t.Fatalf("normalizeFileBrowserBackupFolders(%q) accepted an invalid path", folder)
			}
		})
	}
}

func TestFileBrowserBackupEntryPathSupportsRelativeAndFullListingPaths(t *testing.T) {
	tests := []struct {
		folder string
		entry  string
		want   string
	}{
		{folder: "levels", entry: "main.nw", want: "levels/main.nw"},
		{folder: "levels", entry: "levels/main.nw", want: "levels/main.nw"},
		{folder: "", entry: "scripts/main.gs2", want: "scripts/main.gs2"},
	}
	for _, tt := range tests {
		t.Run(tt.folder+"/"+tt.entry, func(t *testing.T) {
			got, err := fileBrowserBackupEntryPath(tt.folder, tt.entry)
			if err != nil {
				t.Fatalf("fileBrowserBackupEntryPath() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("entry path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFileBrowserBackupEntryPathRejectsTraversal(t *testing.T) {
	if _, err := fileBrowserBackupEntryPath("levels", "../outside.txt"); err == nil {
		t.Fatal("fileBrowserBackupEntryPath() accepted a traversal path")
	}
}

func TestCancelCurrentFileBrowserListingWakesWaiter(t *testing.T) {
	s := NewService()
	wait := s.registerFileBrowserListing("levels")
	wantErr := context.Canceled
	s.cancelCurrentFileBrowserListing(wantErr)

	select {
	case <-wait.done:
		if !errors.Is(wait.err, wantErr) {
			t.Fatalf("listing waiter error = %v, want %v", wait.err, wantErr)
		}
	default:
		t.Fatal("listing waiter was not released")
	}
}
