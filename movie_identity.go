package gextto

// movie_identity.go refuses a payload that is not the film the release
// promises. The title match reads the release name at decision time; from then
// on the payload itself says which film it is, in two moments:
//
//   - the torrent's own name, known as soon as the metadata arrives and before
//     a piece is downloaded (the same moment a season pack is checked against
//     the season it promises);
//   - the real duration of the completed video, read before the import, next to
//     the runtime the film has on TMDB.
//
// A tracker can publish another film under the requested name — the report of a
// film of the list replaced by another — and both moments refuse it instead of
// importing it and naming it after the requested film.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// movieIdentityBlockReason is the code recorded in the blocklist. It reaches
// the Blocklist page, where every reason is shown by its code.
const movieIdentityBlockReason = "movie_identity_mismatch"

// movieIdentityMismatch reports why the payload the torrent really is does not
// belong to the monitored film, or "" when the check cannot tell (a name that
// is empty, an episode, a film that is not in the list).
//
// The check is the same comparison the decision made, applied to the name the
// torrent carries: a release name opens with the film's title, and a name that
// opens with neither this film nor another monitored one is not a copy of it.
func movieIdentityMismatch(cfg *Config, release *models.Release, torrentName string) string {
	if cfg == nil || release == nil || release.Kind != "movie" {
		return ""
	}
	name := strings.TrimSpace(torrentName)
	if name == "" {
		return ""
	}
	// An episodic name is not a film at all; the completion check will judge
	// the file, and a wrong verdict here would refuse a legitimate payload.
	if parsed := parseReleaseTitle(name); parsed.season != nil {
		return ""
	}
	film := cfg.FindMovieMatchManual(release.Title, release.Year)
	if film == nil {
		return ""
	}
	year := releaseYear(name)
	if other := cfg.FindMovieMatch(name, year); other != nil && other.ID != film.ID {
		return fmt.Sprintf("it is another film (%s)", other.Name)
	}
	if movieTitleSpecificity(film, name) == 0 {
		return "its name does not carry this film's title"
	}
	return ""
}

// movieRuntimeTolerance is how far the duration of a real file may sit from the
// runtime TMDB reports for the film: the same cut, plus or minus the seconds of
// credits. It is generous on purpose — an extended edition is still the film —
// and it is a floor of 15 minutes, so a short film is not judged on a rounding.
func movieRuntimeTolerance(expectedSeconds int64) int64 {
	tolerance := expectedSeconds * 12 / 100
	if minimum := int64(15 * 60); tolerance < minimum {
		tolerance = minimum
	}
	return tolerance
}

// movieRuntimeMismatch reports why the completed video cannot be the film the
// release promises, or "" when the check cannot tell: no probe, a film without
// a TMDB runtime, a duration the streams do not carry.
func movieRuntimeMismatch(ctx context.Context, cfg *Config, release *models.Release, video string, tmdb *TmdbClient) string {
	if cfg == nil || release == nil || release.Kind != "movie" || !probeAvailable() {
		return ""
	}
	film := cfg.FindMovieMatchManual(release.Title, release.Year)
	if film == nil || strings.TrimSpace(film.TmdbID) == "" {
		return ""
	}
	expectedMinutes, err := tmdb.MovieRuntimeMinutes(ctx, film.TmdbID)
	if err != nil {
		logging.Debug("could not read the film's runtime from TMDB", "movie", film.Name, "error", err.Error())
		return ""
	}
	if expectedMinutes <= 0 {
		return ""
	}
	info, err := probeFile(video)
	if err != nil || info.RuntimeSeconds <= 0 {
		return ""
	}
	expected := expectedMinutes * 60
	difference := info.RuntimeSeconds - expected
	if difference < 0 {
		difference = -difference
	}
	if difference <= movieRuntimeTolerance(expected) {
		return ""
	}
	return fmt.Sprintf("the file runs %s, the film %s",
		logDuration(time.Duration(info.RuntimeSeconds)*time.Second),
		logDuration(time.Duration(expected)*time.Second))
}

// tevRejectWrongMovie refuses a payload that is not the film: the release is
// blocklisted, the acquisition is marked failed and the payload is discarded,
// so the film stays in the queue and the next search looks for another copy.
func tevRejectWrongMovie(cfg *Config, db *Database, torrents TorrentSession, event *models.TorrentEvent, release *models.Release, reason string) error {
	if err := db.Blocklist(release, movieIdentityBlockReason); err != nil {
		logging.Warn("could not blocklist the payload that is not the film", "hash", event.Hash, "error", err)
	}
	if err := db.MarkTorrentError(event.Hash, reason); err != nil {
		return err
	}
	tevDiscardCompletedSource(cfg, db, torrents, event, reason)
	logging.Warn(fmt.Sprintf("🗑️ %s: the download was discarded, %s; the search goes on",
		logTarget(release), reason), "hash", event.Hash)
	return nil
}
