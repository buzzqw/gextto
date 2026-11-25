package gextto

import (
	"net/http"
	"strconv"
)

// SourceTrustAPI returns factual per-source counters and the most recent
// transfer-final snapshots. It never changes backoff, blocklists or scores.
func SourceTrustAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	days := 30
	if value := r.URL.Query().Get("days"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 90 {
			jsonError(w, http.StatusBadRequest, "days must be between 1 and 90")
			return
		}
		days = parsed
	}
	report, err := s.db.SourceTrustReport(days)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, report)
}
