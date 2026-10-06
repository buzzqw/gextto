package gextto

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestFormatScheduledCycleTime(t *testing.T) {
	location := time.FixedZone("local", 2*60*60)
	now := time.Date(2026, time.October, 4, 11, 40, 0, 0, location)

	if got := formatScheduledCycleTime(now, time.Date(2026, time.October, 4, 14, 52, 0, 0, location)); got != "at 14:52" {
		t.Fatalf("same-day schedule = %q, want at 14:52", got)
	}
	if got := formatScheduledCycleTime(now, time.Date(2026, time.October, 5, 2, 52, 0, 0, location)); got != "tomorrow at 02:52" {
		t.Fatalf("next-day schedule = %q, want tomorrow at 02:52", got)
	}
	if got := formatScheduledCycleTime(now, time.Date(2026, time.October, 6, 2, 52, 0, 0, location)); got != "on 06/10 at 02:52" {
		t.Fatalf("later schedule = %q, want on 06/10 at 02:52", got)
	}
}

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
