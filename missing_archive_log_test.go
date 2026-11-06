package gextto

import (
	"testing"
	"time"
)

// TestMissingArchiveLogGathersOneSeries: the files of one series that vanish in
// the same check become one line, not one per episode.
func TestMissingArchiveLogGathersOneSeries(t *testing.T) {
	type call struct {
		series string
		count  int
	}
	got := make(chan call, 4)
	m := &missingArchiveLog{after: 20 * time.Millisecond, write: func(series string, episodes []missingArchiveEpisode) {
		got <- call{series, len(episodes)}
	}}
	for e := int64(1); e <= 6; e++ {
		m.add("Series A", 1, e, "/lib/Series A/ep.mkv")
	}
	m.add("Series B", 2, 3, "/lib/Series B/ep.mkv")
	counts := map[string]int{}
	for range 2 {
		select {
		case c := <-got:
			counts[c.series] = c.count
		case <-time.After(2 * time.Second):
			t.Fatal("the gathered lines were not written")
		}
	}
	if counts["Series A"] != 6 || counts["Series B"] != 1 {
		t.Fatalf("lines = %v, want one line of 6 for Series A and one of 1 for Series B", counts)
	}
}

func TestMissingArchiveText(t *testing.T) {
	one, path := missingArchiveText("Series A", []missingArchiveEpisode{{1, 6, "/lib/A/e6.mkv"}})
	if one != "The library file of Series A S01E06 is no longer on disk, so the episode can be downloaded again" || path != "/lib/A/e6.mkv" {
		t.Fatalf("single = %q (%q)", one, path)
	}
	many, folder := missingArchiveText("Series A", []missingArchiveEpisode{
		{1, 3, "/lib/A/e3.mkv"}, {1, 1, "/lib/A/e1.mkv"}, {1, 2, "/lib/A/e2.mkv"},
	})
	if many != "3 library files of Series A are no longer on disk, so they can be downloaded again" || folder != "/lib/A" {
		t.Fatalf("many = %q (%q)", many, folder)
	}
}
