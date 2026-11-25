package main

import (
	"strings"
	"testing"
)

func TestMCPSetupKeepsTheOfficialAppsBlock(t *testing.T) {
	official := strings.TrimLeft(officialMCPInstructionBlock, "\n")
	existing := "# My rules\n\n" + official

	got, err := withMCPInstructions(existing)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, existing) {
		t.Fatalf("the setup changed the official block:\n%q", got)
	}
	own := strings.TrimPrefix(got, existing)
	if !strings.Contains(own, "http://127.0.0.1:8767/mcp") || strings.Contains(own, "8765") {
		t.Fatalf("this app's block must point at its own port only:\n%s", own)
	}
	again, err := withMCPInstructions(got)
	if err != nil || again != got {
		t.Fatalf("second run changed the file (err %v)", err)
	}
}

func TestTheOfficialUpdaterIsNeverUsed(t *testing.T) {
	if platform, arch := localReleaseTarget(); platform != "" || arch != "" {
		t.Fatalf("localReleaseTarget() = (%q, %q), want none", platform, arch)
	}
	if err := (&App{}).InstallUpdate(); err == nil || !strings.Contains(err.Error(), "does not update itself") {
		t.Fatalf("InstallUpdate() = %v, want the refusal", err)
	}
}
