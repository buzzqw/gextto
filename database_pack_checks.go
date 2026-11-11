package gextto

// database_pack_checks.go keeps the announcement of a pack whose file list is
// read before it downloads. A pack from a magnet has no file list until the
// engine fetches its metadata: the cycle adds it stopped at the metadata and
// records here the download line and the notification it would have sent. The
// pack inspector sends them when the files turn out to be useful, and drops
// them when the pack is discarded, so nothing announces a download that never
// starts. The row outlives a restart, like the stopped torrent.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

// packFileCheck is the deferred announcement of one pack.
type packFileCheck struct {
	LogMessage string
	Payload    map[string]any
}

// SetPackFileCheck records the announcement of a pack waiting for its files.
func (d *Database) SetPackFileCheck(hash, logMessage string, payload map[string]any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`INSERT INTO pack_file_checks(hash, log_message, payload_json) VALUES(?1, ?2, ?3)
		ON CONFLICT(hash) DO UPDATE SET log_message=excluded.log_message, payload_json=excluded.payload_json`,
		strings.ToLower(hash), logMessage, string(encoded))
	return err
}

// PackFileCheck returns the deferred announcement of a pack, nil when the pack
// was announced when it was added.
func (d *Database) PackFileCheck(hash string) (*packFileCheck, error) {
	var message, payload string
	err := d.db.QueryRow("SELECT log_message, payload_json FROM pack_file_checks WHERE hash=?1",
		strings.ToLower(hash)).Scan(&message, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	check := &packFileCheck{LogMessage: message}
	if err := json.Unmarshal([]byte(payload), &check.Payload); err != nil {
		check.Payload = map[string]any{}
	}
	return check, nil
}

// DeletePackFileCheck forgets the announcement once it is sent or dropped.
func (d *Database) DeletePackFileCheck(hash string) error {
	_, err := d.db.Exec("DELETE FROM pack_file_checks WHERE hash=?1", strings.ToLower(hash))
	return err
}

// PrunePackFileChecks drops the rows of packs no longer in the session (the
// operator removed them, or their metadata never came), once they are a day
// old: a row is never needed after its torrent is gone.
func (d *Database) PrunePackFileChecks(live map[string]struct{}) error {
	rows, err := d.db.Query("SELECT hash FROM pack_file_checks WHERE created_at < datetime('now', '-1 day')")
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			rows.Close()
			return err
		}
		if _, ok := live[hash]; !ok {
			stale = append(stale, hash)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, hash := range stale {
		if err := d.DeletePackFileCheck(hash); err != nil {
			return err
		}
	}
	return nil
}
