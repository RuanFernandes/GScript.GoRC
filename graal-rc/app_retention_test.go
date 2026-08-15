package main

import (
	"path/filepath"
	"testing"
	"time"

	auditlib "graal-rc/internal/audit"
	deploylib "graal-rc/internal/deploy"
)

func TestChangeRetentionStoreRoundTripsAndValidates(t *testing.T) {
	store, err := newChangeRetentionStore(filepath.Join(t.TempDir(), "retention.json"))
	if err != nil {
		t.Fatal(err)
	}
	settings := ChangeRetentionSettings{AuditDays: 30, BackupDays: 90}
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
	if err := app.SetChangeRetention(ChangeRetentionSettings{AuditDays: 1, BackupDays: 1}); err != nil {
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
