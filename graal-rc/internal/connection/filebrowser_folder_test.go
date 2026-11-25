package connection

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"graal-rc/rclib"
)

// fakeBrowser models the server side: a listing request changes the current
// folder and is answered with a listing callback.
type fakeBrowser struct {
	view     *fileBrowserView
	folders  map[string][]string
	requests []string
	shown    []string // folders whose listing reached the window
}

func (f *fakeBrowser) list(_ context.Context, folder string) ([]rclib.FileBrowserEntry, error) {
	f.requests = append(f.requests, folder)
	names, ok := f.folders[folder]
	if !ok {
		return nil, errors.New("file-browser folder listing timed out")
	}
	reported := folder + "/"
	if len(names) == 0 {
		reported = ""
	}
	f.deliver(reported, len(names))
	entries := make([]rclib.FileBrowserEntry, len(names))
	for i, name := range names {
		entries[i] = rclib.FileBrowserEntry{Path: name}
	}
	return entries, nil
}

func (f *fakeBrowser) deliver(folder string, count int) {
	if label, hidden := f.view.delivered(folder, count); !hidden {
		f.shown = append(f.shown, label)
	}
}

func newFakeBrowser() *fakeBrowser {
	return &fakeBrowser{view: &fileBrowserView{}, folders: map[string][]string{
		"levels": {"a.nw"}, "images": {"b.png"}, "empty": {},
	}}
}

func (f *fakeBrowser) open(folder string) {
	f.view.requested(folder + "/")
	_, _ = f.list(context.Background(), folder)
	f.requests, f.shown = nil, nil
}

func TestWithFolderUsesTheTargetFolderAndRestoresTheShownOne(t *testing.T) {
	browser := newFakeBrowser()
	browser.open("images")

	var during []string
	err := browser.view.withFolder(context.Background(), "levels", browser.list, func(entries []rclib.FileBrowserEntry) error {
		during = append([]string(nil), browser.requests...)
		if len(entries) != 1 || entries[0].Path != "a.nw" {
			t.Fatalf("entries = %+v, want the target folder's", entries)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"levels"}; !reflect.DeepEqual(during, want) {
		t.Fatalf("requests before use = %v, want %v", during, want)
	}
	if want := []string{"levels", "images"}; !reflect.DeepEqual(browser.requests, want) {
		t.Fatalf("requests = %v, want %v", browser.requests, want)
	}
	if want := []string{"images/"}; !reflect.DeepEqual(browser.shown, want) {
		t.Fatalf("window saw %v, want only its own folder", browser.shown)
	}
}

func TestWithFolderDoesNotRestoreWhenTheTargetIsShown(t *testing.T) {
	browser := newFakeBrowser()
	browser.open("levels")
	if err := browser.view.withFolder(context.Background(), "levels", browser.list, func([]rclib.FileBrowserEntry) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if want := []string{"levels"}; !reflect.DeepEqual(browser.requests, want) {
		t.Fatalf("requests = %v, want %v", browser.requests, want)
	}
}

func TestWithFolderDoesNothingWhenTheFolderCannotBeListed(t *testing.T) {
	browser := newFakeBrowser()
	browser.open("images")
	err := browser.view.withFolder(context.Background(), "secret", browser.list, func([]rclib.FileBrowserEntry) error {
		t.Fatal("ran without knowing the target folder's files")
		return nil
	})
	if err == nil {
		t.Fatal("an unreadable target folder was accepted")
	}
	if want := []string{"secret", "images"}; !reflect.DeepEqual(browser.requests, want) {
		t.Fatalf("requests = %v, want the shown folder restored", browser.requests)
	}
}

func TestWithFolderRefusesAnAmbiguousEmptyListing(t *testing.T) {
	browser := newFakeBrowser()
	browser.view.settle = 10 * time.Millisecond
	browser.open("images")
	browser.view.requested("other/") // never answered yet
	err := browser.view.withFolder(context.Background(), "empty", browser.list, func([]rclib.FileBrowserEntry) error {
		t.Fatal("trusted an empty listing that may belong to another request")
		return nil
	})
	if err == nil {
		t.Fatal("ambiguous empty listing was accepted")
	}
}

func TestWithFolderAcceptsAnEmptyTargetFolder(t *testing.T) {
	browser := newFakeBrowser()
	browser.open("images")
	ran := false
	if err := browser.view.withFolder(context.Background(), "empty", browser.list, func(entries []rclib.FileBrowserEntry) error {
		ran = len(entries) == 0
		return nil
	}); err != nil || !ran {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
}

// The native library does not name the folder of an empty listing, which would
// otherwise show (and upload into) the root.
func TestEmptyListingIsLabelledWithTheRequestedFolder(t *testing.T) {
	view := &fileBrowserView{}
	view.requested("levels/new/")
	if label, hidden := view.delivered("", 0); label != "levels/new/" || hidden {
		t.Fatalf("label=%q hidden=%v", label, hidden)
	}
}
