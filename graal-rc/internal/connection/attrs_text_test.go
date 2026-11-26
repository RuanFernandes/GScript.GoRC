package connection

import "testing"

// grclib decodes the animation as "gani" but its editor text reads another key,
// so the line came out empty and Apply could clear the animation.
func TestAttrsEditorTextShowsTheAnimation(t *testing.T) {
	const editor = "[Look]\nBody Image: body.png\nAnimation: \nSkin Color: white\n[Script Flags]\nAnimation: \n"
	got := attrsEditorText(`{"account":"bob","gani":"idle,sword1.png"}`, editor)
	const want = "[Look]\nBody Image: body.png\nAnimation: idle,sword1.png\nSkin Color: white\n[Script Flags]\nAnimation: \n"
	if got != want {
		t.Fatalf("editor text = %q, want %q", got, want)
	}
}

func TestAttrsEditorTextIsKeptWithoutAnAnimation(t *testing.T) {
	const editor = "[Look]\nAnimation: \n"
	for _, properties := range []string{`{"account":"bob"}`, `{"gani":""}`, "not json", `{"gani":"a\nWeapons: x"}`} {
		if got := attrsEditorText(properties, editor); got != editor {
			t.Errorf("attrsEditorText(%q) = %q", properties, got)
		}
	}
}
