package gextto

// web_calendar.go builds the calendar of the next episode of the enabled series:
// the cache key and TTL that decide when it is rebuilt, and the bounded metadata
// fan-out that orders the entries by air date.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// calendarCacheTTL is how long a calendar is reused. Air dates change once a
// day at most; at 120 s the warm-up rebuilt it every ~3 minutes, two TMDB calls
// per enabled series each time (~40,000 a day for 55 series).
const calendarCacheTTL = 15 * time.Minute

// calendarCacheKey ties the cached calendar to what it is built from: the
// metadata provider, its language and the enabled series with their ids, so
// adding, removing or re-identifying a series rebuilds it at once.
func calendarCacheKey(cfg *Config) string {
	hash := sha256.New()
	meta := seriesMetadataFor(cfg)
	fmt.Fprintf(hash, "%s\x00%s\x00", meta.Source(), cfg.TmdbLanguage())
	for _, series := range cfg.Series {
		if series.Enabled {
			fmt.Fprintf(hash, "%s\x00%s\x00%s\x00", series.Name, series.TmdbID, series.TvdbID)
		}
	}
	return "calendar:" + hex.EncodeToString(hash.Sum(nil)[:8])
}

// CalendarParallel bounds the calendar's concurrent metadata lookups.
const CalendarParallel = 8

func buildCalendar(ctx context.Context, cfg *Config) map[string]any {
	meta := seriesMetadataFor(cfg)
	enabled := []SeriesConfig{}
	for _, series := range cfg.Series {
		if series.Enabled {
			enabled = append(enabled, series)
		}
	}
	items := make([]map[string]any, len(enabled))
	var wg sync.WaitGroup
	// A bounded fan-out: one goroutine per series sent every lookup at once,
	// so each opened its own TLS connection to the provider (55 handshakes for
	// 55 series) instead of sharing a few, and risked its rate limit.
	slots := make(chan struct{}, CalendarParallel)
	for index := range enabled {
		wg.Add(1)
		go func(position int, series SeriesConfig) {
			defer recoverGoroutine("series list")
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			seriesID, err := metadataSeriesID(ctx, meta, &series)
			if err != nil || seriesID == nil {
				return
			}
			episode, err := meta.NextEpisode(ctx, *seriesID)
			if err != nil {
				logging.Debug("calendar lookup failed", "series", series.Name, "source", meta.Source(), "error", err)
				return
			}
			if episode == nil {
				return
			}
			var poster *string
			if path, err := meta.PosterForSeries(ctx, series.Name); err == nil && path != nil {
				value := metadataImageURL(*path, "w154")
				poster = &value
			}
			tmdbID, tvdbID := *seriesID, strings.TrimSpace(series.TvdbID)
			if meta.Source() == "tvdb" {
				tmdbID, tvdbID = strings.TrimSpace(series.TmdbID), *seriesID
			}
			items[position] = map[string]any{
				"series":  series.Name,
				"tmdb_id": tmdbID,
				"tvdb_id": tvdbID,
				"episode": toMap(episode),
				"poster":  poster,
			}
		}(index, enabled[index])
	}
	wg.Wait()

	out := []any{}
	for _, item := range items {
		if item != nil {
			out = append(out, item)
		}
	}
	sortCalendarItems(out)
	return map[string]any{"ok": true, "items": out}
}

func sortCalendarItems(items []any) {
	airDate := func(value any) string {
		entry, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		episode, ok := entry["episode"].(map[string]any)
		if !ok {
			return ""
		}
		text, ok := episode["air_date"].(string)
		if !ok {
			return ""
		}
		return text
	}
	sort.SliceStable(items, func(a, b int) bool {
		left := airDate(items[a])
		right := airDate(items[b])
		if left == "" && right == "" {
			return false
		}
		if left == "" {
			return false
		}
		if right == "" {
			return true
		}
		return left < right
	})
}
