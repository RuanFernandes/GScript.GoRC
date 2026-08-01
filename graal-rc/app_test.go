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

func TestLegacyAccountValues(t *testing.T) {
	profile, nickname := legacyAccountValues("", "Preagonal:Repinho")
	if profile != preagonalPrefix || nickname != "Repinho" {
		t.Fatalf("legacy values = (%q, %q), want (%q, %q)", profile, nickname, preagonalPrefix, "Repinho")
	}

	profile, nickname = legacyAccountValues("", "Repinho")
	if profile != "" || nickname != "Repinho" {
		t.Fatalf("default values = (%q, %q), want (%q, %q)", profile, nickname, "", "Repinho")
	}
}
