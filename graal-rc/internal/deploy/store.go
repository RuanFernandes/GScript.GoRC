// Package deploy stores recoverable local snapshots taken before remote writes
// or Local Sync overwrites.
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

// BackupDiff contains the snapshot and the current remote version for the
// restore review screen. Binary or oversized content is represented by the
// metadata fields only and leaves the text payloads empty.
type BackupDiff struct {
	Backup         Backup `json:"backup"`
	BackupContent  string `json:"backupContent,omitempty"`
	CurrentContent string `json:"currentContent,omitempty"`
	Language       string `json:"language"`
	Diffable       bool   `json:"diffable"`
	DiffReason     string `json:"diffReason,omitempty"`
	CurrentExists  bool   `json:"currentExists"`
	CurrentSize    int64  `json:"currentSize"`
	CurrentSHA256  string `json:"currentSha256,omitempty"`
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(meta, content)
}

// SaveLimited saves a backup and keeps at most limit backups for the same
// server, resource and target. The newest snapshots are retained and older
// snapshots are removed while the store lock is held, so concurrent saves
// cannot exceed the requested limit.
func (s *Store) SaveLimited(meta Backup, content []byte, limit int) (Backup, error) {
	if s == nil {
		return Backup{}, errors.New("backup store is unavailable")
	}
	if limit < 1 {
		return Backup{}, errors.New("backup limit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	backup, err := s.saveLocked(meta, content)
	if err != nil {
		return Backup{}, err
	}
	if err := s.pruneTargetLocked(backup, limit); err != nil {
		return backup, fmt.Errorf("prune backups for %s:%s: %w", backup.Resource, backup.Target, err)
	}
	return backup, nil
}

func (s *Store) saveLocked(meta Backup, content []byte) (Backup, error) {
	if strings.TrimSpace(meta.Resource) == "" || strings.TrimSpace(meta.Target) == "" {
		return Backup{}, errors.New("backup resource and target are required")
	}
	if meta.Timestamp == 0 {
		meta.Timestamp = time.Now().UnixMilli()
	}
	if meta.ID == "" {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s:%s", meta.Timestamp, time.Now().UnixNano(), meta.Resource, meta.Target)))
		meta.ID = fmt.Sprintf("%d-%x", meta.Timestamp, digest[:4])
	}
	meta.Size = int64(len(content))
	digest := sha256.Sum256(content)
	meta.SHA256 = hex.EncodeToString(digest[:])

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

func (s *Store) pruneTargetLocked(target Backup, limit int) error {
	backups, err := s.listMetadataLocked()
	if err != nil {
		return err
	}

	matching := make([]Backup, 0, limit+1)
	for _, backup := range backups {
		if sameBackupTarget(backup, target) {
			matching = append(matching, backup)
		}
	}
	if len(matching) <= limit {
		return nil
	}
	sortBackupsNewestFirst(matching)
	var deleteErrs []error
	for _, backup := range matching[limit:] {
		if _, err := s.deleteLocked(backup.ID); err != nil {
			deleteErrs = append(deleteErrs, err)
		}
	}
	return errors.Join(deleteErrs...)
}

// PruneToLimit enforces a per-server, per-resource and per-target count across
// all existing backups. It is used when a user lowers the configured limit.
func (s *Store) PruneToLimit(limit int) (int, error) {
	if s == nil {
		return 0, errors.New("backup store is unavailable")
	}
	if limit < 1 {
		return 0, errors.New("backup limit must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	backups, err := s.listMetadataLocked()
	if err != nil {
		return 0, err
	}
	groups := make(map[backupTarget][]Backup)
	for _, backup := range backups {
		scope := backupTarget{Server: backup.Server, Resource: backup.Resource, Target: backup.Target}
		groups[scope] = append(groups[scope], backup)
	}

	removed := 0
	var pruneErrs []error
	for _, matching := range groups {
		if len(matching) <= limit {
			continue
		}
		sortBackupsNewestFirst(matching)
		for _, backup := range matching[limit:] {
			if _, err := s.deleteLocked(backup.ID); err != nil {
				pruneErrs = append(pruneErrs, fmt.Errorf("delete backup %q: %w", backup.ID, err))
				continue
			}
			removed++
		}
	}
	return removed, errors.Join(pruneErrs...)
}

type backupTarget struct {
	Server   string
	Resource string
	Target   string
}

func sameBackupTarget(left, right Backup) bool {
	return left.Server == right.Server && left.Resource == right.Resource && left.Target == right.Target
}

func sortBackupsNewestFirst(backups []Backup) {
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].Timestamp != backups[j].Timestamp {
			return backups[i].Timestamp > backups[j].Timestamp
		}
		return backups[i].ID > backups[j].ID
	})
}

func (s *Store) listMetadataLocked() ([]Backup, error) {
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
		id := strings.TrimSuffix(file.Name(), ".meta.json")
		if !validID(id) {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(s.root, file.Name()))
		if readErr != nil {
			continue
		}
		var backup Backup
		if json.Unmarshal(b, &backup) != nil || backup.ID != id || !validID(backup.ID) ||
			strings.TrimSpace(backup.Resource) == "" || strings.TrimSpace(backup.Target) == "" {
			continue
		}
		backups = append(backups, backup)
	}
	return backups, nil
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
	backups, err := s.listMetadataLocked()
	if err != nil {
		return nil, err
	}
	sortBackupsNewestFirst(backups)
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

// Delete removes one local backup and returns its metadata. The identifier is
// validated before it is used to construct either filesystem path.
func (s *Store) Delete(id string) (Backup, error) {
	if s == nil {
		return Backup{}, errors.New("backup store is unavailable")
	}
	if !validID(id) {
		return Backup{}, errors.New("invalid backup id")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteLocked(id)
}

// PruneOlderThan removes backups whose metadata timestamp is before cutoff.
// Invalid metadata is skipped so automatic cleanup never deletes a file based
// on an untrusted or malformed record.
func (s *Store) PruneOlderThan(cutoff time.Time) (int, error) {
	if s == nil {
		return 0, errors.New("backup store is unavailable")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	files, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("list backups for cleanup: %w", err)
	}

	removed := 0
	var cleanupErrs []error
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".meta.json") {
			continue
		}
		id := strings.TrimSuffix(file.Name(), ".meta.json")
		if !validID(id) {
			continue
		}
		metaBytes, readErr := os.ReadFile(filepath.Join(s.root, file.Name()))
		if readErr != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("read backup metadata %q: %w", id, readErr))
			continue
		}
		var meta Backup
		if json.Unmarshal(metaBytes, &meta) != nil || !validID(meta.ID) || meta.ID != id {
			continue
		}
		if meta.Timestamp <= 0 || !time.UnixMilli(meta.Timestamp).Before(cutoff) {
			continue
		}
		if _, deleteErr := s.deleteLocked(id); deleteErr != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("delete backup %q: %w", id, deleteErr))
			continue
		}
		removed++
	}
	return removed, errors.Join(cleanupErrs...)
}

func (s *Store) deleteLocked(id string) (Backup, error) {
	metaPath := filepath.Join(s.root, id+".meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return Backup{}, fmt.Errorf("read backup metadata: %w", err)
	}
	var meta Backup
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return Backup{}, fmt.Errorf("decode backup metadata: %w", err)
	}
	if !validID(meta.ID) || meta.ID != id {
		return Backup{}, errors.New("backup metadata id mismatch")
	}

	var deleteErrs []error
	for _, path := range []string{filepath.Join(s.root, id+".data"), metaPath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			deleteErrs = append(deleteErrs, fmt.Errorf("remove %q: %w", filepath.Base(path), err))
		}
	}
	if err := errors.Join(deleteErrs...); err != nil {
		return meta, err
	}
	return meta, nil
}

func validID(id string) bool {
	return id != "" && filepath.Base(id) == id && !strings.Contains(id, "..") && !strings.ContainsAny(id, `/\\`)
}
