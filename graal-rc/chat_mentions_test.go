package main

import "testing"

func TestChatTextForMentionEvent(t *testing.T) {
	tests := []struct {
		name  string
		event string
		data  []any
		want  string
		ok    bool
	}{
		{name: "rc message", event: "rc:message", data: []any{"Zodiac: oi @RuanF"}, want: "Zodiac: oi @RuanF", ok: true},
		{name: "irc message", event: "rc:irc", data: []any{"staff", "<Zodiac> oi @RuanF"}, want: "<Zodiac> oi @RuanF", ok: true},
		{name: "nc message", event: "rc:serverdata", data: []any{"nc_message", "Zodiac: oi @RuanF"}, want: "Zodiac: oi @RuanF", ok: true},
		{name: "non chat server data", event: "rc:serverdata", data: []any{"statuslist", "@RuanF"}, ok: false},
		{name: "wrong payload", event: "rc:message", data: []any{42}, ok: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := chatTextForMentionEvent(test.event, test.data)
			if ok != test.ok || got != test.want {
				t.Fatalf("chatTextForMentionEvent(%q, %#v) = (%q, %v), want (%q, %v)", test.event, test.data, got, ok, test.want, test.ok)
			}
		})
	}
}
