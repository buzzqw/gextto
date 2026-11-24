package gextto

import (
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
)

// TestOpenSQLiteKeepsFullDurabilityOnReopenedConnections pins that every
// connection of a Gextto database syncs at each commit, also one the pool
// opens again later: the durability lives in the connection string, not in a
// statement run once after opening.
func TestOpenSQLiteKeepsFullDurabilityOnReopenedConnections(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// No idle connection is kept: every query below runs on a new one.
	db.SetMaxIdleConns(0)
	for i := 0; i < 3; i++ {
		var level int
		if err := db.QueryRow("PRAGMA synchronous").Scan(&level); err != nil {
			t.Fatal(err)
		}
		if level != 2 {
			t.Fatalf("connection %d has synchronous=%d, want 2 (FULL)", i, level)
		}
	}
}

func TestSQLiteDSNEscapesTheDatabasePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive ?#% ' ü.db")
	pragmas := []string{"busy_timeout(10000)", "query_only(1)"}
	parsed, err := url.Parse(sqliteDSN(path, true, pragmas...))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "file" || parsed.Path != filepath.ToSlash(path) {
		t.Fatalf("DSN path = %q, want the literal path %q", parsed.Path, path)
	}
	if got := parsed.Query().Get("mode"); got != "ro" {
		t.Fatalf("DSN mode = %q, want ro", got)
	}
	if got := parsed.Query()["_pragma"]; !reflect.DeepEqual(got, pragmas) {
		t.Fatalf("DSN pragmas = %v, want %v", got, pragmas)
	}
}
