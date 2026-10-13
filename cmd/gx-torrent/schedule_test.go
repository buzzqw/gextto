package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/settings"
)

func clockAt(hour, minute int) time.Time {
	return time.Date(2026, 1, 1, hour, minute, 0, 0, time.Local)
}

func TestScheduledSpeedLimits(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	s := d.opts.Settings

	if _, _, active := d.scheduledSpeedLimits(clockAt(10, 0)); active {
		t.Fatal("a disabled schedule must never be active")
	}

	s.Set(scheduleEnabledKey, "true")
	s.Set(scheduleStartKey, "08:00")
	s.Set(scheduleEndKey, "22:00")
	s.Set(scheduleDLKey, "5000")
	s.Set(scheduleULKey, "500")

	if dl, ul, active := d.scheduledSpeedLimits(clockAt(10, 0)); !active || dl != 5000 || ul != 500 {
		t.Fatalf("inside window: dl=%d ul=%d active=%v", dl, ul, active)
	}
	if _, _, active := d.scheduledSpeedLimits(clockAt(22, 30)); active {
		t.Fatal("outside window must be inactive")
	}

	// A window wrapping midnight.
	s.Set(scheduleStartKey, "22:00")
	s.Set(scheduleEndKey, "06:00")
	if _, _, active := d.scheduledSpeedLimits(clockAt(23, 0)); !active {
		t.Fatal("23:00 must be inside the wrapping window")
	}
	if _, _, active := d.scheduledSpeedLimits(clockAt(3, 0)); !active {
		t.Fatal("03:00 must be inside the wrapping window")
	}
	if _, _, active := d.scheduledSpeedLimits(clockAt(12, 0)); active {
		t.Fatal("12:00 must be outside the wrapping window")
	}

	// Invalid clock values disable the schedule rather than misfire.
	s.Set(scheduleEndKey, "bad")
	if _, _, active := d.scheduledSpeedLimits(clockAt(23, 0)); active {
		t.Fatal("an invalid schedule must be inactive")
	}
}

// TestScheduledSpeedLimitsDays checks the weekday restriction: an empty list
// means every day, a list restricts the window to those days.
func TestScheduledSpeedLimitsDays(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	s := d.opts.Settings
	s.Set(scheduleEnabledKey, "true")
	s.Set(scheduleStartKey, "08:00")
	s.Set(scheduleEndKey, "22:00")
	s.Set(scheduleDLKey, "1000")
	s.Set(scheduleULKey, "100")

	// clockAt builds a Thursday (2026-01-01).
	thursday := clockAt(10, 0)
	if thursday.Weekday() != time.Thursday {
		t.Fatalf("test clock is not a Thursday: %v", thursday.Weekday())
	}
	if _, _, active := d.scheduledSpeedLimits(thursday); !active {
		t.Fatal("an empty day list must apply every day")
	}
	s.Set(scheduleDaysKey, "1,2,3") // Mon-Wed, not Thursday
	if _, _, active := d.scheduledSpeedLimits(thursday); active {
		t.Fatal("a day outside the selected list must be inactive")
	}
	s.Set(scheduleDaysKey, "4,5") // Thursday and Friday
	if _, _, active := d.scheduledSpeedLimits(thursday); !active {
		t.Fatal("a selected day must be active")
	}
}

func TestScheduleDaysForm(t *testing.T) {
	all := [7]bool{true, true, true, true, true, true, true}
	if got := scheduleDaysSet(""); got != all {
		t.Fatalf("empty storage must mean every day: %v", got)
	}
	if got := scheduleDaysSet("0,4"); !got[0] || !got[4] || got[1] {
		t.Fatalf("parsed days wrong: %v", got)
	}
	if got := formatScheduleDays(all); got != "" {
		t.Fatalf("all days must collapse to empty, got %q", got)
	}
	if got := formatScheduleDays(parseScheduleDayForm([]string{"4", "0", "4", "x", "9"})); got != "0,4" {
		t.Fatalf("formatted days = %q, want 0,4", got)
	}
	if n := scheduleDaysSelected(parseScheduleDayForm([]string{"1", "2"})); n != 2 {
		t.Fatalf("selected = %d, want 2", n)
	}
}

func TestApplyEffectiveSpeedLimitsSchedule(t *testing.T) {
	store, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	store.Set(scheduleEnabledKey, "true")
	store.Set(scheduleStartKey, "00:00")
	store.Set(scheduleEndKey, "23:59")
	store.Set(scheduleDLKey, "1234")
	store.Set(scheduleULKey, "99")
	d := newStandaloneDaemon(t, store)

	d.mu.Lock()
	d.applyEffectiveSpeedLimitsLocked(clockAt(12, 0))
	d.mu.Unlock()
	if d.appliedDL != 1234 || d.appliedUL != 99 {
		t.Fatalf("scheduled limits not applied: %d/%d", d.appliedDL, d.appliedUL)
	}

	// The same target is not pushed again (idempotent).
	d.mu.Lock()
	d.applyEffectiveSpeedLimitsLocked(clockAt(12, 30))
	d.mu.Unlock()
	if d.appliedDL != 1234 || d.appliedUL != 99 {
		t.Fatalf("limits changed unexpectedly: %d/%d", d.appliedDL, d.appliedUL)
	}
}
