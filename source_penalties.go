package gextto

// source_penalties.go stores explicit, operator-entered penalties for a source.
// Every row carries the reason, and removing a row is a real undo: nothing is
// hidden in an opaque score that survives without an explanation.

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/utils"
)

type SourcePenalty struct {
	ID        int64  `json:"id"`
	Source    string `json:"source"`
	Reason    string `json:"reason"`
	Points    int64  `json:"points"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func AddSourcePenalty(db *Database, source, reason string, points int64, expiresAt string) error {
	source, reason = strings.TrimSpace(source), strings.TrimSpace(reason)
	if source == "" || reason == "" {
		return errors.New("a penalty needs a source and a reason")
	}
	if points < 1 || points > 1000 {
		return errors.New("penalty points must be between 1 and 1000")
	}
	if expiresAt != "" {
		if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
			return errors.New("expiry must be an RFC3339 date")
		}
	}
	_, err := db.db.Exec(`INSERT INTO source_penalties(source, reason, points, created_at, expires_at) VALUES(?,?,?,?,?)`,
		source, reason, points, time.Now().UTC().Format(time.RFC3339), expiresAt)
	if err != nil {
		return err
	}
	logging.Info(fmt.Sprintf("🔻 Releases from «%s» now lose %d points until the penalty is removed: %s", source, points, reason))
	return nil
}

func SourcePenalties(db *Database) ([]SourcePenalty, error) {
	rows, err := db.db.Query(`SELECT id, source, reason, points, created_at, expires_at FROM source_penalties ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SourcePenalty{}
	for rows.Next() {
		var item SourcePenalty
		if err := rows.Scan(&item.ID, &item.Source, &item.Reason, &item.Points, &item.CreatedAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func RemoveSourcePenalty(db *Database, id int64) (bool, error) {
	var source, reason string
	if err := db.db.QueryRow(`SELECT source, reason FROM source_penalties WHERE id=?`, id).Scan(&source, &reason); err != nil {
		return false, err
	}
	result, err := db.db.Exec(`DELETE FROM source_penalties WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err == nil && count > 0 {
		logging.Info(fmt.Sprintf("🔺 The penalty on «%s» was removed: %s", source, reason))
	}
	return count > 0, nil
}

// SourcePenaltyPoints returns the total penalty that applies to one source
// right now, together with the reason the operator wrote. Expired rows are
// ignored but kept in the table, so the history stays visible.
func SourcePenaltyPoints(db *Database, source string, now time.Time) (int64, string) {
	if db == nil || strings.TrimSpace(source) == "" {
		return 0, ""
	}
	items, err := SourcePenalties(db)
	if err != nil || len(items) == 0 {
		return 0, ""
	}
	needle := strings.ToLower(strings.TrimSpace(source))
	var points int64
	reasons := []string{}
	for _, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.Source), needle) && strings.TrimSpace(item.Source) != "*" {
			continue
		}
		if item.ExpiresAt != "" {
			expiry, err := time.Parse(time.RFC3339, item.ExpiresAt)
			if err == nil && now.After(expiry) {
				continue
			}
		}
		points += item.Points
		reasons = append(reasons, item.Reason)
	}
	return points, strings.Join(reasons, "; ")
}

// SourcePenaltiesAPI implements GET and POST /api/source-penalties.
func SourcePenaltiesAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Method == http.MethodGet {
		items, err := SourcePenalties(s.db)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonResponse(w, map[string]any{"ok": true, "items": items})
		return
	}
	var input SourcePenalty
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := AddSourcePenalty(s.db, input.Source, input.Reason, input.Points, input.ExpiresAt); err != nil {
		jsonError(w, http.StatusBadRequest, utils.RedactURLSecrets(err.Error()))
		return
	}
	items, err := SourcePenalties(s.db)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "items": items})
}

// RemoveSourcePenaltyAPI implements DELETE /api/source-penalties/{id}.
func RemoveSourcePenaltyAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, ok := pathInt(r, "id")
	if !ok {
		jsonError(w, http.StatusBadRequest, "invalid id")
		return
	}
	removed, err := RemoveSourcePenalty(s.db, int64(id))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		jsonError(w, http.StatusNotFound, "penalty not found")
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}
