package main

import (
	"fmt"
	"os"

	"graal-rc/internal/sqlite"
)

// commitSqliteStaged applies changes to a scratch copy and adopts it as the
// local cache only after upload succeeds. patch, when set, edits the bytes
// before they are uploaded.
func commitSqliteStaged(local string, changes sqlite.Changes, patch func([]byte), upload func([]byte) error) error {
	db, err := sqlite.Open(local)
	if err != nil {
		return err
	}
	// Flush the WAL so the main file alone is a complete copy.
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	original, err := os.ReadFile(local)
	if err != nil {
		return err
	}
	staging := local + ".commit"
	removeStaging := func() {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			_ = os.Remove(staging + suffix)
		}
	}
	removeStaging()
	defer removeStaging()
	if err := os.WriteFile(staging, original, 0o600); err != nil {
		return fmt.Errorf("prepare SQLite commit: %w", err)
	}
	staged, err := sqlite.Open(staging)
	if err != nil {
		return err
	}
	defer sqlite.Close(staging)
	if err := sqlite.Commit(staged, changes); err != nil {
		return err
	}
	// Closing checkpoints the changes into the staging file.
	sqlite.Close(staging)
	content, err := os.ReadFile(staging)
	if err != nil {
		return err
	}
	if patch != nil {
		patch(content)
	}
	if err := upload(content); err != nil {
		return err
	}
	sqlite.Close(local)
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(local + suffix)
	}
	if err := os.WriteFile(local, content, 0o600); err != nil {
		return fmt.Errorf("the database was uploaded, but the local copy could not be refreshed; reopen it: %w", err)
	}
	return nil
}
