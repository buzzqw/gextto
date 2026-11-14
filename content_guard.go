package gextto

// content_guard.go refuses a media release whose payload cannot be what its
// title promises, before a byte is downloaded. Two verdicts, both read from the
// file list and neither a matter of taste:
//
//   - a program or a script in the payload ("setup.exe", "crack.bat"): a film
//     or an episode never needs one, and an executable left in the download
//     folder is one double-click away from running;
//   - no video file at all, samples aside (a torrent of text files and a
//     sample): the release would be downloaded and then fail at the import,
//     which looks only for video files.
//
// A RAR set is not "no video": the video is inside and is extracted before the
// import (rar_extract.go), so the payload is accepted. The same holds for the
// other archive containers, which Gextto either extracts or leaves to the
// operator: refusing them would be a judgement on a format, not on the payload.
//
// The verdict is taken twice, because a file list becomes available twice: on
// the `.torrent` the cycle already downloaded (before the download starts, see
// candidateRun.unsafePayload) and on the file list the engine reports for a
// magnet (as soon as the metadata arrives, usually before any data, see
// InspectActiveContent).

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// unsafePayloadBlockReason is the code recorded in the blocklist. It reaches
// the Blocklist page, where every reason is shown by its code.
const unsafePayloadBlockReason = "unsafe_payload"

// unsafePayloadBlockPeriod is how long a refused payload stays out of the
// searches. A file list does not change, so the release would be refused again
// at every cycle and repeat the same line in the log; but an indexer can
// re-list the same episode with a corrected torrent, so the refusal is not
// forever. A month is long enough to stop the noise.
const unsafePayloadBlockPeriod = 30 * 24 * time.Hour

// unsafePayloadExtensions are the file kinds a film or an episode never needs.
// Scene sidecars (.nfo, .txt, .url, .sfv, .jpg) are not here: they are harmless
// and travel with many legitimate releases.
var unsafePayloadExtensions = map[string]bool{
	"exe": true, "scr": true, "bat": true, "cmd": true, "com": true,
	"msi": true, "ps1": true, "vbs": true, "vbe": true, "js": true,
	"jse": true, "wsf": true, "wsh": true, "lnk": true, "pif": true,
	"cpl": true, "hta": true, "reg": true, "jar": true, "apk": true,
	"dmg": true, "app": true, "sh": true, "bash": true,
}

// payloadArchiveExtensions are the containers a release can legitimately
// arrive in, with the video inside. The RAR volumes come in both namings
// ("name.rar", "name.r00"), and the split archives of the other formats end in
// digits as well (".7z.001"), so the numeric ones are accepted by shape.
var payloadArchiveExtensions = map[string]bool{
	"rar": true, "zip": true, "7z": true, "001": true,
}

// isPayloadArchive reports whether a file is one of the containers the video
// can arrive inside.
func isPayloadArchive(name string) bool {
	extension := fileExtension(name)
	if payloadArchiveExtensions[extension] {
		return true
	}
	// "name.r00", "name.r01": the rest of a RAR set.
	return len(extension) == 3 && extension[0] == 'r' &&
		extension[1] >= '0' && extension[1] <= '9' && extension[2] >= '0' && extension[2] <= '9'
}

// fileExtension returns the lower-case extension of a file name without the
// dot, "" when it has none.
func fileExtension(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// isSampleFile reports whether a video name marks it as a sample rather than
// the feature: a "Sample" or "Samples" folder anywhere in the path, or a name
// whose last dot-separated word is "sample" ("Movie.2024.sample.mkv"). A title
// that happens to contain the word ("The.Sample.Case.2024.mkv") is not a
// sample: the word would not be the last segment.
func isSampleFile(name string) bool {
	parts := strings.Split(filepath.ToSlash(name), "/")
	if len(parts) == 0 {
		return false
	}
	for _, dir := range parts[:len(parts)-1] {
		switch strings.ToLower(dir) {
		case "sample", "samples":
			return true
		}
	}
	base := parts[len(parts)-1]
	base = strings.TrimSuffix(base, filepath.Ext(base))
	segments := strings.FieldsFunc(strings.ToLower(base), func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ' '
	})
	if len(segments) == 0 {
		return false
	}
	last := segments[len(segments)-1]
	return last == "sample" || last == "samples"
}

// unsafePayloadReason returns why a file list must not be downloaded, "" when
// it is fine. The "no video file" rule applies to series and movies only:
// another payload (a comic, an archive of images) is not a video and has its
// own import.
func unsafePayloadReason(names []string, kind string) string {
	for _, name := range names {
		if unsafePayloadExtensions[fileExtension(name)] {
			return fmt.Sprintf("its payload carries a program or a script (%s)", path.Base(name))
		}
	}
	if kind != "series" && kind != "movie" {
		return ""
	}
	for _, name := range names {
		if isPayloadArchive(name) {
			return ""
		}
		if isVideoExtension(name) && !isSampleFile(name) {
			return ""
		}
	}
	return "its payload holds no video file"
}

// refuseUnsafePayload keeps the release out of the next searches and
// quarantines its link, so the cycle does not read the same file list again.
// The block expires: a corrected torrent of the same episode is taken.
func refuseUnsafePayload(db *Database, release *models.Release, now time.Time) {
	if db == nil || release == nil {
		return
	}
	if err := db.BlocklistUntil(release, unsafePayloadBlockReason, now.Add(unsafePayloadBlockPeriod)); err != nil {
		logging.Debug("could not blocklist a release whose payload is refused",
			"target", releaseTarget(release), "error", err)
	}
	if release.TorrentURL != nil {
		_ = db.QuarantineTorrentLink(*release.TorrentURL, unsafePayloadBlockReason, now, unsafePayloadBlockPeriod)
	}
}
