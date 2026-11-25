package gextto

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// sqliteCacheSizeKiB is the per-connection page cache; a negative value is in
// KiB (SQLite convention). 2 MiB is SQLite's classic default: measured on the
// 510 MB release archive, a larger cache (up to 16 MiB) does not speed up the
// FTS searches or the listing (the OS page cache serves the file anyway) — it
// only adds resident memory, ~14 MB per connection from 2 to 8 MiB.
const sqliteCacheSizeKiB = "-2000"

// sqliteSynchronous is the durability of every Gextto database: FULL syncs
// the WAL at each commit, so a power cut never loses a committed change (an
// archived file whose row would be gone). It lives in the connection string,
// so a connection the pool reopens after an error has it too: set only by a
// statement after opening, a reopened connection fell back to the string's
// value without anyone noticing.
const sqliteSynchronous = "FULL"

// sqliteDSN builds a SQLite connection string without treating characters in
// the database path as URI query parameters. Each pragma is a separate
// _pragma value, as required by modernc.org/sqlite.
func sqliteDSN(path string, readOnly bool, pragmas ...string) string {
	return sqliteDSNExtra(path, readOnly, nil, pragmas...)
}

// sqliteDSNExtra is sqliteDSN with extra connection parameters, such as the
// _txlock that makes every transaction take the write lock at BEGIN.
func sqliteDSNExtra(path string, readOnly bool, extra url.Values, pragmas ...string) string {
	dsn := &url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := url.Values{}
	if readOnly {
		query.Set("mode", "ro")
	}
	for key, values := range extra {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	for _, pragma := range pragmas {
		query.Add("_pragma", pragma)
	}
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

// sqliteTxImmediate is the _txlock value that turns every transaction of a
// connection into BEGIN IMMEDIATE.
var sqliteTxImmediate = url.Values{"_txlock": {"immediate"}}

// OpenSQLite opens a SQLite database with conservative pragmas
// (`busy_timeout`, WAL, foreign keys, synchronous FULL). A single connection
// is kept so concurrent goroutines never race on write locks.
func OpenSQLite(path string) (*sql.DB, error) {
	return openSQLite(path, nil)
}

// OpenSQLiteWritable is OpenSQLite for a database that two connections may
// write at the same time. Its transactions take the write lock at BEGIN, so a
// save that loses the race waits for the busy timeout: SQLite does not consult
// it when a deferred transaction that has already read tries to upgrade to a
// write after another connection committed, and answers SQLITE_BUSY_SNAPSHOT at
// once instead.
func OpenSQLiteWritable(path string) (*sql.DB, error) {
	return openSQLite(path, sqliteTxImmediate)
}

func openSQLite(path string, extra url.Values) (*sql.DB, error) {
	dsn := sqliteDSNExtra(path, false, extra,
		"busy_timeout(10000)",
		"foreign_keys(1)",
		"synchronous("+sqliteSynchronous+")",
		"cache_size("+sqliteCacheSizeKiB+")",
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// modernc.org/sqlite is serialized per connection; one connection avoids
	// SQLITE_BUSY under the daemon's mixed read/write workload.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("wal %s: %w", path, err)
	}
	return db, nil
}

// sqlExec runs a statement and returns the error.
func sqlExec(db *sql.DB, query string, args ...any) error {
	_, err := db.Exec(query, args...)
	return err
}

// tableColumns returns the set of column names of a table.
func tableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

// ensureColumn adds a column when it is missing (mirrors the ALTER TABLE
// migrations scattered through database.rs).
func ensureColumn(db *sql.DB, table, column, definition string) error {
	columns, err := tableColumns(db, table)
	if err != nil {
		return err
	}
	if columns[column] {
		return nil
	}
	return sqlExec(db, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
}
