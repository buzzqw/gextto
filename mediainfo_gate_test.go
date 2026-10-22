package gextto

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestGroundTruthQualityTrustsTheStreams(t *testing.T) {
	named := ParseQuality("Show.S01E01.2160p.WEB-DL.HDR.DDP5.1.Atmos.H265-GRP")
	real := groundTruthQuality(named, MediaInfo{Width: 1920, Height: 1080, VideoCodec: "h264", AudioCodec: "aac", AudioChannels: 2})
	if real.Resolution != "1080p" || real.Codec != "h264" || real.Audio != "aac" || real.AudioChannels != "2.0" || real.HDR != "" {
		t.Fatalf("ground truth = %+v", real)
	}
	if real.Source != "webdl" {
		t.Fatalf("the source is not measurable and must stay as named: %+v", real)
	}
	// DTS-HD MA is «dts» for ffprobe: the name is not contradicted.
	dts := groundTruthQuality(models.Quality{Resolution: "1080p", Audio: "dts-hd"}, MediaInfo{Width: 1920, Height: 1080, AudioCodec: "dts", AudioChannels: 8})
	if dts.Audio != "dts-hd" || dts.AudioChannels != "7.1" {
		t.Fatalf("DTS-HD over a dts stream = %+v", dts)
	}
	// Dolby Vision is not reliably visible: a DV name keeps its flag.
	dv := groundTruthQuality(models.Quality{Resolution: "2160p", HDR: "DV", IsDV: true}, MediaInfo{Width: 3840, Height: 2160})
	if !dv.IsDV || dv.HDR != "DV" {
		t.Fatalf("DV name = %+v", dv)
	}
}

func TestMediaInfoGateDiscardsAnUpgradeTheStreamsDoNotConfirm(t *testing.T) {
	dir := t.TempDir()
	oldFile := filepath.Join(dir, "Example - S01E01 - [1080p][h264].mkv")
	newFile := filepath.Join(dir, "download", "Example.S01E01.2160p.WEB-DL.H265-GRP.mkv")
	for _, path := range []string{oldFile, newFile} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db := newTestDB(t)
	if _, err := db.db.Exec("INSERT INTO series(name) VALUES ('Example')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("INSERT INTO episodes(series_id,season,episode,title,quality_score,downloaded_at,archive_path) SELECT id,1,1,'Example.S01E01.1080p.WEB-DL.H264-OLD',1250,'2024-01-01T00:00:00+00:00',?1 FROM series WHERE name='Example'", oldFile); err != nil {
		t.Fatal(err)
	}
	release := testRelease()
	release.Title = "Example.S01E01.2160p.WEB-DL.H265-GRP"
	release.Quality = ParseQuality(release.Title)
	context := &models.ApprovalContext{Live: &models.LiveDownloads{}}
	if approved, reason, err := db.CheckSeriesScored(&release, release.Quality.Score(), 200, context); err != nil || !approved || reason != "upgrade" {
		t.Fatalf("setup upgrade: %v %q %v", approved, reason, err)
	}
	hash, err := releaseIdentityHash(&release)
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{}

	streams := map[string]MediaInfo{oldFile: {Width: 1920, Height: 1080, VideoCodec: "h264"}}
	t.Cleanup(func() { probeFile, probeAvailable = ProbeResult, Available })
	probeAvailable = func() bool { return true }
	probeFile = func(path string) (MediaInfo, error) {
		if info, ok := streams[path]; ok {
			return info, nil
		}
		return MediaInfo{}, errors.New("no probe")
	}

	// An upscale: named 2160p, really 1080p. Discarded.
	streams[newFile] = MediaInfo{Width: 1920, Height: 1080, VideoCodec: "hevc"}
	if reason := mediaInfoUpgradeRejection(&cfg, db, hash, newFile, &release); reason == "" {
		t.Fatal("an upscaled «2160p» must not replace the archived 1080p")
	}
	// A real 2160p: confirmed.
	streams[newFile] = MediaInfo{Width: 3840, Height: 2160, VideoCodec: "hevc"}
	if reason := mediaInfoUpgradeRejection(&cfg, db, hash, newFile, &release); reason != "" {
		t.Fatalf("a real 2160p upgrade was rejected: %s", reason)
	}
	// Without a probe the earlier decision stands.
	mediaInfoConfirmed.Delete(strings.ToLower(hash))
	delete(streams, newFile)
	if reason := mediaInfoUpgradeRejection(&cfg, db, hash, newFile, &release); reason != "" {
		t.Fatalf("a failed probe must not discard the download: %s", reason)
	}
}
