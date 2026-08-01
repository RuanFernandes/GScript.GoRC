package main

import (
	"testing"

	"graal-rc/rclib"
)

func TestListserverForProfile(t *testing.T) {
	tests := []struct {
		name     string
		profile  string
		wantHost string
	}{
		{name: "default profile", profile: "", wantHost: rclib.DefaultListserverHost},
		{name: "reborn profile", profile: "Preagonal:", wantHost: PreagonalListserverHost},
		{name: "reborn profile with whitespace", profile: "  Preagonal:  ", wantHost: PreagonalListserverHost},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := listserverForProfile(tt.profile)
			if host != tt.wantHost {
				t.Fatalf("host = %q, want %q", host, tt.wantHost)
			}
			if port != rclib.DefaultListserverPort {
				t.Fatalf("port = %d, want %d", port, rclib.DefaultListserverPort)
			}
		})
	}
}

func TestProfileNameForLegacyNickname(t *testing.T) {
	if got := profileNameForLegacyNickname("Preagonal:Repinho"); got != preagonalPrefix {
		t.Fatalf("profile = %q, want %q", got, preagonalPrefix)
	}
	if got := profileNameForLegacyNickname("Repinho"); got != "" {
		t.Fatalf("profile = %q, want empty", got)
	}
}
