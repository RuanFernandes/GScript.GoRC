package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"graal-rc/internal/fileutil"
)

const (
	defaultChangeRetentionDays = 0
	maxChangeRetentionDays     = 3650
)

// ChangeRetentionSettings controls how long local change history remains on
// this computer. A value of zero means that the category is kept indefinitely.
type ChangeRetentionSettings struct {
	AuditDays  int `json:"auditDays"`
	BackupDays int `json:"backupDays"`
}

type changeRetentionStore struct {
	mu   sync.Mutex
	path string
}

func newChangeRetentionStore(path string) (*changeRetentionStore, error) {
	if filepath.Clean(path) == "." || path == "" {
		return nil, errors.New("retention settings path is required")
	}
	return &changeRetentionStore{path: path}, nil
}

func defaultChangeRetention() ChangeRetentionSettings {
	return ChangeRetentionSettings{
		AuditDays:  defaultChangeRetentionDays,
		BackupDays: defaultChangeRetentionDays,
	}
}

func validateChangeRetention(settings ChangeRetentionSettings) error {
	if settings.AuditDays < 0 || settings.AuditDays > maxChangeRetentionDays {
		return fmt.Errorf("audit retention must be between 0 and %d days", maxChangeRetentionDays)
	}
	if settings.BackupDays < 0 || settings.BackupDays > maxChangeRetentionDays {
		return fmt.Errorf("backup retention must be between 0 and %d days", maxChangeRetentionDays)
	}
	return nil
}

func (s *changeRetentionStore) Load() (ChangeRetentionSettings, error) {
	if s == nil {
		return defaultChangeRetention(), errors.New("retention settings are unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, ok, err := fileutil.ReadAndRecover(s.path, 0o600, func(data []byte) error {
		var settings ChangeRetentionSettings
		if err := json.Unmarshal(data, &settings); err != nil {
			return err
		}
		return validateChangeRetention(settings)
	})
	if err != nil {
		return defaultChangeRetention(), fmt.Errorf("read retention settings: %w", err)
	}
	if !ok {
		return defaultChangeRetention(), nil
	}
	var settings ChangeRetentionSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return defaultChangeRetention(), fmt.Errorf("decode retention settings: %w", err)
	}
	if err := validateChangeRetention(settings); err != nil {
		return defaultChangeRetention(), err
	}
	return settings, nil
}

func (s *changeRetentionStore) Save(settings ChangeRetentionSettings) error {
	if s == nil {
		return errors.New("retention settings are unavailable")
	}
	if err := validateChangeRetention(settings); err != nil {
		return err
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode retention settings: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fileutil.AtomicWriteFile(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("save retention settings: %w", err)
	}
	return nil
}

func (a *App) cleanupExpiredChangeData(settings ChangeRetentionSettings) error {
	if err := validateChangeRetention(settings); err != nil {
		return err
	}
	cutoff := time.Now()
	var cleanupErrs []error
	if settings.AuditDays > 0 && a != nil && a.audit != nil {
		_, err := a.audit.PruneOlderThan(cutoff.Add(-retentionDuration(settings.AuditDays)))
		if err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("prune audit history: %w", err))
		}
	}
	if settings.BackupDays > 0 && a != nil && a.backups != nil {
		if _, err := a.backups.PruneOlderThan(cutoff.Add(-retentionDuration(settings.BackupDays))); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("prune deployment backups: %w", err))
		}
	}
	return errors.Join(cleanupErrs...)
}

func (a *App) GetChangeRetention() (ChangeRetentionSettings, error) {
	if a == nil || a.retention == nil {
		return defaultChangeRetention(), nil
	}
	return a.retention.Load()
}

func (a *App) SetChangeRetention(settings ChangeRetentionSettings) error {
	if a == nil || a.retention == nil {
		return errors.New("retention settings are unavailable")
	}
	if err := validateChangeRetention(settings); err != nil {
		return err
	}
	if err := a.retention.Save(settings); err != nil {
		return err
	}
	cleanupErr := a.cleanupExpiredChangeData(settings)
	a.emitChangeHistoryChanged()
	return cleanupErr
}

func (a *App) emitChangeHistoryChanged() {
	if a != nil && a.app != nil {
		a.app.Event.Emit("rc:auditChanged")
	}
}

func retentionDuration(days int) time.Duration {
	return time.Duration(days) * 24 * time.Hour
}
