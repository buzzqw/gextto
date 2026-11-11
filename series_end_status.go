package gextto

// series_end_status.go decides whether a monitored series is over, that is
// whether no more episodes will come. One source alone is often late: TMDB
// keeps a cancelled show as "Returning Series" for months. So the verdict
// weighs four signals, strongest first:
//
//  1. the operator's choice on the series page (status_override), which beats
//     every source;
//  2. an announced next episode (TMDB/TVDB next_episode_to_air): a series with
//     a date ahead is not over, whatever a status says;
//  3. the status of the metadata provider (TMDB, or TVDB without a TMDB key);
//  4. TVmaze, asked by TVDB/IMDb id as a second opinion: it records
//     cancellations sooner and has a "To Be Determined" state for shows
//     waiting for a renewal.
//
// The verdict is only a label: an ended series is still searched like any
// other until the operator pauses it.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/tvmaze"
)

// SeriesEndRecord is what the sources said about a series at the last refresh.
type SeriesEndRecord struct {
	// Status is the provider status in TMDB words ("Ended", "Canceled",
	// "Returning Series", "In Production", ...).
	Status      string
	LastAirDate string
	// NextAirDate is the air date of the next announced episode ("" if none).
	NextAirDate string
	// TvmazeStatus is TVmaze's status ("Ended", "Running", "To Be Determined",
	// "In Development"); "" when TVmaze was not asked or does not know it.
	TvmazeStatus string
}

// The values of SeriesConfig.StatusOverride.
const (
	seriesStatusOverrideAuto       = ""
	seriesStatusOverrideEnded      = "ended"
	seriesStatusOverrideContinuing = "continuing"
)

// normalizeSeriesStatusOverride returns the stored form of an override and
// whether the value is one of the accepted ones ("auto" means no override).
func normalizeSeriesStatusOverride(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return seriesStatusOverrideAuto, true
	case seriesStatusOverrideEnded:
		return seriesStatusOverrideEnded, true
	case seriesStatusOverrideContinuing:
		return seriesStatusOverrideContinuing, true
	}
	return "", false
}

// The states of a verdict.
const (
	seriesEndEnded      = "ended"
	seriesEndContinuing = "continuing"
	// seriesEndUncertain: no episode announced and TVmaze waits for a renewal.
	seriesEndUncertain = "uncertain"
	// seriesEndUpcoming: announced, nothing aired yet.
	seriesEndUpcoming = "upcoming"
	seriesEndUnknown  = ""
)

// The sources a verdict can come from (besides "tmdb"/"tvdb").
const (
	seriesEndSourceManual   = "manual"
	seriesEndSourceSchedule = "schedule"
	seriesEndSourceTvmaze   = "tvmaze"
)

// SeriesEndVerdict is the combined answer for one series.
type SeriesEndVerdict struct {
	State string
	// Source is who decided: "manual", "schedule" (an announced episode),
	// "tmdb"/"tvdb" or "tvmaze".
	Source string
	// Disagreement is true when the provider and TVmaze give opposite answers
	// (one says over, the other running): the page says so.
	Disagreement bool
}

func providerStatusEnded(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ended", "canceled", "cancelled":
		return true
	}
	return false
}

func providerStatusRunning(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "returning series", "continuing":
		return true
	}
	return false
}

// seriesEndVerdict combines the override and the stored signals. provider is
// the metadata source of the record ("tmdb" or "tvdb"); today is the date the
// next episode is compared with.
func seriesEndVerdict(override string, record SeriesEndRecord, provider string, today time.Time) SeriesEndVerdict {
	switch override {
	case seriesStatusOverrideEnded:
		return SeriesEndVerdict{State: seriesEndEnded, Source: seriesEndSourceManual}
	case seriesStatusOverrideContinuing:
		return SeriesEndVerdict{State: seriesEndContinuing, Source: seriesEndSourceManual}
	}
	if provider == "" {
		provider = "tmdb"
	}
	tvmazeStatus := strings.ToLower(strings.TrimSpace(record.TvmazeStatus))
	tvmazeEnded := tvmazeStatus == "ended"
	tvmazeRunning := tvmazeStatus == "running" || tvmazeStatus == "to be determined"
	providerEnded := providerStatusEnded(record.Status)
	disagreement := (providerEnded && tvmazeRunning) || (tvmazeEnded && providerStatusRunning(record.Status))
	if next := strings.TrimSpace(record.NextAirDate); next != "" {
		if date, err := time.Parse("2006-01-02", next); err == nil && !date.Before(today.UTC().Truncate(24*time.Hour)) {
			return SeriesEndVerdict{State: seriesEndContinuing, Source: seriesEndSourceSchedule}
		}
	}
	switch {
	case providerEnded:
		return SeriesEndVerdict{State: seriesEndEnded, Source: provider, Disagreement: disagreement}
	case tvmazeEnded:
		return SeriesEndVerdict{State: seriesEndEnded, Source: seriesEndSourceTvmaze, Disagreement: disagreement}
	case seriesNeverAired(record.Status, record.LastAirDate):
		return SeriesEndVerdict{State: seriesEndUpcoming, Source: provider}
	case tvmazeStatus == "to be determined":
		return SeriesEndVerdict{State: seriesEndUncertain, Source: seriesEndSourceTvmaze}
	case providerStatusRunning(record.Status):
		return SeriesEndVerdict{State: seriesEndContinuing, Source: provider}
	case tvmazeStatus == "running":
		return SeriesEndVerdict{State: seriesEndContinuing, Source: seriesEndSourceTvmaze}
	}
	return SeriesEndVerdict{State: seriesEndUnknown}
}

// seriesEndVerdictFor is seriesEndVerdict for a configured series, today.
func seriesEndVerdictFor(cfg *Config, series *SeriesConfig, record SeriesEndRecord) SeriesEndVerdict {
	override := ""
	if series != nil {
		override = series.StatusOverride
	}
	return seriesEndVerdict(override, record, seriesMetadataFor(cfg).Source(), time.Now())
}

// seriesEndRecordFromInfo reads the provider part of a record from a
// SeriesInfo answer (TMDB /tv shape, which the TVDB client reproduces).
func seriesEndRecordFromInfo(info map[string]any) SeriesEndRecord {
	record := SeriesEndRecord{}
	record.Status, _ = info["status"].(string)
	record.LastAirDate, _ = info["last_air_date"].(string)
	if next, ok := info["next_episode_to_air"].(map[string]any); ok {
		record.NextAirDate, _ = next["air_date"].(string)
	}
	record.Status = strings.TrimSpace(record.Status)
	record.LastAirDate = strings.TrimSpace(record.LastAirDate)
	record.NextAirDate = strings.TrimSpace(record.NextAirDate)
	return record
}

// seriesCatalogIDs returns the TheTVDB and IMDb ids of a series, from the
// SeriesInfo answer (TMDB external_ids, or the TVDB record) and the ids stored
// with the series.
func seriesCatalogIDs(info map[string]any, series *SeriesConfig) (int64, string) {
	var tvdbID int64
	imdbID := ""
	parse := func(value any) int64 {
		switch typed := value.(type) {
		case float64:
			return int64(typed)
		case int64:
			return typed
		case int:
			return int64(typed)
		case string:
			parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
			return parsed
		}
		return 0
	}
	if external, ok := info["external_ids"].(map[string]any); ok {
		tvdbID = parse(external["tvdb_id"])
		imdbID, _ = external["imdb_id"].(string)
	}
	if tvdbID <= 0 {
		tvdbID = parse(info["tvdb_id"])
	}
	if tvdbID <= 0 && series != nil {
		tvdbID = parse(series.TvdbID)
	}
	return tvdbID, strings.TrimSpace(imdbID)
}

// tvmazeSeriesStatusEnabled reports whether TVmaze may be asked (on by
// default: it needs no key and receives only catalog ids).
func tvmazeSeriesStatusEnabled(cfg *Config) bool {
	return cfg != nil && settingsBool(cfg, settingTvmazeSeriesStatus, true)
}

// newSeriesTvmazeClient builds the TVmaze client (replaced in tests).
var newSeriesTvmazeClient = func() *tvmaze.Client {
	client := tvmaze.New()
	client.UserAgent = "gextto"
	return client
}

// tvmazePacing keeps the TVmaze calls of a refresh under its public limit
// (20 calls every 10 seconds).
var tvmazePacing = struct {
	sync.Mutex
	last time.Time
	gap  time.Duration
}{gap: 600 * time.Millisecond}

func tvmazeWait(ctx context.Context) error {
	tvmazePacing.Lock()
	defer tvmazePacing.Unlock()
	if wait := tvmazePacing.gap - time.Since(tvmazePacing.last); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	tvmazePacing.last = time.Now()
	return nil
}

// tvmazeSeriesStatus asks TVmaze for the status of a series by id. It returns
// the status, or previous when TVmaze could not be reached (a transient error
// must not wipe what is known); a series TVmaze does not know clears it.
func tvmazeSeriesStatus(ctx context.Context, info map[string]any, series *SeriesConfig, previous string) string {
	tvdbID, imdbID := seriesCatalogIDs(info, series)
	if tvdbID <= 0 && imdbID == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := tvmazeWait(ctx); err != nil {
		return previous
	}
	show, err := newSeriesTvmazeClient().Lookup(ctx, tvdbID, imdbID)
	if errors.Is(err, tvmaze.ErrNotFound) {
		return ""
	}
	if err != nil || show == nil {
		logging.Debug("TVmaze status unavailable", "series", series.Name, "error", err)
		return previous
	}
	return strings.TrimSpace(show.Status)
}

// refreshSeriesEndRecord stores the end-of-series signals of a series from a
// fresh SeriesInfo answer, asking TVmaze too when allowed. previous is the
// stored record (it keeps TVmaze's answer when TVmaze is unreachable).
func refreshSeriesEndRecord(ctx context.Context, cfg *Config, db *Database, series *SeriesConfig, info map[string]any, previous SeriesEndRecord) (SeriesEndRecord, error) {
	record := seriesEndRecordFromInfo(info)
	if raw, ok := info["tvmaze_status"].(string); ok {
		// TVmaze is the provider itself: its answer is already here.
		record.TvmazeStatus = strings.TrimSpace(raw)
	} else if tvmazeSeriesStatusEnabled(cfg) {
		record.TvmazeStatus = tvmazeSeriesStatus(ctx, info, series, previous.TvmazeStatus)
	}
	if db == nil {
		return record, nil
	}
	if err := db.SaveSeriesEndRecord(series.Name, record); err != nil {
		return record, fmt.Errorf("saving the series status: %w", err)
	}
	before := seriesEndVerdict("", previous, seriesMetadataFor(cfg).Source(), time.Now())
	after := seriesEndVerdict("", record, seriesMetadataFor(cfg).Source(), time.Now())
	if before.State != seriesEndEnded && after.State == seriesEndEnded && previous != (SeriesEndRecord{}) {
		logging.Info(fmt.Sprintf("«%s» looks over: %s says no more episodes will come", series.Name, seriesEndSourceName(after.Source)))
	}
	return record, nil
}

// seriesEndSourceName is the name of a verdict source as a person says it.
func seriesEndSourceName(source string) string {
	switch source {
	case "tmdb":
		return "TMDB"
	case "tvdb":
		return "TVDB"
	case seriesEndSourceTvmaze:
		return "TVmaze"
	case seriesEndSourceManual:
		return "the series page"
	case seriesEndSourceSchedule:
		return "the episode calendar"
	}
	return source
}
