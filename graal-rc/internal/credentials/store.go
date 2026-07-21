// Package credentials persists the last successful login (nickname/account/
// password) so the login screen can be pre-filled on the next launch.
//
// NOTE: the password is stored in plaintext, as the reference C++ client did
// (its "Don't save password" option was opt-out). This trades convenience for
// security and is fine for a local staff tool, but keep the file's directory
// private.
package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"

	"graal-rc/internal/connection"
)

// Credentials is the persisted payload (mirrors connection.Credentials).
type Credentials struct {
	Nickname string `json:"nickname"`
	Account  string `json:"account"`
	Password string `json:"password"`
}

// Store reads/writes the credentials file under the OS config directory.
type Store struct {
	path string
}

// NewStore resolves <UserConfigDir>/graal-rc/credentials.json.
func NewStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dir, "graal-rc")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(root, "credentials.json")}, nil
}

// Path returns the on-disk file location (useful for debugging).
func (s *Store) Path() string { return s.path }

// Save writes the credentials, replacing any previous file.
func (s *Store) Save(c Credentials) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Load reads the stored credentials. ok is false when no file exists.
func (s *Store) Load() (c Credentials, ok bool, err error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return Credentials{}, false, nil
	}
	if err != nil {
		return Credentials{}, false, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, false, err
	}
	return c, true, nil
}

// Clear removes the stored credentials, if any.
func (s *Store) Clear() error {
	err := os.Remove(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// FromConnection adapts a connection.Credentials value for storage.
func FromConnection(c connection.Credentials) Credentials {
	return Credentials{Nickname: c.Nickname, Account: c.Account, Password: c.Password}
}
