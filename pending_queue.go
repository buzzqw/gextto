package gextto

// pending_queue.go exposes the delay-profile queue (the titles held before the
// download starts) with how long each has been waiting. The scheduler already
// reads these tables (ReadyPending/ReadyPendingMovies); this is a read-only view
// for the observability panel.

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// pendingTimeLayout matches how the database stores timestamps.
const pendingTimeLayout = "2006-01-02 15:04:05"

// PendingQueueItem is a title held by the delay profile, with how long it has
// been waiting and how long until it is due.
type PendingQueueItem struct {
	Title   string
	Label   string
	Waiting time.Duration
	Due     time.Duration
	Ready   bool
}

// PendingQueue lists the series and movie releases currently held by the delay
// profile, oldest first, with the wait time since they were first seen.
func (d *Database) PendingQueue(now time.Time) ([]PendingQueueItem, error) {
	items := make([]PendingQueueItem, 0)

	rows, err := d.db.Query(`SELECT s.name, p.season, p.episode, p.first_seen_at, p.due_at
		FROM pending_downloads p JOIN series s ON s.id=p.series_id
		WHERE p.status='pending' ORDER BY p.first_seen_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var season, episode int64
		var firstSeen, dueAt sql.NullString
		if err := rows.Scan(&name, &season, &episode, &firstSeen, &dueAt); err != nil {
			return nil, err
		}
		items = append(items, pendingQueueItem(now, name, fmt.Sprintf("S%02dE%02d", season, episode), firstSeen, dueAt))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mrows, err := d.db.Query(`SELECT COALESCE(name, best_title), year, first_seen_at, due_at
		FROM pending_movies WHERE status='pending' ORDER BY first_seen_at`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var name string
		var year sql.NullInt64
		var firstSeen, dueAt sql.NullString
		if err := mrows.Scan(&name, &year, &firstSeen, &dueAt); err != nil {
			return nil, err
		}
		label := ""
		if year.Valid {
			label = strconv.FormatInt(year.Int64, 10)
		}
		items = append(items, pendingQueueItem(now, name, label, firstSeen, dueAt))
	}
	return items, mrows.Err()
}

func pendingQueueItem(now time.Time, title, label string, firstSeen, dueAt sql.NullString) PendingQueueItem {
	item := PendingQueueItem{Title: title, Label: label}
	if t, ok := parsePendingTime(firstSeen); ok {
		item.Waiting = now.Sub(t)
		if item.Waiting < 0 {
			item.Waiting = 0
		}
	}
	if t, ok := parsePendingTime(dueAt); ok {
		item.Due = t.Sub(now)
		item.Ready = !now.Before(t)
	}
	return item
}

func parsePendingTime(v sql.NullString) (time.Time, bool) {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(pendingTimeLayout, v.String)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}
