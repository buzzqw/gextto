package gextto

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	subtitleprovider "github.com/buzzqw/gextto/internal/subtitles"
)

const (
	// A title whose subtitle could not be found is left alone for this long:
	// "try every cycle" must not turn into a request storm against the provider.
	subtitleAutoCooldown = 6 * time.Hour
	// Bound the work of one cycle, so a large library cannot monopolise the daemon.
	subtitleAutoMaxPerCycle = 40
)

type subtitleAutoState struct {
	mu      sync.Mutex
	last    map[string]time.Time
	running bool
}

func newSubtitleAutoState() *subtitleAutoState {
	return &subtitleAutoState{last: map[string]time.Time{}}
}

func (s *subtitleAutoState) begin() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *subtitleAutoState) end() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

func (s *subtitleAutoState) due(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.last[key]
	return !ok || now.Sub(last) >= subtitleAutoCooldown
}

func (s *subtitleAutoState) mark(key string, now time.Time) {
	s.mu.Lock()
	s.last[key] = now
	s.mu.Unlock()
}

// subtitleSidecarPresent reports whether a compatible subtitle is already next
// to the video. Names are checked literally: a title may contain glob
// metacharacters, and forced/SDH preferences must match the existing sidecar.
func subtitleSidecarPresent(videoPath, language string, prefs subtitlePreferences) bool {
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	dir := filepath.Dir(videoPath)
	present := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	// An untagged sidecar is a complete, non-SDH subtitle.
	if prefs.Forced != "yes" && prefs.SDH != "yes" && present(stem+".srt") {
		return true
	}
	// The name Gextto writes uses the normalised code; a sidecar placed by
	// another program may carry the code as configured. Both count.
	codes := []string{subtitleLanguageSuffix(language)}
	if raw := strings.ToLower(strings.TrimSpace(language)); raw != "" && raw != codes[0] {
		codes = append(codes, raw)
	}
	for _, code := range codes {
		if subtitleSidecarVariantPresent(present, stem+"."+code, prefs) {
			return true
		}
	}
	return false
}

// subtitleSidecarVariantPresent checks the forced/SDH variants of one
// language base name that the preferences accept.
func subtitleSidecarVariantPresent(present func(string) bool, base string, prefs subtitlePreferences) bool {
	for _, forced := range []bool{false, true} {
		if prefs.Forced == "yes" && !forced || prefs.Forced == "no" && forced {
			continue
		}
		for _, sdh := range []bool{false, true} {
			if prefs.SDH == "yes" && !sdh || prefs.SDH == "no" && sdh {
				continue
			}
			name := base
			if forced {
				name += ".forced"
			}
			if sdh {
				name += ".sdh"
			}
			if present(name + ".srt") {
				return true
			}
		}
	}
	return false
}

// subtitleAutoTargets collects the archived titles whose series or movie
// configuration asks for subtitles. Only the final video paths are kept: the
// provider search and the write happen later, once a language is due.
func subtitleAutoTargets(cfg *Config, db *Database) ([]subtitleTarget, error) {
	targets := []subtitleTarget{}
	index, err := db.AllSeriesEpisodes()
	if err != nil {
		return nil, err
	}
	for _, series := range cfg.Series {
		preferred := strings.TrimSpace(series.Subtitle)
		if preferred == "" || strings.EqualFold(preferred, "any") {
			continue
		}
		for _, episode := range index.Episodes(series.Name, nil) {
			if episode.ArchivePath == nil {
				continue
			}
			path := strings.TrimSpace(*episode.ArchivePath)
			if path == "" || !subtitleVideoExtension(path) {
				continue
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			targets = append(targets, subtitleTarget{
				Kind: "episode", Label: fmt.Sprintf("%s S%02dE%02d", series.Name, episode.Season, episode.Episode),
				Title: series.Name, TMDBID: series.TmdbID, Season: episode.Season, Episode: episode.Episode,
				VideoPath: path, SubtitlePreference: preferred,
			})
		}
	}
	copies, err := db.DownloadedMovieCopies()
	if err != nil {
		return nil, err
	}
	for _, copy := range copies {
		var movie *MovieConfig
		for i := range cfg.Movies {
			if cfg.Movies[i].ID == copy.ID || strings.EqualFold(strings.TrimSpace(cfg.Movies[i].Name), strings.TrimSpace(copy.Name)) {
				movie = &cfg.Movies[i]
				break
			}
		}
		if movie == nil {
			continue
		}
		preferred := strings.TrimSpace(movie.Subtitle)
		if preferred == "" || strings.EqualFold(preferred, "any") {
			continue
		}
		path := strings.TrimSpace(copy.Path)
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path, err = resolveMovieCompletedFile(path)
			if err != nil || path == "" {
				continue
			}
		}
		if !subtitleVideoExtension(path) {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		target := subtitleTarget{
			Kind: "movie", Label: movie.Name, Title: movie.Name, TMDBID: movie.TmdbID,
			VideoPath: path, SubtitlePreference: preferred,
		}
		if year, err := parsePositiveYear(movie.Year); err == nil {
			target.Year = year
		}
		targets = append(targets, target)
	}
	return targets, nil
}

// runAutomaticSubtitles writes the subtitles requested by the series and movie
// configurations. Integration settings only configure the provider: the
// languages come from each title, never from a global override.
func runAutomaticSubtitles(ctx context.Context, cfg *Config, db *Database, state *subtitleAutoState, now time.Time) (int, error) {
	if cfg == nil || db == nil || cfg.DryRun {
		return 0, nil
	}
	prefs := subtitlePreferencesFrom(cfg)
	if !prefs.Enabled {
		return 0, nil
	}
	if _, _, err := subtitleProviderClient(cfg); err != nil {
		return 0, err
	}
	targets, err := subtitleAutoTargets(cfg, db)
	if err != nil {
		return 0, err
	}
	added, attempts := 0, 0
	for _, target := range targets {
		for _, language := range subtitleLanguagesFor(cfg, target) {
			if attempts >= subtitleAutoMaxPerCycle {
				return added, nil
			}
			if subtitleSidecarPresent(target.VideoPath, language, prefs) {
				continue
			}
			key := target.VideoPath + "|" + language
			if !state.due(key, now) {
				continue
			}
			attempts++
			search := target
			search.SubtitlePreference = language
			hash, size, err := subtitleprovider.HashFile(target.VideoPath)
			if err == nil {
				search.OSHash, search.SizeBytes = hash, size
			}
			candidates, err := searchSubtitleCandidates(ctx, cfg, db, search)
			if err == nil && len(candidates) > 0 {
				best := candidates[0]
				// Unattended writes need a strong match: a plain title hit is
				// left to the preview, where the operator decides.
				if best.HashMatch {
					_, err = applySubtitle(ctx, cfg, db, target, best.FileID, language, best.IsForced(), best.IsSDH(), false)
					if err == nil {
						added++
						logging.Info("Added a subtitle next to a video", "title", target.Label, "language", language)
						continue
					}
				}
			}
			if err != nil {
				logging.Debug("Automatic subtitle search did not find a file", "title", target.Label, "language", language, "error", err.Error())
			}
			state.mark(key, now)
		}
	}
	return added, nil
}
