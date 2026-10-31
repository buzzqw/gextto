package gextto

// web_handlers_media_integrity.go is the JSON API of the Media check: start (or
// resume) a background pass, stop it, and read its progress and findings. The
// Maintenance panel (uiweb_v2_media_integrity.go) is the user-facing side.

import (
	"net/http"

	"github.com/buzzqw/gextto/internal/logging"
)

// MediaIntegrityStartInput is the optional body of the start request.
type MediaIntegrityStartInput struct {
	// Restart starts a fresh pass from the first file even when the previous
	// one was interrupted or completed.
	Restart bool `json:"restart"`
}

// MediaIntegrityStart begins (or resumes) the check. It answers 202 at once:
// the scan runs in the background and its progress is read from
// GET /api/maintenance/media-integrity.
func MediaIntegrityStart(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input MediaIntegrityStartInput
	if _, err := gh0_decodeOptional(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if missing := mediaIntegrityMissingTools(); len(missing) > 0 {
		jsonStatus(w, http.StatusConflict, map[string]any{
			"ok":      false,
			"error":   "ffprobe and ffmpeg are required: install them and restart Gextto",
			"missing": missing,
		})
		return
	}
	job, created, err := beginMediaIntegrityScan(s, input.Restart)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !created {
		jsonStatus(w, http.StatusAccepted, map[string]any{
			"ok":      true,
			"running": true,
			"job_id":  job.ID,
			"message": "A media integrity check is already running",
		})
		return
	}
	logging.Debug("media integrity check queued", "job", job.ID, "restart", input.Restart)
	jsonStatus(w, http.StatusAccepted, map[string]any{
		"ok":      true,
		"running": true,
		"job_id":  job.ID,
	})
}

// MediaIntegrityStop asks the running check to stop after the current file. The
// saved position is kept, so the next start resumes from there.
func MediaIntegrityStop(w http.ResponseWriter, r *http.Request, s *AppState) {
	canceled := false
	if s.jobs != nil {
		for _, job := range s.jobs.List(mediaIntegrityJobKind, "") {
			if job.finished() {
				continue
			}
			if s.jobs.Cancel(job.ID) {
				canceled = true
			}
		}
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "canceled": canceled})
}

// MediaIntegrityStatusAPI returns the saved position, the counters and the
// damaged files of the last pass.
func MediaIntegrityStatusAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonStatus(w, http.StatusOK, mediaIntegrityStatusFor(s))
}
