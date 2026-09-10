package connection

import "testing"

func TestMentionTextNormalization(t *testing.T) {
	text := "@Ruan\u200bF"
	if got := findExplicitMention(text, []string{"RuanF"}); got != "RuanF" {
		t.Fatalf("findExplicitMention(%q) = %q, want %q", text, got, "RuanF")
	}
}
