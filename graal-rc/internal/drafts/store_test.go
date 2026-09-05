package drafts

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testIdentity() Identity {
	return Identity{Listserver: "login.test:14900", Endpoint: "server.test:14900", Account: "staff", Server: "world", Kind: "class", Resource: "../outside"}
}

func testRecord(revision, content string) Record {
	return Record{Revision: revision, Original: "remote", Content: content, UpdatedAt: 1000}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func acquire(t *testing.T, store *Store, identity Identity) Lease {
	t.Helper()
	lease, err := store.Acquire(identity)
	if err != nil {
		t.Fatal(err)
	}
	return lease
}

func TestPersistenceAndIdentityIsolation(t *testing.T) {
	store := newTestStore(t)
	identity := testIdentity()
	lease := acquire(t, store, identity)
	want := testRecord("revision-1", "unsaved\ntext")
	if err := store.Write(lease.Token, 1, want); err != nil {
		t.Fatal(err)
	}
	if len(lease.Key) != 64 || strings.Contains(lease.Key, ".") {
		t.Fatalf("unsafe key %q", lease.Key)
	}
	reopened, err := New(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	next := acquire(t, reopened, identity)
	got, err := reopened.Load(next.Token)
	if err != nil || got == nil || *got != want {
		t.Fatalf("recovered %+v, %v", got, err)
	}
	for _, mutate := range []func(*Identity){
		func(i *Identity) { i.Account = "other" }, func(i *Identity) { i.Endpoint = "other:14900" },
		func(i *Identity) { i.Listserver = "other:14900" }, func(i *Identity) { i.Server = "other" },
		func(i *Identity) { i.Kind = "weapon" }, func(i *Identity) { i.Resource = "other" },
	} {
		other := identity
		mutate(&other)
		isolated := acquire(t, reopened, other)
		got, err := reopened.Load(isolated.Token)
		if err != nil || got != nil {
			t.Fatalf("identity leaked %+v: %+v %v", other, got, err)
		}
	}
}

func TestReopenedResourceRejectsOldWindowAndOutOfOrderWrites(t *testing.T) {
	store := newTestStore(t)
	old := acquire(t, store, testIdentity())
	current := acquire(t, store, testIdentity())
	if err := store.Write(old.Token, 3, testRecord("old", "stale")); !errors.Is(err, ErrStaleLease) {
		t.Fatalf("got %v", err)
	}
	if err := store.Write(current.Token, 2, testRecord("new", "latest")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(current.Token, 1, testRecord("late", "obsolete")); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(current.Token)
	if err != nil || got.Content != "latest" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestClearOnlyAcknowledgedRevisionAndRetainsTombstone(t *testing.T) {
	store := newTestStore(t)
	lease := acquire(t, store, testIdentity())
	if err := store.Write(lease.Token, 1, testRecord("sent", "sent")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(lease.Token, 2, testRecord("typing", "new typing")); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.Clear(lease.Token, "sent"); err != nil || ok {
		t.Fatalf("cleared newer revision: %v %v", ok, err)
	}
	if ok, err := store.Clear(lease.Token, "typing"); err != nil || !ok {
		t.Fatalf("clear: %v %v", ok, err)
	}
	got, err := store.Load(lease.Token)
	if err != nil || !got.Cleared || got.Revision != "typing" || got.Content != "" || got.Original != "" {
		t.Fatalf("tombstone %+v %v", got, err)
	}
	if err := store.Write(lease.Token, 1, testRecord("sent", "late packet")); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load(lease.Token)
	if err != nil || !got.Cleared {
		t.Fatalf("late write resurrected draft %+v %v", got, err)
	}
}

func TestConcurrentSaveAcknowledgementPreservesNewRevision(t *testing.T) {
	store := newTestStore(t)
	lease := acquire(t, store, testIdentity())
	if err := store.Write(lease.Token, 1, testRecord("saved", "snapshot")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := store.Clear(lease.Token, "saved"); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := store.Write(lease.Token, 2, testRecord("new", "new edit")); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	got, err := store.Load(lease.Token)
	if err != nil || got.Cleared || got.Revision != "new" {
		t.Fatalf("lost concurrent edit %+v %v", got, err)
	}
}

func TestInvalidOrCorruptDraftDoesNotReplaceGoodData(t *testing.T) {
	store := newTestStore(t)
	lease := acquire(t, store, testIdentity())
	if err := store.Write(lease.Token, 1, testRecord("good", "kept")); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(lease.Token, 2, testRecord("large", strings.Repeat("x", MaxTextBytes+1))); err == nil {
		t.Fatal("accepted oversized draft")
	}
	got, err := store.Load(lease.Token)
	if err != nil || got.Content != "kept" {
		t.Fatalf("lost valid draft %+v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(store.dir, lease.Key+".json"), []byte("truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(testIdentity()); err == nil {
		t.Fatal("silently ignored corrupt draft")
	}
}
