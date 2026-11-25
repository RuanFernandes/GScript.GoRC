package main

import (
	"testing"

	"graal-rc/internal/connection"
)

// The native add packets are "add or update": adding an existing name would
// replace that script with an empty one.
func TestEnsureScriptNameFree(t *testing.T) {
	existing := []string{"Utility", "gui/Menu"}
	for _, name := range []string{"Utility", "utility", " UTILITY ", "GUI/menu"} {
		if err := ensureScriptNameFree("class", name, existing); err == nil {
			t.Errorf("%q: expected an already-exists error", name)
		}
	}
	if err := ensureScriptNameFree("class", "utility2", existing); err != nil {
		t.Errorf("a free name was refused: %v", err)
	}
}

// Delete, overwrite and SQLite save only ask for a backup of files that exist,
// so a failed download must stop them.
func TestBackupRemoteContentReportsDownloadFailure(t *testing.T) {
	a := &App{sessions: connection.NewService()}
	if _, ok, err := a.backupRemoteContent("file", "levels/town.nw"); err == nil || ok {
		t.Fatalf("ok=%v err=%v, want the download failure", ok, err)
	}
}

func TestServerChangedSinceOpen(t *testing.T) {
	tests := []struct {
		name                    string
		baseline, current, next string
		hasBaseline, want       bool
	}{
		{name: "unchanged server", hasBaseline: true, baseline: "a", current: "a", next: "mine"},
		{name: "someone else saved", hasBaseline: true, baseline: "a", current: "theirs", next: "mine", want: true},
		{name: "server already has my text", hasBaseline: true, baseline: "a", current: "mine", next: "mine"},
		{name: "only line endings differ", hasBaseline: true, baseline: "a\nb", current: "a\r\nb\r\n", next: "mine"},
		{name: "no open editor", current: "theirs", next: "mine"},
	}
	for _, tt := range tests {
		if got := serverChangedSinceOpen(tt.baseline, tt.hasBaseline, tt.current, tt.next); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
