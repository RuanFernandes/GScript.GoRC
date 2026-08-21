package gallery

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"graal-rc/internal/credentials"
	"graal-rc/internal/fileutil"
)

// StoredSession is the only gallery credential persisted by the RC. It is an
// opaque API session, never the gallery password, and is encrypted with the
// same OS/user-bound protection used by the account vault.
type StoredSession struct {
	Token       string `json:"token"`
	Subject     string `json:"subject"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	ExpiresAt   int64  `json:"expiresAt"`
}

type SessionStore struct {
	path string
	mu   sync.Mutex
}

type sessionStoreFile struct {
	Sessions map[string]StoredSession `json:"sessions"`
}

// NewSessionStore resolves the encrypted per-user gallery session file.
func NewSessionStore() (*SessionStore, error) {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base, err = os.UserHomeDir()
		if err != nil {
			return nil, err
		}
	}
	root := filepath.Join(base, "graal-rc")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return NewSessionStoreAt(filepath.Join(root, "gallery-sessions.dat")), nil
}

// NewSessionStoreAt is used by tests and keeps the storage implementation
// independent from a particular platform's user-config directory.
func NewSessionStoreAt(path string) *SessionStore { return &SessionStore{path: path} }

// Load returns the session associated with the RC account key. Expired
// sessions are treated as absent and removed from the encrypted file.
func (s *SessionStore) Load(accountKey string) (StoredSession, bool, error) {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return StoredSession{}, false, errors.New("gallery session store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUnlocked()
	if err != nil {
		return StoredSession{}, false, err
	}
	key := normalizeAccountKey(accountKey)
	session, ok := file.Sessions[key]
	if !ok || session.ExpiresAt <= time.Now().Unix() || strings.TrimSpace(session.Token) == "" {
		if ok {
			delete(file.Sessions, key)
			if err := s.saveUnlocked(file); err != nil {
				return StoredSession{}, false, err
			}
		}
		return StoredSession{}, false, nil
	}
	return session, true, nil
}

// Save replaces the session for one RC account without affecting sessions for
// other accounts.
func (s *SessionStore) Save(accountKey string, session StoredSession) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("gallery session store is unavailable")
	}
	if normalizeAccountKey(accountKey) == "" || strings.TrimSpace(session.Token) == "" || strings.TrimSpace(session.Subject) == "" {
		return errors.New("gallery session and account key are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	file.Sessions[normalizeAccountKey(accountKey)] = session
	return s.saveUnlocked(file)
}

// Delete removes the gallery session for one RC account, normally when the
// user explicitly signs out of the gallery.
func (s *SessionStore) Delete(accountKey string) error {
	if s == nil || strings.TrimSpace(s.path) == "" {
		return errors.New("gallery session store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	key := normalizeAccountKey(accountKey)
	if _, ok := file.Sessions[key]; !ok {
		return nil
	}
	delete(file.Sessions, key)
	return s.saveUnlocked(file)
}

func (s *SessionStore) loadUnlocked() (sessionStoreFile, error) {
	data, ok, err := fileutil.ReadAndRecover(s.path, 0o600, validateSessionFile)
	if err != nil {
		return sessionStoreFile{}, err
	}
	if !ok {
		return sessionStoreFile{Sessions: map[string]StoredSession{}}, nil
	}
	plain, err := credentials.Unprotect(data)
	if err != nil {
		return sessionStoreFile{}, err
	}
	file := sessionStoreFile{}
	if err := json.Unmarshal(plain, &file); err != nil {
		return sessionStoreFile{}, err
	}
	if file.Sessions == nil {
		file.Sessions = map[string]StoredSession{}
	}
	return file, nil
}

func (s *SessionStore) saveUnlocked(file sessionStoreFile) error {
	if file.Sessions == nil {
		file.Sessions = map[string]StoredSession{}
	}
	plain, err := json.Marshal(file)
	if err != nil {
		return err
	}
	ciphertext, err := credentials.Protect(plain)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileWithValidator(s.path, ciphertext, 0o600, validateSessionFile)
}

func validateSessionFile(data []byte) error {
	plain, err := credentials.Unprotect(data)
	if err != nil {
		return err
	}
	file := sessionStoreFile{}
	if err := json.Unmarshal(plain, &file); err != nil {
		return err
	}
	if file.Sessions == nil {
		return errors.New("gallery session file has no sessions map")
	}
	return nil
}

func normalizeAccountKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
