package audit

import (
	"os"
	"testing"
	"time"
)

func TestStoreRecordsAndListsNewestFirst(t *testing.T) {
	store, err := New(t.TempDir() + "\\audit.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(Entry{ID: "one", Action: "save", Resource: "script", Target: "weapons/a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(Entry{ID: "two", Action: "save", Resource: "script", Target: "weapons/b"}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != "two" || entries[1].ID != "one" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestStoreSupportsClear(t *testing.T) {
	store, err := New(t.TempDir() + "\\audit.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Record(Entry{ID: "ok", Action: "delete", Resource: "file", Target: "x"}); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(1)
	if err != nil || len(entries) != 1 {
		t.Fatalf("list before clear: %v %#v", err, entries)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	entries, err = store.List(10)
	if err != nil || len(entries) != 0 {
		t.Fatalf("list after clear: %v %#v", err, entries)
	}
}

func TestStorePrunesOnlyOlderEntries(t *testing.T) {
	store, err := New(t.TempDir() + "\\audit.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	oldTimestamp := time.Now().Add(-48 * time.Hour).UnixMilli()
	newTimestamp := time.Now().Add(-2 * time.Hour).UnixMilli()
	if err := store.Record(Entry{ID: "old", Timestamp: oldTimestamp, Action: "save", Resource: "script", Target: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Record(Entry{ID: "new", Timestamp: newTimestamp, Action: "save", Resource: "script", Target: "new"}); err != nil {
		t.Fatal(err)
	}

	removed, err := store.PruneOlderThan(time.Now().Add(-24 * time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("prune: %v removed=%d", err, removed)
	}
	entries, err := store.List(10)
	if err != nil || len(entries) != 1 || entries[0].ID != "new" {
		t.Fatalf("unexpected entries after prune: %v %#v", err, entries)
	}
	if _, err := os.Stat(store.path); err != nil {
		t.Fatalf("expected retained audit log: %v", err)
	}
}
