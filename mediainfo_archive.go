package gextto

// mediainfo_archive.go reads the real streams of a file that is already in the
// library before an upgrade is accepted.
//
// The acquisition decision used to trust the release name and the database row:
// when the archived copy had no MediaInfo stored, a title without quality tags
// ("Silo - S02E04 - L'armonium") looked worse than any release whose name said
// «2160p DV», so Gextto downloaded an upgrade that the completion gate then
// rejected against the real streams of the same file — a whole download, a copy
// into the library and a move to the trash for nothing.
//
// Here the file that would be replaced is probed once, before the download
// starts. The result is stored in the row it belongs to, so the periodic
// backfill does not repeat it and the next decision is free.

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// archivedProbeTimeout bounds one probe done inside a decision. ffprobe's own
// bound is a minute: right for the backfill worker, too long for the search
// cycle, which would wait on a slow archive mount for every candidate.
const archivedProbeTimeout = 20 * time.Second

// archivedProbes remembers what one file answered, so a cycle that evaluates
// many releases for the same episode asks ffprobe once. The key carries the
// size and modification time: a replaced file is a different file.
var archivedProbes = struct {
	sync.Mutex
	entries map[string]archivedProbeResult
}{entries: map[string]archivedProbeResult{}}

type archivedProbeResult struct {
	media string
}

// archivedQualityTruth returns quality with the real streams of the archived
// file merged in, reading the file once when the caller has no MediaInfo for
// it. It answers quality unchanged when nothing can be read (no ffprobe, file
// gone or unreadable): an unreadable library is not evidence either way, so the
// decision stands as it was.
//
// persist receives the probe result to store on the row it came from; it may be
// nil (dry run, or a row that does not exist yet).
func archivedQualityTruth(path, stored string, quality models.Quality, persist func(string) error) models.Quality {
	media := archivedMediaInfo(path, stored)
	if media == "" {
		return quality
	}
	if media != stored && persist != nil {
		if err := persist(media); err != nil {
			logging.Debug("the real quality of the library file could not be recorded",
				"path", path, "error", err)
		}
	}
	var info MediaInfo
	if err := json.Unmarshal([]byte(media), &info); err != nil {
		enrichQualityWithMediaInfo(media, &quality)
		return quality
	}
	// The same reading the completion gate uses before discarding a download:
	// resolution, codec, audio and HDR come from the streams, not from the
	// name, so the decision before the download and the check after it agree.
	return groundTruthQuality(quality, info)
}

// archivedMediaInfo returns the stored MediaInfo or, when it is empty, the one
// ffprobe reads from the file. Empty means "nothing to compare".
func archivedMediaInfo(path, stored string) string {
	path = strings.TrimSpace(path)
	if strings.TrimSpace(stored) != "" || path == "" || !probeAvailable() {
		return stored
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	key := path + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" +
		info.ModTime().UTC().Format(time.RFC3339Nano)
	archivedProbes.Lock()
	cached, seen := archivedProbes.entries[key]
	archivedProbes.Unlock()
	if seen {
		return cached.media
	}
	probed, probeErr := probeFileWithin(path, archivedProbeTimeout)
	result := archivedProbeResult{}
	if probeErr == nil {
		if encoded, encodeErr := json.Marshal(probed); encodeErr == nil {
			result.media = string(encoded)
		}
	}
	archivedProbes.Lock()
	// A search touches a few hundred library files at most; the cap only
	// guards a long-running process that keeps seeing new files.
	if len(archivedProbes.entries) > 4096 {
		archivedProbes.entries = map[string]archivedProbeResult{}
	}
	archivedProbes.entries[key] = result
	archivedProbes.Unlock()
	if result.media == "" {
		logging.Debug("the real quality of the library file could not be read; the decision keeps what the database knows",
			"path", path)
	}
	return result.media
}
