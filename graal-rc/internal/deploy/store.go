// Package deploy stores recoverable local snapshots taken before remote writes.
// Backup IDs are opaque filenames and never contain a user-provided path.
package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Backup struct {
	ID        string `json:"id"`
	Timestamp int64  `json:"timestamp"`
	Server    string `json:"server,omitempty"`
	Account   string `json:"account,omitempty"`
	Resource  string `json:"resource"`
	Target    string `json:"target"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

type Store struct {
	mu   sync.Mutex
	root string
}

func New(root string) (*Store, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("backup directory is required")
	}
	return &Store{root: root}, nil
}

func (s *Store) Save(meta Backup, content []byte) (Backup, error) {
	if s == nil {
		return Backup{}, errors.New("backup store is unavailable")
	}
	if strings.TrimSpace(meta.Resource) == "" || strings.TrimSpace(meta.Target) == "" {
		return Backup{}, errors.New("backup resource and target are required")
	}
	if meta.Timestamp == 0 {
		meta.Timestamp = time.Now().UnixMilli()
	}
	if meta.ID == "" {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", meta.Timestamp, meta.Resource, meta.Target)))
		meta.ID = fmt.Sprintf("%d-%x", meta.Timestamp, digest[:4])
	}
	meta.Size = int64(len(content))
	digest := sha256.Sum256(content)
	meta.SHA256 = hex.EncodeToString(digest[:])

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return Backup{}, fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.root, meta.ID+".data"), content, 0o600); err != nil {
		return Backup{}, fmt.Errorf("write backup content: %w", err)
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return Backup{}, fmt.Errorf("encode backup metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.root, meta.ID+".meta.json"), b, 0o600); err != nil {
		_ = os.Remove(filepath.Join(s.root, meta.ID+".data"))
		return Backup{}, fmt.Errorf("write backup metadata: %w", err)
	}
	return meta, nil
}

func (s *Store) List(limit int) ([]Backup, error) {
	if s == nil {
		return nil, errors.New("backup store is unavailable")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	files, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list backups: %w", err)
	}
	backups := make([]Backup, 0, len(files))
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".meta.json") {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(s.root, file.Name()))
		if readErr != nil {
			continue
		}
		var backup Backup
		if json.Unmarshal(b, &backup) != nil || !validID(backup.ID) {
			continue
		}
		backups = append(backups, backup)
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].Timestamp > backups[j].Timestamp })
	if len(backups) > limit {
		backups = backups[:limit]
	}
	return backups, nil
}

func (s *Store) Read(id string) (Backup, []byte, error) {
	if s == nil {
		return Backup{}, nil, errors.New("backup store is unavailable")
	}
	if !validID(id) {
		return Backup{}, nil, errors.New("invalid backup id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	metaBytes, err := os.ReadFile(filepath.Join(s.root, id+".meta.json"))
	if err != nil {
		return Backup{}, nil, fmt.Errorf("read backup metadata: %w", err)
	}
	var meta Backup
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return Backup{}, nil, fmt.Errorf("decode backup metadata: %w", err)
	}
	content, err := os.ReadFile(filepath.Join(s.root, id+".data"))
	if err != nil {
		return Backup{}, nil, fmt.Errorf("read backup content: %w", err)
	}
	digest := sha256.Sum256(content)
	if meta.SHA256 != hex.EncodeToString(digest[:]) {
		return Backup{}, nil, errors.New("backup checksum mismatch")
	}
	return meta, content, nil
}

func validID(id string) bool {
	return id != "" && filepath.Base(id) == id && !strings.Contains(id, "..") && !strings.ContainsAny(id, `/\\`)
}
