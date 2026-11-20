package gextto

// The notification log: what was sent, when, over which channels and with what
// outcome. A failed webhook used to be a single line in the log, so "the
// notification did not arrive" could not be answered from the interface; the
// record outlives the log rotation and is what the Notifications page reads.

import (
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// The outcomes a delivery can end with, as the database stores them (the page
// translates them).
const (
	notificationSent       = "sent"
	notificationFailed     = "failed"
	notificationSuppressed = "suppressed"
)

// notificationLogSchema stores one row per delivery attempt. The title is the
// message's subject, already translated, so the page reads like the
// notification itself did.
const notificationLogSchema = `CREATE TABLE IF NOT EXISTS notification_log (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                at TEXT NOT NULL,
                event TEXT NOT NULL,
                title TEXT NOT NULL DEFAULT '',
                channels TEXT NOT NULL DEFAULT '',
                outcome TEXT NOT NULL,
                detail TEXT NOT NULL DEFAULT ''
            );
            CREATE INDEX IF NOT EXISTS idx_notification_log_at ON notification_log(at);`

const (
	// notificationLogRows is how many attempts the page keeps: enough to
	// follow a bad evening, small enough to stay a page.
	notificationLogRows = 500
	// notificationLogRetention ages out attempts even when the log is quiet.
	notificationLogRetention = 30 * 24 * time.Hour
	notificationTimeLayout   = "2006-01-02 15:04:05"
)

// NotificationRecord is one delivery attempt as the page shows it.
type NotificationRecord struct {
	At       time.Time
	Event    string
	Title    string
	Channels []string
	Outcome  string
	Detail   string
}

// notifyAudit receives every attempt. The daemon installs its database sink at
// startup; without one the record is simply not kept.
var notifyAudit func(NotificationRecord)

// auditNotification hands one attempt to the installed sink.
func auditNotification(record NotificationRecord) {
	if notifyAudit != nil {
		notifyAudit(record)
	}
}

// InstallNotificationLog points the audit sink at the database. It is called
// once when the daemon state is built.
func InstallNotificationLog(db *Database) {
	if db == nil {
		notifyAudit = nil
		return
	}
	// The insert trims the log to its newest rows; this drops the attempts
	// that aged out while the log was quiet.
	if removed, err := db.PruneNotificationLog(time.Now()); err != nil {
		logging.Warn("the notification log could not be pruned", "error", err)
	} else if removed > 0 {
		logging.Debug("notification log pruned", "removed", removed)
	}
	notifyAudit = func(record NotificationRecord) {
		if err := db.RecordNotification(record); err != nil {
			logging.Warn("the notification log could not be written", "error", err)
		}
	}
}

// RecordNotification appends one attempt and trims the log to its newest rows.
func (d *Database) RecordNotification(record NotificationRecord) error {
	if d == nil || d.db == nil {
		return nil
	}
	at := record.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err := d.db.Exec(`INSERT INTO notification_log(at, event, title, channels, outcome, detail)
		VALUES(?1, ?2, ?3, ?4, ?5, ?6)`,
		at.Format(notificationTimeLayout), strings.TrimSpace(record.Event), record.Title,
		strings.Join(record.Channels, ", "), record.Outcome, record.Detail)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`DELETE FROM notification_log WHERE id NOT IN (
		SELECT id FROM notification_log ORDER BY id DESC LIMIT ?1)`, notificationLogRows)
	return err
}

// NotificationLog returns the most recent attempts, newest first, at most
// limit rows, filtered by outcome when it is one of the known ones.
func (d *Database) NotificationLog(limit int, outcome string) ([]NotificationRecord, error) {
	if limit <= 0 || limit > notificationLogRows {
		limit = notificationLogRows
	}
	query := `SELECT at, event, title, channels, outcome, detail FROM notification_log`
	args := []any{}
	switch outcome {
	case notificationSent, notificationFailed, notificationSuppressed:
		query += ` WHERE outcome=?1`
		args = append(args, outcome)
	}
	query += ` ORDER BY id DESC LIMIT ?` + strconv.Itoa(len(args)+1)
	args = append(args, limit)
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []NotificationRecord{}
	for rows.Next() {
		var record NotificationRecord
		var at, channels string
		if err := rows.Scan(&at, &record.Event, &record.Title, &channels, &record.Outcome, &record.Detail); err != nil {
			return nil, err
		}
		if parsed, err := time.ParseInLocation(notificationTimeLayout, at, time.Local); err == nil {
			record.At = parsed
		}
		if strings.TrimSpace(channels) != "" {
			record.Channels = strings.Split(channels, ", ")
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// ClearNotificationLog drops every recorded attempt.
func (d *Database) ClearNotificationLog() error {
	if d == nil || d.db == nil {
		return nil
	}
	_, err := d.db.Exec(`DELETE FROM notification_log`)
	return err
}

// PruneNotificationLog drops the attempts older than the retention.
func (d *Database) PruneNotificationLog(now time.Time) (int64, error) {
	if d == nil || d.db == nil {
		return 0, nil
	}
	cutoff := now.Add(-notificationLogRetention).Format(notificationTimeLayout)
	result, err := d.db.Exec(`DELETE FROM notification_log WHERE at < ?1`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
