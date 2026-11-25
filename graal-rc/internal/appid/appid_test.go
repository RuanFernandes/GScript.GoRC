package appid

import "testing"

// The Alpaxis app installs and runs beside the official Graal app, so nothing
// it names on the machine may match the official one.
func TestAlpaxisIdentityDoesNotCollideWithTheOfficialApp(t *testing.T) {
	if ID() != "alpaxis-rc" || DisplayName() != "Alpaxis Remote Control" || MCPAddress() != "127.0.0.1:8767" {
		t.Fatalf("identity = (%q, %q, %q)", ID(), DisplayName(), MCPAddress())
	}
	if MCPAddress() == OfficialMCPAddress {
		t.Fatal("the MCP port is the official one")
	}
	if SelfUpdates {
		t.Fatal("the release service would replace this app with the official one")
	}
}
