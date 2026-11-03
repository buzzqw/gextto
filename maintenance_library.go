// maintenance_library.go is the library part of the night maintenance:
//
//   - the library report lists the files the database knows and the disk no
//     longer has. A series folder that cannot be read at all (a NAS that is
//     not mounted) is reported as unreachable, never as hundreds of missing
//     files. The periodic library check (rename repair) already renames, links
//     and de-duplicates; its last result is shown next to the report;
//   - the audio cleanup of the library runs the same cleanup as the import on
//     the files already archived, one at a time, while the window is open and
//     Gextto idle. Every file is checked once (its size and modification time
//     are remembered); a file still hardlinked to a seeding download waits for
//     the end of the seed, so the cleaned copy never doubles the space.

package gextto

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	// maintenanceMissingLimit bounds the list kept for the panel.
	maintenanceMissingLimit = 500
	// maintenanceAudioMinAge leaves alone a file written in the last hour: the
	// import cleanup handles new files, and a copy may still be in progress.
	maintenanceAudioMinAge = time.Hour
	// maintenanceAudioRetryFailed is when a file whose cleanup failed is tried
	// again although it did not change (a full disk, a NAS hiccup).
	maintenanceAudioRetryFailed = 7 * 24 * time.Hour
)

// maintenanceAudioLibraryEnabled reports whether the archive is cleaned at
// night: it needs the cleanup itself to be on.
func maintenanceAudioLibraryEnabled(cfg *Config) bool {
	if _, enabled := audioCleanupSettings(cfg); !enabled {
		return false
	}
	return settingsBool(cfg, maintenanceAudioSetting, false)
}

// archivedEpisodeRow is one archived episode with its stored path.
type archivedEpisodeRow struct {
	Series  string
	Season  int64
	Episode int64
	Path    string
}

// ArchivedEpisodeRows lists the archived episodes with a stored path.
func (d *Database) ArchivedEpisodeRows() ([]archivedEpisodeRow, error) {
	rows, err := d.db.Query(`SELECT s.name, e.season, e.episode, e.archive_path FROM episodes e
		JOIN series s ON s.id=e.series_id
		WHERE COALESCE(e.archive_path,'')<>'' AND e.downloaded_at IS NOT NULL
		ORDER BY s.name, e.season, e.episode`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []archivedEpisodeRow{}
	for rows.Next() {
		var row archivedEpisodeRow
		if err := rows.Scan(&row.Series, &row.Season, &row.Episode, &row.Path); err != nil {
			return nil, err
		}
		list = append(list, row)
	}
	return list, rows.Err()
}

// ArchivedMoviePaths lists the library paths of the movies still in the
// library.
func (d *Database) ArchivedMoviePaths() ([]string, error) {
	rows, err := d.db.Query(`SELECT COALESCE(t.processed_path,'') FROM movies m
		JOIN torrent_meta t ON lower(t.hash)=lower(m.magnet_hash)
		WHERE m.removed_at IS NULL AND COALESCE(t.processed_path,'')<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	paths := []string{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}
	return paths, rows.Err()
}

// ForgetMissingEpisode clears the archive row of an episode whose file is
// confirmed gone, so the search treats it as missing. It refuses when the file
// is back or the folder cannot be read.
func (d *Database) ForgetMissingEpisode(series string, season, episode int64) (bool, error) {
	var path string
	err := d.db.QueryRow(`SELECT COALESCE(e.archive_path,'') FROM episodes e JOIN series s ON s.id=e.series_id
		WHERE s.name=?1 AND e.season=?2 AND e.episode=?3`, series, season, episode).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) || strings.TrimSpace(path) == "" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) || !archiveFileConfirmedMissing(path) {
		return false, nil
	}
	_, err = d.db.Exec(`UPDATE episodes SET downloaded_at=NULL,archive_path=NULL,size_bytes=0,media_info_json=''
		WHERE series_id=(SELECT id FROM series WHERE name=?1) AND season=?2 AND episode=?3`, series, season, episode)
	return err == nil, err
}

// runLibraryReport finds the archived files that are gone.
func runLibraryReport(ctx context.Context, state *AppState) {
	if state == nil || state.db == nil {
		return
	}
	rows, err := state.db.ArchivedEpisodeRows()
	if err != nil {
		logging.Warn("the library report could not read the archive", "error", err.Error())
		return
	}
	result := maintenanceLibraryResult{At: time.Now(), Files: len(rows)}
	unreachable := map[string]bool{}
	missing := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		if unreachable[row.Series] {
			continue
		}
		_, statErr := os.Stat(row.Path)
		if statErr == nil {
			continue
		}
		if !errors.Is(statErr, fs.ErrNotExist) || !archiveFileConfirmedMissing(row.Path) {
			// Not a deletion: the folder or the share cannot be read now.
			unreachable[row.Series] = true
			continue
		}
		missing++
		if len(result.Missing) < maintenanceMissingLimit {
			result.Missing = append(result.Missing, maintenanceMissingFile{
				Series: row.Series, Season: row.Season, Episode: row.Episode, Path: row.Path,
			})
		}
	}
	for series := range unreachable {
		result.Unreachable = append(result.Unreachable, series)
	}
	sort.Strings(result.Unreachable)
	maintenanceStatusUpdate(func(status *maintenanceStatus) {
		status.Library = result
		status.Night.ReportDone = true
		status.Night.Missing = missing
	})
	if missing > 0 || len(result.Unreachable) > 0 {
		logging.Info(fmt.Sprintf("📚 Library report: %d of %d archived files are missing from the disk, %d series folder(s) could not be read",
			missing, len(rows), len(result.Unreachable)))
	}
}

// forgetMaintenanceMissing removes one entry from the report after the
// operator acted on it.
func forgetMaintenanceMissing(series string, season, episode int64) {
	maintenanceStatusUpdate(func(status *maintenanceStatus) {
		kept := status.Library.Missing[:0]
		for _, item := range status.Library.Missing {
			if item.Series == series && item.Season == season && item.Episode == episode {
				continue
			}
			kept = append(kept, item)
		}
		status.Library.Missing = kept
	})
}

// ---------------------------------------------------------------------------
// Audio cleanup of the library
// ---------------------------------------------------------------------------

const audioCleanupLibrarySchema = `CREATE TABLE IF NOT EXISTS audio_cleanup_library (
	path TEXT PRIMARY KEY,
	size_bytes INTEGER NOT NULL DEFAULT 0,
	modified_ns INTEGER NOT NULL DEFAULT 0,
	outcome TEXT NOT NULL DEFAULT '',
	checked_at TEXT NOT NULL DEFAULT ''
)`

// audioLibraryRecord is what the sweep remembers about one file.
type audioLibraryRecord struct {
	Size      int64
	Modified  int64
	Outcome   string
	CheckedAt time.Time
}

func (d *Database) audioLibraryRecords() (map[string]audioLibraryRecord, error) {
	if _, err := d.db.Exec(audioCleanupLibrarySchema); err != nil {
		return nil, err
	}
	rows, err := d.db.Query(`SELECT path, size_bytes, modified_ns, outcome, checked_at FROM audio_cleanup_library`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := map[string]audioLibraryRecord{}
	for rows.Next() {
		var path, outcome, checked string
		var record audioLibraryRecord
		if err := rows.Scan(&path, &record.Size, &record.Modified, &outcome, &checked); err != nil {
			return nil, err
		}
		record.Outcome = outcome
		record.CheckedAt, _ = time.Parse(time.RFC3339, checked)
		records[path] = record
	}
	return records, rows.Err()
}

func (d *Database) saveAudioLibraryRecord(path string, record audioLibraryRecord) error {
	_, err := d.db.Exec(`INSERT INTO audio_cleanup_library(path,size_bytes,modified_ns,outcome,checked_at)
		VALUES (?1,?2,?3,?4,?5) ON CONFLICT(path) DO UPDATE SET size_bytes=excluded.size_bytes,
		modified_ns=excluded.modified_ns, outcome=excluded.outcome, checked_at=excluded.checked_at`,
		path, record.Size, record.Modified, record.Outcome, record.CheckedAt.UTC().Format(time.RFC3339))
	return err
}

// audioLibraryCandidate is one library video file and the series it belongs
// to (nil for a movie).
type audioLibraryCandidate struct {
	Path   string
	Series *string
}

// audioLibraryCandidates lists the video files of the library: the archived
// episodes (a stored folder is opened) and the movies.
func audioLibraryCandidates(state *AppState) ([]audioLibraryCandidate, error) {
	rows, err := state.db.ArchivedEpisodeRows()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	candidates := []audioLibraryCandidate{}
	add := func(path string, series *string) {
		files, err := VideoFiles(path)
		if err != nil {
			return
		}
		for _, file := range files {
			if seen[file] {
				continue
			}
			seen[file] = true
			candidates = append(candidates, audioLibraryCandidate{Path: file, Series: series})
		}
	}
	for _, row := range rows {
		series := row.Series
		add(row.Path, &series)
	}
	movies, err := state.db.ArchivedMoviePaths()
	if err == nil {
		for _, path := range movies {
			add(path, nil)
		}
	}
	return candidates, nil
}

// audioLibraryNeedsCheck decides from the remembered record whether the file
// has to be probed again.
func audioLibraryNeedsCheck(record audioLibraryRecord, known bool, size, modified int64, now time.Time) bool {
	if !known || record.Size != size || record.Modified != modified {
		return true
	}
	return strings.HasPrefix(record.Outcome, "failed") && now.Sub(record.CheckedAt) >= maintenanceAudioRetryFailed
}

// fileLinkCount is the number of hard links of a file (1 for a plain file).
func fileLinkCount(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Nlink)
	}
	return 1
}

// runLibraryAudioCleanup cleans the library files not checked yet, one at a
// time, while keepGoing allows it.
func runLibraryAudioCleanup(ctx context.Context, state *AppState, keepGoing func() bool) {
	if state == nil || state.db == nil || maintenanceStatusSnapshot().Night.audioDone() || !AudioCleanupAvailable() {
		return
	}
	candidates, err := audioLibraryCandidates(state)
	if err != nil {
		logging.Warn("the audio cleanup of the library could not list the files", "error", err.Error())
		return
	}
	records, err := state.db.audioLibraryRecords()
	if err != nil {
		logging.Warn("the audio cleanup of the library could not read its progress", "error", err.Error())
		return
	}
	now := time.Now()
	type pending struct {
		candidate audioLibraryCandidate
		info      os.FileInfo
	}
	todo := []pending{}
	checked := 0
	for _, candidate := range candidates {
		info, err := os.Stat(candidate.Path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		record, known := records[candidate.Path]
		if !audioLibraryNeedsCheck(record, known, info.Size(), info.ModTime().UnixNano(), now) {
			checked++
			continue
		}
		todo = append(todo, pending{candidate, info})
	}
	maintenanceStatusUpdate(func(status *maintenanceStatus) {
		status.Audio.At = now
		status.Audio.Total = len(candidates)
		status.Audio.Checked = checked
	})

	waiting := 0
	for _, item := range todo {
		if ctx.Err() != nil || !keepGoing() {
			return
		}
		cfg := latestConfig(state)
		opts, enabled := audioCleanupSettings(cfg)
		if !enabled || !maintenanceAudioLibraryEnabled(cfg) {
			return
		}
		// Seen again right before the work: it may have changed while the
		// earlier files were cleaned.
		info, err := os.Stat(item.candidate.Path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if time.Since(info.ModTime()) < maintenanceAudioMinAge || fileLinkCount(info) > 1 {
			// Too new, or still a hardlink of a seeding download: next night.
			waiting++
			continue
		}
		outcome, report := runLibraryAudioCleanupFile(ctx, state, item.candidate, opts)
		if ctx.Err() != nil {
			return
		}
		after, err := os.Stat(item.candidate.Path)
		if err != nil {
			continue
		}
		if err := state.db.saveAudioLibraryRecord(item.candidate.Path, audioLibraryRecord{
			Size: after.Size(), Modified: after.ModTime().UnixNano(), Outcome: outcome, CheckedAt: time.Now(),
		}); err != nil {
			logging.Debug("the audio cleanup progress could not be saved", "path", item.candidate.Path, "error", err.Error())
		}
		maintenanceStatusUpdate(func(status *maintenanceStatus) {
			status.Audio.At = time.Now()
			status.Audio.Checked++
			switch {
			case report.Applied:
				status.Audio.Cleaned++
				status.Audio.Saved += report.SavedBytes
				status.Night.AudioCleaned++
				status.Night.AudioSaved += report.SavedBytes
			case strings.HasPrefix(outcome, "failed"):
				status.Audio.Failed++
				status.Audio.LastFail = filepath.Base(item.candidate.Path) + ": " + strings.TrimPrefix(outcome, "failed: ")
				status.Night.AudioFailed++
			}
		})
	}
	// The list is finished for tonight: the files that were too new or still
	// seeding are tried again next night, not every two minutes.
	if waiting > 0 {
		logging.Debug("audio cleanup of the library: files left for the next night", "files", waiting)
	}
	maintenanceStatusUpdate(func(status *maintenanceStatus) { status.Night.AudioDone = true })
}

// runLibraryAudioCleanupFile cleans one file holding the heavy slot and the
// import guard of its series, like a cleanup after an import.
func runLibraryAudioCleanupFile(ctx context.Context, state *AppState, candidate audioLibraryCandidate, opts AudioCleanupOptions) (string, AudioCleanupReport) {
	release := acquireMaintenanceHeavy(ctx)
	if release == nil {
		return "", AudioCleanupReport{}
	}
	defer release()
	maintenanceSetLive("Pulizia audio della libreria", "")
	guard := AcquireArchiveImport(candidate.Series)
	defer guard.Release()

	fileCtx, cancel := context.WithTimeout(ctx, audioCleanupTimeout)
	defer cancel()
	report, err := RunAudioCleanup(fileCtx, candidate.Path, opts)
	switch {
	case err != nil:
		logging.Warn(fmt.Sprintf("🎧 «%s»: audio cleanup failed, the file was left untouched: %v",
			filepath.Base(candidate.Path), err), "path", candidate.Path)
		return "failed: " + err.Error(), report
	case report.Applied:
		if err := state.db.UpdateArchivedSizeAfterCleanup("", candidate.Path, report.NewSize); err != nil {
			logging.Debug("the archived size was not updated after the cleanup", "path", candidate.Path, "error", err.Error())
		}
		logging.Debug("library file cleaned of other audio languages", "path", candidate.Path,
			"removed", len(report.Dropped), "saved", report.SavedBytes, "tool", report.Backend)
		return "cleaned", report
	default:
		return "skipped: " + report.SkipReason, report
	}
}
