package deploy

import (
	"os"
	"testing"
	"time"
)

func TestStoreRoundTripsAndValidatesChecksum(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.Save(Backup{ID: "backup-1", Resource: "script", Target: "weapon:sword"}, []byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	if meta.Size != 3 || meta.SHA256 == "" {
		t.Fatalf("invalid metadata: %#v", meta)
	}
	readMeta, content, err := store.Read("backup-1")
	if err != nil || readMeta.ID != meta.ID || string(content) != "old" {
		t.Fatalf("round trip failed: %v %#v %q", err, readMeta, content)
	}
}

func TestStoreRejectsTraversalIDs(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Read("..\\outside"); err == nil {
		t.Fatal("expected traversal id to be rejected")
	}
}

func TestStoreDeletesBackupFiles(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(Backup{ID: "delete-me", Resource: "file", Target: "x"}, []byte("old")); err != nil {
		t.Fatal(err)
	}
	meta, err := store.Delete("delete-me")
	if err != nil || meta.ID != "delete-me" {
		t.Fatalf("delete: %v %#v", err, meta)
	}
	if _, _, err := store.Read("delete-me"); err == nil {
		t.Fatal("expected deleted backup to be unreadable")
	}
	for _, name := range []string{"delete-me.data", "delete-me.meta.json"} {
		if _, err := os.Stat(root + "\\" + name); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, got %v", name, err)
		}
	}
}

func TestStorePrunesOlderBackups(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(Backup{ID: "old", Timestamp: time.Now().Add(-48 * time.Hour).UnixMilli(), Resource: "file", Target: "old"}, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(Backup{ID: "new", Timestamp: time.Now().Add(-2 * time.Hour).UnixMilli(), Resource: "file", Target: "new"}, []byte("new")); err != nil {
		t.Fatal(err)
	}

	removed, err := store.PruneOlderThan(time.Now().Add(-24 * time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("prune: %v removed=%d", err, removed)
	}
	if _, _, err := store.Read("old"); err == nil {
		t.Fatal("expected old backup to be pruned")
	}
	if _, content, err := store.Read("new"); err != nil || string(content) != "new" {
		t.Fatalf("expected new backup to remain: %v %q", err, content)
	}
}
