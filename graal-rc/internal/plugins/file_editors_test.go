package plugins

import (
	"path/filepath"
	"testing"
)

func fileEditorManager(t *testing.T) *Manager {
	manifest := validManifest()
	manifest.Permissions = Permissions{
		APIs:  []string{"filebrowser.read", "filebrowser.write", "filebrowser.editor"},
		Files: FileScopes{Read: []string{"levels/**/*.arc", "shared/*.json"}, Write: []string{"levels/**/*.arc"}},
	}
	return &Manager{
		state: filepath.Join(t.TempDir(), "plugins.json"),
		plugins: map[string]PluginInfo{manifest.ID: {
			Manifest: manifest, Enabled: true, Status: "ready",
			ApprovedAPIs:      []string{"filebrowser.read", "filebrowser.write", "filebrowser.editor"},
			ApprovedFileRead:  []string{"levels/**/*.arc", "shared/*.json"},
			ApprovedFileWrite: []string{"levels/**/*.arc"},
		}},
		settings:    map[string]persistedState{},
		fileEditors: map[string]FileEditorRegistration{},
	}
}

func TestFileScopeMatching(t *testing.T) {
	cases := []struct {
		scope string
		path  string
		want  bool
	}{
		{"levels/**/*.arc", "levels/items/sword.arc", true},
		{"levels/**/*.arc", "levels/sword.arc", true},
		{"levels/**/*.arc", "levels/items/sword.json", false},
		{"shared/*.json", "shared/config.json", true},
		{"shared/*.json", "shared/nested/config.json", false},
	}
	for _, test := range cases {
		if got := fileScopeMatches(test.scope, test.path); got != test.want {
			t.Errorf("fileScopeMatches(%q, %q) = %v, want %v", test.scope, test.path, got, test.want)
		}
	}
}

func TestFileAccessRequiresApprovedScope(t *testing.T) {
	m := fileEditorManager(t)
	if err := m.RequireFileAccess("com.example.test", "levels/items/sword.arc", false); err != nil {
		t.Fatalf("approved read rejected: %v", err)
	}
	if err := m.RequireFileAccess("com.example.test", "shared/config.json", true); err == nil {
		t.Fatal("write outside the write scope was accepted")
	}
	if err := m.RequireFileAccess("com.example.test", "../secret.arc", false); err == nil {
		t.Fatal("parent traversal was accepted")
	}
}

func TestFileEditorRegistrationAndPriority(t *testing.T) {
	m := fileEditorManager(t)
	if err := m.RegisterFileEditor("com.example.test", FileEditorRegistration{ID: "default", Label: "ARC editor", Extensions: []string{".arc"}, Priority: 1}); err != nil {
		t.Fatal(err)
	}
	if err := m.RegisterFileEditor("com.example.test", FileEditorRegistration{ID: "high", Label: "Visual ARC editor", Extensions: []string{".arc"}, Priority: 10}); err != nil {
		t.Fatal(err)
	}
	match, ok := m.MatchFileEditor("levels/item.arc")
	if !ok || match.ID != "high" {
		t.Fatalf("unexpected editor match: %+v, %v", match, ok)
	}
	m.ClosePluginEditors("com.example.test")
	if _, ok := m.MatchFileEditor("levels/item.arc"); ok {
		t.Fatal("editor remained registered after plugin cleanup")
	}
}
