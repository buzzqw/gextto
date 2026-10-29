package gextto

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

// mediainfo_archive_test.go pins the check that stops an upgrade the library
// copy already wins: the probe is used only when the database has no MediaInfo,
// its answer is remembered, and a failed probe leaves the decision as it was.

func TestArchivedQualityTruthUsesTheStoredMediaInfo(t *testing.T) {
	called := false
	previous := probeArchiveFile
	probeArchiveFile = func(string, time.Duration) (MediaInfo, error) {
		called = true
		return MediaInfo{}, errors.New("must not run")
	}
	t.Cleanup(func() { probeArchiveFile = previous })

	stored := `{"width":3840,"height":2076,"hdr":"DV"}`
	quality := archivedQualityTruth("/tmp/unused.mkv", stored, models.Quality{Resolution: "1080p"}, nil)
	if called {
		t.Fatal("the probe ran although the MediaInfo was already stored")
	}
	if quality.Resolution != "2160p" || !quality.HasHDR() {
		t.Fatalf("quality = %+v, want the stored 2160p HDR", quality)
	}
}

func TestArchivedQualityTruthProbesOnceAndRemembers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Silo - S02E04 - L'armonium - [2160p][h265].mkv")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	previousAvailable, previousProbe := probeAvailable, probeArchiveFile
	probeAvailable = func() bool { return true }
	probeArchiveFile = func(string, time.Duration) (MediaInfo, error) {
		calls++
		return MediaInfo{Width: 3840, Height: 2076, VideoCodec: "hevc", HDR: "DV", AudioCodec: "eac3", AudioChannels: 6}, nil
	}
	t.Cleanup(func() { probeAvailable, probeArchiveFile = previousAvailable, previousProbe })

	persisted := 0
	persist := func(value string) error {
		persisted++
		var info MediaInfo
		if err := json.Unmarshal([]byte(value), &info); err != nil {
			t.Fatalf("persisted value is not MediaInfo: %v", err)
		}
		return nil
	}
	// The name lost its HDR tag: the streams are what reveal it.
	quality := archivedQualityTruth(path, "", models.Quality{Resolution: "2160p", Codec: "h265"}, persist)
	if !quality.HasHDR() || quality.AudioChannels != "5.1" {
		t.Fatalf("quality = %+v, want the HDR and 5.1 the streams carry", quality)
	}
	_ = archivedQualityTruth(path, "", models.Quality{Resolution: "2160p"}, persist)
	if calls != 1 {
		t.Fatalf("the file was probed %d times, want 1 (the answer is remembered)", calls)
	}
	if persisted != 1 {
		t.Fatalf("the result was stored %d times, want 1", persisted)
	}
}

func TestArchivedQualityTruthKeepsTheDecisionWhenTheProbeFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.mkv")
	if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	previousAvailable, previousProbe := probeAvailable, probeArchiveFile
	probeAvailable = func() bool { return true }
	probeArchiveFile = func(string, time.Duration) (MediaInfo, error) {
		return MediaInfo{}, errors.New("ffprobe failed")
	}
	t.Cleanup(func() { probeAvailable, probeArchiveFile = previousAvailable, previousProbe })

	named := models.Quality{Resolution: "1080p", Source: "webdl"}
	quality := archivedQualityTruth(path, "", named, func(string) error {
		t.Fatal("nothing must be stored when the probe failed")
		return nil
	})
	if quality.Resolution != named.Resolution || quality.Source != named.Source || quality.IsDV {
		t.Fatalf("quality = %+v, want the one passed in", quality)
	}
}
