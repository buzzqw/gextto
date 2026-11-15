package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// rotateReportEvery is how often the torrents the queue set aside are written
// to the log. A torrent that brings no data is rotated out at every pass, and
// the queue can start it again a minute later: the same names repeated for
// hours, one line each, without adding a single fact. The first rotation is
// written at once (it is news), the ones that follow are collected and summed
// up when the window closes.
const rotateReportEvery = 30 * time.Minute

// rotateReportNames bounds the names quoted in the summary: a person reads the
// first few and the count says how many others there are.
const rotateReportNames = 3

// noteRotationLocked records that the queue set a torrent aside and returns
// the sentence to write, or "" while the report window is still open. It is
// called under d.mu (see the rotation plan in tick).
//
// One torrent keeps the sentence it always had; several become one line with a
// count and the first names, like the automatic peer cap does
// (logAutoConnectionChanges).
func (d *Daemon) noteRotationLocked(now time.Time, id, name string) string {
	if d.rotatePending == nil {
		d.rotatePending = make(map[string]string, 4)
	}
	d.rotatePending[id] = name
	if !d.rotateReportAt.IsZero() && now.Sub(d.rotateReportAt) < rotateReportEvery {
		return ""
	}
	d.rotateReportAt = now
	names := make([]string, 0, len(d.rotatePending))
	for _, pending := range d.rotatePending {
		names = append(names, pending)
	}
	d.rotatePending = make(map[string]string, 4)
	return formatRotations(names)
}

// formatRotations renders the torrents set aside since the last report as one
// sentence a person can follow.
func formatRotations(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("«%s» moves no data: set aside so another torrent can run", names[0])
	}
	sort.Strings(names)
	quoted := make([]string, 0, rotateReportNames)
	for _, name := range names[:min(rotateReportNames, len(names))] {
		quoted = append(quoted, "«"+name+"»")
	}
	more := ""
	if len(names) > rotateReportNames {
		more = fmt.Sprintf(" +%d", len(names)-rotateReportNames)
	}
	return fmt.Sprintf("%d torrents move no data: set aside so another torrent can run: %s%s",
		len(names), strings.Join(quoted, ", "), more)
}
