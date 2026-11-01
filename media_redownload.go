package gextto

// media_redownload.go holds the body of the "Riscarica" action, shared by the
// HTTP handlers and by the file integrity check: when the check finds a damaged
// file and the automatic re-download is on, the same steps run without a
// request. The damaged copy leaves the library, the release that produced it is
// blocklisted so the same one is not chosen again, and the episode or the movie
// becomes eligible for a new download.

import (
	"fmt"

	"github.com/buzzqw/gextto/internal/logging"
)

// redownloadEpisode puts an episode back in the download queue. With
// avoidPrevious it blocklists the release stored on the row and records the
// pending replacement read by the completion log; keepFiles leaves the archived
// file where it is. It resets the episode row and forces a new gap, so the
// episode is eligible again even when a season pack covers the season.
// It returns the blocklisted title and where the damaged file was moved.
func redownloadEpisode(s *AppState, cfg *Config, series string, season, episode int64, keepFiles, avoidPrevious bool) (blockedTitle, moved string, err error) {
	oldTitle := ""
	if avoidPrevious {
		if release, found, err := s.db.EpisodeCurrentRelease(series, season, episode); err != nil {
			logging.Warn("could not read the release to blocklist before the re-download", "series", series, "season", season, "episode", episode, "error", err)
		} else if found {
			oldTitle = release.Title
			if err := s.db.Blocklist(&release, "corrupt_file"); err != nil {
				logging.Warn("could not blocklist the damaged release; the search may pick it again", "series", series, "season", season, "episode", episode, "error", err)
			} else {
				blockedTitle = release.Title
				logging.Info(fmt.Sprintf("🚫 «%s» S%02dE%02d: the damaged release was blocklisted, a different one will be searched", series, season, episode))
			}
		}
	}
	oldPath := ""
	if avoidPrevious {
		oldPath, _ = s.db.ArchivedEpisodePath(series, season, episode)
	}
	if !keepFiles {
		if archived, err := s.db.ArchivedEpisodePath(series, season, episode); err == nil && archived != "" {
			if avoidPrevious {
				// A damaged file must leave the library even when no trash
				// folder is configured: quarantineCorruptFile renames it in
				// place as a last resort.
				moved = quarantineCorruptFile(archived, cfg)
			} else if trash := trashRoot(cfg); trash != "" {
				if destination, err := MoveToTrash(archived, trash); err != nil {
					logging.Warn("redownload: could not move the file to the trash", "path", archived, "error", err.Error())
				} else {
					moved = destination
				}
			}
		}
	}
	if avoidPrevious {
		_ = s.db.MarkMediaIntegrityReplacement(MediaIntegrityProblem{
			Kind: "episode", Series: series, Season: season, Episode: episode, Path: oldPath,
		}, oldTitle)
	}
	if err := s.db.ResetEpisode(series, season, episode, true); err != nil {
		return blockedTitle, moved, err
	}
	if err := s.db.AddForcedGap(series, season, episode); err != nil {
		return blockedTitle, moved, err
	}
	if avoidPrevious {
		_ = s.db.ClearMediaIntegrityProblem(MediaIntegrityProblem{Kind: "episode", Series: series, Season: season, Episode: episode})
	}
	return blockedTitle, moved, nil
}

// redownloadMovie puts a movie back in the download queue. With avoidPrevious it
// blocklists the release stored on the movie row, quarantines the damaged copy
// and records the pending replacement. It returns the blocklisted title and how
// many movie rows were made eligible again.
func redownloadMovie(s *AppState, cfg *Config, id int64, name string, movieYear int64, avoidPrevious bool) (blockedTitle string, reset int, err error) {
	if avoidPrevious {
		oldTitle := ""
		oldPath := ""
		if release, found, err := s.db.MovieCurrentRelease(id); err != nil {
			logging.Warn("could not read the release to blocklist before the re-download", "movie", name, "error", err)
		} else if found {
			oldTitle = release.Title
			if err := s.db.Blocklist(&release, "corrupt_file"); err != nil {
				logging.Warn("could not blocklist the damaged release; the search may pick it again", "movie", name, "error", err)
			} else {
				blockedTitle = release.Title
				logging.Info(fmt.Sprintf("🚫 «%s»: the damaged release was blocklisted, a different one will be searched", name))
			}
		}
		if archived, err := s.db.MovieProcessedPath(id); err == nil {
			oldPath = archived
			if archived != "" && quarantineCorruptFile(archived, cfg) == "" {
				logging.Warn("redownload: could not move the damaged movie file out of the library", "path", archived)
			}
		}
		_ = s.db.MarkMediaIntegrityReplacement(MediaIntegrityProblem{
			Kind: "movie", MovieName: name, MovieYear: movieYear, Path: oldPath,
		}, oldTitle)
	}
	reset, err = s.db.ResetMovieByName(name)
	if err != nil {
		return blockedTitle, 0, err
	}
	if avoidPrevious {
		_ = s.db.ClearMediaIntegrityProblem(MediaIntegrityProblem{Kind: "movie", MovieName: name, MovieYear: movieYear})
	}
	return blockedTitle, reset, nil
}
