package main

import (
	"strings"
	"testing"
	"time"
)

// TestRotateReportStaysQuietBetweenSummaries: a torrent rotated out at every
// pass is one fact, not a line per pass. The first rotation is written at once
// (it is news), the ones inside the window are summed up when it closes, and
// the same torrent rotating again does not add itself twice.
func TestRotateReportStaysQuietBetweenSummaries(t *testing.T) {
	d := &Daemon{}
	at := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

	first := d.noteRotationLocked(at, "a", "Series A S01E01")
	if !strings.Contains(first, "«Series A S01E01» moves no data") {
		t.Fatalf("the first rotation = %q, want the torrent named", first)
	}
	if line := d.noteRotationLocked(at.Add(time.Minute), "a", "Series A S01E01"); line != "" {
		t.Fatalf("the same torrent inside the window wrote %q, want nothing", line)
	}
	if line := d.noteRotationLocked(at.Add(2*time.Minute), "b", "Series B S02E03"); line != "" {
		t.Fatalf("a second torrent inside the window wrote %q, want nothing", line)
	}

	// The window closes on the next rotation, which carries both names.
	line := d.noteRotationLocked(at.Add(rotateReportEvery+time.Minute), "c", "Series C S03E05")
	if !strings.Contains(line, "3 torrents move no data") {
		t.Fatalf("the summary = %q, want the three torrents counted", line)
	}
	for _, name := range []string{"Series A S01E01", "Series B S02E03", "Series C S03E05"} {
		if !strings.Contains(line, name) {
			t.Fatalf("the summary = %q, want it to name %q", line, name)
		}
	}
	// A new window starts empty: nothing is carried over.
	if line := d.noteRotationLocked(at.Add(rotateReportEvery+2*time.Minute), "d", "Series D S04E01"); line != "" {
		t.Fatalf("a rotation at the start of a new window wrote %q, want nothing", line)
	}
}

// TestFormatRotationsNamesTheFirstOnes: a long list of torrents set aside
// quotes the first few and counts the others, so the line stays readable.
func TestFormatRotationsNamesTheFirstOnes(t *testing.T) {
	if got := formatRotations(nil); got != "" {
		t.Fatalf("formatRotations(nil) = %q, want empty", got)
	}
	if got := formatRotations([]string{"only"}); got != "«only» moves no data: set aside so another torrent can run" {
		t.Fatalf("formatRotations(one) = %q", got)
	}
	names := []string{"e", "d", "c", "b", "a"}
	got := formatRotations(names)
	if !strings.Contains(got, "5 torrents move no data") || !strings.Contains(got, "+2") {
		t.Fatalf("formatRotations(five) = %q, want the count and the ones left out", got)
	}
	// Sorted, so the same set reads the same way at every report.
	if !strings.HasPrefix(got, "5 torrents move no data: set aside so another torrent can run: «a», «b», «c» +2") {
		t.Fatalf("formatRotations(five) = %q, want the first three names in order", got)
	}
}
