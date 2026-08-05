package credentials

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestStoreRecoversCorruptPrimaryFromBackup(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), "credentials.json")}
	first := Credentials{Nickname: "first", Account: "account-1", Password: "secret-1"}
	second := Credentials{Nickname: "second", Account: "account-2", Password: "secret-2"}
	if err := store.Save(first); err != nil {
		t.Fatalf("save first credentials: %v", err)
	}
	if err := store.Save(second); err != nil {
		t.Fatalf("save second credentials: %v", err)
	}
	if err := os.WriteFile(store.path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatalf("corrupt primary credentials: %v", err)
	}

	got, ok, err := store.Load()
	if err != nil {
		t.Fatalf("load recovered credentials: %v", err)
	}
	if !ok || got != first {
		t.Fatalf("recovered credentials = (%+v, %v), want (%+v, true)", got, ok, first)
	}
	if _, err := os.Stat(store.path + ".bak"); err != nil {
		t.Fatalf("backup was not retained: %v", err)
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("clear credentials: %v", err)
	}
	for _, path := range []string{store.path, store.path + ".bak"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after clear: %v", path, err)
		}
	}
}

func TestStoreConcurrentOperationsRemainValid(t *testing.T) {
	store := &Store{path: filepath.Join(t.TempDir(), "credentials.json")}
	if err := store.Save(Credentials{Account: "initial", Password: "initial"}); err != nil {
		t.Fatalf("save initial credentials: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Save(Credentials{Account: "account", Password: "password", Nickname: string(rune('a' + i))}); err != nil {
				t.Errorf("concurrent save %d: %v", i, err)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := store.Load(); err != nil {
				t.Errorf("concurrent load %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if _, ok, err := store.Load(); err != nil || !ok {
		t.Fatalf("final credentials load = (ok=%v, err=%v)", ok, err)
	}
}
