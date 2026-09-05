package main

import (
	"errors"
	"strings"
	"testing"

	"graal-rc/internal/connection"
	"graal-rc/internal/drafts"
)

func TestEditorDraftBindingsPersistAfterDisconnectAndRejectUploads(t *testing.T) {
	store, err := drafts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire(drafts.Identity{
		Listserver: "login.test:14900", Endpoint: "server.test:14900", Account: "staff", Server: "world", Kind: "class", Resource: "test", Epoch: 42,
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &App{draftStore: store, sessions: &connection.Service{}}
	want := drafts.Record{Revision: "last-typing", Original: "remote", Content: "unsaved", UpdatedAt: 100}
	if err := a.WriteEditorDraft(lease.Token, 1, want); err != nil {
		t.Fatal(err)
	}
	for name, operation := range map[string]func() error{
		"save":     func() error { return a.SaveEditorDraft(lease.Token, "wrong session upload") },
		"load":     func() error { _, err := a.GetEditorDraftContent(lease.Token); return err },
		"conflict": func() error { return a.ResolveEditorDraftConflict(lease.Token, "local", "") },
		"dirty":    func() error { return a.SetEditorDraftDirty(lease.Token, false) },
		"close":    func() error { return a.CloseEditorDraft(lease.Token) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := operation(); err == nil || !strings.Contains(err.Error(), "disconnected") {
				t.Fatalf("got %v", err)
			}
		})
	}
	got, err := a.GetEditorDraft(lease.Token)
	if err != nil || got == nil || *got != want {
		t.Fatalf("lost disconnected draft: %+v %v", got, err)
	}
	if err := a.SaveEditorDraft("unknown", "upload"); !errors.Is(err, drafts.ErrStaleLease) {
		t.Fatalf("unknown token: %v", err)
	}
}

func TestReopenedEditorKeepsDiskIdentityAcrossEpochs(t *testing.T) {
	store, err := drafts.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	identity := drafts.Identity{Listserver: "login:14900", Endpoint: "world:14900", Account: "staff", Server: "world", Kind: "textfile", Resource: "folder/config.txt", Epoch: 1}
	old, err := store.Acquire(identity)
	if err != nil {
		t.Fatal(err)
	}
	want := drafts.Record{Revision: "old", Original: "remote", Content: "draft", UpdatedAt: 100}
	if err := store.Write(old.Token, 1, want); err != nil {
		t.Fatal(err)
	}
	identity.Epoch = 2
	current, err := store.Acquire(identity)
	if err != nil {
		t.Fatal(err)
	}
	if current.Key != old.Key {
		t.Fatal("epoch contaminated persistent identity")
	}
	got, err := store.Load(current.Token)
	if err != nil || got == nil || *got != want {
		t.Fatalf("draft did not survive reconnect %+v %v", got, err)
	}
	metadata, err := store.Identity(current.Token)
	if err != nil || metadata.Epoch != 2 {
		t.Fatalf("lease lost current epoch %+v %v", metadata, err)
	}
	if _, err := store.Identity(old.Token); !errors.Is(err, drafts.ErrStaleLease) {
		t.Fatalf("old window still valid: %v", err)
	}
}
