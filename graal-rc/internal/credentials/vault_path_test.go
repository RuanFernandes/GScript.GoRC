package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

// Accounts saved by the official app are logins for other listservers. This app
// keeps its own folder, so it neither sends those passwords to Alpaxis nor
// rewrites that app's vault.
func TestNewVaultStaysOutOfTheOfficialAppsFolder(t *testing.T) {
	config := t.TempDir()
	t.Setenv("AppData", config)         // Windows
	t.Setenv("XDG_CONFIG_HOME", config) // Linux
	t.Setenv("HOME", config)            // macOS

	vault, err := NewVault()
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(filepath.Dir(vault.path)); got != "alpaxis-rc" {
		t.Fatalf("vault folder = %q, want alpaxis-rc", got)
	}

	officialDir := filepath.Join(filepath.Dir(filepath.Dir(vault.path)), "graal-rc")
	if err := os.MkdirAll(officialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	official := &Vault{path: filepath.Join(officialDir, "accounts.dat")}
	if err := official.Save([]Account{{Account: "old", Password: "secret"}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(official.path)
	if err != nil {
		t.Fatal(err)
	}

	accounts, err := vault.Load()
	if err != nil || len(accounts) != 0 {
		t.Fatalf("Load() = %+v, %v, want no accounts", accounts, err)
	}
	if err := vault.Add(Account{Account: "new", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(official.path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("the official vault changed (err %v)", err)
	}
}
