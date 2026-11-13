package gextto

import (
	"path/filepath"
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
