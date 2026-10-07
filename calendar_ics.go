package gextto

// calendar_ics.go publishes an iCalendar feed (RFC 5545) for Thunderbird,
// Google Calendar or the phone's calendar app.
//
// Gextto is built for an Italian audience: series often reach Italian
// releases months or years after the original broadcast, so the original air
// date is not when an episode becomes available. The feed therefore shows:
//   - 📥 what actually arrived in the library in the last 30 days, on the day
//     it arrived (always true);
//   - 📺 the original broadcast of the current season, from a week ago to two
//     months ahead, clearly labelled as such;
//   - 🎬 the release of monitored movies in the country of the TMDB language
//     (Italy by default: digital, then home video, then cinema), falling back
//     to the original release, labelled, when that country has no date.

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	icsPastDays    = 7
	icsFutureDays  = 60
	icsArrivalDays = 30
	icsCacheTTL    = 30 * time.Minute
)

// icsEvent is one all-day calendar entry.
type icsEvent struct {
	UID         string
	Date        time.Time
	Summary     string
	Description string
}

var icsCache struct {
	sync.Mutex
	body    []byte
	builtAt time.Time
	lang    string
}

// CalendarICS serves GET /feed/calendar.ics.
func CalendarICS(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if cfg.TmdbAPIKey == nil {
		http.Error(w, "TMDB API key is not configured", http.StatusConflict)
		return
	}
	lang := v2Language(s)
	tr := func(italian string) string { return uiText(s, italian) }
	icsCache.Lock()
	body := icsCache.body
	if body == nil || time.Since(icsCache.builtAt) > icsCacheTTL || icsCache.lang != lang {
		events := icsCollectEvents(r.Context(), cfg, s.db, tr, time.Now())
		body = []byte(renderICS(events, tr("Gextto — uscite"), time.Now()))
		icsCache.body, icsCache.builtAt, icsCache.lang = body, time.Now(), lang
	}
	icsCache.Unlock()
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="gextto.ics"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}

// tr translates the Italian texts into the interface language.
func icsCollectEvents(ctx context.Context, cfg *Config, db *Database, tr func(string) string, now time.Time) []icsEvent {
	tmdb := NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, 0, -icsPastDays)
	to := today.AddDate(0, 0, icsFutureDays)
	inWindow := func(date string) (time.Time, bool) {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(date))
		if err != nil || parsed.Before(from) || parsed.After(to) {
			return time.Time{}, false
		}
		return parsed, true
	}

	country := icsCountry(cfg.TmdbLanguage())
	var mu sync.Mutex
	events := []icsEvent{}
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for _, series := range cfg.Series {
		if !series.Enabled {
			continue
		}
		wg.Add(1)
		go func(series SeriesConfig) {
			defer recoverGoroutine("calendar feed")
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			tmdbID := strings.TrimSpace(series.TmdbID)
			if tmdbID == "" {
				resolved, err := tmdb.ResolveSeriesID(ctx, series.Name)
				if err != nil || resolved == nil {
					return
				}
				tmdbID = *resolved
			}
			next, err := tmdb.NextEpisode(ctx, tmdbID)
			if err != nil {
				logging.Debug("calendar feed: TMDB lookup failed", "series", series.Name, "error", err)
				return
			}
			seasons := map[int64]bool{}
			if next != nil && next.SeasonNumber != nil {
				seasons[*next.SeasonNumber] = true
			}
			if latest := icsLatestArchivedSeason(db, series.Name); latest > 0 {
				seasons[latest] = true
			}
			downloaded := icsDownloadedEpisodes(db, series.Name)
			found := []icsEvent{}
			for season := range seasons {
				episodes, err := tmdb.SeasonEpisodes(ctx, tmdbID, season)
				if err != nil {
					continue
				}
				for _, episode := range episodes {
					if episode.AirDate == nil || episode.SeasonNumber == nil || episode.EpisodeNumber == nil {
						continue
					}
					date, ok := inWindow(*episode.AirDate)
					if !ok {
						continue
					}
					code := fmt.Sprintf("S%02dE%02d", *episode.SeasonNumber, *episode.EpisodeNumber)
					summary := "📺 " + series.Name + " " + code
					if episode.Name != nil && strings.TrimSpace(*episode.Name) != "" {
						summary += " · " + strings.TrimSpace(*episode.Name)
					}
					description := tr("Prima messa in onda originale (TMDB). La versione italiana può arrivare molto più tardi: quando gextto la scarica compare come «📥».")
					if downloaded[[2]int64{*episode.SeasonNumber, *episode.EpisodeNumber}] {
						summary = "✓ " + summary
						description = tr("Prima messa in onda originale (TMDB). Già in libreria.")
					}
					found = append(found, icsEvent{
						UID:         fmt.Sprintf("gextto-tv-%s-%s@gextto", tmdbID, code),
						Date:        date,
						Summary:     summary,
						Description: description,
					})
				}
			}
			mu.Lock()
			events = append(events, found...)
			mu.Unlock()
		}(series)
	}
	for _, movie := range cfg.Movies {
		if !movie.Enabled || strings.TrimSpace(movie.TmdbID) == "" {
			continue
		}
		wg.Add(1)
		go func(movie MovieConfig) {
			defer recoverGoroutine("calendar feed")
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			summary := "🎬 " + movie.Name
			var description, day string
			if localDay, kind, found, err := tmdb.MovieReleaseDate(ctx, movie.TmdbID, country); err == nil && found {
				day = localDay
				description = icsMovieReleaseText(tr, kind) + " · " + country
			} else {
				details, err := tmdb.MovieDetails(ctx, movie.TmdbID)
				if err != nil || details == nil || details.ReleaseDate == nil {
					return
				}
				day = *details.ReleaseDate
				summary += " " + tr("(uscita originale)")
				description = tr("Uscita originale (TMDB): nel paese della lingua TMDB non c'è ancora una data.") + " · " + country
			}
			date, ok := inWindow(day)
			if !ok {
				return
			}
			mu.Lock()
			events = append(events, icsEvent{
				UID:         fmt.Sprintf("gextto-movie-%s@gextto", strings.TrimSpace(movie.TmdbID)),
				Date:        date,
				Summary:     summary,
				Description: description,
			})
			mu.Unlock()
		}(movie)
	}
	wg.Wait()
	events = append(events, icsArrivals(db, tr, today.AddDate(0, 0, -icsArrivalDays))...)
	sort.SliceStable(events, func(a, b int) bool {
		if !events[a].Date.Equal(events[b].Date) {
			return events[a].Date.Before(events[b].Date)
		}
		return events[a].Summary < events[b].Summary
	})
	return events
}

// icsCountry takes the region of the TMDB language ("it-IT" → "IT").
func icsCountry(language string) string {
	if _, region, ok := strings.Cut(strings.TrimSpace(language), "-"); ok && len(region) == 2 {
		return strings.ToUpper(region)
	}
	return "IT"
}

func icsMovieReleaseText(tr func(string) string, kind string) string {
	switch kind {
	case "digital":
		return tr("Uscita digitale (TMDB)")
	case "physical":
		return tr("Uscita home video (TMDB)")
	default:
		return tr("Uscita al cinema (TMDB)")
	}
}

// icsArrivals lists what entered the library since from, on the day it
// arrived.
func icsArrivals(db *Database, tr func(string) string, from time.Time) []icsEvent {
	if db == nil {
		return nil
	}
	events := []icsEvent{}
	since := from.Format("2006-01-02")
	day := func(value string) (time.Time, bool) {
		if len(value) < 10 {
			return time.Time{}, false
		}
		parsed, err := time.Parse("2006-01-02", value[:10])
		return parsed, err == nil
	}
	if rows, err := db.db.Query("SELECT s.name, e.season, e.episode, e.downloaded_at FROM episodes e JOIN series s ON s.id=e.series_id WHERE e.downloaded_at IS NOT NULL AND substr(e.downloaded_at,1,10) >= ?1 AND e.episode > 0", since); err == nil {
		for rows.Next() {
			var name, downloadedAt string
			var season, episode int64
			if rows.Scan(&name, &season, &episode, &downloadedAt) != nil {
				continue
			}
			if date, ok := day(downloadedAt); ok {
				code := fmt.Sprintf("S%02dE%02d", season, episode)
				events = append(events, icsEvent{
					UID:         fmt.Sprintf("gextto-arrived-%s-%s@gextto", icsUIDPart(name), code),
					Date:        date,
					Summary:     "📥 " + name + " " + code,
					Description: tr("Arrivato in libreria."),
				})
			}
		}
		rows.Close()
	}
	if rows, err := db.db.Query("SELECT name, COALESCE(year,0), downloaded_at FROM movies WHERE downloaded_at IS NOT NULL AND removed_at IS NULL AND substr(downloaded_at,1,10) >= ?1", since); err == nil {
		for rows.Next() {
			var name, downloadedAt string
			var year int64
			if rows.Scan(&name, &year, &downloadedAt) != nil {
				continue
			}
			if date, ok := day(downloadedAt); ok {
				title := name
				if year > 0 {
					title = fmt.Sprintf("%s (%d)", name, year)
				}
				events = append(events, icsEvent{
					UID:         fmt.Sprintf("gextto-arrived-movie-%s-%d@gextto", icsUIDPart(name), year),
					Date:        date,
					Summary:     "📥 " + title,
					Description: tr("Arrivato in libreria."),
				})
			}
		}
		rows.Close()
	}
	return events
}

// icsUIDPart keeps a name usable inside a UID.
func icsUIDPart(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(value) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
		} else if out.Len() > 0 && !strings.HasSuffix(out.String(), "-") {
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-")
}

func icsDownloadedEpisodes(db *Database, series string) map[[2]int64]bool {
	out := map[[2]int64]bool{}
	if db == nil {
		return out
	}
	rows, err := db.db.Query("SELECT e.season, e.episode FROM episodes e JOIN series s ON s.id=e.series_id WHERE lower(s.name)=lower(?1) AND e.downloaded_at IS NOT NULL", series)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var season, episode int64
		if rows.Scan(&season, &episode) == nil {
			out[[2]int64{season, episode}] = true
		}
	}
	return out
}

func icsLatestArchivedSeason(db *Database, series string) int64 {
	if db == nil {
		return 0
	}
	var season int64
	_ = db.db.QueryRow("SELECT COALESCE(MAX(e.season),0) FROM episodes e JOIN series s ON s.id=e.series_id WHERE lower(s.name)=lower(?1) AND e.downloaded_at IS NOT NULL", series).Scan(&season)
	return season
}

// icsEscape escapes a TEXT value (RFC 5545 §3.3.11).
func icsEscape(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)
	return replacer.Replace(value)
}

// icsFold splits a content line at 75 octets without breaking a UTF-8
// character (RFC 5545 §3.1).
func icsFold(line string) string {
	if len(line) <= 75 {
		return line + "\r\n"
	}
	var out strings.Builder
	limit := 75
	for len(line) > limit {
		cut := limit
		for cut > 0 && (line[cut]&0xC0) == 0x80 {
			cut--
		}
		out.WriteString(line[:cut])
		out.WriteString("\r\n ")
		line = line[cut:]
		limit = 74
	}
	out.WriteString(line)
	out.WriteString("\r\n")
	return out.String()
}

func renderICS(events []icsEvent, name string, now time.Time) string {
	var out strings.Builder
	write := func(line string) { out.WriteString(icsFold(line)) }
	write("BEGIN:VCALENDAR")
	write("VERSION:2.0")
	write("PRODID:-//gextto//calendar//IT")
	write("CALSCALE:GREGORIAN")
	write("METHOD:PUBLISH")
	write("X-WR-CALNAME:" + icsEscape(name))
	write("X-PUBLISHED-TTL:PT1H")
	write("REFRESH-INTERVAL;VALUE=DURATION:PT1H")
	stamp := now.UTC().Format("20060102T150405Z")
	for _, event := range events {
		write("BEGIN:VEVENT")
		write("UID:" + icsEscape(event.UID))
		write("DTSTAMP:" + stamp)
		write("DTSTART;VALUE=DATE:" + event.Date.Format("20060102"))
		write("DTEND;VALUE=DATE:" + event.Date.AddDate(0, 0, 1).Format("20060102"))
		write("SUMMARY:" + icsEscape(event.Summary))
		if event.Description != "" {
			write("DESCRIPTION:" + icsEscape(event.Description))
		}
		write("TRANSP:TRANSPARENT")
		write("END:VEVENT")
	}
	write("END:VCALENDAR")
	return out.String()
}
