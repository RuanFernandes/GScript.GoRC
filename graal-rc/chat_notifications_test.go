package main

import (
	"strings"
	"testing"
)

func TestChatMentionNotificationTitle(t *testing.T) {
	tests := map[string]string{
		"pt-BR": "Você foi chamado no RC Chat",
		"es":    "Te mencionaron en RC Chat",
		"en":    "You were mentioned in RC Chat",
	}

	for language, want := range tests {
		if got := chatMentionNotificationTitle(language); got != want {
			t.Fatalf("language %q: got %q, want %q", language, got, want)
		}
	}
}

func TestNotificationTextSanitizesAndBoundsMessage(t *testing.T) {
	if got := notificationText("  Player: oi\x00\x1b[31m @RuanF  "); got != "Player: oi[31m @RuanF" {
		t.Fatalf("sanitized notification text = %q", got)
	}

	longMessage := strings.Repeat("x", maxChatMentionNotificationRunes+100)
	got := []rune(notificationText(longMessage))
	if len(got) != maxChatMentionNotificationRunes {
		t.Fatalf("notification rune count = %d, want %d", len(got), maxChatMentionNotificationRunes)
	}
	if got[len(got)-1] != '…' {
		t.Fatalf("notification should end with ellipsis, got %q", got[len(got)-1])
	}
}
