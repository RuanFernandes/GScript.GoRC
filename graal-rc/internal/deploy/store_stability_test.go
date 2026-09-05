package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreDoesNotOverwriteAnExistingBackupID(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previous, err := store.Save(Backup{ID: "fixed-id", Resource: "script", Target: "Example"}, []byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(Backup{ID: previous.ID, Resource: "script", Target: "Example"}, []byte("replacement")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("duplicate ID save error=%v, want file already exists", err)
	}
	meta, content, err := store.Read(previous.ID)
	if err != nil || meta != previous || string(content) != "original" {
		t.Fatalf("duplicate ID changed original backup: meta=%+v content=%q err=%v", meta, content, err)
	}
}

func TestStoreRejectsTraversalIDBeforeWriting(t *testing.T) {
	root := t.TempDir()
	store, err := New(filepath.Join(root, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(Backup{ID: "../outside", Resource: "script", Target: "Example"}, []byte("must not escape")); err == nil {
		t.Fatal("save accepted a path traversal ID")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("invalid ID created files: entries=%v err=%v", entries, err)
	}
}

func TestSaveLimitedWithSameTimestampRetainsLatestSnapshots(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Model a clock that cannot advance beyond its previous reading. The
	// ordering must remain monotonic independently of the random ID suffix.
	store.lastIDTime = 9_000_000_000_000_000_000
	var saved []Backup
	for i := 0; i < 3; i++ {
		backup, err := store.SaveLimited(Backup{Timestamp: 100, Resource: "script", Target: "Example"}, []byte{byte(i)}, 2)
		if err != nil {
			t.Fatal(err)
		}
		saved = append(saved, backup)
		if _, content, err := store.Read(backup.ID); err != nil || len(content) != 1 || content[0] != byte(i) {
			t.Fatalf("SaveLimited pruned the snapshot it just returned: index=%d content=%v err=%v", i, content, err)
		}
	}
	if _, _, err := store.Read(saved[0].ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest equal-timestamp backup remains: err=%v", err)
	}
	for i := 1; i < len(saved); i++ {
		if _, content, err := store.Read(saved[i].ID); err != nil || len(content) != 1 || content[0] != byte(i) {
			t.Fatalf("newer equal-timestamp backup was lost: index=%d content=%v err=%v", i, content, err)
		}
	}
	backups, err := store.List(100)
	if err != nil || len(backups) != 2 || backups[0].ID != saved[2].ID || backups[1].ID != saved[1].ID {
		t.Fatalf("equal-timestamp retention order is incorrect: backups=%+v err=%v", backups, err)
	}
}

func TestSaveLimitedRetainsConfiguredCountForGeneratedIDs(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]Backup)
	for _, resource := range []string{"script", "file", "sqlite", "servertext"} {
		for i := 0; i < 3; i++ {
			backup, err := store.SaveLimited(Backup{Resource: resource, Target: resource + "-target"}, []byte{byte(i)}, 2)
			if err != nil {
				t.Fatalf("save %s #%d: %v", resource, i, err)
			}
			if previous, duplicate := ids[backup.ID]; duplicate {
				t.Fatalf("generated backup ID collided: previous=%+v new=%+v", previous, backup)
			}
			ids[backup.ID] = backup
		}
	}
	backups, err := store.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 8 {
		t.Logf("saved IDs=%+v listed=%+v", ids, backups)
		entries, readErr := os.ReadDir(store.root)
		t.Logf("directory count=%d err=%v", len(entries), readErr)
		for _, entry := range entries {
			data, readErr := os.ReadFile(filepath.Join(store.root, entry.Name()))
			t.Logf("file=%q contents=%q err=%v", entry.Name(), data, readErr)
		}
		t.Fatalf("backup count=%d, want 8", len(backups))
	}
}
