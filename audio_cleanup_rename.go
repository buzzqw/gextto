// audio_cleanup_rename.go keeps the name true after the audio cleanup: the
// cleanup removes the audio tracks in other languages but the name was written
// before it, so a file that said [IT+EN] kept saying it forever — the periodic
// library check compares only the prefix of the name and never looks at the
// languages again. The cleanup now gives the file the name its remaining tracks
// deserve, both after an import (the queue) and at night (the library).

package gextto

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// The rename that follows a cleanup needs the archive of the series for
// itself: while the cleanup holds it, a rename reservation is refused. The
// cleanup releases it first, and these bounds cover the other operation that
// may have taken it in the meantime (an import copying a file, the periodic
// library check).
const (
	audioCleanupRenameWait = 30 * time.Second
	audioCleanupRenameStep = 2 * time.Second
)

// renameAfterAudioCleanup renames the file just cleaned to the name its new
// tracks produce and records the new path, returning the path the file has
// now. It returns the same path when there is nothing to rename, the library
// does not know the file, or its series left the library.
func renameAfterAudioCleanup(cfg *Config, db *Database, path string) (string, error) {
	if cfg == nil || db == nil || !cfg.RenameEpisodes || strings.TrimSpace(path) == "" {
		return path, nil
	}
	series, season, episode, title, hash, ok, err := db.ArchivedEpisodeForPath(path)
	if err != nil {
		return path, err
	}
	if ok {
		return renameEpisodeAfterAudioCleanup(cfg, db, path, series, season, episode, title, hash)
	}
	hash, name, year, ok, err := db.ArchivedMovieForPath(path)
	if err != nil {
		return path, err
	}
	if ok {
		return renameMovieAfterAudioCleanup(cfg, db, path, hash, name, year)
	}
	return path, nil
}

// renameEpisodeAfterAudioCleanup renames one archived episode after its
// cleanup. The series must still be in the library: without its template there
// is no name to compute, and the file keeps the one it has. A torrent the
// operator excluded from renaming keeps its file's name too.
func renameEpisodeAfterAudioCleanup(cfg *Config, db *Database, path, seriesName string, season, episode int64, title, hash string) (string, error) {
	series := cfg.FindSeriesByName(seriesName)
	if series == nil {
		return path, nil
	}
	if noRename, err := db.TorrentNoRename(hash); err != nil || noRename {
		return path, nil
	}
	seasonValue, episodeValue := season, episode
	release := models.Release{
		Title:        title,
		Source:       "archive",
		Quality:      MergeQuality(ParseQuality(title), ParseQuality(filepath.Base(path))),
		Kind:         "series",
		Series:       &series.Name,
		Season:       &seasonValue,
		Episode:      &episodeValue,
		EpisodeRange: []int64{episode},
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	target, err := withArchiveRename(series.Name, func() (string, error) {
		return RenameEpisode(context.Background(), path, &release, cfg, tmdb)
	})
	if errors.Is(err, ErrInferiorDuplicate) {
		// A better copy already carries the target name: this file stays where
		// it is, as the import would leave it.
		return path, nil
	}
	if err != nil {
		return path, err
	}
	if target == "" || SamePath(path, target) {
		return path, nil
	}
	if err := db.SetEpisodeArchivePath(cfg, seriesName, season, episode, target); err != nil {
		return target, err
	}
	// The torrent that brought this episode keeps pointing at the name it had
	// before the cleanup: left behind, "the archived copy is gone" was answered
	// for a file that is in the library, and the next add of the same release
	// was taken for a download that never happened.
	if hash != "" {
		if err := db.SetTorrentProcessedPath(hash, target); err != nil {
			return target, err
		}
	}
	logging.Debug("the cleaned file was renamed to match its remaining audio tracks",
		"series", seriesName, "season", season, "episode", episode, "from", path, "to", target)
	return target, nil
}

// renameMovieAfterAudioCleanup does the same for an archived movie, updating
// the path its torrent records.
func renameMovieAfterAudioCleanup(cfg *Config, db *Database, path, hash, name string, year int64) (string, error) {
	if noRename, err := db.TorrentNoRename(hash); err != nil || noRename {
		return path, nil
	}
	release := models.Release{
		Title:        name,
		Source:       "archive",
		Quality:      MergeQuality(ParseQuality(name), ParseQuality(filepath.Base(path))),
		Kind:         "movie",
		Seeders:      -1,
		Peers:        -1,
		DiscoveredAt: time.Now().UTC(),
	}
	if year > 0 {
		release.Year = &year
	}
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	target, err := RenameMovie(context.Background(), path, &release, cfg, tmdb)
	if errors.Is(err, ErrInferiorDuplicate) {
		return path, nil
	}
	if err != nil {
		return path, err
	}
	if target == "" || SamePath(path, target) {
		return path, nil
	}
	if err := db.SetTorrentProcessedPath(hash, target); err != nil {
		return target, err
	}
	logging.Debug("the cleaned movie was renamed to match its remaining audio tracks",
		"from", path, "to", target)
	return target, nil
}

// withArchiveRename reserves the archive of the series and runs the rename,
// waiting a short while for an operation that holds it right now. When the
// archive stays busy the name is left as it is, never forced.
func withArchiveRename(series string, rename func() (string, error)) (string, error) {
	deadline := time.Now().Add(audioCleanupRenameWait)
	for {
		release, reason, ok := TryAcquireArchiveRename(series)
		if ok {
			defer release()
			return rename()
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the archive is busy: %s", archiveRenameBusyText(reason))
		}
		time.Sleep(audioCleanupRenameStep)
	}
}
