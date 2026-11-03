package gextto

// uiweb_v2_night_maintenance.go is the Maintenance panel of the night
// maintenance: the window and why it waits, the database, the audio cleanup of
// the library, the last library check and the files gone from the disk, each
// with the action that marks the episode missing again.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// v2NightMaintenanceView is the model of the panel.
type v2NightMaintenanceView struct {
	Window       string
	Open         bool
	NextOpening  string
	HeavyWindow  bool
	Activity     string
	Waiting      string
	DatabaseOn   bool
	DBAt         string
	DBSize       string
	DBFreed      string
	DBCompacted  string
	DBCheckedAt  string
	DBProblems   []string
	DBError      string
	AudioOn      bool
	AudioAt      string
	AudioChecked int
	AudioTotal   int
	AudioPercent int
	AudioCleaned int
	AudioSaved   string
	AudioFailed  int
	AudioLast    string
	CheckAt      string
	CheckSeries  int
	CheckCorrect int64
	CheckRenamed int64
	CheckRemoved int64
	CheckErrors  int64
	ReportAt     string
	ReportFiles  int
	Missing      []v2NightMissing
	MissingMore  bool
	Unreachable  string
	Message      string
	Error        bool
}

// v2NightMissing is one library file gone from the disk.
type v2NightMissing struct {
	Label   string
	Path    string
	Series  string
	Season  int64
	Episode int64
}

// v2NightMissingShown bounds the rows rendered; the rest is counted.
const v2NightMissingShown = 100

func v2NightTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04")
}

// v2NightMaintenanceViewFrom reads the persisted status and the live state.
func v2NightMaintenanceViewFrom(s *AppState) v2NightMaintenanceView {
	cfg := latestConfig(s)
	window := maintenanceWindowFrom(cfg)
	status := maintenanceStatusSnapshot()
	now := time.Now()
	view := v2NightMaintenanceView{
		Window:       window.Label(),
		Open:         window.Contains(now),
		NextOpening:  window.NextOpening(now).Local().Format("15:04"),
		HeavyWindow:  maintenanceHeavyInWindow(cfg),
		Activity:     status.Activity,
		Waiting:      status.Waiting,
		DatabaseOn:   settingsBool(cfg, maintenanceDBSetting, true),
		DBAt:         v2NightTime(status.DB.At),
		DBCheckedAt:  v2NightTime(status.DB.CheckedAt),
		DBProblems:   status.DB.Problems,
		DBError:      status.DB.ErrorMessage,
		DBCompacted:  strings.Join(status.DB.Compacted, ", "),
		AudioOn:      maintenanceAudioLibraryEnabled(cfg),
		AudioAt:      v2NightTime(status.Audio.At),
		AudioChecked: status.Audio.Checked,
		AudioTotal:   status.Audio.Total,
		AudioCleaned: status.Audio.Cleaned,
		AudioSaved:   formatBytes(status.Audio.Saved),
		AudioFailed:  status.Audio.Failed,
		AudioLast:    status.Audio.LastFail,
		CheckAt:      v2NightTime(status.LibraryCheck.At),
		CheckSeries:  status.LibraryCheck.Series,
		CheckCorrect: status.LibraryCheck.Correct,
		CheckRenamed: status.LibraryCheck.Renamed,
		CheckRemoved: status.LibraryCheck.Discarded + status.LibraryCheck.Duplicates,
		CheckErrors:  status.LibraryCheck.Errors,
		ReportAt:     v2NightTime(status.Library.At),
		ReportFiles:  status.Library.Files,
		Unreachable:  strings.Join(status.Library.Unreachable, ", "),
	}
	if view.Waiting == "" && view.Activity == "" && view.Open {
		view.Waiting = maintenanceBusyReason(s)
	}
	if status.DB.BytesAfter > 0 {
		view.DBSize = formatBytes(status.DB.BytesAfter)
	}
	if freed := status.DB.BytesBefore - status.DB.BytesAfter; freed > 0 {
		view.DBFreed = formatBytes(freed)
	}
	if status.Audio.Total > 0 {
		view.AudioPercent = int(clampFloat(float64(status.Audio.Checked)/float64(status.Audio.Total), 0, 1) * 100)
	}
	for index, item := range status.Library.Missing {
		if index >= v2NightMissingShown {
			view.MissingMore = true
			break
		}
		view.Missing = append(view.Missing, v2NightMissing{
			Label:  fmt.Sprintf("%s S%02dE%02d", item.Series, item.Season, item.Episode),
			Path:   item.Path,
			Series: item.Series, Season: item.Season, Episode: item.Episode,
		})
	}
	return view
}

// V2NightMaintenancePanel renders the panel on demand: it polls itself while a
// job runs.
func V2NightMaintenancePanel(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=settings&tab=maintenance#v2-night-maintenance-panel", http.StatusSeeOther)
		return
	}
	v2Render(w, http.StatusOK, "v2_night_maintenance_panel", v2NightMaintenanceViewFrom(s), s)
}

// V2NightMaintenanceAction runs a pass now or marks a missing episode, then
// re-renders the panel.
func V2NightMaintenanceAction(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=settings&tab=maintenance#v2-night-maintenance-panel", http.StatusSeeOther)
		return
	}
	message, isErr := "", false
	switch strings.TrimSpace(r.FormValue("action")) {
	case "run":
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/maintenance/night/run", nil, []byte("{}"))
		if status >= http.StatusBadRequest {
			message, isErr = v2JSONError(raw), true
		} else {
			message = "Manutenzione avviata: parte appena nessun download sta trasferendo."
		}
	case "forget":
		season, _ := strconv.ParseInt(r.FormValue("season"), 10, 64)
		episode, _ := strconv.ParseInt(r.FormValue("episode"), 10, 64)
		body, _ := json.Marshal(NightMaintenanceForgetInput{Series: r.FormValue("series"), Season: season, Episode: episode})
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/maintenance/night/forget", nil, body)
		if status >= http.StatusBadRequest {
			if status == http.StatusConflict {
				message = "Il file è di nuovo sul disco, oppure la sua cartella non si legge: niente da fare."
			} else {
				message = v2JSONError(raw)
			}
			isErr = true
		} else {
			message = "Puntata segnata come mancante: la ricerca la scaricherà di nuovo."
		}
	}
	view := v2NightMaintenanceViewFrom(s)
	view.Message, view.Error = message, isErr
	v2Render(w, http.StatusOK, "v2_night_maintenance_panel", view, s)
}
