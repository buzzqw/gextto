package gextto

// uiweb_v2_media_integrity.go is the Maintenance panel of the Media check: it
// starts, resumes and stops the background pass and lists the damaged files,
// each with a "Riscarica" action that blocklists the old release and searches a
// different one for the same episode (or movie).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// v2MediaCheckView is the model of the panel.
type v2MediaCheckView struct {
	Available       bool
	Missing         string
	Enabled         bool
	IntervalMinutes int
	Batch           int
	Running         bool
	Done            bool
	NothingToCheck  bool
	Started         bool
	Checked         int
	Total           int
	Damaged         int
	Percent         int
	Current         string
	StartedAt       string
	FinishedAt      string
	Problems        []v2MediaCheckProblem
	Message         string
	Error           bool
}

// v2MediaCheckProblem is one damaged file with the action that re-downloads it.
type v2MediaCheckProblem struct {
	Label          string
	Path           string
	Status         string
	Detail         string
	CheckedAt      string
	RedownloadPath string
}

// v2MediaCheckViewFrom reads the persisted scan state and the live job state.
func v2MediaCheckViewFrom(s *AppState) v2MediaCheckView {
	status := mediaIntegrityStatusFor(s)
	cfg := latestConfig(s)
	view := v2MediaCheckView{
		Available:       status.Available,
		Enabled:         mediaIntegrityEnabled(cfg),
		IntervalMinutes: mediaIntegrityIntervalMinutes(cfg),
		Batch:           mediaIntegrityBatchSize(cfg),
		Running:         status.Running,
		Done:            status.Done,
		NothingToCheck:  status.Done && status.Total == 0,
		Checked:         status.Checked,
		Total:           status.Total,
		Damaged:         status.Damaged,
		Current:         status.Current,
		StartedAt:       v2UpdateTimeLabel(status.StartedAt),
		FinishedAt:      v2UpdateTimeLabel(status.FinishedAt),
	}
	view.Started = status.Checked > 0 || (strings.TrimSpace(status.Cursor) != "" && status.Cursor != "e:0")
	if len(status.Missing) > 0 {
		view.Missing = strings.Join(status.Missing, ", ")
	}
	if status.Total > 0 {
		view.Percent = int(clampFloat(float64(status.Checked)/float64(status.Total), 0, 1) * 100)
	}
	for _, problem := range status.Problems {
		item := v2MediaCheckProblem{
			Label:     problem.Label(),
			Path:      problem.Path,
			Status:    problem.Status,
			Detail:    problem.Detail,
			CheckedAt: v2UpdateTimeLabel(problem.CheckedAt),
		}
		if problem.Kind == "movie" {
			if cfg != nil && v2MovieIDExists(cfg, problem.TargetID) {
				item.RedownloadPath = fmt.Sprintf("/api/movies/%d/redownload?avoid_previous=1", problem.TargetID)
			}
		} else if cfg != nil && problem.Series != "" {
			if _, ok := gh7_find_series(cfg, problem.Series); ok {
				item.RedownloadPath = "/api/episodes/" + url.PathEscape(problem.Series) + "/" +
					strconv.FormatInt(problem.Season, 10) + "/" + strconv.FormatInt(problem.Episode, 10) +
					"/redownload?avoid_previous=1"
			}
		}
		view.Problems = append(view.Problems, item)
	}
	return view
}

// v2MovieIDExists reports whether the configured movie library still knows the
// id, so the panel never shows a dead re-download button.
func v2MovieIDExists(cfg *Config, id int64) bool {
	for i := range cfg.Movies {
		if cfg.Movies[i].ID == id {
			return true
		}
	}
	return false
}

// V2MediaIntegrityPanel renders the panel on demand: the maintenance page polls
// it while a check is running.
func V2MediaIntegrityPanel(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=maintenance", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_media_integrity_panel", v2MediaCheckViewFrom(s), dict, eng)
}

// V2MediaIntegrityAction starts, restarts or stops the check and re-renders the
// panel with the new state.
func V2MediaIntegrityAction(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/?view=maintenance", http.StatusSeeOther)
		return
	}
	action := strings.TrimSpace(r.FormValue("action"))
	message, isErr := "", false
	switch action {
	case "start", "restart":
		batch := mediaIntegrityBatchSize(latestConfig(s))
		if action == "restart" {
			// A forced full pass rechecks everything, not just one batch.
			batch = 0
		}
		body, _ := json.Marshal(map[string]any{"restart": action == "restart", "batch": batch})
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/maintenance/media-integrity/start", nil, body)
		if status >= http.StatusBadRequest {
			if status == http.StatusConflict {
				message = "Servono ffprobe e ffmpeg: installali e riavvia Gextto per attivare il controllo."
			} else {
				message = v2JSONError(raw)
			}
			isErr = true
		}
	case "stop":
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/maintenance/media-integrity/stop", nil, []byte("{}"))
		if status >= http.StatusBadRequest {
			message, isErr = v2JSONError(raw), true
		}
	}
	view := v2MediaCheckViewFrom(s)
	view.Message, view.Error = message, isErr
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_media_integrity_panel", view, dict, eng)
}
