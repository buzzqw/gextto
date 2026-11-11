package gextto

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// TestASpecialNumberedZeroIsNotTheWholeSeason: S03E00 is a special, a single
// episode. Only a pack numbered 0 stands for the whole season: the special
// must not fill every gap of its season, nor count as covering its episodes.
func TestASpecialNumberedZeroIsNotTheWholeSeason(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	special := ParseRelease("Show.S03E00.Special.1080p.WEB-DL.ITA.mkv", magnet, "x")
	pack := ParseRelease("Show - Stagione 3 (2025) [COMPLETA] 1080p ITA", magnet, "x")
	if special == nil || pack == nil {
		t.Fatal("releases did not parse")
	}
	if hasCompleteRange(special) {
		t.Fatal("a special numbered 0 was taken for the whole season")
	}
	if !hasCompleteRange(pack) {
		t.Fatal("a whole-season pack lost its sentinel")
	}

	series := "Show"
	gaps := map[gapTarget]struct{}{
		{Series: series, Season: 3, Episode: 0}: {},
		{Series: series, Season: 3, Episode: 4}: {},
		{Series: series, Season: 3, Episode: 9}: {},
	}
	special.Series, pack.Series = &series, &series
	if got := gapEpisodesForRelease(special, gaps); len(got) != 1 || got[0] != 0 {
		t.Fatalf("special fills %v, want only the missing S03E00", got)
	}
	if got := gapEpisodesForRelease(pack, gaps); len(got) != 3 {
		t.Fatalf("whole-season pack fills %v, want every gap of the season", got)
	}

	cfg := &SeriesConfig{Name: series}
	if releaseMatchesConfiguredEpisode(special, cfg, 3, 4) {
		t.Fatal("the special was taken for S03E04")
	}
	if !releaseMatchesConfiguredEpisode(special, cfg, 3, 0) {
		t.Fatal("the special does not match its own S03E00")
	}
}

// TestTheReplacingTextNamesASpecialButNotAPackSentinel: "S03E00 missing" is
// right for a special and wrong for a whole-season pack.
func TestTheReplacingTextNamesASpecialButNotAPackSentinel(t *testing.T) {
	season, zero := int64(3), int64(0)
	archive := &models.ArchiveQualityIndex{}
	special := &models.Release{Kind: "series", Season: &season, Episode: &zero, EpisodeRange: []int64{0}}
	pack := &models.Release{Kind: "series", Season: &season, Episode: &zero, EpisodeRange: []int64{0}, IsPack: true}
	if got := upgradeSourceText(archive, pack); got != "" {
		t.Fatalf("pack replacing text = %q, want none", got)
	}
	_ = special // an empty index says nothing either way; the special keeps its episode below
	if got := positiveEpisodes([]int64{0, 2}); len(got) != 1 || got[0] != 2 {
		t.Fatalf("positiveEpisodes = %v", got)
	}
}

// TestADiscardedPackIsNotified: the download of a pack was notified when it
// started, so its discard is notified too; otherwise the operator believes it
// is still downloading. The text names what the pack really held.
func TestADiscardedPackIsNotified(t *testing.T) {
	bodies := make(chan []byte, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg, db, engine, hash := packInspectFixture(t, []models.FileView{{Path: "Show.S01E01.mkv"}})
	cfg.NotifyWebhookURL = &server.URL
	cfg.NotifyEventDownloads = true
	InspectActivePacks(cfg, engine, db, FromConfig(cfg), map[string]packInspectionCheck{})
	if _, ok := engine.removed[hash]; !ok {
		t.Fatal("useless pack not removed")
	}
	select {
	case body := <-bodies:
		var envelope struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("webhook body: %v", err)
		}
		if envelope.Event != "download_discarded" || envelope.Data["held_episodes"] != "S01E01" {
			t.Fatalf("notification = %s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notification for the discarded pack")
	}
	text := formatEventLang("it", "download_discarded", map[string]any{
		"series": "Show", "season": int64(3), "held_episodes": "S03E01", "whole_season": true, "downloaded_bytes": int64(0),
	})
	if !strings.Contains(text, "intera stagione 3 ma contiene solo S03E01") {
		t.Fatalf("notification text = %q", text)
	}
}
