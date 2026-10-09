package gextto

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// Acquisition history: every INFO/WARN/ERROR log line about a torrent (a line
// with a hash field) is also stored as an event of that acquisition, i.e. the
// path of one download from its start to the library. The log line carries the
// short ID `acq: 7f3a2c` (logging.AcqID); the events table keeps the story
// readable after the log rotates and after the torrent leaves the session.

// databaseAcquisitionSchema stores the events. Identity columns (kind, title,
// series, season, episode) are copied from torrent_meta when the event is
// written, so the story survives the removal of the torrent row.
const databaseAcquisitionSchema = `CREATE TABLE IF NOT EXISTS acquisition_events (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                hash TEXT NOT NULL,
                first_at TEXT NOT NULL,
                at TEXT NOT NULL,
                repeat INTEGER NOT NULL DEFAULT 1,
                level TEXT NOT NULL,
                message TEXT NOT NULL,
                fields TEXT NOT NULL DEFAULT '',
                kind TEXT NOT NULL DEFAULT '',
                title TEXT NOT NULL DEFAULT '',
                series_name TEXT NOT NULL DEFAULT '',
                season INTEGER,
                episode INTEGER
            );
            CREATE INDEX IF NOT EXISTS idx_acquisition_events_hash ON acquisition_events(hash, id);
            CREATE INDEX IF NOT EXISTS idx_acquisition_events_series ON acquisition_events(series_name, season, episode);
            CREATE INDEX IF NOT EXISTS idx_acquisition_events_at ON acquisition_events(at);`

const (
	// acquisitionRetention is how long events are kept.
	acquisitionRetention = 180 * 24 * time.Hour
	// acquisitionMaxPerHash caps one acquisition's story: a torrent that keeps
	// failing in different ways must not grow the table without bound.
	acquisitionMaxPerHash = 300
	// acquisitionQueueSize bounds the events waiting to be written; beyond it
	// they are dropped rather than slowing the logger down.
	acquisitionQueueSize  = 1024
	acquisitionTimeLayout = "2006-01-02 15:04:05"
)

// AcquisitionEvent is one stored step of an acquisition.
type AcquisitionEvent struct {
	ID         int64  `json:"id"`
	Hash       string `json:"hash"`
	AcqID      string `json:"acq"`
	FirstAt    string `json:"first_at"`
	At         string `json:"at"`
	Repeat     int64  `json:"repeat"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	Fields     string `json:"fields"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	SeriesName string `json:"series_name"`
	Season     *int64 `json:"season"`
	Episode    *int64 `json:"episode"`
}

// LevelClass is the lower-case level, used as a CSS class suffix.
func (e AcquisitionEvent) LevelClass() string { return strings.ToLower(e.Level) }

// EpisodeLabel names what the event is about inside its series: S01E03, a
// whole season (S01) or, for a movie, its title.
func (e AcquisitionEvent) EpisodeLabel() string {
	switch {
	case e.Season != nil && e.Episode != nil:
		return fmt.Sprintf("S%02dE%02d", *e.Season, *e.Episode)
	case e.Season != nil:
		return fmt.Sprintf("S%02d", *e.Season)
	default:
		return e.Title
	}
}

// RecordAcquisitionEvent stores one event. An event equal to the last one of
// the same acquisition (same level, message and fields) only bumps its repeat
// count and time: retries logged every few minutes stay one row.
func (d *Database) RecordAcquisitionEvent(event logging.Event) error {
	hash := strings.ToLower(strings.TrimSpace(event.Hash))
	if hash == "" {
		return nil
	}
	at := event.At.Format(acquisitionTimeLayout)
	level := event.Level.String()
	var lastID int64
	var lastLevel, lastMessage, lastFields string
	err := d.db.QueryRow(`SELECT id, level, message, fields FROM acquisition_events WHERE hash=?1 ORDER BY id DESC LIMIT 1`, hash).
		Scan(&lastID, &lastLevel, &lastMessage, &lastFields)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && lastLevel == level && lastMessage == event.Message && lastFields == event.Fields {
		_, err = d.db.Exec(`UPDATE acquisition_events SET repeat=repeat+1, at=?2 WHERE id=?1`, lastID, at)
		return err
	}
	var kind, title, name, seriesName sql.NullString
	var season, episode sql.NullInt64
	metaErr := d.db.QueryRow(`SELECT kind, title, name, series_name, season, episode FROM torrent_meta WHERE hash=?1`, hash).
		Scan(&kind, &title, &name, &seriesName, &season, &episode)
	if metaErr != nil && metaErr != sql.ErrNoRows {
		return metaErr
	}
	if strings.TrimSpace(title.String) == "" {
		title = name
	}
	_, err = d.db.Exec(`INSERT INTO acquisition_events(hash, first_at, at, level, message, fields, kind, title, series_name, season, episode)
		VALUES(?1, ?2, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10)`,
		hash, at, level, event.Message, event.Fields, kind.String, title.String, seriesName.String, season, episode)
	return err
}

// PruneAcquisitionEvents drops events older than the retention and trims each
// acquisition to its most recent acquisitionMaxPerHash rows.
func (d *Database) PruneAcquisitionEvents(now time.Time) (int64, error) {
	cutoff := now.Add(-acquisitionRetention).Format(acquisitionTimeLayout)
	result, err := d.db.Exec(`DELETE FROM acquisition_events WHERE at < ?1`, cutoff)
	if err != nil {
		return 0, err
	}
	removed, _ := result.RowsAffected()
	result, err = d.db.Exec(`DELETE FROM acquisition_events WHERE id IN (
		SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY hash ORDER BY id DESC) AS position FROM acquisition_events)
		WHERE position > ?1)`, acquisitionMaxPerHash)
	if err != nil {
		return removed, err
	}
	trimmed, _ := result.RowsAffected()
	return removed + trimmed, nil
}

// AcquisitionEvents returns the story of one torrent, oldest first.
func (d *Database) AcquisitionEvents(hash string) ([]AcquisitionEvent, error) {
	return d.queryAcquisitionEvents(`WHERE hash=?1 ORDER BY id`, strings.ToLower(strings.TrimSpace(hash)))
}

// AcquisitionEventsForTitle returns the most recent events of a series (or of
// one season/episode when given), newest first, at most limit rows. A season
// pack has no episode: it is included in the story of each of its episodes.
func (d *Database) AcquisitionEventsForTitle(series string, season, episode *int64, limit int) ([]AcquisitionEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	where := `WHERE series_name=?1 COLLATE NOCASE`
	args := []any{strings.TrimSpace(series)}
	if season != nil {
		args = append(args, *season)
		where += ` AND season=?2`
		if episode != nil {
			args = append(args, *episode)
			where += ` AND (episode=?3 OR episode IS NULL)`
		}
	}
	args = append(args, limit)
	return d.queryAcquisitionEvents(where+` ORDER BY id DESC LIMIT ?`+strconv.Itoa(len(args)), args...)
}

// AcquisitionEventsByAcqID returns the story of the acquisition whose short ID
// (as printed in the log) is acq, oldest first.
func (d *Database) AcquisitionEventsByAcqID(acq string) ([]AcquisitionEvent, error) {
	acq = strings.ToLower(strings.TrimSpace(acq))
	if acq == "" {
		return nil, nil
	}
	rows, err := d.db.Query(`SELECT DISTINCT hash FROM acquisition_events`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	match := ""
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		if logging.AcqID(hash) == acq {
			match = hash
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	_ = rows.Close()
	if match == "" {
		return nil, nil
	}
	return d.AcquisitionEvents(match)
}

func (d *Database) queryAcquisitionEvents(clause string, args ...any) ([]AcquisitionEvent, error) {
	rows, err := d.db.Query(`SELECT id, hash, first_at, at, repeat, level, message, fields, kind, title, series_name, season, episode
		FROM acquisition_events `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []AcquisitionEvent
	for rows.Next() {
		var event AcquisitionEvent
		var season, episode sql.NullInt64
		if err := rows.Scan(&event.ID, &event.Hash, &event.FirstAt, &event.At, &event.Repeat, &event.Level, &event.Message,
			&event.Fields, &event.Kind, &event.Title, &event.SeriesName, &season, &episode); err != nil {
			return nil, err
		}
		if season.Valid {
			event.Season = &season.Int64
		}
		if episode.Valid {
			event.Episode = &episode.Int64
		}
		event.AcqID = logging.AcqID(event.Hash)
		events = append(events, event)
	}
	return events, rows.Err()
}

// acquisitionRecorder writes the events on its own goroutine: the logger hands
// them over without waiting for SQLite (a single connection, possibly busy in
// the very transaction that is logging).
type acquisitionRecorder struct {
	db      *Database
	queue   chan logging.Event
	done    chan struct{}
	dropped atomic.Uint64
	stop    sync.Once
	// mu guards closed: the logger may still hold the sink for a moment after
	// it is removed, and a send on the closed queue would panic.
	mu     sync.RWMutex
	closed bool
}

// StartAcquisitionRecorder installs the event sink that stores the acquisition
// history in db, prunes old events, and returns the function that flushes the
// queue and removes the sink (call it before closing db).
func StartAcquisitionRecorder(db *Database) func() {
	recorder := &acquisitionRecorder{db: db, queue: make(chan logging.Event, acquisitionQueueSize), done: make(chan struct{})}
	go recorder.run()
	logging.SetEventSink(recorder.offer)
	return recorder.close
}

func (r *acquisitionRecorder) offer(event logging.Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return
	}
	select {
	case r.queue <- event:
	default:
		r.dropped.Add(1)
	}
}

func (r *acquisitionRecorder) run() {
	defer close(r.done)
	if removed, err := r.db.PruneAcquisitionEvents(time.Now()); err != nil {
		logging.Warn("acquisition history: pruning failed", "error", err)
	} else if removed > 0 {
		logging.Debug("acquisition history pruned", "removed", removed)
	}
	prune := time.NewTicker(24 * time.Hour)
	defer prune.Stop()
	for {
		select {
		case event, ok := <-r.queue:
			if !ok {
				return
			}
			if err := r.db.RecordAcquisitionEvent(event); err != nil {
				// No hash field here: this line must not become an event itself.
				logging.Debug("acquisition history: event not stored", "error", err)
			}
		case <-prune.C:
			if _, err := r.db.PruneAcquisitionEvents(time.Now()); err != nil {
				logging.Warn("acquisition history: pruning failed", "error", err)
			}
		}
	}
}

func (r *acquisitionRecorder) close() {
	r.stop.Do(func() {
		logging.SetEventSink(nil)
		r.mu.Lock()
		r.closed = true
		close(r.queue)
		r.mu.Unlock()
		<-r.done
		if dropped := r.dropped.Load(); dropped > 0 {
			logging.Warn("acquisition history: some events were not stored (queue full)", "dropped", dropped)
		}
	})
}
