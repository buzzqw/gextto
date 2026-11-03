package gextto

// ed2k_log_test.go pins the filter of aMule's log: the router's UPnP event
// blocks are dropped, every other line (including the useful UPnP ones) stays.

import (
	"strings"
	"testing"
)

func TestEd2kLogWithoutRouterEvents(t *testing.T) {
	lines := []string{
		"!2026-11-03 00:58:40: Universal Plug and Play: UPNP_EVENT_RECEIVED:",
		"!2026-11-03 00:58:40:     SID: uuid:3f1ab6aa-d67b-11f1-a7a8-d1cbb3658326",
		"!2026-11-03 00:58:40:     Key: 190",
		"!2026-11-03 00:58:40:     Property list:",
		"!2026-11-03 00:58:40:         SystemUpdateID='6737'",
		"!2026-11-03 00:58:40: Universal Plug and Play: UPNP_EVENT_RECEIVED:",
		"!2026-11-03 00:58:40:     SID: uuid:3f1ab6aa-d67b-11f1-a7a8-d1cbb3658326",
		"!2026-11-03 00:58:40:     Key: 191",
		"!2026-11-03 00:58:40:     Property list:",
		"!2026-11-03 00:58:40:         PortMappingNumberOfEntries='4'",
		"!2026-11-03 00:58:41: Universal Plug and Play: UPNP_ADD_PORT_MAPPING failed with error 718",
		"!2026-11-03 00:58:42: eD2k: connected to a server",
	}
	got := ed2kLogWithoutRouterEvents(lines)
	want := []string{
		lines[10],
		lines[11],
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("filtered log =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A line that begins the block without the timestamp (the log can be cut at the
// start) is dropped as well, and the block does not swallow the line after it.
func TestEd2kLogWithoutRouterEventsKeepsTheNextLine(t *testing.T) {
	lines := []string{
		"Universal Plug and Play: UPNP_EVENT_RECEIVED:",
		"    SID: uuid:abc",
		"    Key: 12",
		"    Property list:",
		"        SystemUpdateID='99'",
		"!2026-11-03 01:00:00: Downloading Amelie.mkv",
	}
	got := ed2kLogWithoutRouterEvents(lines)
	if len(got) != 1 || got[0] != lines[5] {
		t.Fatalf("filtered = %q, want only the last line", got)
	}
}

// A five-line shape that does not start with the event header is left alone:
// the date alone is not a reason to drop a line.
func TestEd2kLogWithoutRouterEventsLeavesOtherLines(t *testing.T) {
	lines := []string{
		"!2026-11-03 00:58:40:     Key: 190",
		"!2026-11-03 00:58:40:     SID: uuid:abc",
	}
	if got := ed2kLogWithoutRouterEvents(lines); len(got) != 2 {
		t.Fatalf("filtered = %q, want both lines kept", got)
	}
}
