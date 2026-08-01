// Package credentials also owns the encrypted account vault: the multi-account
// store written as a DPAPI-encrypted JSON blob (accounts.dat). Passwords live
// here only; the frontend receives AccountSummary values without them.
package credentials

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Account is a full persisted login (profile name/nickname/account/password). DisplayName and
// Photo are client-only profile data — they are never sent to the server; only
// Account/Password are. The listserver endpoint is selected from the
// ProfileName, while the nickname is supplied per session.
type Account struct {
	ProfileName string `json:"profileName,omitempty"`
	Account     string `json:"account"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName,omitempty"`
	Photo       string `json:"photo,omitempty"`
}

// AccountSummary is the password-less projection exposed to the frontend.
type AccountSummary struct {
	ProfileName string `json:"profileName"`
	Account     string `json:"account"`
	DisplayName string `json:"displayName"`
	Photo       string `json:"photo"`
}

// Summary drops the password for safe hand-off to the frontend.
func (a Account) Summary() AccountSummary {
	return AccountSummary{
		ProfileName: a.ProfileName,
		Account:     a.Account,
		DisplayName: a.DisplayName,
		Photo:       a.Photo,
	}
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
	var stored []struct {
		ProfileName string `json:"profileName,omitempty"`
		Nickname    string `json:"nickname,omitempty"`
		Account     string `json:"account"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName,omitempty"`
		Photo       string `json:"photo,omitempty"`
	}
	if err := json.Unmarshal(plain, &stored); err != nil {
		return nil, err
	}
	accounts := make([]Account, len(stored))
	migrated := false
	for i, old := range stored {
		accounts[i] = Account{
			ProfileName: old.ProfileName,
			Account:     old.Account,
			Password:    old.Password,
			DisplayName: old.DisplayName,
			Photo:       old.Photo,
		}
		if accounts[i].ProfileName == "" && strings.HasPrefix(old.Nickname, "Preagonal:") {
			accounts[i].ProfileName = "Preagonal:"
			migrated = true
		}
	}
	if migrated {
		if err := v.Save(accounts); err != nil {
			return nil, err
		}
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

// Mutate applies fn to the account matched by name and persists. Returns
// ErrAccountNotFound if no account matches.
func (v *Vault) Mutate(accountName string, fn func(*Account)) error {
	accounts, err := v.Load()
	if err != nil {
		return err
	}
	for i := range accounts {
		if accounts[i].Account == accountName {
			fn(&accounts[i])
			return v.Save(accounts)
		}
	}
	return ErrAccountNotFound
}

// ErrAccountNotFound is returned by Mutate/Get when no account matches.
var ErrAccountNotFound = errors.New("account not found")

// Get returns the account matched by name (ErrAccountNotFound if none).
func (v *Vault) Get(accountName string) (Account, error) {
	accounts, err := v.Load()
	if err != nil {
		return Account{}, err
	}
	for _, a := range accounts {
		if a.Account == accountName {
			return a, nil
		}
	}
	return Account{}, ErrAccountNotFound
}
