package gextto

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestLastCycleAtResumesSchedule(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "series.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.db.Close()

	if _, ok := db.LastCycleAt(); ok {
		t.Fatal("fresh database should report no cycle")
	}

	started := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	stats := &models.CycleStats{LastStartedAt: &started, Scraped: 1}
	if err := db.SaveCycle(stats); err != nil {
		t.Fatalf("save cycle: %v", err)
	}
	got, ok := db.LastCycleAt()
	if !ok {
		t.Fatal("expected a persisted cycle")
	}
	if delta := got.Sub(started); delta > 2*time.Second || delta < -2*time.Second {
		t.Fatalf("last cycle = %v, want ~%v", got, started)
	}
}
