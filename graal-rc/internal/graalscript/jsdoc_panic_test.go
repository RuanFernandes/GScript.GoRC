package graalscript

import (
	"os"
	"path/filepath"
	"testing"
)

// A lone "@" used to create a tag with no name, which panicked when formatted.
// Scripts come from the server, so any staff member could trigger it.
func TestLoneAtSignInJSDocDoesNotPanic(t *testing.T) {
	const script = "/**\n * @\n * text\n */\nfunction f() {}\n"
	if doc := parseDocument("file:///a.gs2", script, 0); len(doc.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(doc.Functions))
	}
}

// The scan parses in bare goroutines, where a panic would end the whole app.
func TestScanSurvivesAParserPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.gs2")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, ok := parseForScan(path, func() *Document { panic("boom") })
	if ok {
		t.Fatal("a panicking parse was reported as successful")
	}
}
