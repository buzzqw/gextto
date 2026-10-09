package gextto

import (
	"net/http"
	"strconv"
	"strings"
)

// AcquisitionsApi returns the story of downloads (acquisition.go): by torrent
// `hash`, by the short `acq` ID printed in the log, or the latest events of a
// `series` (optionally narrowed to `season` and `episode`, at most `limit`).
func AcquisitionsApi(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := r.URL.Query()
	var events []AcquisitionEvent
	var err error
	switch {
	case strings.TrimSpace(query.Get("hash")) != "":
		events, err = s.db.AcquisitionEvents(query.Get("hash"))
	case strings.TrimSpace(query.Get("acq")) != "":
		events, err = s.db.AcquisitionEventsByAcqID(query.Get("acq"))
	case strings.TrimSpace(query.Get("series")) != "":
		season, seasonErr := optionalInt64(query.Get("season"))
		episode, episodeErr := optionalInt64(query.Get("episode"))
		if seasonErr != nil || episodeErr != nil {
			jsonError(w, http.StatusBadRequest, "season and episode must be numbers")
			return
		}
		limit, _ := strconv.Atoi(query.Get("limit"))
		events, err = s.db.AcquisitionEventsForTitle(query.Get("series"), season, episode, limit)
	default:
		jsonError(w, http.StatusBadRequest, "one of hash, acq or series is required")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if events == nil {
		events = []AcquisitionEvent{}
	}
	jsonResponse(w, map[string]any{"ok": true, "events": events})
}

// optionalInt64 parses an optional query number: empty means nil.
func optionalInt64(value string) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
