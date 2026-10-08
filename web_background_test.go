package gextto

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCompletedArchivePresentAcceptsPersistedFullPayload(t *testing.T) {
	db, _, view, _, processed := seedTestSetup(t)
	path, present := bg_completedArchivePresent(db, view.Hash, view.TotalSize)
	if !present || path != processed {
		t.Fatalf("archive payload = %q, %v; want %q, true", path, present, processed)
	}
}

func TestCompletedArchivePresentRejectsInsufficientPayload(t *testing.T) {
	db, _, view, _, _ := seedTestSetup(t)
	if _, present := bg_completedArchivePresent(db, view.Hash, view.TotalSize+1); present {
		t.Fatal("undersized archive payload was accepted")
	}
}

func TestHousekeepingDoesNotRerunAfterARestart(t *testing.T) {
	now := time.Now()
	fresh := &Config{Settings: map[string]string{}}
	if got := housekeepingFirstDelay(fresh, now); got != 10*time.Minute {
		t.Fatalf("first run ever: delay %v, want 10m", got)
	}
	recent := &Config{Settings: map[string]string{"housekeeping_last_run": strconv.FormatInt(now.Add(-2*time.Hour).Unix(), 10)}}
	if got := housekeepingFirstDelay(recent, now); got < 21*time.Hour || got > 22*time.Hour {
		t.Fatalf("ran 2h ago with a 24h interval: delay %v, want about 22h", got)
	}
	old := &Config{Settings: map[string]string{"housekeeping_last_run": strconv.FormatInt(now.Add(-48*time.Hour).Unix(), 10)}}
	if got := housekeepingFirstDelay(old, now); got != 10*time.Minute {
		t.Fatalf("overdue: delay %v, want 10m", got)
	}
}

func TestCompactSkipsADatabaseWithoutFreeSpace(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE t(x BLOB)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		if _, err := db.Exec("INSERT INTO t VALUES (randomblob(4000))"); err != nil {
			t.Fatal(err)
		}
	}
	if connectionWorthCompacting(db) {
		t.Fatal("a database without free pages must not be vacuumed")
	}
	if _, err := db.Exec("DELETE FROM t WHERE rowid > 50"); err != nil {
		t.Fatal(err)
	}
	if !connectionWorthCompacting(db) {
		t.Fatal("a database with most pages free should be compacted")
	}
	if err := OptimizeConnection(db, "compact"); err != nil {
		t.Fatal(err)
	}
	if connectionWorthCompacting(db) {
		t.Fatal("compact did not vacuum")
	}
}
