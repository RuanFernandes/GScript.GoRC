package deploy

import "testing"

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
