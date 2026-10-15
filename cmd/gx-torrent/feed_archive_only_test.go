package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestArchiveOnlyNoticeLoggedOncePerFeed: a feed that only grows the archive is
// a valid configuration, so its notice is printed once, not at every poll, and
// with neutral wording (no instruction to change the feed).
func TestArchiveOnlyNoticeLoggedOncePerFeed(t *testing.T) {
	var buf bytes.Buffer
	previous := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previous)
		log.SetFlags(flags)
	})

	d := &Daemon{}
	d.logArchiveOnly([]string{"ppb1", "leech"})
	d.logArchiveOnly([]string{"ppb1", "leech"})

	out := buf.String()
	if got := strings.Count(out, "archive only"); got != 1 {
		t.Fatalf("the notice was printed %d time(s), want 1:\n%s", got, out)
	}
	// One line for all the feeds, not one line per feed: the list of a bulk
	// import must not fill the log.
	if !strings.Contains(out, "ppb1") || !strings.Contains(out, "leech") {
		t.Fatalf("the line does not name the feeds:\n%s", out)
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n") + 1; lines != 1 {
		t.Fatalf("the notice takes %d lines, want 1:\n%s", lines, out)
	}
	if strings.Contains(out, "take everything") || strings.Contains(out, "add a filter") {
		t.Fatalf("the notice still carries advice meant for a broken feed:\n%s", out)
	}
}
