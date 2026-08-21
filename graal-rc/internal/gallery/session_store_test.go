package gallery

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionStoreIsolatedByAccountAndPersistsEncryptedSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gallery-sessions.dat")
	store := NewSessionStoreAt(path)
	ruan := StoredSession{Token: "ruan-token", Subject: "ruan-community", Username: "Ruan", DisplayName: "Ruan", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	repinho := StoredSession{Token: "repinho-token", Subject: "repinho-community", Username: "Repinho", DisplayName: "Repinho", ExpiresAt: time.Now().Add(time.Hour).Unix()}
	if err := store.Save("Ruan", ruan); err != nil {
		t.Fatalf("save Ruan session: %v", err)
	}
	if err := store.Save("Repinho", repinho); err != nil {
		t.Fatalf("save Repinho session: %v", err)
	}

	if got, ok, err := store.Load("ruan"); err != nil || !ok || got.Token != ruan.Token {
		t.Fatalf("load Ruan session = (%+v, %v, %v)", got, ok, err)
	}
	if got, ok, err := NewSessionStoreAt(path).Load("REPinho"); err != nil || !ok || got.Token != repinho.Token {
		t.Fatalf("load Repinho session = (%+v, %v, %v)", got, ok, err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read encrypted store: %v", err)
	}
	if string(raw) == "" || string(raw) == ruan.Token || string(raw) == repinho.Token {
		t.Fatal("gallery sessions were written as plaintext")
	}

	if err := store.Delete("RUAN"); err != nil {
		t.Fatalf("delete Ruan session: %v", err)
	}
	if _, ok, err := store.Load("ruan"); err != nil || ok {
		t.Fatalf("Ruan session after delete = (ok=%v, err=%v)", ok, err)
	}
	if _, ok, err := store.Load("repinho"); err != nil || !ok {
		t.Fatalf("Repinho session was affected by Ruan delete = (ok=%v, err=%v)", ok, err)
	}
}

func TestSessionStoreDropsExpiredSessions(t *testing.T) {
	store := NewSessionStoreAt(filepath.Join(t.TempDir(), "gallery-sessions.dat"))
	if err := store.Save("Ruan", StoredSession{Token: "expired-token", Subject: "ruan", ExpiresAt: time.Now().Add(-time.Minute).Unix()}); err != nil {
		t.Fatalf("save expired session: %v", err)
	}
	if _, ok, err := store.Load("Ruan"); err != nil || ok {
		t.Fatalf("expired session = (ok=%v, err=%v)", ok, err)
	}
}
