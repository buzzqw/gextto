package gextto

import (
	"testing"

	"github.com/buzzqw/gextto/internal/logging"
)

// TestFailedSourcesSummaryNamesTheSource: the cycle warning names each failing
// source with how many of its searches failed and why, not just "1 indexer".
func TestFailedSourcesSummaryNamesTheSource(t *testing.T) {
	stats := []logging.SourceStatEntry{
		{Kind: "indexer", Name: "Jackett: ilcorsaronero", Stats: logging.SourceStat{OK: 103, Fail: 3, LastError: "timeout after 15s"}},
		{Kind: "web", Name: "bt4g", Stats: logging.SourceStat{Fail: 2, LastError: "HTTP 503"}},
		{Kind: "feed", Name: "ok feed", Stats: logging.SourceStat{OK: 4}},
	}
	got := failedSourcesSummary(stats)
	want := "indexer Jackett: ilcorsaronero: 3 of 106 searches failed — timeout after 15s; search engine bt4g: all 2 searches failed — HTTP 503"
	if got != want {
		t.Fatalf("summary =\n %q\nwant\n %q", got, want)
	}
}
