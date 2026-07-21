// Package credentials also owns the encrypted account vault: the multi-account
// store written as a DPAPI-encrypted JSON blob (accounts.dat). Passwords live
// here only; the frontend receives AccountSummary values without them.
package credentials

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Account is a full persisted login (nickname/account/password).
type Account struct {
	Nickname string `json:"nickname"`
	Account  string `json:"account"`
	Password string `json:"password"`
}

// AccountSummary is the password-less projection exposed to the frontend.
type AccountSummary struct {
	Nickname string `json:"nickname"`
	Account  string `json:"account"`
}

// Summary drops the password for safe hand-off to the frontend.
func (a Account) Summary() AccountSummary {
	return AccountSummary{Nickname: a.Nickname, Account: a.Account}
}

// Vault reads/writes the DPAPI-encrypted account list under the OS config dir.
type Vault struct {
	path string
}

// NewVault resolves <UserConfigDir>/graal-rc/accounts.dat.
func NewVault() (*Vault, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dir, "graal-rc")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Vault{path: filepath.Join(root, "accounts.dat")}, nil
}

// Path returns the on-disk file location (useful for debugging).
func (v *Vault) Path() string { return v.path }

// Load reads and decrypts the account list. A missing file is reported as an
// empty list (not an error) so a fresh install behaves like an empty vault.
func (v *Vault) Load() ([]Account, error) {
	data, err := os.ReadFile(v.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := unprotect(data)
	if err != nil {
		return nil, err
	}
	var accounts []Account
	if err := json.Unmarshal(plain, &accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

// Save encrypts and writes the full account list, replacing any previous file.
func (v *Vault) Save(accounts []Account) error {
	plain, err := json.MarshalIndent(accounts, "", "  ")
	if err != nil {
		return err
	}
	cipher, err := protect(plain)
	if err != nil {
		return err
	}
	return os.WriteFile(v.path, cipher, 0o600)
}

// Add inserts or replaces (matched by Account name) and persists.
func (v *Vault) Add(a Account) error {
	if a.Account == "" {
		return errors.New("account name is required")
	}
	accounts, err := v.Load()
	if err != nil {
		return err
	}
	replaced := false
	for i := range accounts {
		if accounts[i].Account == a.Account {
			accounts[i] = a
			replaced = true
			break
		}
	}
	if !replaced {
		accounts = append(accounts, a)
	}
	return v.Save(accounts)
}

// Remove deletes the account with the given name and persists. Removing a name
// that does not exist is not an error.
func (v *Vault) Remove(accountName string) error {
	accounts, err := v.Load()
	if err != nil {
		return err
	}
	next := accounts[:0]
	for _, a := range accounts {
		if a.Account != accountName {
			next = append(next, a)
		}
	}
	return v.Save(next)
}
