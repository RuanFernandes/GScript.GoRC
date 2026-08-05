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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"graal-rc/internal/connection"
	"graal-rc/internal/fileutil"
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
	mu   sync.Mutex
}

var credentialOperationLocks sync.Map

func credentialOperationLock(path string) *sync.Mutex {
	key, err := filepath.Abs(path)
	if err != nil {
		key = path
	}
	candidate := &sync.Mutex{}
	actual, _ := credentialOperationLocks.LoadOrStore(key, candidate)
	return actual.(*sync.Mutex)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	operationLock := credentialOperationLock(s.path)
	operationLock.Lock()
	defer operationLock.Unlock()
	return fileutil.AtomicWriteFileWithValidator(s.path, data, 0o600, func(current []byte) error {
		_, err := decodeCredentials(current)
		return err
	})
}

// Load reads the stored credentials. ok is false when no file exists.
func (s *Store) Load() (c Credentials, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operationLock := credentialOperationLock(s.path)
	operationLock.Lock()
	defer operationLock.Unlock()
	data, ok, err := fileutil.ReadAndRecover(s.path, 0o600, func(data []byte) error {
		_, err := decodeCredentials(data)
		return err
	})
	if err != nil || !ok {
		return Credentials{}, ok, err
	}
	c, err = decodeCredentials(data)
	return c, true, err
}

// Clear removes the stored credentials and its recovery backup, if any.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	operationLock := credentialOperationLock(s.path)
	operationLock.Lock()
	defer operationLock.Unlock()
	var errs []error
	for _, path := range []string{s.path, s.path + ".bak"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func decodeCredentials(data []byte) (Credentials, error) {
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, fmt.Errorf("decode credentials JSON: %w", err)
	}
	return c, nil
}

// FromConnection adapts a connection.Credentials value for storage.
func FromConnection(c connection.Credentials) Credentials {
	return Credentials{Nickname: c.Nickname, Account: c.Account, Password: c.Password}
}
