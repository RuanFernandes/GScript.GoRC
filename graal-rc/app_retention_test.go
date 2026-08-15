package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	auditlib "graal-rc/internal/audit"
	"graal-rc/internal/connection"
	deploylib "graal-rc/internal/deploy"
)

func TestChangeRetentionStoreRoundTripsAndValidates(t *testing.T) {
	store, err := newChangeRetentionStore(filepath.Join(t.TempDir(), "retention.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := ChangeRetentionSettings{AuditDays: 30, BackupDays: 90, BackupCount: 5}
	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil || loaded != settings {
		t.Fatalf("load settings: %v %#v", err, loaded)
	}
	if err := store.Save(ChangeRetentionSettings{AuditDays: -1}); err == nil {
		t.Fatal("expected negative retention to be rejected")
	}
	if err := store.Save(ChangeRetentionSettings{BackupDays: maxChangeRetentionDays + 1}); err == nil {
		t.Fatal("expected excessive retention to be rejected")
	}
	if err := store.Save(ChangeRetentionSettings{BackupCount: maxBackupCount + 1}); err == nil {
		t.Fatal("expected excessive backup count to be rejected")
	}
	legacyStore, err := newChangeRetentionStore(filepath.Join(t.TempDir(), "legacy-retention.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyStore.path, []byte(`{"auditDays":0,"backupDays":0}`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy, err := legacyStore.Load()
	if err != nil || legacy.BackupCount != defaultBackupCount {
		t.Fatalf("default backup count = %d, err=%v; want %d", legacy.BackupCount, err, defaultBackupCount)
	}
}

func TestSetChangeRetentionPrunesBothStores(t *testing.T) {
	auditStore, err := auditlib.New(filepath.Join(t.TempDir(), "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	backupStore, err := deploylib.New(filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	retentionStore, err := newChangeRetentionStore(filepath.Join(t.TempDir(), "retention.json"))
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	if err := auditStore.Record(auditlib.Entry{ID: "old-audit", Timestamp: old, Action: "save", Resource: "file", Target: "old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := backupStore.Save(deploylib.Backup{ID: "old-backup", Timestamp: old, Resource: "file", Target: "old"}, []byte("old")); err != nil {
		t.Fatal(err)
	}

	app := &App{audit: auditStore, backups: backupStore, retention: retentionStore}
	if err := app.SetChangeRetention(ChangeRetentionSettings{AuditDays: 1, BackupDays: 1, BackupCount: defaultBackupCount}); err != nil {
		t.Fatal(err)
	}
	entries, err := auditStore.List(10)
	if err != nil || len(entries) != 0 {
		t.Fatalf("audit after retention: %v %#v", err, entries)
	}
	backups, err := backupStore.List(10)
	if err != nil || len(backups) != 0 {
		t.Fatalf("backups after retention: %v %#v", err, backups)
	}
}

func TestSetChangeRetentionPrunesExistingBackupsByTarget(t *testing.T) {
	backupStore, err := deploylib.New(filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 4; i++ {
		if _, err := backupStore.Save(deploylib.Backup{
			ID:        fmt.Sprintf("file-%d", i),
			Timestamp: i,
			Server:    "Zodiac",
			Resource:  "file",
			Target:    "levels/main.nw",
		}, []byte("version")); err != nil {
			t.Fatal(err)
		}
	}
	retentionStore, err := newChangeRetentionStore(filepath.Join(t.TempDir(), "retention.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{backups: backupStore, retention: retentionStore}
	if err := app.SetChangeRetention(ChangeRetentionSettings{BackupCount: 2}); err != nil {
		t.Fatal(err)
	}
	backups, err := backupStore.List(100)
	if err != nil || len(backups) != 2 {
		t.Fatalf("backups after count prune: %v %#v", err, backups)
	}
	for _, id := range []string{"file-1", "file-2"} {
		if _, _, err := backupStore.Read(id); err == nil {
			t.Fatalf("old backup %q was not pruned", id)
		}
	}
}

func TestSaveDeploymentBackupUsesConfiguredLimitForEveryResource(t *testing.T) {
	backupStore, err := deploylib.New(filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	retentionStore, err := newChangeRetentionStore(filepath.Join(t.TempDir(), "retention.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := retentionStore.Save(ChangeRetentionSettings{BackupCount: 2}); err != nil {
		t.Fatal(err)
	}
	app := &App{sessions: connection.NewService(), backups: backupStore, retention: retentionStore}
	for _, resource := range []string{"script", "file", "sqlite", "servertext"} {
		for i := 0; i < 3; i++ {
			if _, ok, err := app.saveDeploymentBackup(resource, resource+"-target", []byte{byte(i)}, true); err != nil || !ok {
				t.Fatalf("save %s #%d: ok=%v err=%v", resource, i, ok, err)
			}
		}
	}
	backups, err := backupStore.List(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 8 {
		t.Fatalf("backup count = %d, want 8 (2 per resource)", len(backups))
	}
}
