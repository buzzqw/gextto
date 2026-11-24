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
	absFrom, err := filepath.Abs(from)
	if err != nil {
		return report, fmt.Errorf("migrate: resolve source directory: %w", err)
	}
	absTo, err := filepath.Abs(to)
	if err != nil {
		return report, fmt.Errorf("migrate: resolve destination directory: %w", err)
	}
	if absFrom == absTo {
		return report, fmt.Errorf("migrate: source and destination are the same directory")
	}
	if pathWithin(absTo, absFrom) {
		return report, fmt.Errorf("migrate: destination directory must not be inside the source directory")
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
// The destination is only replaced by the final rename: every failure before
// that point leaves the existing database untouched.
func copyDatabase(source, destination string) error {
	if err := validateDatabaseCopyPaths(source, destination); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create the destination directory: %w", err)
	}
	// A stale WAL beside the destination could otherwise be replayed over the
	// renamed database. Refuse an active destination instead of removing files
	// from underneath its owner.
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(destination + suffix); err == nil {
			return fmt.Errorf("destination database has an active %s journal; close it before migrating", suffix)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect the destination %s journal: %w", suffix, err)
		}
	}

	// The exclusive directory is created beside the destination, so the final
	// rename stays on one filesystem. VACUUM INTO needs a path that does not yet
	// exist; the private directory provides that without a create/remove race.
	stageDir, err := os.MkdirTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".migrate-*")
	if err != nil {
		return fmt.Errorf("create the migration staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageDir) }()

	staging := filepath.Join(stageDir, "database")
	if vacuumErr := vacuumInto(source, staging); vacuumErr != nil {
		fallback := filepath.Join(stageDir, "fallback.db")
		if err := copyDatabaseBytes(source, fallback); err != nil {
			return fmt.Errorf("VACUUM copy failed (%v); byte copy failed: %w", vacuumErr, err)
		}
		staging = fallback
	}
	if err := sealSQLiteCopy(staging); err != nil {
		return fmt.Errorf("verify the copied database: %w", err)
	}
	if err := syncFile(staging); err != nil {
		return fmt.Errorf("sync the copied database: %w", err)
	}
	if err := os.Rename(staging, destination); err != nil {
		return fmt.Errorf("replace the destination database: %w", err)
	}
	return nil
}

func validateDatabaseCopyPaths(source, destination string) error {
	absSource, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve source database: %w", err)
	}
	absDestination, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve destination database: %w", err)
	}
	if absSource == absDestination {
		return fmt.Errorf("source and destination databases are the same file")
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("read source database: %w", err)
	}
	if sourceInfo.IsDir() && pathWithin(absDestination, absSource) {
		return fmt.Errorf("destination database must not be inside the source directory")
	}
	if !sourceInfo.Mode().IsRegular() {
		return fmt.Errorf("source database %q is not a regular file", source)
	}
	if destinationInfo, err := os.Stat(destination); err == nil && os.SameFile(sourceInfo, destinationInfo) {
		return fmt.Errorf("source and destination databases are the same file")
	}
	return nil
}

func copyDatabaseBytes(source, destination string) error {
	if err := migrateCopyFile(source, destination); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(source + suffix); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect the SQLite %s journal of %s: %w", suffix, source, err)
		}
		if err := migrateCopyFile(source+suffix, destination+suffix); err != nil {
			return fmt.Errorf("copy the SQLite %s journal of %s: %w", suffix, source, err)
		}
	}
	return nil
}

// sealSQLiteCopy checkpoints a copied WAL, checks every page and leaves the
// staging file in rollback-journal mode so it is self-contained at rename time.
func sealSQLiteCopy(path string) error {
	db, err := sql.Open("sqlite", sqliteDSN(path, false, "busy_timeout(10000)"))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var busy, logFrames, checkpointed int
	if err := db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("checkpoint the copy: %w", err)
	}
	if busy != 0 {
		return fmt.Errorf("checkpoint the copy: SQLite is still busy")
	}
	checks, err := QuickCheck(db)
	if err != nil {
		return err
	}
	if len(checks) != 1 || checks[0] != "ok" {
		return fmt.Errorf("quick_check failed: %s", strings.Join(checks, "; "))
	}
	if _, err := db.Exec("PRAGMA journal_mode=DELETE"); err != nil {
		return fmt.Errorf("make the copy self-contained: %w", err)
	}
	if err := db.Close(); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove the copied %s journal: %w", suffix, err)
		}
	}
	return nil
}

func syncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func vacuumInto(source, destination string) error {
	db, err := sql.Open("sqlite", sqliteDSN(source, true, "busy_timeout(10000)"))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
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
