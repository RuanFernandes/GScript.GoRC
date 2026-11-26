package mcp

import (
	"context"
	"strings"
	"testing"
)

type readBackend struct {
	*fakeBackend
	content []byte
	reads   []string
}

func (b *readBackend) ReadFile(_ context.Context, path string) ([]byte, error) {
	b.reads = append(b.reads, path)
	return b.content, nil
}

func readFile(t *testing.T, backend *readBackend, path string) (string, bool) {
	t.Helper()
	return toolText(t, callToolOn(t, backend, "filebrowser_read", map[string]any{"path": path}))
}

func TestFileBrowserReadRefusesABinaryNameWithoutAskingTheServer(t *testing.T) {
	for _, path := range []string{"world/images/a.png", "world/images/A.PNG", "a.gif", "a.jpg", "a.jpeg", "a.mng", "sounds/a.wav", "a.mp3", "a.ogg", "a.zip", "scripts/data.db", "a.ttf"} {
		backend := &readBackend{fakeBackend: &fakeBackend{}, content: []byte("text")}
		text, isError := readFile(t, backend, path)
		if !isError || !strings.Contains(text, "filebrowser_download") {
			t.Errorf("%s: result = %q (error %v)", path, text, isError)
		}
		if len(backend.reads) != 0 {
			t.Errorf("%s: the server was asked for the file", path)
		}
	}
}

func TestFileBrowserReadStillReadsTextNames(t *testing.T) {
	for _, path := range []string{"a.txt", "levels/a.nw", "a.gani", "weapons/weaponTest", "a.png.txt", "world/.empty"} {
		backend := &readBackend{fakeBackend: &fakeBackend{}, content: []byte("olá")}
		text, isError := readFile(t, backend, path)
		if isError || !strings.Contains(text, "olá") {
			t.Errorf("%s: result = %q (error %v)", path, text, isError)
		}
	}
}

// A binary file can have any name, and NUL bytes are valid UTF-8.
func TestFileBrowserReadRefusesBinaryContent(t *testing.T) {
	for name, content := range map[string][]byte{"not UTF-8": {0xff, 0xfe}, "NUL bytes": []byte("SQLite format 3\x00")} {
		backend := &readBackend{fakeBackend: &fakeBackend{}, content: content}
		text, isError := readFile(t, backend, "accounts/unknown")
		if !isError || !strings.Contains(text, "filebrowser_download") {
			t.Errorf("%s: result = %q (error %v)", name, text, isError)
		}
	}
}
