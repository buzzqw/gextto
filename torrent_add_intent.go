package gextto

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

// A release the cycle approves is recorded in three steps: the engine Add, the
// approval commit (library placeholder or upgrade backup) and the torrent_meta
// row. A process that dies between them leaves a torrent in the engine with no
// torrent_meta row — never archived — and possibly a placeholder that makes the
// episode look already taken. An add intent, written before the Add and cleared
// once the outcome is recorded, lets the next cycle finish or undo the
// interrupted add (recoverInterruptedAdds).

// processStartedAt separates the intents of a previous process (interrupted)
// from the ones of adds still in flight in this one.
var processStartedAt = time.Now()

// torrentAddIntent is one add that was started but whose outcome was not
// recorded yet.
type torrentAddIntent struct {
	Hash    string
	Release models.Release
	Score   int64
}

// BeginTorrentAdd records the intent to add the release's torrent. It fails
// for a release without a magnet hash: such a torrent could be neither
// registered nor withdrawn, so it must not reach the engine.
func (d *Database) BeginTorrentAdd(release *models.Release, score int64) error {
	hash, err := releaseIdentityHash(release)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(release)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(
		"INSERT INTO torrent_add_intents(hash,release_json,score,created_ms) VALUES (?1,?2,?3,?4) ON CONFLICT(hash) DO UPDATE SET release_json=excluded.release_json,score=excluded.score,created_ms=excluded.created_ms",
		hash, string(payload), score, time.Now().UnixMilli(),
	)
	return err
}

// EndTorrentAdd clears the intent once the add's outcome is recorded.
func (d *Database) EndTorrentAdd(release *models.Release) error {
	hash, err := releaseIdentityHash(release)
	if err != nil {
		return err
	}
	_, err = d.db.Exec("DELETE FROM torrent_add_intents WHERE hash=?1", hash)
	return err
}

// TorrentAddIntentsBefore lists the intents recorded before the given instant.
func (d *Database) TorrentAddIntentsBefore(before time.Time) ([]torrentAddIntent, error) {
	rows, err := d.db.Query("SELECT hash,release_json,score FROM torrent_add_intents WHERE created_ms < ?1 ORDER BY created_ms", before.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var intents []torrentAddIntent
	for rows.Next() {
		var intent torrentAddIntent
		var payload string
		if err := rows.Scan(&intent.Hash, &payload, &intent.Score); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &intent.Release); err != nil {
			// An unreadable intent cannot be acted on; keep the hash so it is
			// still cleared.
			logging.Warn("unreadable torrent add intent", "hash", intent.Hash, "error", err)
		}
		intents = append(intents, intent)
	}
	return intents, rows.Err()
}

// hasTorrentMetaRow reports whether the torrent has its torrent_meta row.
func (d *Database) hasTorrentMetaRow(hash string) (bool, error) {
	var found int
	err := d.db.QueryRow("SELECT 1 FROM torrent_meta WHERE hash=?1", hash).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// hasReleasePlaceholder reports whether the approval commit already wrote the
// library row of this torrent.
func (d *Database) hasReleasePlaceholder(hash string) (bool, error) {
	var found int
	err := d.db.QueryRow("SELECT 1 FROM episodes WHERE magnet_hash=?1 UNION ALL SELECT 1 FROM movies WHERE magnet_hash=?1 LIMIT 1", hash).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// Outcomes of startApprovedRelease other than success; each is also the
// cycle-stats error key.
const (
	addIntentFailed   = "add_intent_failed"
	addFailed         = "add_failed"
	addRejected       = "torrent_rejected"
	addCommitFailed   = "approval_commit_failed"
	addRegisterFailed = "register_failed"
)

// startApprovedRelease hands an approved release to the engine and records it,
// in the order that keeps a failure at any step consistent:
//
//	intent → Add → approval commit → torrent_meta row → intent cleared
//
// A failure after the Add withdraws the torrent (and, after the commit, rolls
// the placeholder back), so the release stays eligible for a later cycle. A
// crash anywhere leaves the intent, which recoverInterruptedAdds resolves. In
// dry-run nothing is written: only the Add runs, against the dry-run engine.
//
// It returns "" when the torrent was started, otherwise one of the add*
// outcome keys; reason is the commit's approval reason ("upgrade", …).
func startApprovedRelease(db *Database, torrents TorrentEngine, release *models.Release, score int64, dryRun bool,
	add func() (bool, error), commit func() (bool, string, error)) (outcome, reason string, err error) {
	if !dryRun {
		if err := db.BeginTorrentAdd(release, score); err != nil {
			return addIntentFailed, "", err
		}
	}
	end := func() {
		if dryRun {
			return
		}
		if err := db.EndTorrentAdd(release); err != nil {
			logging.Warn("could not clear the torrent add intent", "error", err)
		}
	}
	added, err := add()
	if err != nil {
		end()
		return addFailed, "", err
	}
	if !added {
		end()
		return addRejected, "", nil
	}
	if dryRun {
		return "", "", nil
	}
	committed, reason, err := commit()
	if err != nil || !committed {
		withdrawAddedTorrent(torrents, release)
		end()
		return addCommitFailed, reason, err
	}
	if err := db.RegisterTorrentScored(release, score); err != nil {
		// The torrent is already in the engine: without its database row it
		// would be treated as a foreign torrent and never archived. Undo the add
		// and the approval.
		withdrawAddedTorrent(torrents, release)
		if rollbackErr := db.RollbackRelease(release); rollbackErr != nil {
			logging.Warn("release rollback failed", "error", rollbackErr)
		}
		end()
		return addRegisterFailed, reason, err
	}
	end()
	return "", reason, nil
}

// recoverInterruptedAdds resolves the adds a previous process left half done.
// live is the engine's current snapshot; absenceKnown says whether a hash
// missing from it really is not in the engine (an empty snapshot from a backend
// that cannot report its health is no evidence, so those intents wait). Only
// intents recorded before startedAt are touched: newer ones belong to adds
// still in flight.
//
//   - torrent_meta row present: the add completed, only the clean-up was lost;
//   - torrent in the engine and placeholder written: only the registration was
//     missing, so it is completed and the download goes on;
//   - torrent in the engine without placeholder: the approval never reached the
//     library; the torrent is taken out of the session (its data is kept, so a
//     later add of the same release resumes it) and the release is retried;
//   - torrent not in the engine: any placeholder or upgrade change is undone.
func recoverInterruptedAdds(db *Database, torrents TorrentEngine, live map[string]struct{}, absenceKnown bool, startedAt time.Time) (int, error) {
	intents, err := db.TorrentAddIntentsBefore(startedAt)
	if err != nil {
		return 0, err
	}
	resolved := 0
	for _, intent := range intents {
		release := intent.Release
		if release.Magnet != "" {
			_, inEngine := live[intent.Hash]
			if !inEngine && !absenceKnown {
				continue
			}
			if err := resolveInterruptedAdd(db, torrents, inEngine, intent); err != nil {
				logging.Warn("could not resolve an interrupted torrent add; it will be retried", "hash", intent.Hash, "error", err)
				continue
			}
		}
		if _, err := db.db.Exec("DELETE FROM torrent_add_intents WHERE hash=?1", intent.Hash); err != nil {
			return resolved, err
		}
		resolved++
	}
	return resolved, nil
}

func resolveInterruptedAdd(db *Database, torrents TorrentEngine, inEngine bool, intent torrentAddIntent) error {
	release := intent.Release
	registered, err := db.hasTorrentMetaRow(intent.Hash)
	if err != nil || registered {
		return err
	}
	placeholder, err := db.hasReleasePlaceholder(intent.Hash)
	if err != nil {
		return err
	}
	switch {
	case inEngine && placeholder:
		if err := db.RegisterTorrentScored(&release, intent.Score); err != nil {
			return err
		}
		logging.Info(fmt.Sprintf("🔁 %s: interrupted add completed after the restart", logTarget(&release)), "hash", intent.Hash)
	case inEngine:
		if _, err := torrents.Remove(intent.Hash, false); err != nil {
			return err
		}
		if err := db.RollbackRelease(&release); err != nil {
			return err
		}
		logging.Info(fmt.Sprintf("🔁 %s: interrupted add withdrawn after the restart; it will be retried", logTarget(&release)), "hash", intent.Hash)
	default:
		if err := db.RollbackRelease(&release); err != nil {
			return err
		}
		if placeholder {
			logging.Info(fmt.Sprintf("🔁 %s: interrupted add undone after the restart; it will be retried", logTarget(&release)), "hash", intent.Hash)
		}
	}
	return nil
}
