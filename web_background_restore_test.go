package gextto

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// The startup lines must name the torrents they restore, not just count them:
// the live engine name wins and an unknown hash still gives a usable label.
func TestRestoredTorrentNames(t *testing.T) {
	session := &stubTorrentSession{list: []models.TorrentView{
		{Hash: "AAAA", Name: "Zeta"},
		{Hash: "bbbb", Name: "Alfa"},
	}}
	names := bg_restoredTorrentNames(session, nil, []string{"aaaa", "bbbb", "cccc"})
	want := []string{"Alfa", "Zeta", "cccc"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

// A large restore keeps the log line bounded.
func TestRestoredTorrentNamesLineIsCapped(t *testing.T) {
	if got := bg_restoredTorrentNamesLine([]string{"a", "b", "c"}, 2); got != "a, b (+1)" {
		t.Fatalf("capped line = %q", got)
	}
	if got := bg_restoredTorrentNamesLine([]string{"a", "b"}, 2); got != "a, b" {
		t.Fatalf("uncapped line = %q", got)
	}
	if got := bg_restoredTorrentNamesLine(nil, 2); got != "" {
		t.Fatalf("empty line = %q", got)
	}
}
