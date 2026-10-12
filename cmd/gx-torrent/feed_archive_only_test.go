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
	d.logArchiveOnlyOnce("ppb1", 50)
	d.logArchiveOnlyOnce("ppb1", 50)
	d.logArchiveOnlyOnce("leech", 469)

	out := buf.String()
	if got := strings.Count(out, "feed ppb1: archive only"); got != 1 {
		t.Fatalf("ppb1 notice printed %d time(s), want 1:\n%s", got, out)
	}
	if got := strings.Count(out, "feed leech: archive only"); got != 1 {
		t.Fatalf("leech notice printed %d time(s), want 1:\n%s", got, out)
	}
	if strings.Contains(out, "take everything") || strings.Contains(out, "add a filter") {
		t.Fatalf("the notice still carries advice meant for a broken feed:\n%s", out)
	}
}
