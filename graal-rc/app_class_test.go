package main

import (
	"testing"

	"graal-rc/internal/connection"
)

func TestInitialClassScriptPrefersCommunityName(t *testing.T) {
	got := initialClassScript(connection.Status{CommunityName: "Zodiac", Account: "Graal123"})
	if got != "// Scripted by Zodiac" {
		t.Fatalf("initial class script = %q", got)
	}
}

func TestInitialClassScriptFallsBackToAccount(t *testing.T) {
	got := initialClassScript(connection.Status{Account: "Graal123"})
	if got != "// Scripted by Graal123" {
		t.Fatalf("initial class script = %q", got)
	}
}
