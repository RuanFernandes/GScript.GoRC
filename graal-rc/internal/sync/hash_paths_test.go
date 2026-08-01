package sync

import "testing"

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
