// Package audit persists a small, append-only record of administrative actions.
// It is intentionally local to the desktop profile: the remote server remains
// the authority for permissions and server-side audit history.
package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultLimit = 200

type Entry struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"timestamp"`
	Server    string `json:"server,omitempty"`
	Account   string `json:"account,omitempty"`
	Action    string `json:"action"`
	Resource  string `json:"resource"`
	Target    string `json:"target"`
	Outcome   string `json:"outcome"`
	Detail    string `json:"detail,omitempty"`
}

type Store struct {
	mu   sync.Mutex
	path string
}

func New(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("audit path is required")
	}
	return &Store{path: path}, nil
}

func (s *Store) Record(entry Entry) error {
	if s == nil {
		return errors.New("audit store is unavailable")
	}
	if strings.TrimSpace(entry.Action) == "" || strings.TrimSpace(entry.Resource) == "" {
		return errors.New("audit action and resource are required")
	}
	if entry.Timestamp == 0 {
		entry.Timestamp = time.Now().UnixMilli()
	}
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%d-%d", entry.Timestamp, time.Now().UnixNano()%1_000_000)
	}

	b, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode audit entry: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create audit directory: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("write audit log: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync audit log: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close audit log: %w", err)
	}
	return nil
}

func (s *Store) List(limit int) ([]Entry, error) {
	if s == nil {
		return nil, errors.New("audit store is unavailable")
	}
	if limit <= 0 || limit > 1000 {
		limit = defaultLimit
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	entries := make([]Entry, 0, limit)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var entry Entry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.ID == "" {
			continue
		}
		entries = append(entries, entry)
		if len(entries) > limit {
			copy(entries, entries[len(entries)-limit:])
			entries = entries[:limit]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read audit log: %w", err)
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	return entries, nil
}

func (s *Store) Clear() error {
	if s == nil {
		return errors.New("audit store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("clear audit log: %w", err)
	}
	return nil
}
