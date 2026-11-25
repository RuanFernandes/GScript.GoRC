package connection

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
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

func TestResolveFileBrowserListingMatchesRequestedFolder(t *testing.T) {
	tests := []struct {
		name     string
		folder   string
		count    int
		resolved bool
	}{
		{name: "same folder", folder: "levels/", count: 3, resolved: true},
		{name: "other folder", folder: "scripts/", count: 3, resolved: false},
		{name: "root with entries", folder: "", count: 3, resolved: false},
		// The native library does not name the folder of an empty listing.
		{name: "empty listing", folder: "", count: 0, resolved: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewService()
			wait := s.registerFileBrowserListing("levels")
			s.resolveFileBrowserListing(tt.folder, tt.count)
			resolved := false
			select {
			case <-wait.done:
				resolved = true
			default:
			}
			if resolved != tt.resolved {
				t.Fatalf("resolved = %v, want %v", resolved, tt.resolved)
			}
		})
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

// A pending RWMutex writer blocks every new reader, so waiting for a long
// download must not queue one.
func TestLockFileBrowserOperationGivesUpWithoutBlockingReaders(t *testing.T) {
	s := NewService()
	s.fileBrowserOperationMu.RLock() // a download in flight
	defer s.fileBrowserOperationMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.lockFileBrowserOperation(ctx) }()

	time.Sleep(100 * time.Millisecond)
	if !s.fileBrowserOperationMu.TryRLock() {
		t.Fatal("a waiting listing blocked other File Browser operations")
	}
	s.fileBrowserOperationMu.RUnlock()

	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want deadline exceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lock ignored the context")
	}
}
