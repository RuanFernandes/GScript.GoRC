package sync

import (
	"os"
	"path/filepath"
	"testing"
)

type recordingBackend struct {
	permissionBackendStub
	addedClasses []string
	addedWeapons []string
	savedClasses map[string]string
	savedWeapons map[string]string
}

func (b *recordingBackend) AddClass(name string) error {
	b.addedClasses = append(b.addedClasses, name)
	return nil
}

func (b *recordingBackend) AddWeapon(name string) error {
	b.addedWeapons = append(b.addedWeapons, name)
	return nil
}

func (b *recordingBackend) SaveClass(name, content string) error {
	if b.savedClasses == nil {
		b.savedClasses = map[string]string{}
	}
	b.savedClasses[name] = content
	return nil
}

func (b *recordingBackend) SaveWeapon(name, content string) error {
	if b.savedWeapons == nil {
		b.savedWeapons = map[string]string{}
	}
	b.savedWeapons[name] = content
	return nil
}

func TestPushLocalCreatesClassAndInitializesEmptyBody(t *testing.T) {
	backend := &recordingBackend{}
	engine := NewEngine(backend, "TestServer", nil)
	dir := t.TempDir()
	engine.ApplyConfig(SyncConfig{Enabled: true, OutputDir: dir, AutoPushLocal: true})
	engine.SetClassScriptHeader(func() string { return "// Scripted by Zodiac" })
	path := filepath.Join(dir, "classes", "personal_test.gs2")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	engine.pushLocal(t.Context(), path)
	if len(backend.addedClasses) != 1 || backend.addedClasses[0] != "personal_test" {
		t.Fatalf("created classes = %#v", backend.addedClasses)
	}
	if got := backend.savedClasses["personal_test"]; got != "// Scripted by Zodiac" {
		t.Fatalf("saved class body = %q", got)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "// Scripted by Zodiac" {
		t.Fatalf("local class body = %q, err=%v", got, err)
	}
}

func TestRemoveUnlistedLocalFilesAppliesServerPriority(t *testing.T) {
	engine := NewEngine(&permissionBackendStub{}, "TestServer", nil)
	dir := t.TempDir()
	for _, kind := range []string{"weapons", "classes", "npcs"} {
		if err := os.MkdirAll(filepath.Join(dir, kind), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(dir, "classes", "kept.gs2")
	removeClass := filepath.Join(dir, "classes", "removed.gs2")
	removeWeapon := filepath.Join(dir, "weapons", "removed.gs2")
	removeNPC := filepath.Join(dir, "npcs", "removed.gs2")
	for _, path := range []string{keep, removeClass, removeWeapon, removeNPC} {
		if err := os.WriteFile(path, []byte("script"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	engine.removeUnlistedLocalFiles(dir, map[string]bool{"classes/kept.gs2": true})
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("kept file was removed: %v", err)
	}
	for _, path := range []string{removeClass, removeWeapon, removeNPC} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("server-missing file still exists: %s", path)
		}
	}
}

func TestWriteServerVersionDoesNotOverwriteLocalEditMadeDuringSync(t *testing.T) {
	engine := NewEngine(&permissionBackendStub{}, "TestServer", nil)
	path := filepath.Join(t.TempDir(), "classes", "Example.gs2")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("local edit"), 0o644); err != nil {
		t.Fatal(err)
	}

	ref := scriptRef{kind: "class", key: "Example", name: "Example"}
	engine.writeServerVersion(ref, path, "server version", HashScript("server version"), stringPtr("old local version"))
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "local edit" {
		t.Fatalf("local edit was overwritten: %q", got)
	}
}

func stringPtr(value string) *string { return &value }
