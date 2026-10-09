package gextto

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
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

// TestStallAlternativeToleratesLowerScore covers the temporary quality cutoff:
// while a download is stuck, an alternative within the configured drop may be
// started even though the episode is already downloading. The stuck torrent
// itself is never removed here, and a healthy download in the same season
// disables the tolerance.
func TestStallAlternativeToleratesLowerScore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gextto-stall.db")
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}

	stuck := decisionTestRelease()
	stuckScore := cfg.ReleaseScore(stuck)

	candidate := decisionTestRelease()
	candidate.Magnet = "magnet:?xt=urn:btih:" + strings.Repeat("c", 40)
	candidate.Title = "Example Show S01E01 720p WEB-DL"
	candidate.Quality.Resolution = "720p"
	candScore := cfg.ReleaseScore(candidate)
	if candScore >= stuckScore {
		t.Fatalf("fixture: candidate %d must score below stuck %d", candScore, stuckScore)
	}

	// Approve the stuck release so its placeholder and torrent row exist.
	if ok, reason, err := db.CheckSeriesScored(stuck, stuckScore, cfg.UpgradeMinScoreDiff, &models.ApprovalContext{}); err != nil || !ok {
		t.Fatalf("stuck approval: ok=%v reason=%q err=%v", ok, reason, err)
	}
	if err := db.RegisterTorrentScored(stuck, stuckScore); err != nil {
		t.Fatalf("register stuck: %v", err)
	}
	hash, ok := utils.MagnetHash(stuck.Magnet)
	if !ok {
		t.Fatal("stuck magnet has no hash")
	}
	stalledSince := time.Now().Add(-2 * time.Hour)
	if err := db.SaveStallWatch(hash, StallWatch{lastProgressAt: stalledSince, stalledSince: &stalledSince, nextRetryAt: time.Now()}); err != nil {
		t.Fatalf("save stall watch: %v", err)
	}

	key := models.LiveEpisodeKey{Series: NormalizeSeriesName("Example Show"), Season: 1, Episode: 1}

	// Without the tolerance the active episode blocks the candidate.
	ok, reason, err := db.CheckSeriesScored(candidate, candScore, cfg.UpgradeMinScoreDiff, &models.ApprovalContext{})
	if err != nil || ok || reason != "active_episode" {
		t.Fatalf("baseline: ok=%v reason=%q err=%v", ok, reason, err)
	}

	// The map only lists stalls older than the requested window.
	fresh, err := db.StalledAlternatives(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("stalled alternatives: %v", err)
	}
	if _, present := fresh[key]; !present {
		t.Fatalf("stalled map missing %+v: %v", key, fresh)
	}
	tooRecent, err := db.StalledAlternatives(time.Now().Add(-10 * time.Hour))
	if err != nil {
		t.Fatalf("stalled alternatives (old cutoff): %v", err)
	}
	if len(tooRecent) != 0 {
		t.Fatalf("stall newer than the window was included: %v", tooRecent)
	}

	// A drop wide enough accepts the lower alternative.
	drop := stuckScore - candScore
	tolerant := &models.ApprovalContext{StallAlternatives: fresh, StallScoreDrop: drop}
	if ok, reason, err := db.CheckSeriesScored(candidate, candScore, cfg.UpgradeMinScoreDiff, tolerant); err != nil || !ok {
		t.Fatalf("tolerant approval: ok=%v reason=%q err=%v", ok, reason, err)
	}
	// One point less and the candidate is no longer adequate.
	narrow := &models.ApprovalContext{StallAlternatives: fresh, StallScoreDrop: drop - 1}
	if ok, reason, err := db.CheckSeriesScored(candidate, candScore, cfg.UpgradeMinScoreDiff, narrow); err != nil || ok || reason != "active_episode" {
		t.Fatalf("narrow tolerance: ok=%v reason=%q err=%v", ok, reason, err)
	}

	// A healthy active torrent in the same season disables the tolerance, so a
	// stuck episode never piles a second alternative on a progressing season.
	healthy := decisionTestRelease()
	healthy.Magnet = "magnet:?xt=urn:btih:" + strings.Repeat("d", 40)
	healthyEpisode := int64(2)
	healthy.Episode = &healthyEpisode
	healthy.EpisodeRange = []int64{2}
	if err := db.RegisterTorrentScored(healthy, cfg.ReleaseScore(healthy)); err != nil {
		t.Fatalf("register healthy: %v", err)
	}
	blocked, err := db.StalledAlternatives(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("stalled alternatives (blocked): %v", err)
	}
	if _, present := blocked[key]; present {
		t.Fatalf("a healthy active torrent must disable the tolerance: %v", blocked)
	}
}

// TestStalledAlternativeIncludesMetadataWaiter covers a magnet still waiting
// for its file list: it is stuck too, so the search may start an alternative
// without waiting for the metadata give-up.
func TestStalledAlternativeIncludesMetadataWaiter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gextto-metadata.db")
	db, err := OpenDatabase(path)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}
	cfg.StallAlternativeAfterMin = 360
	cfg.StallAlternativeScoreDrop = 100

	release := decisionTestRelease()
	if err := db.RegisterTorrentScored(release, cfg.ReleaseScore(release)); err != nil {
		t.Fatalf("register: %v", err)
	}
	hash, ok := utils.MagnetHash(release.Magnet)
	if !ok {
		t.Fatal("stuck magnet has no hash")
	}
	key := models.LiveEpisodeKey{Series: NormalizeSeriesName("Example Show"), Season: 1, Episode: 1}

	// Waiting for metadata, but not for long enough yet.
	young := &stubTorrentSession{list: []models.TorrentView{{
		Hash: hash, Name: release.Title, State: "downloading_metadata", ActiveSeconds: 60,
	}}}
	if alternatives, drop := stalledAlternativeContext(&cfg, db, young); drop != 0 || len(alternatives) != 0 {
		t.Fatalf("young metadata waiter must not qualify: %v/%d", alternatives, drop)
	}

	// Waiting long enough: the episode becomes eligible.
	old := &stubTorrentSession{list: []models.TorrentView{{
		Hash: hash, Name: release.Title, State: "downloading_metadata", ActiveSeconds: 7 * 3600,
	}}}
	alternatives, drop := stalledAlternativeContext(&cfg, db, old)
	if drop != 100 {
		t.Fatalf("drop = %d, want 100", drop)
	}
	if _, present := alternatives[key]; !present {
		t.Fatalf("metadata waiter missing from %v", alternatives)
	}
}
