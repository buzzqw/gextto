package gextto

// web_mediainfo.go holds the MediaInfo side of the API: the probe of one file,
// the backfill of the archived files (targets, probe, stored details) and the
// report the UI shows.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

func runMediaInfoBackfill(ctx context.Context, s *AppState, limit int, onProgress func(done, total int)) (map[string]any, error) {
	targets, err := s.db.MediaInfoBackfillTargets(limit)
	if err != nil {
		return nil, err
	}
	total := len(targets)
	probed := 0
	failed := 0
	skipped := 0
	newDetails := 0
	failedItems := []any{}
	missingItems := []any{}
	missingFiles := []string{}
	probedNames := []string{}
	for index, target := range targets {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		if onProgress != nil {
			onProgress(index, total)
		}
		if _, err := os.Stat(target.Path); err != nil {
			skipped++
			reason := "file does not exist"
			if !os.IsNotExist(err) {
				reason = "access error: " + err.Error()
			}
			missingFiles = append(missingFiles, target.Path+" ("+reason+")")
			missingItems = append(missingItems, map[string]any{
				"path":    target.Path,
				"kind":    target.Kind,
				"series":  target.Series,
				"season":  target.Season,
				"episode": target.Episode,
				"name":    target.Name,
				"year":    target.Year,
				"reason":  reason,
			})
			logging.Debug("media info backfill: file missing, skipped",
				"kind", target.Kind, "series", target.Series, "season", target.Season,
				"episode", target.Episode, "name", target.Name, "path", target.Path)
			continue
		}
		info, probeErr := ProbeBestResult(target.Path)
		if probeErr != nil {
			failed++
			logMediaInfoFailure(target, probeErr.Error(), &failedItems)
			continue
		}
		encoded, err := json.Marshal(info)
		if err != nil {
			return nil, err
		}
		var updated int
		if target.Kind == "movie" {
			updated, err = s.db.SetMovieMediaInfo(target.Name, target.Year, string(encoded))
		} else {
			updated, err = s.db.SetEpisodeMediaInfo(target.Series, derefI64(target.Season), derefI64(target.Episode), string(encoded))
		}
		if err != nil {
			return nil, err
		}
		if updated > 0 {
			probed++
			probedNames = append(probedNames, target.displayName())
			if info.AddsToQuality(ParseQuality(target.Title)) {
				newDetails++
			}
			logging.Debug("MediaInfo analyzed for archived file",
				"kind", target.Kind, "series", target.Series, "season", target.Season,
				"episode", target.Episode, "name", target.Name, "resolution", info.Resolution(),
				"hdr", info.HDR, "bit_depth", info.BitDepth, "path", target.Path)
			if text := mediaInfoMismatchText(target.displayName(), ParseQuality(target.Title), ParseQuality(filepath.Base(target.Path))); text != "" {
				logging.Info(text, "path", target.Path)
			}
		} else {
			failed++
			logMediaInfoFailure(target, "no matching row in the database (episode or movie not found)", &failedItems)
		}
	}
	return map[string]any{
		"ok":            true,
		"candidates":    len(targets),
		"total":         total,
		"canceled":      ctx != nil && ctx.Err() != nil,
		"probed":        probed,
		"analyzed":      probed,
		"new_details":   newDetails,
		"failed":        failed,
		"skipped":       skipped,
		"failed_items":  failedItems,
		"missing_items": missingItems,
		"missing_files": missingFiles,
		"probed_names":  probedNames,
	}, nil
}

// memoryMBFor returns the detected physical memory in MiB (0 when unknown).
// Used to size the qBittorrent disk cache proportionally to the RAM.
func memoryMBFor(*Config) uint64 {
	total, _ := memory_info()
	return total / (1024 * 1024)
}

// MediaInfoGet returns the stored media information of one episode or movie.
func MediaInfoGet(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := r.URL.Query()
	var info map[string]any
	var err error
	if query.Has("series") && query.Has("season") && query.Has("episode") {
		season, seasonErr := strconv.ParseInt(query.Get("season"), 10, 64)
		if seasonErr != nil {
			jsonError(w, http.StatusBadRequest, seasonErr.Error())
			return
		}
		episode, episodeErr := strconv.ParseInt(query.Get("episode"), 10, 64)
		if episodeErr != nil {
			jsonError(w, http.StatusBadRequest, episodeErr.Error())
			return
		}
		info, err = s.db.EpisodeMediaInfo(query.Get("series"), season, episode)
	} else if query.Has("movie") {
		var year *int64
		if query.Has("year") {
			parsed, yearErr := strconv.ParseInt(query.Get("year"), 10, 64)
			if yearErr != nil {
				jsonError(w, http.StatusBadRequest, yearErr.Error())
				return
			}
			year = &parsed
		}
		info, err = s.db.MovieMediaInfo(query.Get("movie"), year)
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "info": info})
}

// MediaProbeInput is the body of a media info probe: the path to inspect.
type MediaProbeInput struct {
	Path string `json:"path"`
}

// MediaInfoProbe runs ffprobe on the given path and reports what it returns.
func MediaInfoProbe(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input MediaProbeInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := strings.TrimSpace(input.Path)
	if path == "" {
		jsonError(w, http.StatusBadRequest, "empty path")
		return
	}
	info := Probe(path)
	if info == nil {
		jsonError(w, http.StatusUnprocessableEntity, "ffprobe unavailable or the file is unreadable")
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "info": info})
}
