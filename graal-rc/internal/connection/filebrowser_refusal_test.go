package connection

import (
	"testing"

	"graal-rc/rclib"
)

// The server refuses a download with a message that is only the file name.
func TestRefusedDownloadEndsItsWait(t *testing.T) {
	for _, message := range []string{"a.png", "A.png\r\n", "world/images/a.png"} {
		s := NewService()
		waiter, _ := s.registerFile("world/images/a.png")
		s.handleFileBrowserMessage(message)
		select {
		case <-waiter.done:
			if waiter.err == nil {
				t.Errorf("%q: the refused download reported success", message)
			}
		default:
			t.Errorf("%q: the download is still waiting", message)
		}
	}
}

func TestOtherFileBrowserMessagesKeepTheDownloadWaiting(t *testing.T) {
	s := NewService()
	waiter, _ := s.registerFile("a.png")
	for _, message := range []string{"", "ba.png", "File complete: a.png", "Received chunk: 1/2 bytes for a.png", "Bigfile transfer started: a.png", "levels/"} {
		s.handleFileBrowserMessage(message)
		select {
		case <-waiter.done:
			t.Fatalf("%q ended the download", message)
		default:
		}
	}
}

// The server answers a request for an empty file with a refusal.
func TestDownloadListedDoesNotRequestAnEmptyFile(t *testing.T) {
	entries := []rclib.FileBrowserEntry{{Path: "world/images/.empty", Size: 0}, {Path: "a.png", Size: 3}, {Path: "sub/", Size: 0, IsDirectory: true}}
	var requested []string
	fetch := func(name string) ([]byte, error) {
		requested = append(requested, name)
		return []byte("abc"), nil
	}
	content, err := downloadListed(entries, ".empty", fetch)
	if err != nil || content == nil || len(content) != 0 || len(requested) != 0 {
		t.Fatalf("empty file: content %q, err %v, requested %v", content, err, requested)
	}
	for _, name := range []string{"a.png", "sub", "unlisted"} {
		if _, err := downloadListed(entries, name, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if len(requested) != 3 {
		t.Fatalf("requested = %v", requested)
	}
}
