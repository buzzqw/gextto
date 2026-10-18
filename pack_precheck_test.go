package gextto

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/zeebo/bencode"
)

func writeTestTorrent(t *testing.T, files ...string) string {
	t.Helper()
	entries := make([]any, 0, len(files))
	for _, name := range files {
		entries = append(entries, map[string]any{"path": []any{name}})
	}
	meta := map[string]any{
		"info": map[string]any{
			"name":  "Show.S01",
			"files": entries,
		},
	}
	var buffer bytes.Buffer
	if err := bencode.NewEncoder(&buffer).Encode(meta); err != nil {
		t.Fatalf("encode test torrent: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.torrent")
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatalf("write test torrent: %v", err)
	}
	return path
}

func testInProgressRelease(available int64) *models.Release {
	season := int64(1)
	return &models.Release{
		Title:              "Show - Stagione 1 [IN CORSO]",
		Kind:               "series",
		Season:             &season,
		IsPack:             true,
		InProgressEpisodes: available,
		Quality:            models.Quality{Resolution: "1080p", Codec: "h264"},
	}
}

// testArchive puts one episode on disk with the given quality score.
func testArchive(episode, score int64) *models.ArchiveQualityIndex {
	index := &models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{}}
	index.Best[[2]int64{1, episode}] = models.ArchiveQuality{Quality: models.Quality{Resolution: "1080p"}, Score: score}
	return index
}

func checkPack(t *testing.T, files []string, available int64, needed []int64, index *models.ArchiveQualityIndex, forbidUpgrade bool) string {
	t.Helper()
	path := writeTestTorrent(t, files...)
	reason, err := incompletePackReason(path, testInProgressRelease(available), needed, index, 1000, 100, 0, forbidUpgrade)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return reason
}

func TestIncompletePackReason(t *testing.T) {
	cases := []struct {
		name      string
		files     []string
		available int64
		needed    []int64
		index     *models.ArchiveQualityIndex
		wantEmpty bool
	}{
		{"complete pack", []string{"Show.S01E01.mkv", "Show.S01E02.mkv"}, 2, []int64{2}, testArchive(1, 2000), true},
		{"stale pack for a missing episode already better on disk", []string{"Show.S01E01.mkv"}, 2, []int64{2}, testArchive(1, 2000), false},
		{"stale pack that holds the episode we need", []string{"Show.S01E01.mkv"}, 2, []int64{1}, testArchive(1, 2000), true},
		{"stale pack with an episode that is a real upgrade", []string{"Show.S01E01.mkv"}, 2, []int64{2}, testArchive(1, 500), true},
		{"stale pack with an episode not on disk", []string{"Show.S01E01.mkv"}, 2, []int64{2}, &models.ArchiveQualityIndex{}, true},
		{"multi-episode file covers the range", []string{"Show.S01E01-E02.mkv"}, 2, []int64{2}, testArchive(1, 2000), true},
		{"multi-episode without dash", []string{"Show.S01E01E02.mkv"}, 2, []int64{2}, testArchive(1, 2000), true},
		{"subtitles do not count as episodes", []string{"Show.S01E01.mkv", "Show.S01E02.srt"}, 2, []int64{2}, testArchive(1, 2000), false},
		{"bare numbers identify the episodes", []string{"01.mkv", "02.mkv"}, 2, []int64{2}, testArchive(1, 2000), true},
		{"bare numbers can be checked", []string{"01.mkv"}, 2, []int64{2}, testArchive(1, 2000), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason := checkPack(t, tc.files, tc.available, tc.needed, tc.index, false)
			if tc.wantEmpty && reason != "" {
				t.Fatalf("useful pack rejected: %s", reason)
			}
			if !tc.wantEmpty && reason == "" {
				t.Fatal("stale pack accepted")
			}
		})
	}
}

// A DV/HDR release is a real upgrade even when the numeric score is the same:
// the semantic rules of the acquisition decision must apply to the inspection
// too, or a better pack would be thrown away.
func TestIncompletePackReasonKeepsHDRUpgradeWithEqualScore(t *testing.T) {
	path := writeTestTorrent(t, "Show.S01E01.mkv")
	release := testInProgressRelease(2)
	release.Quality = models.Quality{Resolution: "1080p", Codec: "h265", HDR: "DV", IsDV: true}
	index := &models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{}}
	index.Best[[2]int64{1, 1}] = models.ArchiveQuality{
		Quality: models.Quality{Resolution: "1080p", Codec: "h265"},
		Score:   1000,
	}
	reason, err := incompletePackReason(path, release, []int64{2}, index, 1000, 200, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("an HDR upgrade was rejected: %s", reason)
	}
}

// Upgrades disabled: only a gap makes the pack useful.
func TestIncompletePackReasonForbidUpgrade(t *testing.T) {
	reason := checkPack(t, []string{"Show.S01E01.mkv"}, 2, []int64{2}, testArchive(1, 500), true)
	if reason == "" {
		t.Fatal("a pack with no gap and upgrades disabled was accepted")
	}
	reason = checkPack(t, []string{"Show.S01E01.mkv"}, 2, []int64{1}, testArchive(1, 500), true)
	if reason != "" {
		t.Fatalf("a pack holding the missing episode was rejected: %s", reason)
	}
}

func TestPackEpisodeDecisionsNameBothSides(t *testing.T) {
	release := testInProgressRelease(2)
	release.Quality = models.Quality{Resolution: "1080p", Codec: "h264"}
	old := models.Quality{Resolution: "480p", Codec: "h264", Audio: "aac", Language: "ita"}
	index := &models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{}}
	index.Best[[2]int64{1, 1}] = models.ArchiveQuality{Quality: old, Score: 125}

	decisions := packEpisodeDecisions(release, map[int64]struct{}{1: {}}, nil, index, 1000, 100, 0, false)
	details := packDecisionDetails(1, release.Quality, 1000, decisions, 8)
	want := "S01E01 480p h264 aac ITA (score 125) → 1080p h264 (score 1000): upgrade: resolution"
	if details != want {
		t.Fatalf("details = %q, want %q", details, want)
	}

	better := &models.ArchiveQualityIndex{Best: map[[2]int64]models.ArchiveQuality{}}
	better.Best[[2]int64{1, 1}] = models.ArchiveQuality{Quality: models.Quality{Resolution: "1080p"}, Score: 2000}
	decisions = packEpisodeDecisions(release, map[int64]struct{}{1: {}}, nil, better, 1000, 100, 0, false)
	details = packDecisionDetails(1, release.Quality, 1000, decisions, 8)
	want = "S01E01 1080p (score 2000) vs 1080p h264 (score 1000): already equal or better"
	if details != want {
		t.Fatalf("details = %q, want %q", details, want)
	}
}

func TestIncompletePackReasonNamesTheComparison(t *testing.T) {
	reason := checkPack(t, []string{"Show.S01E01.mkv"}, 2, []int64{2}, testArchive(1, 2000), false)
	want := "S01E01 1080p (score 2000) vs 1080p h264 (score 1000): already equal or better"
	if !strings.Contains(reason, want) {
		t.Fatalf("reason = %q, want it to contain %q", reason, want)
	}
}

func TestIncompletePackReasonIgnoresNormalPacks(t *testing.T) {
	path := writeTestTorrent(t, "Show.S01E01.mkv")
	release := testInProgressRelease(0)
	reason, err := incompletePackReason(path, release, []int64{2}, testArchive(1, 2000), 1000, 100, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reason != "" {
		t.Fatalf("a normal pack was checked against the in-progress claim: %s", reason)
	}
}
