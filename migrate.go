package gextto

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/buzzqw/gextto/internal/constants"
)

// MigrateReport summarises a data-directory migration.
type MigrateReport struct {
	From         string            `json:"from"`
	To           string            `json:"to"`
	Databases    map[string]string `json:"databases"`
	TorrentState string            `json:"torrent_state,omitempty"`
	ConfigJSON   string            `json:"config_json,omitempty"`
	Warnings     []string          `json:"warnings"`
}

// appPrefix is the database name prefix used by this installation.
func appPrefix() string {
	return strings.TrimSuffix(constants.DefaultDBFile, "_series.db")
}

// MigrateDataDir copies a data directory from a previous gextto-compatible
// layout into the current one, renaming the databases and the libtorrent state
// directory. It never modifies the source: each database is copied with
// `VACUUM INTO` from a read-only connection (falling back to a byte copy when
// that is not possible), so a running daemon can be migrated safely.
func MigrateDataDir(from, to string) (MigrateReport, error) {
	report := MigrateReport{From: from, To: to, Databases: map[string]string{}}
	if strings.TrimSpace(from) == "" {
		return report, fmt.Errorf("migrate: source directory is required (--from)")
	}
	info, err := os.Stat(from)
	if err != nil || !info.IsDir() {
		return report, fmt.Errorf("migrate: source directory %q is not readable", from)
	}
	absFrom, _ := filepath.Abs(from)
	absTo, _ := filepath.Abs(to)
	if absFrom == absTo {
		return report, fmt.Errorf("migrate: source and destination are the same directory")
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return report, fmt.Errorf("migrate: create destination: %w", err)
	}

	type target struct {
		suffix string
		name   string
	}
	targets := []target{
		{"series", constants.DefaultDBFile},
		{"archive", constants.DefaultArchiveFile},
		{"config", constants.DefaultConfigFile},
		{"comics", "gextto_comics.db"},
	}

	for _, t := range targets {
		source := findSourceDatabase(from, t.suffix)
		if source == "" {
			continue
		}
		destination := filepath.Join(to, t.name)
		if err := copyDatabase(source, destination); err != nil {
			return report, fmt.Errorf("migrate %s: %w", filepath.Base(source), err)
		}
		report.Databases[t.suffix] = destination
	}
	if len(report.Databases) == 0 {
		return report, fmt.Errorf("migrate: no *_series.db/_archive.db/_config.db/_comics.db found in %q", from)
	}

	// libtorrent session state (fastresume/torrent files).
	if stateSource := findStateDirectory(from); stateSource != "" {
		destination := filepath.Join(to, constants.DefaultStateDir)
		if err := copyTree(stateSource, destination); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("torrent state not copied: %v", err))
		} else {
			report.TorrentState = destination
		}
	}

	// Optional JSON configuration: keep the current name, otherwise reuse the
	// single JSON file found in the source directory.
	primaryJSON := filepath.Join(from, "gextto.json")
	if _, err := os.Stat(primaryJSON); err == nil {
		destination := filepath.Join(to, "gextto.json")
		if err := migrateCopyFile(primaryJSON, destination); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("configuration JSON not copied: %v", err))
		} else {
			report.ConfigJSON = destination
		}
	} else if candidate := singleJSONConfig(from); candidate != "" {
		destination := filepath.Join(to, "gextto.json")
		if err := migrateCopyFile(candidate, destination); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("configuration JSON not copied: %v", err))
		} else {
			report.ConfigJSON = destination
			report.Warnings = append(report.Warnings, fmt.Sprintf("configuration JSON %q copied as gextto.json", filepath.Base(candidate)))
		}
	}

	return report, nil
}

// findSourceDatabase returns the best matching `<something>_<suffix>.db` file,
// preferring the current prefix when several are present.
func findSourceDatabase(dir, suffix string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	wanted := "_" + suffix + ".db"
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, wanted) && !strings.HasSuffix(name, "-wal") && !strings.HasSuffix(name, "-shm") {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	preferred := appPrefix() + wanted
	for _, name := range matches {
		if name == preferred {
			return filepath.Join(dir, name)
		}
	}
	return filepath.Join(dir, matches[0])
}

func findStateDirectory(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), "_torrents_state") {
			matches = append(matches, entry.Name())
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Strings(matches)
	preferred := constants.DefaultStateDir
	for _, name := range matches {
		if name == preferred {
			return filepath.Join(dir, name)
		}
	}
	return filepath.Join(dir, matches[0])
}

func singleJSONConfig(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			matches = append(matches, entry.Name())
		}
	}
	if len(matches) != 1 {
		return ""
	}
	return filepath.Join(dir, matches[0])
}

// copyDatabase produces a consistent, self-contained copy of a SQLite database.
func copyDatabase(source, destination string) error {
	_ = os.Remove(destination)
	if err := vacuumInto(source, destination); err == nil {
		return nil
	}
	// Fallback: byte copy including a live WAL/SHM, then checkpoint.
	if err := migrateCopyFile(source, destination); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(source + suffix); err == nil {
			_ = migrateCopyFile(source+suffix, destination+suffix)
		}
	}
	db, err := sql.Open("sqlite", destination)
	if err != nil {
		return err
	}
	defer db.Close()
	_, _ = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(destination + suffix)
	}
	return nil
}

func vacuumInto(source, destination string) error {
	db, err := sql.Open("sqlite", "file:"+source+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		return err
	}
	defer db.Close()
	literal := strings.ReplaceAll(destination, "'", "''")
	_, err = db.Exec("VACUUM INTO '" + literal + "'")
	return err
}

func migrateCopyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyTree(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return migrateCopyFile(path, target)
	})
}
