package audit

import "testing"

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
