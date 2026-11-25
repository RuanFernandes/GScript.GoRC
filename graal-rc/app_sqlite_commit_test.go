package main

import (
	"errors"
	"path/filepath"
	"testing"

	"graal-rc/internal/sqlite"
)

// A failed upload keeps the staged changes in the window, so the local copy must
// not already contain them or the retry inserts every row twice.
func TestCommitSqliteStagedRetryDoesNotDuplicate(t *testing.T) {
	local := filepath.Join(t.TempDir(), "data.db")
	db, err := sqlite.Open(local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlite.Close(local) })
	if _, err := db.Exec("CREATE TABLE items (name TEXT)"); err != nil {
		t.Fatal(err)
	}
	changes := sqlite.Changes{Table: "items", Inserts: []map[string]any{{"name": "sword"}}}
	count := func() int {
		db, err := sqlite.Open(local)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM items").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	failed := errors.New("upload failed")
	if err := commitSqliteStaged(local, changes, nil, func([]byte) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("error = %v, want the upload failure", err)
	}
	if n := count(); n != 0 {
		t.Fatalf("local copy has %d rows after a failed upload, want 0", n)
	}
	var uploaded []byte
	if err := commitSqliteStaged(local, changes, nil, func(content []byte) error { uploaded = content; return nil }); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 || len(uploaded) == 0 {
		t.Fatalf("rows = %d, uploaded = %d bytes, want 1 row and the new file", n, len(uploaded))
	}
}
