package mcp

import (
	"strings"
	"testing"
)

func browserBackend() *fakeBackend {
	return &fakeBackend{
		folders: []Folder{{Pattern: "levels/*", Rights: "rw"}, {Pattern: "scripts/*", Rights: "r"}},
		listings: map[string][]File{
			"levels":  {{Path: "levels/town.nw", Size: 10}},
			"scripts": {{Path: "scripts/main.txt", Size: 4}},
		},
	}
}

func TestFileBrowserListWithoutCurrentFolderReturnsFolders(t *testing.T) {
	text, _ := toolText(t, callTool(t, browserBackend(), "filebrowser_list", map[string]any{}))
	if !strings.Contains(text, "levels/*") || !strings.Contains(text, "scripts/*") {
		t.Fatalf("expected the accessible folders, got %s", text)
	}
}

func TestFileBrowserCdReturnsListingAndListKeepsFolder(t *testing.T) {
	backend := browserBackend()
	text, _ := toolText(t, callTool(t, backend, "filebrowser_cd", map[string]any{"path": "levels/"}))
	if !strings.Contains(text, "levels/town.nw") {
		t.Fatalf("cd did not return the listing: %s", text)
	}
	text, _ = toolText(t, callTool(t, backend, "filebrowser_list", map[string]any{}))
	if !strings.Contains(text, "levels/town.nw") {
		t.Fatalf("list did not use the current folder: %s", text)
	}
}

func TestFileBrowserListAndSearchAcceptPath(t *testing.T) {
	backend := browserBackend()
	text, _ := toolText(t, callTool(t, backend, "filebrowser_list", map[string]any{"path": "scripts"}))
	if !strings.Contains(text, "scripts/main.txt") {
		t.Fatalf("list ignored the path: %s", text)
	}
	text, _ = toolText(t, callTool(t, backend, "filebrowser_search", map[string]any{"query": "TOWN", "path": "levels"}))
	if !strings.Contains(text, "levels/town.nw") {
		t.Fatalf("search did not match: %s", text)
	}
}

func TestFileBrowserSearchWithoutCurrentFolderMatchesFolders(t *testing.T) {
	text, _ := toolText(t, callTool(t, browserBackend(), "filebrowser_search", map[string]any{"query": "script"}))
	if !strings.Contains(text, "scripts/*") || strings.Contains(text, "levels/*") {
		t.Fatalf("unexpected folder matches: %s", text)
	}
}

func TestFileBrowserListingErrorIsReported(t *testing.T) {
	response := callTool(t, browserBackend(), "filebrowser_cd", map[string]any{"path": "missing"})
	if response["result"].(map[string]any)["isError"] != true {
		t.Fatalf("expected a tool error: %v", response)
	}
}
