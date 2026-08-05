package credentials

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestVaultRecoversCorruptCiphertextFromBackup(t *testing.T) {
	vault := &Vault{path: filepath.Join(t.TempDir(), "accounts.dat")}
	first := []Account{{Account: "account-1", Password: "secret-1"}}
	second := []Account{{Account: "account-2", Password: "secret-2"}}
	if err := vault.Save(first); err != nil {
		t.Fatalf("save first accounts: %v", err)
	}
	if err := vault.Save(second); err != nil {
		t.Fatalf("save second accounts: %v", err)
	}
	if err := os.WriteFile(vault.path, []byte("corrupt ciphertext"), 0o600); err != nil {
		t.Fatalf("corrupt primary vault: %v", err)
	}

	got, err := vault.Load()
	if err != nil {
		t.Fatalf("load recovered accounts: %v", err)
	}
	if len(got) != 1 || got[0] != first[0] {
		t.Fatalf("recovered accounts = %+v, want %+v", got, first)
	}
}

func TestVaultConcurrentAddsAreSerialized(t *testing.T) {
	vault := &Vault{path: filepath.Join(t.TempDir(), "accounts.dat")}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := vault.Add(Account{Account: "account-" + string(rune('a'+i)), Password: "password"}); err != nil {
				t.Errorf("concurrent add %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	accounts, err := vault.Load()
	if err != nil {
		t.Fatalf("load accounts after concurrent adds: %v", err)
	}
	if len(accounts) != 16 {
		t.Fatalf("account count = %d, want 16", len(accounts))
	}
}
