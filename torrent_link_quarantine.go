package gextto

// Dead torrent links: a release that only offers an HTTP `.torrent` link (no
// magnet) is useless when the link keeps failing — a proxy that answers 502, an
// indexer whose session is expired, an entry whose torrent was removed. The
// archive keeps proposing the same URL on every cycle, so the failure repeats
// forever and the cycle never counts as clean.
//
// A link that fails in two different cycles is quarantined: the archive search
// skips it instead of asking the indexer again, for a wait that grows with the
// number of strikes (6h, 24h, then a week). A success removes the row, so a
// repaired indexer is noticed as soon as the link is retried.
//
// `dead_torrent_links` is keyed by the exact URL: the same release re-listed
// with a different URL starts from zero, which is what a rotated link deserves.

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	// torrentLinkFailureLimit is how many failures across separate cycles make
	// a link dead. A single failure is transient (a 502, a timeout).
	torrentLinkFailureLimit = 2
	// torrentLinkStrikeWindow keeps two failures of the same release inside one
	// cycle (the archive and a feed can both propose it) from counting as two
	// strikes: the second failure must come at least this far apart.
	torrentLinkStrikeWindow = 5 * time.Minute
	torrentLinkReasonLimit  = 300
)

const databaseTorrentLinkSchema = `CREATE TABLE IF NOT EXISTS dead_torrent_links (
                url TEXT PRIMARY KEY,
                failures INTEGER NOT NULL DEFAULT 0,
                last_error TEXT NOT NULL DEFAULT '',
                quarantined_until TEXT,
                updated_at TEXT NOT NULL
            );`

// torrentLinkQuarantine is how long a link with the given number of strikes is
// left alone. It stays short at first so a repaired indexer (a renewed login,
// a proxy back online) is noticed within hours, then grows to a week for links
// that are simply gone.
func torrentLinkQuarantine(failures int) time.Duration {
	switch {
	case failures < torrentLinkFailureLimit:
		return 0
	case failures == 2:
		return 6 * time.Hour
	case failures == 3:
		return 24 * time.Hour
	default:
		return 7 * 24 * time.Hour
	}
}

func torrentLinkKey(rawURL string) string {
	return strings.TrimSpace(rawURL)
}

func torrentLinkReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > torrentLinkReasonLimit {
		reason = reason[:torrentLinkReasonLimit] + "…"
	}
	return reason
}

// QuarantinedTorrentLinks returns the URLs still under quarantine as a map from
// URL to the last error, so the acquisition loop checks one set per cycle
// instead of querying the database for every release.
func (d *Database) QuarantinedTorrentLinks(now time.Time) (map[string]string, error) {
	out := map[string]string{}
	if d == nil || d.db == nil {
		return out, nil
	}
	rows, err := d.db.Query(`SELECT url, last_error, quarantined_until
		FROM dead_torrent_links
		WHERE quarantined_until IS NOT NULL AND quarantined_until <> ''`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var url, lastError string
		var until sql.NullString
		if err := rows.Scan(&url, &lastError, &until); err != nil {
			return out, err
		}
		if !until.Valid {
			continue
		}
		at, err := time.Parse(time.RFC3339, until.String)
		if err != nil || !at.After(now) {
			continue
		}
		out[url] = lastError
	}
	return out, rows.Err()
}

// RecordTorrentLinkFailure counts one strike for the URL and quarantines it
// once the limit is reached (or refreshes the error while a quarantine is
// still active). Failures closer than torrentLinkStrikeWindow to the previous
// one only update the message: one cycle proposing the same dead release twice
// is not two strikes. It returns the strike count.
func (d *Database) RecordTorrentLinkFailure(rawURL, reason string, now time.Time) (int, error) {
	url := torrentLinkKey(rawURL)
	if d == nil || d.db == nil || url == "" {
		return 0, nil
	}
	reason = torrentLinkReason(reason)
	stamp := now.UTC().Format(time.RFC3339)

	failures := 0
	lastAt := ""
	err := d.db.QueryRow(`SELECT failures, updated_at FROM dead_torrent_links WHERE url=?`, url).
		Scan(&failures, &lastAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if lastAt != "" {
		if previous, perr := time.Parse(time.RFC3339, lastAt); perr == nil && now.Sub(previous) < torrentLinkStrikeWindow {
			_, err = d.db.Exec(`UPDATE dead_torrent_links SET last_error=?, updated_at=? WHERE url=?`,
				reason, stamp, url)
			return failures, err
		}
	}

	failures++
	var until any
	if wait := torrentLinkQuarantine(failures); wait > 0 {
		until = now.Add(wait).UTC().Format(time.RFC3339)
	}
	_, err = d.db.Exec(`INSERT INTO dead_torrent_links(url, failures, last_error, quarantined_until, updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(url) DO UPDATE SET
			failures=excluded.failures,
			last_error=excluded.last_error,
			quarantined_until=excluded.quarantined_until,
			updated_at=excluded.updated_at`,
		url, failures, reason, until, stamp)
	return failures, err
}

// QuarantineTorrentLink puts a link out of use immediately, for a reason that
// is not a transient HTTP failure: for example an "in progress" pack whose
// .torrent is behind its title. The link is not resolved again until the
// quarantine ends, so the same stale pack does not cost a request every cycle.
func (d *Database) QuarantineTorrentLink(rawURL, reason string, now time.Time, wait time.Duration) error {
	url := torrentLinkKey(rawURL)
	if d == nil || d.db == nil || url == "" {
		return nil
	}
	if wait < 0 {
		wait = 0
	}
	stamp := now.UTC().Format(time.RFC3339)
	until := now.Add(wait).UTC().Format(time.RFC3339)
	_, err := d.db.Exec(`INSERT INTO dead_torrent_links(url, failures, last_error, quarantined_until, updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(url) DO UPDATE SET
			failures=excluded.failures,
			last_error=excluded.last_error,
			quarantined_until=excluded.quarantined_until,
			updated_at=excluded.updated_at`,
		url, torrentLinkFailureLimit, torrentLinkReason(reason), until, stamp)
	return err
}

// ClearTorrentLinkFailure forgets the URL: it worked, so it is not dead and
// must be eligible again if the indexer breaks later.
func (d *Database) ClearTorrentLinkFailure(rawURL string) error {
	url := torrentLinkKey(rawURL)
	if d == nil || d.db == nil || url == "" {
		return nil
	}
	_, err := d.db.Exec(`DELETE FROM dead_torrent_links WHERE url=?`, url)
	return err
}
