package gextto

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// decisionTestRelease implements the test fixture .
func decisionTestRelease() *models.Release {
	series := "Example Show"
	season := int64(1)
	episode := int64(1)
	return &models.Release{
		Title:      "Example Show S01E01 1080p WEB-DL",
		Magnet:     "magnet:?xt=urn:btih:" + strings.Repeat("a", 40),
		TorrentURL: nil,
		Source:     "test",
		Quality: models.Quality{
			Resolution: "1080p",
			Source:     "webdl",
		},
		Kind:         "series",
		Series:       &series,
		Season:       &season,
		Episode:      &episode,
		IsPack:       false,
		EpisodeRange: []int64{1},
		Year:         nil,
		DiscoveredAt: time.Now().UTC(),
		SizeBytes:    0,
		Seeders:      -1,
		Peers:        -1,
	}
}

func TestExplanationIsReadOnlyAndStructured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gextto-decision.db")
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	cfg := DefaultConfig()
	trace, err := Explain(&cfg, db, decisionTestRelease())
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if len(trace.Steps) == 0 {
		t.Fatal("expected a non-empty decision trace")
	}
	found := false
	for _, item := range trace.Steps {
		if item.Rule == "blocklist" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a blocklist step in the decision trace")
	}
}
