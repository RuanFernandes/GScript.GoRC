package sync

import (
	"path/filepath"
	"testing"
)

func TestNormalizeAndHashWireLineEndings(t *testing.T) {
	if got, want := normalizeEOL("a\xA7b\r\nc\rd"), "a\nb\nc\nd"; got != want {
		t.Fatalf("normalizeEOL() = %q, want %q", got, want)
	}
	if HashScript("a\xA7b") != HashScript("a\nb") {
		t.Fatal("wire line ending must not change the content hash")
	}
}

func TestEncodeNameRoundTripsUnsafeCharacters(t *testing.T) {
	for _, name := range []string{"weapon/name", `a\\b:c*?\"<>|`, "normal"} {
		encoded := encodeName(name)
		if got := decodeName(encoded); got != name {
			t.Fatalf("decodeName(encodeName(%q)) = %q", name, got)
		}
	}
	if got := encodeName("a/b"); got != "a%047b" {
		t.Fatalf("encodeName() = %q, want a%%047b", got)
	}
}

func TestLocalScriptPathUsesManagedSyncLayout(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "scripts")
	got, err := LocalScriptPath(outputDir, "npc", "folder/name")
	if err != nil {
		t.Fatalf("LocalScriptPath() error = %v", err)
	}

	want := filepath.Join(outputDir, "npcs", "folder%047name.gs2")
	if got != want {
		t.Fatalf("LocalScriptPath() = %q, want %q", got, want)
	}
}

func TestLocalScriptPathRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct {
		name string
		kind string
	}{
		{name: "", kind: "weapon"},
		{name: "Sword", kind: "unknown"},
	} {
		if _, err := LocalScriptPath(t.TempDir(), test.kind, test.name); err == nil {
			t.Fatalf("LocalScriptPath(%q, %q) returned nil error", test.kind, test.name)
		}
	}
}
