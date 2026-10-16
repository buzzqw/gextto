package gextto

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

// setting_provenance.go records where a setting's value came from, so the guided
// tuning never overwrites a value the user chose by hand and the interface can
// mark the values it set itself. It lives in the same config database as the
// settings it describes (gextto_config.db).

// SettingOrigin is who wrote a setting.
type SettingOrigin string

const (
	// OriginAuto is a value the guided tuning wrote.
	OriginAuto SettingOrigin = "auto"
	// OriginManual is a value the user wrote (page or JSON API).
	OriginManual SettingOrigin = "manual"
)

// Provenance is the recorded origin of one setting.
type Provenance struct {
	Origin    SettingOrigin
	Detail    string
	AppliedAt string
}

const settingProvenanceSchema = `CREATE TABLE IF NOT EXISTS setting_provenance (
	key TEXT PRIMARY KEY,
	origin TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT '',
	applied_at TEXT NOT NULL
);`

// settingProvenanceConn opens the config database and makes sure the provenance
// table exists.
func settingProvenanceConn(dataDir string) (*sql.DB, error) {
	conn, err := OpenConfigDB(filepath.Join(dataDir, "gextto_config.db"))
	if err != nil {
		return nil, err
	}
	if err := sqlExec(conn, settingProvenanceSchema); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// SettingOrigins returns the recorded origin of every setting that has one.
func SettingOrigins(dataDir string) (map[string]Provenance, error) {
	conn, err := settingProvenanceConn(dataDir)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	rows, err := conn.Query("SELECT key, origin, detail, applied_at FROM setting_provenance")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Provenance{}
	for rows.Next() {
		var key, origin, detail, appliedAt string
		if err := rows.Scan(&key, &origin, &detail, &appliedAt); err != nil {
			return nil, err
		}
		out[key] = Provenance{Origin: SettingOrigin(origin), Detail: detail, AppliedAt: appliedAt}
	}
	return out, rows.Err()
}

// ManualSettings is the set of keys the user set by hand: the tuning never
// proposes them again. On a database problem it returns an empty set, so the
// tuning falls back to "nothing is protected" rather than failing.
func ManualSettings(dataDir string) map[string]bool {
	origins, err := SettingOrigins(dataDir)
	if err != nil {
		return map[string]bool{}
	}
	out := map[string]bool{}
	for key, provenance := range origins {
		if provenance.Origin == OriginManual {
			out[key] = true
		}
	}
	return out
}

// MarkSettingOrigin records who wrote a setting and why.
func MarkSettingOrigin(dataDir, key string, origin SettingOrigin, detail string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("empty setting key")
	}
	conn, err := settingProvenanceConn(dataDir)
	if err != nil {
		return err
	}
	defer conn.Close()
	return sqlExec(conn,
		"INSERT INTO setting_provenance(key,origin,detail,applied_at) VALUES (?1,?2,?3,?4) ON CONFLICT(key) DO UPDATE SET origin=excluded.origin, detail=excluded.detail, applied_at=excluded.applied_at",
		key, string(origin), detail, time.Now().UTC().Format(time.RFC3339))
}

// ClearSettingOrigin forgets a setting's origin, used when a value goes back to
// the default.
func ClearSettingOrigin(dataDir, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	conn, err := settingProvenanceConn(dataDir)
	if err != nil {
		return err
	}
	defer conn.Close()
	return sqlExec(conn, "DELETE FROM setting_provenance WHERE key=?1", key)
}
