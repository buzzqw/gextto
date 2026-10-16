package bwschedule

import (
	"strings"
	"testing"
	"time"
)

// 2026-01-05 is a Monday.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 1, day, hour, minute, 0, 0, time.Local)
}

func TestWeekRoundTrip(t *testing.T) {
	var week Week
	week[time.Monday] = Day{On: true, Start: 8 * 60, End: 18 * 60, Download: 1000, Upload: 100}
	week[time.Friday] = Day{On: true, Start: 23 * 60, End: 7 * 60, Upload: 500}
	week[time.Sunday] = Day{Start: 1 * 60, End: 2 * 60}
	text := week.String()
	parsed, ok := Parse(text)
	if !ok || parsed != week {
		t.Fatalf("round trip of %q gave %+v (ok=%v)", text, parsed, ok)
	}
	if _, ok := Parse(""); ok {
		t.Fatal("an empty value must fall back")
	}
	if _, ok := Parse("xyz=01:00-02:00/0/0"); ok {
		t.Fatal("an unknown day must be refused")
	}
}

func TestWeekAt(t *testing.T) {
	var week Week
	week[time.Monday] = Day{On: true, Start: 8 * 60, End: 18 * 60, Download: 1000, Upload: 100}
	week[time.Friday] = Day{On: true, Start: 23 * 60, End: 7 * 60, Upload: 500}
	week[time.Saturday] = Day{On: true, Start: 6 * 60, End: 9 * 60, Download: 42}

	if dl, ul, ok := week.At(at(5, 10, 0)); !ok || dl != 1000 || ul != 100 {
		t.Fatalf("Monday 10:00: %d %d %v", dl, ul, ok)
	}
	if _, _, ok := week.At(at(5, 18, 0)); ok {
		t.Fatal("the end is exclusive")
	}
	if _, _, ok := week.At(at(6, 10, 0)); ok {
		t.Fatal("Tuesday is off")
	}
	if _, ul, ok := week.At(at(9, 23, 30)); !ok || ul != 500 {
		t.Fatal("Friday 23:30 is inside Friday's window")
	}
	if _, ul, ok := week.At(at(10, 3, 0)); !ok || ul != 500 {
		t.Fatal("Saturday 03:00 is the tail of Friday's window")
	}
	if dl, _, ok := week.At(at(10, 6, 30)); !ok || dl != 42 {
		t.Fatal("Saturday's own window wins over Friday's tail")
	}
	if _, _, ok := week.At(at(11, 3, 0)); ok {
		t.Fatal("Saturday's window does not cross midnight")
	}
}

func TestUniform(t *testing.T) {
	days := [7]bool{}
	days[time.Wednesday] = true
	week := Uniform(days, 22*60, 6*60, 10, 20)
	if !week.AnyOn() {
		t.Fatal("a selected day must be on")
	}
	if _, _, ok := week.At(at(8, 3, 0)); !ok { // Thursday 03:00, Wednesday's tail
		t.Fatal("Wednesday's night window covers Thursday morning")
	}
	if _, _, ok := week.At(at(7, 3, 0)); ok { // Wednesday 03:00, Tuesday is off
		t.Fatal("Tuesday is off")
	}
}

// TestAutoWindow: a limit of AutoLimit (-1) means "let the caller decide": it
// round-trips through the stored form and Day.Auto reports it.
func TestAutoWindow(t *testing.T) {
	raw := "mon=23:00-08:00/-1/-1;tue=23:00-08:00/3000/200"
	week, ok := Parse(raw)
	if !ok {
		t.Fatalf("Parse(%q) failed", raw)
	}
	if !week[time.Monday].Auto() || week[time.Monday].Download != AutoLimit {
		t.Fatalf("monday not auto: %+v", week[time.Monday])
	}
	if week[time.Tuesday].Auto() {
		t.Fatalf("tuesday must stay fixed: %+v", week[time.Tuesday])
	}
	if got := week.String(); !strings.Contains(got, "mon=23:00-08:00/-1/-1") {
		t.Fatalf("round trip lost the auto sentinel: %q", got)
	}
	// AtDay returns the day, so the caller can read Auto.
	now := time.Date(2026, 1, 5, 23, 30, 0, 0, time.Local) // Monday 23:30, inside the mon window
	day, active := week.AtDay(now)
	if !active || !day.Auto() {
		t.Fatalf("AtDay(%v) = %+v active=%v, want the monday auto window", now, day, active)
	}
}
