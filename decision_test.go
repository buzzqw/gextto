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

// TestExplanationMatchesRealDecisionForSmartEpisode proves the explanation now
// takes its verdict from the real approval engine: a later episode already
// archived makes both reject the older one, instead of the explanation
// reporting a first download.
func TestExplanationMatchesRealDecisionForSmartEpisode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gextto-decision.db")
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}

	later := decisionTestRelease()
	*later.Episode = 2
	later.EpisodeRange = []int64{2}
	later.Magnet = "magnet:?xt=urn:btih:" + strings.Repeat("b", 40)
	approved, _, err := db.CheckSeriesScored(later, cfg.ReleaseScore(later), cfg.UpgradeMinScoreDiff, &models.ApprovalContext{})
	if err != nil {
		t.Fatalf("seed later episode: %v", err)
	}
	if !approved {
		t.Fatal("later episode was not approved")
	}
	if err := db.MarkReleaseCompleted(later, "Example.Show.S01E02.mkv", 100); err != nil {
		t.Fatalf("mark later completed: %v", err)
	}

	trace, err := Explain(&cfg, db, decisionTestRelease())
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if trace.Decision != "rejected" {
		t.Fatalf("decision = %q, want rejected (smart episode)", trace.Decision)
	}
	if !strings.Contains(strings.ToLower(trace.Reason), "successivo") {
		t.Fatalf("reason = %q, want the smart-episode explanation", trace.Reason)
	}

	ok, reason, err := db.CheckSeriesScored(decisionTestRelease(), cfg.ReleaseScore(decisionTestRelease()), cfg.UpgradeMinScoreDiff, &models.ApprovalContext{})
	if err != nil {
		t.Fatalf("real decision: %v", err)
	}
	if ok || reason != "smart_episode" {
		t.Fatalf("real decision = %v/%q, want false/smart_episode", ok, reason)
	}
}
