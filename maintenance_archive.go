// maintenance_archive.go is the archive part of the night maintenance: once per
// night the archive rows that share a title are merged into the best copy. The
// rule is not repeated here: the night runs the delete query of the "Titoli
// duplicati" operation of the Archive SQL page, so the preview a person reads
// and the job that runs alone at night can never disagree.

package gextto

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// maintenanceArchiveSetting switches the nightly duplicate-title cleanup.
const maintenanceArchiveSetting = "maintenance_archive_dedup"

// maintenanceArchiveOperation is the Archive SQL operation the night runs.
const maintenanceArchiveOperation = "archive_duplicate_titles"

// maintenanceArchiveDedupEnabled reports whether the nightly cleanup is on.
func maintenanceArchiveDedupEnabled(cfg *Config) bool {
	return settingsBool(cfg, maintenanceArchiveSetting, true)
}

// archiveDuplicateCleanupOperation finds the bundled operation and refuses to
// run a read-only one, which would remove nothing.
func archiveDuplicateCleanupOperation() (ArchiveSQLOperation, error) {
	operations, err := LoadArchiveSQLOperations()
	if err != nil {
		return ArchiveSQLOperation{}, err
	}
	for _, operation := range operations {
		if operation.ID != maintenanceArchiveOperation {
			continue
		}
		if !operation.Destructive {
			return ArchiveSQLOperation{}, errors.New("the duplicate-title cleanup is read-only")
		}
		return operation, nil
	}
	return ArchiveSQLOperation{}, errors.New("the duplicate-title cleanup is missing")
}

// runArchiveDuplicateCleanup merges the archive rows that share a title,
// keeping the copy with a magnet and, when a title has none, the most recent
// `.torrent` link. It only touches the release catalogue: downloaded files, the
// library, the download history and the active torrents are separate.
func runArchiveDuplicateCleanup(ctx context.Context, state *AppState, now time.Time) {
	result := maintenanceArchiveResult{At: now}
	operation, err := archiveDuplicateCleanupOperation()
	switch {
	case err != nil:
		result.ErrorMessage = err.Error()
	case state == nil || state.archive == nil:
		result.ErrorMessage = "archive not available"
	default:
		removed, cleanupErr := state.archive.DeleteArchiveSQL(ctx, operation, "")
		result.Removed = removed
		if cleanupErr != nil {
			result.ErrorMessage = cleanupErr.Error()
		}
	}
	maintenanceStatusUpdate(func(status *maintenanceStatus) {
		status.Archive = result
		status.Night.ArchiveDedupDone = true
		status.Night.ArchiveRemoved = result.Removed
	})
	switch {
	case result.ErrorMessage != "":
		logging.Warn("🗂 The duplicate-title archive cleanup did not complete: " + result.ErrorMessage)
	case result.Removed > 0:
		logging.Info(fmt.Sprintf("🗂 Release archive: %s with the same title merged, keeping the copy with a magnet",
			countLabel(result.Removed, "row", "rows")))
	default:
		logging.Debug("no duplicate archive title to merge")
	}
}
