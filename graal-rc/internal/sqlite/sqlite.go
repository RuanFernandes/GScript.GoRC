// Package sqlite is a thin, CGO-free wrapper over modernc.org/sqlite used by the
// File Browser's .db explorer. Each remote .db is downloaded to a local cache
// file (see app.go filecache); this package opens that file in place via a
// cached *sql.DB so the explorer, inline edits, and the SQL console all share
// one connection. modernc writes edits through to the file on commit, so "save"
// is just re-reading the local file (header-patched in app.go) and re-uploading.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver
)

// Table is one row of sqlite_master (type='table').
type Table struct {
	Name   string `json:"name"`
	Schema string `json:"schema"` // CREATE statement text (may be empty)
}

// Result is a query result: either columns+rows (SELECT) or RowsAffected (DML).
type Result struct {
	Columns     []string `json:"columns"`
	Rows        [][]any  `json:"rows"`
	RowsAffected int64   `json:"rowsAffected"`
}

var (
	mu  sync.Mutex
	dbs = map[string]*sql.DB{} // localPath -> open connection
)

// dsn builds a SQLite file: URI for a local path. modernc needs forward slashes
// and a proper URI on Windows; url.URL handles drive letters + space encoding.
func dsn(localPath string) string {
	u := &url.URL{Scheme: "file", Path: "/" + strings.ReplaceAll(localPath, "\\", "/")}
	return u.String() + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

// Open returns the cached connection for localPath, opening it if needed.
func Open(localPath string) (*sql.DB, error) {
	mu.Lock()
	defer mu.Unlock()
	if db, ok := dbs[localPath]; ok {
		return db, nil
	}
	db, err := sql.Open("sqlite", dsn(localPath))
	if err != nil {
		return nil, err
	}
	// Single writer connection keeps the in-place file consistent.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	dbs[localPath] = db
	return db, nil
}

// Close drops the cached connection for localPath (called when the explorer
// window closes).
func Close(localPath string) {
	mu.Lock()
	db, ok := dbs[localPath]
	if ok {
		delete(dbs, localPath)
	}
	mu.Unlock()
	if db != nil {
		// WAL checkpoint so edits are flushed to the main file before re-upload.
		_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		db.Close()
	}
}

// quoteIdent wraps a table/column identifier in double quotes (basic injection
// guard for generated SQL).
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// Tables lists user tables (sqlite_master type='table') with their CREATE text.
func Tables(db *sql.DB) ([]Table, error) {
	rows, err := db.Query(`SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Table
	for rows.Next() {
		var t Table
		if err := rows.Scan(&t.Name, &t.Schema); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// returnsRows decides whether a statement yields a result set by its leading
// keyword (pragmatic dispatch for an explorer; avoids driver-dependent errors).
func returnsRows(query string) bool {
	q := strings.TrimSpace(query)
	if i := strings.IndexByte(q, ' '); i >= 0 {
		q = q[:i]
	}
	switch strings.ToUpper(strings.TrimPrefix(q, "(")) {
	case "SELECT", "WITH", "PRAGMA", "EXPLAIN", "VALUES", "TABLE":
		return true
	}
	return false
}

// Query runs sql (+args) and returns a result set (SELECT) or rowsAffected (DML).
func Query(db *sql.DB, query string, args []any) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if !returnsRows(query) {
		res, err := db.ExecContext(ctx, query, args...)
		if err != nil {
			return Result{}, err
		}
		aff, _ := res.RowsAffected()
		return Result{RowsAffected: aff}, nil
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return Result{}, err
	}
	res := Result{Columns: cols}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return Result{}, err
		}
		row := make([]any, len(cols))
		for i, v := range raw {
			row[i] = normalize(v)
		}
		res.Rows = append(res.Rows, row)
	}
	return res, rows.Err()
}

// normalize makes a scanned cell JSON-friendly: []byte → base64 string (BLOBs),
// time.Time → RFC3339 string; ints/floats/strings/nil pass through.
func normalize(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		return "base64:" + base64.StdEncoding.EncodeToString(x)
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return x
	}
}

// UpdateCell runs `UPDATE "table" SET "col" = ? WHERE rowid = ?`.
func UpdateCell(db *sql.DB, table, column string, rowid int64, value any) error {
	q := fmt.Sprintf("UPDATE %s SET %s = ?", quoteIdent(table), quoteIdent(column))
	_, err := db.Exec(q, value, rowid)
	return err
}

// InsertRow inserts a default-values row into table and returns its rowid.
func InsertRow(db *sql.DB, table string) (int64, error) {
	q := fmt.Sprintf("INSERT INTO %s DEFAULT VALUES", quoteIdent(table))
	res, err := db.Exec(q)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteRow deletes the row with the given rowid from table.
func DeleteRow(db *sql.DB, table string, rowid int64) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", quoteIdent(table))
	_, err := db.Exec(q, rowid)
	return err
}
