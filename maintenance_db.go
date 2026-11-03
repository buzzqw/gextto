// maintenance_db.go is the database part of the night maintenance: every
// night SQLite refreshes its query statistics (PRAGMA optimize) and a database
// with a lot of free pages is compacted (VACUUM); once a week a full
// integrity_check reads every page and every index, and a damaged database
// sends a notification at once instead of waiting for the night summary.

package gextto

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// maintenanceIntegrityEvery is how often the full integrity check runs.
const maintenanceIntegrityEvery = 7 * 24 * time.Hour

// maintenanceDatabase is one SQLite file Gextto keeps open.
type maintenanceDatabase struct {
	name string
	db   *sql.DB
}

// maintenanceDatabases lists the open databases, by the name the panel shows.
func maintenanceDatabases(state *AppState) []maintenanceDatabase {
	databases := []maintenanceDatabase{}
	if state == nil {
		return databases
	}
	if state.db != nil && state.db.db != nil {
		databases = append(databases, maintenanceDatabase{"series", state.db.db})
	}
	if state.archive != nil && state.archive.db != nil {
		databases = append(databases, maintenanceDatabase{"archive", state.archive.db})
	}
	if state.comics != nil && state.comics.db != nil {
		databases = append(databases, maintenanceDatabase{"comics", state.comics.db})
	}
	if state.i18n != nil && state.i18n.db != nil {
		databases = append(databases, maintenanceDatabase{"config", state.i18n.db})
	}
	return databases
}

// runDatabaseMaintenance optimises every database and, when due, checks it.
func runDatabaseMaintenance(ctx context.Context, state *AppState, now time.Time) {
	previous := maintenanceStatusSnapshot().DB
	checkDue := previous.CheckedAt.IsZero() || now.Sub(previous.CheckedAt) >= maintenanceIntegrityEvery
	result := maintenanceDBResult{At: now, CheckedAt: previous.CheckedAt}
	if !checkDue {
		result.Problems = previous.Problems
	}
	errorsSeen := []string{}
	for _, database := range maintenanceDatabases(state) {
		if ctx.Err() != nil {
			break
		}
		before := ConnectionSizeBytes(database.db)
		result.BytesBefore += before
		if _, err := database.db.ExecContext(ctx, "PRAGMA optimize;"); err != nil {
			errorsSeen = append(errorsSeen, fmt.Sprintf("%s: %v", database.name, err))
		}
		if connectionWorthCompacting(database.db) {
			if _, err := database.db.ExecContext(ctx, "VACUUM;"); err != nil {
				errorsSeen = append(errorsSeen, fmt.Sprintf("%s: %v", database.name, err))
			} else {
				_ = CheckpointConnection(database.db)
				result.Compacted = append(result.Compacted, database.name)
			}
		}
		result.BytesAfter += ConnectionSizeBytes(database.db)
		if checkDue {
			problems, err := maintenanceIntegrityCheck(ctx, database.db)
			if err != nil {
				errorsSeen = append(errorsSeen, fmt.Sprintf("%s: %v", database.name, err))
			}
			for _, problem := range problems {
				result.Problems = append(result.Problems, database.name+": "+problem)
			}
		}
	}
	if checkDue && ctx.Err() == nil {
		result.CheckedAt = now
	}
	result.ErrorMessage = strings.Join(errorsSeen, "; ")
	freed := result.BytesBefore - result.BytesAfter
	if freed < 0 {
		freed = 0
	}
	maintenanceStatusUpdate(func(status *maintenanceStatus) {
		status.DB = result
		status.Night.DBDone = true
		status.Night.DBProblems = len(result.Problems)
		status.Night.DBFreed = freed
	})
	switch {
	case len(result.Problems) > 0 && checkDue:
		logging.Error(fmt.Sprintf("🗄 The database integrity check found %d problem(s): %s",
			len(result.Problems), strings.Join(result.Problems, "; ")))
		maintenanceNotifyDatabaseProblem(latestConfig(state), result.Problems)
	case result.ErrorMessage != "":
		logging.Warn("🗄 The database maintenance did not complete: " + result.ErrorMessage)
	case len(result.Compacted) > 0:
		logging.Info(fmt.Sprintf("🗄 Databases optimised, %s compacted: %s freed",
			strings.Join(result.Compacted, ", "), logging.HumanBytesI64(freed)))
	default:
		logging.Debug("databases optimised, nothing to compact")
	}
}

// maintenanceIntegrityCheck runs PRAGMA integrity_check and returns its
// complaints; a healthy database answers a single "ok".
func maintenanceIntegrityCheck(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check(20);")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	problems := []string{}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return problems, err
		}
		if !strings.EqualFold(strings.TrimSpace(line), "ok") {
			problems = append(problems, line)
		}
	}
	return problems, rows.Err()
}

// maintenanceNotifyDatabaseProblem warns at once: a damaged database is not
// something to read in the morning summary.
func maintenanceNotifyDatabaseProblem(cfg *Config, problems []string) {
	if cfg == nil {
		return
	}
	if err := FromConfig(cfg).NotifyEvent("maintenance_report", map[string]any{
		"title":       "Database",
		"db_problems": len(problems),
		"detail":      strings.Join(problems, "\n"),
	}); err != nil {
		logging.Warn("the database problem notification could not be sent", "error", err.Error())
	}
}
