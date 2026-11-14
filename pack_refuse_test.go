package gextto

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

func TestRefuseUselessPackRetriesInProgressPackAfterADay(t *testing.T) {
	db := newTestDB(t)
	now := time.Now()
	season := int64(3)
	series := "Series A"
	url := "https://example.invalid/a.torrent"
	ongoing := models.Release{Title: "Series A - Stagione 3 [IN CORSO 04/08] 1080p", Kind: "series", Series: &series, Season: &season, IsPack: true, InProgressEpisodes: 4, TorrentURL: &url, Magnet: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if wait := refuseUselessPack(db, &ongoing, "pack_inspection_useless", "nothing useful", now); wait != inProgressPackRetry {
		t.Fatalf("in-progress pack wait = %v, want %v", wait, inProgressPackRetry)
	}
	hash, err := releaseIdentityHash(&ongoing)
	if err != nil {
		t.Fatal(err)
	}
	if blocked, _ := db.IsBlocklisted(hash); !blocked {
		t.Fatal("pack should be blocked during the wait")
	}

	done := models.Release{Title: "Series B - Stagione 3 1080p", Kind: "series", Series: &series, Season: &season, IsPack: true, Magnet: "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	if wait := refuseUselessPack(db, &done, "pack_inspection_useless", "nothing useful", now); wait != 0 {
		t.Fatalf("closed pack wait = %v, want 0 (for good)", wait)
	}
}
