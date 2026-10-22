package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/buzzqw/gextto/internal/models"
)

// mediainfo_gate.go checks an upgrade against the real streams before the
// downloaded file reaches the library. The decision to download trusted the
// release name; here ffprobe reads the new file (still in the download folder)
// and the archived copy it would replace, and an upgrade the streams do not
// confirm (an upscaled «2160p», a fake «HDR», an «Atmos» that is AAC) is
// discarded: the archived copy stays.

// groundTruthQuality returns quality with the attributes ffprobe measures
// reliably taken from the streams instead of the name: resolution, video codec,
// audio codec family and channels, and the HDR10/HLG flag. Dolby Vision, the
// source, REPACK/PROPER and object audio stay as named: ffprobe does not see
// them reliably.
func groundTruthQuality(quality models.Quality, info MediaInfo) models.Quality {
	if resolution := info.Resolution(); resolution != "" {
		quality.Resolution = resolution
	}
	if label, ok := CodecLabel(info.VideoCodec); ok {
		quality.Codec = label
	}
	if label, ok := AudioLabel(info.AudioCodec); ok {
		// ffprobe names DTS, DTS-HD HRA and DTS-HD MA all «dts»: a DTS-HD name
		// over a DTS stream is not a lie.
		named := strings.ToLower(quality.Audio)
		if !(label == "dts" && strings.HasPrefix(named, "dts")) {
			quality.Audio = label
		}
	}
	switch {
	case info.AudioChannels >= 8:
		quality.AudioChannels = "7.1"
	case info.AudioChannels >= 6:
		quality.AudioChannels = "5.1"
	case info.AudioChannels > 0:
		quality.AudioChannels = "2.0"
	}
	if info.Width > 0 || info.Height > 0 {
		if info.HDR != "" {
			if quality.HDR == "" {
				quality.HDR = info.HDR
			}
		} else if !quality.IsDV {
			// No PQ/HLG transfer in the stream: the «HDR» of the name is not real.
			quality.HDR = ""
		}
	}
	return quality
}

// probeFile is ProbeResult behind a variable, so tests can feed fixed streams.
var probeFile = ProbeResult

// probeAvailable is Available behind a variable, for the same reason.
var probeAvailable = Available

// mediaInfoConfirmed remembers the upgrades already confirmed: a completed
// episode passes through the gate before the copy into the library and again
// when the copied file is processed, and one ffprobe pass is enough.
var mediaInfoConfirmed sync.Map

// mediaInfoUpgradeRejection returns why the real streams do not confirm the
// upgrade the release named, or "" when they do, when the download is not an
// upgrade, or when a probe is not possible (no ffprobe, archived copy
// unreachable): in doubt the earlier decision stands.
func mediaInfoUpgradeRejection(cfg *Config, db *Database, hash, video string, release *models.Release) string {
	if cfg == nil || db == nil || release == nil || !probeAvailable() {
		return ""
	}
	if _, done := mediaInfoConfirmed.Load(strings.ToLower(hash)); done {
		return ""
	}
	backups, err := db.UpgradeBackupsFor(hash)
	if err != nil || len(backups) != 1 {
		// Packs replace several episodes at once: they are judged per file
		// after the rename.
		return ""
	}
	backup := backups[0]
	if backup.ArchivePath == nil || strings.TrimSpace(*backup.ArchivePath) == "" {
		return ""
	}
	oldPath := *backup.ArchivePath
	if _, err := os.Stat(oldPath); err != nil {
		return ""
	}
	newInfo, err := probeFile(video)
	if err != nil {
		return ""
	}
	oldInfo, err := probeFile(oldPath)
	if err != nil {
		return ""
	}
	oldNamed := MergeQuality(ParseQuality(filepath.Base(oldPath)), ParseQuality(backup.Title))
	newQuality := groundTruthQuality(release.Quality, newInfo)
	oldQuality := groundTruthQuality(oldNamed, oldInfo)
	newScore := ScoreQuality(&newQuality, cfg.Settings)
	oldScore := ScoreQuality(&oldQuality, cfg.Settings)
	if upgradeReasonWith(&newQuality, &oldQuality, newScore, oldScore, cfg.UpgradeMinScoreDiff, cfg.Settings) != "" {
		mediaInfoConfirmed.Store(strings.ToLower(hash), struct{}{})
		return ""
	}
	return fmt.Sprintf("MediaInfo: %s (%d) does not improve on the archived %s (%d)",
		friendlyQuality(newQuality), newScore, friendlyQuality(oldQuality), oldScore)
}
