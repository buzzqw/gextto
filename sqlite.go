package gextto

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// OpenSQLite opens a SQLite database with the same conservative pragmas the
// daemon used through rusqlite (`busy_timeout`, WAL, foreign keys).
// A single connection is kept so concurrent goroutines never race on write
// locks, matching the single `Mutex<Connection>` of the original.
func OpenSQLite(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)"
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
