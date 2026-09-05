// Package drafts persists unsaved editor revisions independently of RC sessions.
package drafts

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	MaxTextBytes   = 4 << 20
	maxRecordBytes = 2*MaxTextBytes + 4096
	maxStoreBytes  = 128 << 20
	maxRecords     = 4096
)

var ErrStaleLease = errors.New("editor draft belongs to a superseded editor")

type Identity struct {
	Listserver string
	Endpoint   string
	Account    string
	Server     string
	Kind       string
	Resource   string
	Epoch      uint64 `json:"-"`
}

type Record struct {
	Revision  string `json:"revision"`
	Original  string `json:"original"`
	Content   string `json:"content"`
	UpdatedAt int64  `json:"updatedAt"`
	Cleared   bool   `json:"cleared"`
}

type Lease struct {
	Token string `json:"token"`
	Key   string `json:"key"`
}

type leaseState struct {
	key      string
	sequence uint64
	identity Identity
}

// Store serializes writes, compares revisions on clear, and fences a window
// when the same resource is reopened. Tokens never resolve through the current
// connection, so a delayed write cannot leak into a different account/server.
type Store struct {
	mu     sync.Mutex
	dir    string
	leases map[string]*leaseState
	active map[string]string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, leases: make(map[string]*leaseState), active: make(map[string]string)}, nil
}

func (s *Store) Acquire(identity Identity) (Lease, error) {
	for _, value := range []string{identity.Listserver, identity.Endpoint, identity.Account, identity.Server, identity.Kind, identity.Resource} {
		if strings.TrimSpace(value) == "" || len(value) > 4096 || !utf8.ValidString(value) {
			return Lease{}, errors.New("invalid editor draft identity")
		}
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return Lease{}, err
	}
	sum := sha256.Sum256(encoded)
	key := hex.EncodeToString(sum[:])
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Lease{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.read(key); err != nil {
		return Lease{}, err
	}
	if old := s.active[key]; old != "" {
		delete(s.leases, old)
	}
	if len(s.leases) >= maxRecords {
		return Lease{}, errors.New("too many editor draft sessions; restart RC")
	}
	s.active[key] = token
	s.leases[token] = &leaseState{key: key, identity: identity}
	return Lease{Token: token, Key: key}, nil
}

func (s *Store) Identity(token string) (Identity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[token]
	if !ok {
		return Identity{}, ErrStaleLease
	}
	return lease.identity, nil
}

func (s *Store) Load(token string) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[token]
	if !ok {
		return nil, ErrStaleLease
	}
	return s.read(lease.key)
}

func (s *Store) Write(token string, sequence uint64, record Record) error {
	if err := validate(record); err != nil {
		return err
	}
	if record.Cleared {
		return errors.New("draft writes cannot clear a revision")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[token]
	if !ok {
		return ErrStaleLease
	}
	if sequence == 0 {
		return errors.New("draft sequence must be positive")
	}
	if sequence <= lease.sequence {
		return nil
	}
	if err := s.persist(lease.key, record); err != nil {
		return err
	}
	lease.sequence = sequence
	return nil
}

// Clear retains a small tombstone for the acknowledged revision so a browser
// fallback surviving a crash after this write cannot resurrect discarded work.
func (s *Store) Clear(token, revision string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease, ok := s.leases[token]
	if !ok {
		return false, ErrStaleLease
	}
	record, err := s.read(lease.key)
	if err != nil {
		return false, err
	}
	if record == nil || record.Revision != revision {
		return false, nil
	}
	if record.Cleared {
		return true, nil
	}
	record.Original = ""
	record.Content = ""
	record.Cleared = true
	return true, s.persist(lease.key, *record)
}

func validate(record Record) error {
	if len(record.Revision) < 1 || len(record.Revision) > 128 || record.UpdatedAt <= 0 {
		return errors.New("invalid draft revision")
	}
	if len(record.Original) > MaxTextBytes || len(record.Content) > MaxTextBytes {
		return fmt.Errorf("draft text exceeds %d MiB", MaxTextBytes>>20)
	}
	if !utf8.ValidString(record.Original) || !utf8.ValidString(record.Content) {
		return errors.New("draft text is not UTF-8")
	}
	return nil
}

func (s *Store) read(key string) (*Record, error) {
	f, err := os.Open(filepath.Join(s.dir, key+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRecordBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRecordBytes {
		return nil, errors.New("editor draft file is too large")
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("read editor draft: %w", err)
	}
	if err := validate(record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) persist(key string, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(data) > maxRecordBytes {
		return errors.New("encoded editor draft is too large")
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	var total int64
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == key+".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
	}
	if total+int64(len(data)) > maxStoreBytes {
		return errors.New("editor draft storage is full; save or discard existing drafts")
	}
	tmp, err := os.CreateTemp(s.dir, ".draft-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceFile(tmpPath, filepath.Join(s.dir, key+".json"))
}
