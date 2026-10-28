package gextto

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// blockedSeriesRelease builds a series release with a stable identity hash.
func blockedSeriesRelease(t *testing.T) (*models.Release, string) {
	t.Helper()
	year := int64(2026)
	season, episode := int64(1), int64(1)
	name := "Blocklist Show"
	release := &models.Release{
		Series:       &name,
		Season:       &season,
		Episode:      &episode,
		EpisodeRange: []int64{1},
		Title:        "Blocklist Show S01E01 1080p WEB-DL",
		Magnet:       "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		Source:       "feed",
		Kind:         "series",
		Year:         &year,
		DiscoveredAt: time.Now().UTC(),
		Seeders:      -1,
		Peers:        -1,
	}
	hash, err := releaseIdentityHash(release)
	if err != nil {
		t.Fatalf("releaseIdentityHash: %v", err)
	}
	return release, hash
}

// TestApprovalUsesTheBlocklistSnapshot checks that the set the cycle already
// loaded decides the blocklist question: the release must be refused without a
// blocklist row in the database.
func TestApprovalUsesTheBlocklistSnapshot(t *testing.T) {
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	defer db.Close()
	release, hash := blockedSeriesRelease(t)

	// Without the snapshot the database says "not blocked".
	approved, reason, err := db.CheckSeriesScored(release, release.Quality.Score(), DefaultUpgradeMinScoreDiff, &models.ApprovalContext{})
	if err != nil {
		t.Fatalf("CheckSeriesScored: %v", err)
	}
	if !approved && reason == "blocklisted" {
		t.Fatal("the release is not in the blocklist table, it must not be reported as blocked")
	}

	// With the snapshot the same release is refused as blocklisted.
	snapshot := map[string]struct{}{hash: {}}
	approved, reason, err = db.CheckSeriesScored(release, release.Quality.Score(), DefaultUpgradeMinScoreDiff, &models.ApprovalContext{Blocklist: snapshot})
	if err != nil {
		t.Fatalf("CheckSeriesScored with snapshot: %v", err)
	}
	if approved || reason != "blocklisted" {
		t.Fatalf("approved=%v reason=%q, want the release refused as blocklisted", approved, reason)
	}
}
