package gextto

// web_handlers_night_maintenance.go is the JSON API of the night maintenance:
// read its state, ask for a pass now, and clear the archive row of a library
// file that is gone. The Maintenance panel (uiweb_v2_night_maintenance.go) is
// the user-facing side.

import (
	"net/http"
	"strings"
	"time"
)

// NightMaintenanceStatusAPI answers GET /api/maintenance/night.
func NightMaintenanceStatusAPI(w http.ResponseWriter, _ *http.Request, s *AppState) {
	cfg := latestConfig(s)
	window := maintenanceWindowFrom(cfg)
	status := maintenanceStatusSnapshot()
	now := time.Now()
	jsonResponse(w, map[string]any{
		"ok":             true,
		"window":         window.Label(),
		"open":           window.Contains(now),
		"next_opening":   window.NextOpening(now).Format(time.RFC3339),
		"busy_reason":    maintenanceBusyReason(s),
		"activity":       status.Activity,
		"heavy_window":   maintenanceHeavyInWindow(cfg),
		"database":       status.DB,
		"library":        status.Library,
		"library_check":  status.LibraryCheck,
		"audio":          status.Audio,
		"audio_enabled":  maintenanceAudioLibraryEnabled(cfg),
		"database_on":    settingsBool(cfg, maintenanceDBSetting, true),
		"last_night":     status.Night,
		"waiting_reason": status.Waiting,
	})
}

// NightMaintenanceRunAPI answers POST /api/maintenance/night/run: one pass now,
// outside the window. Busy downloads and imports still make it wait.
func NightMaintenanceRunAPI(w http.ResponseWriter, _ *http.Request, _ *AppState) {
	requestMaintenanceRun()
	jsonStatus(w, http.StatusAccepted, map[string]any{"ok": true})
}

// NightMaintenanceForgetInput names the episode whose file is gone.
type NightMaintenanceForgetInput struct {
	Series  string `json:"series"`
	Season  int64  `json:"season"`
	Episode int64  `json:"episode"`
}

// NightMaintenanceForgetAPI answers POST /api/maintenance/night/forget: the
// episode is marked missing, so the search downloads it again.
func NightMaintenanceForgetAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input NightMaintenanceForgetInput
	if _, err := gh0_decodeOptional(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(input.Series) == "" {
		jsonError(w, http.StatusBadRequest, "series is required")
		return
	}
	if s.db == nil {
		jsonError(w, http.StatusServiceUnavailable, "the database is not open")
		return
	}
	cleared, err := s.db.ForgetMissingEpisode(input.Series, input.Season, input.Episode)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !cleared {
		jsonError(w, http.StatusConflict, "the file is on the disk again, or its folder cannot be read")
		return
	}
	forgetMaintenanceMissing(input.Series, input.Season, input.Episode)
	jsonResponse(w, map[string]any{"ok": true})
}
