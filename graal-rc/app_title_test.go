package main

import "testing"

func TestEditorTitlePlacesResourceBeforeServer(t *testing.T) {
	tests := []struct {
		name       string
		scriptType string
		key        string
		server     string
		want       string
	}{
		{name: "weapon", scriptType: "weapon", key: "Sword", server: "Zodiac", want: "W: Sword - Zodiac"},
		{name: "class", scriptType: "class", key: "Player", server: "Zodiac", want: "C: Player - Zodiac"},
		{name: "npc", scriptType: "npc", key: "Guard", server: "Zodiac", want: "N: Guard - Zodiac"},
		{name: "npc flags", scriptType: "npcflags", key: "Guard", server: "Zodiac", want: "F: Guard - Zodiac"},
		{name: "npc attributes", scriptType: "npcattr", key: "Guard", server: "Zodiac", want: "A: Guard - Zodiac"},
		{name: "server options", scriptType: "options", key: "Server Options", server: "Zodiac", want: "Server Options - Zodiac"},
		{name: "folder config", scriptType: "folder_config", key: "Folder Config", server: "Zodiac", want: "Folder Config - Zodiac"},
		{name: "server flags", scriptType: "flags", key: "Server Flags", server: "Zodiac", want: "Server Flags - Zodiac"},
		{name: "text file", scriptType: "textfile", key: "serveroptions.txt", server: "Zodiac", want: "serveroptions.txt - Zodiac"},
		{name: "without server", scriptType: "weapon", key: "Sword", want: "W: Sword"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := editorTitle(tt.server, tt.scriptType, tt.key); got != tt.want {
				t.Fatalf("editorTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScriptEditorTitlePlacesResourceBeforeServer(t *testing.T) {
	tests := []struct {
		name       string
		scriptType string
		key        string
		server     string
		want       string
	}{
		{name: "weapon", scriptType: "weapon", key: "Sword", server: "Zodiac", want: "W: Sword - Zodiac"},
		{name: "npc flags", scriptType: "npcflags", key: "Guard", server: "Zodiac", want: "F: Guard - Zodiac"},
		{name: "without server", scriptType: "weapon", key: "Sword", want: "W: Sword"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scriptEditorTitle(tt.server, tt.scriptType, tt.key); got != tt.want {
				t.Fatalf("scriptEditorTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServerWindowTitle(t *testing.T) {
	tests := []struct {
		name   string
		server string
		title  string
		want   string
	}{
		{name: "connected", server: "Zodiac", title: "File Browser", want: "File Browser - Zodiac"},
		{name: "trims whitespace", server: " Zodiac ", title: " File Browser ", want: "File Browser - Zodiac"},
		{name: "without server", title: "Script Manager", want: "Script Manager"},
		{name: "without title", server: "Zodiac", want: "Zodiac"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serverWindowTitle(tt.server, tt.title); got != tt.want {
				t.Fatalf("serverWindowTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}
