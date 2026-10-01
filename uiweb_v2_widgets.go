package gextto

// uiweb_v2_widgets.go completes the v2 migration of the remaining widgets that
// the classic UI rendered with JavaScript: duplicate cleanup, database
// optimization, RAM-disk selection, folder-rename review, rename progress,
// OAuth/PIN flows, the background-job monitor and the .torrent uploader.
//
// Like the rest of v2, every action is forwarded to the existing JSON API
// through the in-process router (v2InternalJSON) so validation and behaviour
// stay identical to the classic interface; only the rendering is server-side.

import (
	"encoding/json"
	"fmt"
	stdhtml "html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Duplicati
// ---------------------------------------------------------------------------

type v2DuplicatesItem struct {
	Series         string
	Season         int64
	Episode        int64
	Path           string
	ResolutionRank int
	BestRank       int
}

type v2DuplicatesView struct {
	Items    []v2DuplicatesItem
	Count    int
	Removed  int
	Executed bool
	Message  string
	Error    bool
}

func v2DuplicatesViewFrom(s *AppState, r *http.Request) v2DuplicatesView {
	view := v2DuplicatesView{}
	execute := r.FormValue("execute") == "1"
	body, _ := json.Marshal(map[string]any{"execute": execute})
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/maintenance/clean-duplicates", nil, body)
	if status >= 400 {
		view.Error = true
		view.Message = v2JSONError(raw)
		return view
	}
	var payload struct {
		Removed int                `json:"removed"`
		Count   int                `json:"count"`
		Items   []v2DuplicatesItem `json:"items"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		view.Removed = payload.Removed
		view.Count = payload.Count
		view.Items = payload.Items
	}
	view.Executed = execute
	return view
}

// V2Duplicates runs the duplicate scan/cleanup and re-renders the panel.
func V2Duplicates(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" && r.FormValue("execute") == "" && r.FormValue("preview") == "" {
		http.Redirect(w, r, "/v2?view=maintenance", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_duplicates_panel", v2DuplicatesViewFrom(s, r), dict, eng)
}

// ---------------------------------------------------------------------------
// Ottimizzazione database
// ---------------------------------------------------------------------------

type v2DBFile struct {
	Name      string
	Path      string
	SizeBytes int64
	Exists    bool
}

type v2DBView struct {
	Files      []v2DBFile
	TotalBytes int64
	Action     string
	BeforeSize int64
	AfterSize  int64
	BeforeRows int64
	AfterRows  int64
	Message    string
	Error      bool
}

func v2DBViewFrom(s *AppState, r *http.Request) v2DBView {
	view := v2DBView{Action: strings.TrimSpace(r.FormValue("action"))}
	if view.Action == "vacuum" || view.Action == "analyze" {
		body, _ := json.Marshal(map[string]string{"action": view.Action})
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/db/action", nil, body)
		if status >= 400 {
			view.Error = true
			view.Message = v2JSONError(raw)
		} else {
			var payload struct {
				Before struct {
					SizeBytes int64 `json:"size_bytes"`
					Rows      int64 `json:"rows"`
				} `json:"before"`
				After struct {
					SizeBytes int64 `json:"size_bytes"`
					Rows      int64 `json:"rows"`
				} `json:"after"`
			}
			if json.Unmarshal(raw, &payload) == nil {
				view.BeforeSize = payload.Before.SizeBytes
				view.BeforeRows = payload.Before.Rows
				view.AfterSize = payload.After.SizeBytes
				view.AfterRows = payload.After.Rows
			}
		}
	}
	if raw, status := v2InternalJSON(s, http.MethodGet, "/api/db/info", nil, nil); status < 400 {
		var payload struct {
			Files []v2DBFile `json:"files"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			view.Files = payload.Files
			for _, file := range payload.Files {
				view.TotalBytes += file.SizeBytes
			}
		}
	} else if view.Message == "" {
		view.Error = true
		view.Message = v2JSONError(raw)
	}
	return view
}

// V2DBMaintenance runs VACUUM/ANALYZE (or just refreshes sizes) and re-renders.
func V2DBMaintenance(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/v2?view=maintenance", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_db_panel", v2DBViewFrom(s, r), dict, eng)
}

// ---------------------------------------------------------------------------
// RAM disk
// ---------------------------------------------------------------------------

type v2RamdiskPath struct {
	Path       string
	Filesystem string
	FreeBytes  int64
	TotalBytes int64
	Writable   bool
	Configured bool
}

type v2RamdiskView struct {
	Configured   string
	Enabled      bool
	ConfiguredOK bool
	Problem      string
	Paths        []v2RamdiskPath
	CreatePath   string
	CreateAvail  bool
	Warning      string
	Message      string
	Error        bool
}

func v2RamdiskViewFrom(s *AppState, r *http.Request) v2RamdiskView {
	view := v2RamdiskState(s)
	action := strings.TrimSpace(r.FormValue("action"))
	path := strings.TrimSpace(r.FormValue("path"))
	if action != "" && path != "" {
		endpoint := "/api/ramdisk/select"
		if action == "create" {
			endpoint = "/api/ramdisk/create"
		}
		body, _ := json.Marshal(map[string]string{"path": path})
		raw, status := v2InternalJSON(s, http.MethodPost, endpoint, nil, body)
		if status >= 400 {
			view.Error = true
			view.Message = v2JSONError(raw)
		} else {
			view = v2RamdiskState(s)
			view.Message = "RAM disk selezionato: " + path
		}
	}
	return view
}

func v2RamdiskState(s *AppState) v2RamdiskView {
	view := v2RamdiskView{}
	if raw, status := v2InternalJSON(s, http.MethodGet, "/api/ramdisk", nil, nil); status < 400 {
		var payload struct {
			Enabled         bool   `json:"enabled"`
			Configured      any    `json:"configured"`
			ConfiguredOK    bool   `json:"configured_ok"`
			Problem         string `json:"problem"`
			CreatePath      string `json:"create_path"`
			CreateAvailable bool   `json:"create_available"`
			Warning         string `json:"warning"`
			Paths           []struct {
				Path       string `json:"path"`
				Filesystem string `json:"filesystem"`
				FreeBytes  int64  `json:"free_bytes"`
				TotalBytes int64  `json:"total_bytes"`
				Writable   bool   `json:"writable"`
				Configured bool   `json:"configured"`
			} `json:"paths"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			view.Enabled = payload.Enabled
			view.ConfiguredOK = payload.ConfiguredOK
			view.Problem = payload.Problem
			view.CreatePath = payload.CreatePath
			view.CreateAvail = payload.CreateAvailable
			view.Warning = payload.Warning
			if text, ok := payload.Configured.(string); ok {
				view.Configured = text
			}
			for _, item := range payload.Paths {
				view.Paths = append(view.Paths, v2RamdiskPath{
					Path: item.Path, Filesystem: item.Filesystem,
					FreeBytes: item.FreeBytes, TotalBytes: item.TotalBytes,
					Writable: item.Writable, Configured: item.Configured,
				})
			}
		}
	}
	return view
}

// V2Ramdisk selects/creates a RAM disk and re-renders the panel.
func V2Ramdisk(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/v2?view=maintenance", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ramdisk_panel", v2RamdiskViewFrom(s, r), dict, eng)
}

// ---------------------------------------------------------------------------
// Rinomina contenuto cartella (revisione)
// ---------------------------------------------------------------------------

type v2FolderRenameCandidate struct {
	Provider string
	Title    string
	Year     string
	Target   string
}

type v2FolderRenameItem struct {
	Index      int
	Source     string
	Relative   string
	Kind       string
	Detected   string
	Target     string
	Status     string
	Reason     string
	Conflict   bool
	Season     *int64
	Episode    *int64
	Candidates []v2FolderRenameCandidate
}

type v2FolderRenameView struct {
	Path    string
	Items   []v2FolderRenameItem
	Scanned bool
	Renamed int
	Message string
	Error   bool
}

// scanJSON is the raw scan payload kept in a hidden form field so the apply
// request can rebuild the exact source/target pairs the user accepted.
func (v v2FolderRenameView) ScanJSON() string {
	raw := make([]map[string]any, 0, len(v.Items))
	for _, item := range v.Items {
		candidates := make([]map[string]any, 0, len(item.Candidates))
		for _, candidate := range item.Candidates {
			candidates = append(candidates, map[string]any{
				"provider": candidate.Provider, "title": candidate.Title,
				"year": candidate.Year, "target": candidate.Target,
			})
		}
		raw = append(raw, map[string]any{
			"source": item.Source, "target": item.Target, "candidates": candidates,
		})
	}
	encoded, _ := json.Marshal(raw)
	return string(encoded)
}

func v2FolderRenameScan(s *AppState, path string) v2FolderRenameView {
	view := v2FolderRenameView{Path: path}
	body, _ := json.Marshal(map[string]string{"path": path})
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/maintenance/rename-folder/scan", nil, body)
	if status >= 400 {
		view.Error = true
		view.Message = v2JSONError(raw)
		return view
	}
	var payload struct {
		Path  string `json:"path"`
		Items []struct {
			Source     string `json:"source"`
			Relative   string `json:"relative"`
			Kind       string `json:"kind"`
			Detected   string `json:"detected"`
			Season     *int64 `json:"season"`
			Episode    *int64 `json:"episode"`
			Target     string `json:"target"`
			Status     string `json:"status"`
			Reason     string `json:"reason"`
			Conflict   bool   `json:"conflict"`
			Candidates []struct {
				Provider string `json:"provider"`
				Title    string `json:"title"`
				Year     string `json:"year"`
				Target   string `json:"target"`
			} `json:"candidates"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		view.Error = true
		view.Message = "risposta di scansione non valida"
		return view
	}
	if payload.Path != "" {
		view.Path = payload.Path
	}
	view.Scanned = true
	for index, item := range payload.Items {
		entry := v2FolderRenameItem{
			Index: index, Source: item.Source, Relative: item.Relative, Kind: item.Kind,
			Detected: item.Detected, Target: item.Target, Status: item.Status,
			Reason: item.Reason, Conflict: item.Conflict, Season: item.Season, Episode: item.Episode,
		}
		for _, candidate := range item.Candidates {
			entry.Candidates = append(entry.Candidates, v2FolderRenameCandidate{
				Provider: candidate.Provider, Title: candidate.Title, Year: candidate.Year, Target: candidate.Target,
			})
		}
		view.Items = append(view.Items, entry)
	}
	return view
}

func v2FolderRenameApply(s *AppState, r *http.Request, view *v2FolderRenameView) {
	_ = r.ParseForm()
	type applyItem struct {
		Source string `json:"source"`
		Target string `json:"target"`
	}
	var scanned []struct {
		Source     string `json:"source"`
		Target     string `json:"target"`
		Candidates []struct {
			Target string `json:"target"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(r.FormValue("scan")), &scanned); err != nil {
		view.Error = true
		view.Message = "elenco di scansione non valido"
		return
	}
	items := []applyItem{}
	for index, entry := range scanned {
		if r.FormValue(fmt.Sprintf("accept__%d", index)) == "" {
			continue
		}
		target := entry.Target
		if choice := r.FormValue(fmt.Sprintf("cand__%d", index)); choice != "" {
			candidateIndex, err := strconv.Atoi(choice)
			if err == nil && candidateIndex >= 0 && candidateIndex < len(entry.Candidates) {
				target = entry.Candidates[candidateIndex].Target
			}
		}
		if strings.TrimSpace(target) == "" {
			continue
		}
		items = append(items, applyItem{Source: entry.Source, Target: target})
	}
	if len(items) == 0 {
		view.Message = "Nessuna proposta selezionata."
		return
	}
	if len(items) > folderRenameMaxItems {
		view.Error = true
		view.Message = "troppe proposte selezionate"
		return
	}
	body, _ := json.Marshal(map[string]any{"path": view.Path, "items": items})
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/maintenance/rename-folder/apply", nil, body)
	if status >= 400 {
		view.Error = true
		view.Message = v2JSONError(raw)
		return
	}
	var payload struct {
		Renamed int `json:"renamed"`
		Items   []struct {
			Source string `json:"source"`
			OK     bool   `json:"ok"`
		} `json:"items"`
	}
	_ = json.Unmarshal(raw, &payload)
	view.Renamed = payload.Renamed
	view.Message = fmt.Sprintf("Rinominati %d file.", payload.Renamed)
}

// V2FolderRenameScan scans a folder and re-renders the review panel.
func V2FolderRenameScan(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2FolderRenameScan(s, strings.TrimSpace(r.FormValue("path")))
	if view.Error {
		// Keep the typed path visible even when the scan fails.
		view.Path = strings.TrimSpace(r.FormValue("path"))
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_folder_rename_panel", view, dict, eng)
}

// V2FolderRenameApply applies the accepted proposals and re-renders.
func V2FolderRenameApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2FolderRenameView{Path: strings.TrimSpace(r.FormValue("path")), Scanned: true}
	v2FolderRenameApply(s, r, &view)
	// The applied rows are gone from disk: show the outcome and let the user
	// run a fresh scan instead of pretending the preview is still valid.
	view.Scanned = false
	view.Items = nil
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_folder_rename_panel", view, dict, eng)
}

// ---------------------------------------------------------------------------
// Progresso rinomina
// ---------------------------------------------------------------------------

type v2ProgressView struct {
	Running bool
	Current int
	Total   int
	Series  string
	Message string
	Errors  int
	Percent int
}

func v2ProgressViewFrom(s *AppState, section uiProgressSection) v2ProgressView {
	view := v2ProgressView{}
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/rename-progress", nil, nil)
	if status < 400 {
		var payload struct {
			Progress RenameProgress `json:"progress"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			progress := payload.Progress
			view = v2ProgressView{
				Running: progress.Running, Current: progress.Current, Total: progress.Total,
				Series: progress.Series, Message: progress.Message, Errors: progress.Errors,
			}
		}
	}
	if view.Total > 0 {
		view.Percent = view.Current * 100 / view.Total
	}
	if view.Percent > 100 {
		view.Percent = 100
	}
	return view
}

// V2RenameProgress renders the progress fragment used by the polling panel.
func V2RenameProgress(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_rename_progress", v2ProgressViewFrom(s, uiProgressSection{}), dict, eng)
}

// ---------------------------------------------------------------------------
// OAuth / PIN
// ---------------------------------------------------------------------------

type v2OutputView struct {
	Text template.HTML
	URL  string
}

func v2OutputFromJSON(raw []byte) v2OutputView {
	view := v2OutputView{}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		view.Text = template.HTML("<pre class=\"log-view\">" + stdhtml.EscapeString(string(raw)) + "</pre>")
		return view
	}
	if pretty, err := json.MarshalIndent(decoded, "", "  "); err == nil {
		view.Text = template.HTML("<pre class=\"log-view\">" + stdhtml.EscapeString(string(pretty)) + "</pre>")
	}
	view.URL = v2FindURL(decoded)
	if view.URL != "" {
		view.Text = template.HTML("<p>Visita <a href=\""+template.HTMLEscapeString(view.URL)+"\" target=\"_blank\" rel=\"noopener noreferrer\">"+template.HTMLEscapeString(view.URL)+"</a> e conferma il codice.</p>") + view.Text
	}
	return view
}

func v2FindURL(value any) string {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "http://") || strings.HasPrefix(typed, "https://") {
			return typed
		}
	case map[string]any:
		for _, key := range []string{"verification_url", "verification_uri", "url", "verification_url_complete"} {
			if found, ok := typed[key]; ok {
				if text := v2FindURL(found); text != "" {
					return text
				}
			}
		}
		for _, candidate := range typed {
			if text := v2FindURL(candidate); text != "" {
				return text
			}
		}
	case []any:
		for _, candidate := range typed {
			if text := v2FindURL(candidate); text != "" {
				return text
			}
		}
	}
	return ""
}

// V2OAuthStart starts the device/PIN flow and renders the provider answer.
func V2OAuthStart(w http.ResponseWriter, r *http.Request, s *AppState) {
	path := strings.TrimSpace(r.FormValue("path"))
	raw := []byte(`{"ok":false,"error":"endpoint non valido"}`)
	if strings.HasPrefix(path, "/api/") {
		raw, _ = v2InternalJSON(s, http.MethodPost, path, nil, []byte("{}"))
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_oauth_output", v2OutputFromJSON(raw), dict, eng)
}

// V2OAuthPoll confirms the code/PIN entered by the user.
func V2OAuthPoll(w http.ResponseWriter, r *http.Request, s *AppState) {
	path := strings.TrimSpace(r.FormValue("path"))
	code := strings.TrimSpace(r.FormValue("code"))
	raw := []byte(`{"ok":false,"error":"endpoint non valido"}`)
	if strings.HasPrefix(path, "/api/") {
		body, _ := json.Marshal(map[string]string{"code": code})
		raw, _ = v2InternalJSON(s, http.MethodPost, path, nil, body)
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_oauth_output", v2OutputFromJSON(raw), dict, eng)
}

// ---------------------------------------------------------------------------
// Job in background
// ---------------------------------------------------------------------------

type v2Job struct {
	ID         string
	Kind       string
	State      string
	StateLabel string
	StateClass string
	Progress   int
	Message    string
	Error      string
	Started    string
	Finished   string
	Done       bool
}

type v2JobsView struct {
	Jobs   []v2Job
	Active int
}

func v2JobsViewFrom(s *AppState) v2JobsView {
	view := v2JobsView{}
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/jobs", nil, nil)
	if status >= 400 {
		return view
	}
	var payload struct {
		Jobs []struct {
			ID         string     `json:"id"`
			Kind       string     `json:"kind"`
			State      string     `json:"state"`
			Progress   float64    `json:"progress"`
			Message    string     `json:"message"`
			Error      string     `json:"error"`
			StartedAt  *time.Time `json:"started_at"`
			FinishedAt *time.Time `json:"finished_at"`
		} `json:"jobs"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return view
	}
	for _, job := range payload.Jobs {
		entry := v2Job{
			ID: job.ID, Kind: job.Kind, State: job.State, Message: job.Message, Error: job.Error,
			Progress: int(job.Progress + 0.5), Done: job.State == "succeeded" || job.State == "failed" || job.State == "canceled",
		}
		switch job.State {
		case "queued":
			entry.StateLabel, entry.StateClass = "in coda", "warn"
		case "running":
			entry.StateLabel, entry.StateClass = "in corso", "warn"
		case "succeeded":
			entry.StateLabel, entry.StateClass = "completato", "ok"
		case "failed":
			entry.StateLabel, entry.StateClass = "non riuscito", "err"
		case "canceled":
			entry.StateLabel, entry.StateClass = "annullato", ""
		default:
			entry.StateLabel = job.State
		}
		if job.StartedAt != nil {
			entry.Started = job.StartedAt.Local().Format("15:04:05")
		}
		if job.FinishedAt != nil {
			entry.Finished = job.FinishedAt.Local().Format("15:04:05")
		}
		if !entry.Done {
			view.Active++
		}
		view.Jobs = append(view.Jobs, entry)
	}
	sort.SliceStable(view.Jobs, func(i, j int) bool {
		if view.Jobs[i].Done != view.Jobs[j].Done {
			return !view.Jobs[i].Done
		}
		return view.Jobs[i].Started > view.Jobs[j].Started
	})
	return view
}

// V2JobsPartial renders the background-job list (polled by HTMX).
func V2JobsPartial(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_jobs", v2JobsViewFrom(s), dict, eng)
}

// V2JobCancel cancels a queued/running job and refreshes the list.
func V2JobCancel(w http.ResponseWriter, r *http.Request, s *AppState) {
	if id := strings.TrimSpace(r.FormValue("id")); id != "" {
		v2InternalJSON(s, http.MethodPost, "/api/jobs/"+url.PathEscape(id)+"/cancel", nil, []byte("{}"))
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/v2?view=maintenance", http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_jobs", v2JobsViewFrom(s), dict, eng)
}

// ---------------------------------------------------------------------------
// Aggiungi torrent (magnet/URL o file .torrent)
// ---------------------------------------------------------------------------

// v2TorrentAddOptions carries the startup flags ported from the classic form.
type v2TorrentAddOptions struct {
	Start        bool
	NoRename     bool
	Sequential   bool
	SeedMode     bool
	QueueTop     bool
	FirstLast    bool
	MetadataOnly bool
	Preallocate  bool
}

func v2TorrentAddOptionsFrom(r *http.Request) v2TorrentAddOptions {
	return v2TorrentAddOptions{
		Start:        r.FormValue("start") == "1",
		NoRename:     r.FormValue("no_rename") == "1",
		Sequential:   r.FormValue("sequential") == "1",
		SeedMode:     r.FormValue("seed_mode") == "1",
		QueueTop:     r.FormValue("queue_top") == "1",
		FirstLast:    r.FormValue("first_last") == "1",
		MetadataOnly: r.FormValue("metadata_only") == "1",
		Preallocate:  r.FormValue("preallocate") == "1",
	}
}

// V2DownloadsAdd accepts the magnet/URL or the uploaded .torrent and forwards it
// to the same API the classic form uses. It answers with a browser redirect so
// the form keeps working without JavaScript.
func V2DownloadsAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	message := ""
	isErr := false
	target := strings.TrimSpace(r.FormValue("magnet"))
	savePath := strings.TrimSpace(r.FormValue("save_path"))
	options := v2TorrentAddOptionsFrom(r)

	if file, header, err := r.FormFile("torrent"); err == nil {
		defer file.Close()
		raw, readErr := io.ReadAll(io.LimitReader(file, 5_000_001))
		if readErr != nil || len(raw) == 0 {
			message, isErr = "file .torrent illeggibile", true
		} else {
			query := url.Values{}
			if savePath != "" {
				query.Set("save_path", savePath)
			}
			body, status := v2InternalJSON(s, http.MethodPost, "/api/upload-torrent", query, raw)
			if status >= 400 {
				message, isErr = v2JSONError(body), true
			} else {
				message = "Torrent aggiunto: " + header.Filename
			}
		}
	} else if target != "" {
		payload := AddTorrentInput{
			Magnet:         target,
			StartPaused:    !options.Start,
			NoRename:       options.NoRename,
			Sequential:     options.Sequential,
			SeedMode:       options.SeedMode,
			QueueTop:       options.QueueTop,
			FirstLast:      options.FirstLast,
			StopAtMetadata: options.MetadataOnly,
			Preallocate:    &options.Preallocate,
		}
		if savePath != "" {
			payload.SavePath = &savePath
		}
		body, _ := json.Marshal(payload)
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/send-magnet", nil, body)
		if status >= 400 {
			message, isErr = v2JSONError(raw), true
		} else {
			message = "Torrent aggiunto."
		}
	} else {
		message, isErr = "inserisci un magnet, un URL o un file .torrent", true
	}

	if r.Header.Get("HX-Request") == "" {
		query := url.Values{"view": {"downloads"}}
		if message != "" {
			query.Set("msg", message)
			if isErr {
				query.Set("msg_err", "1")
			}
		}
		http.Redirect(w, r, "/v2?"+query.Encode(), http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_torrent_add_result", map[string]any{"Message": message, "Error": isErr}, dict, eng)
}

// ---------------------------------------------------------------------------
// Traduzioni: modifica per chiave
// ---------------------------------------------------------------------------

// V2SettingsI18nSet stores one translation and re-renders the table.
func V2SettingsI18nSet(w http.ResponseWriter, r *http.Request, s *AppState) {
	language := strings.TrimSpace(r.FormValue("lang"))
	key := strings.TrimSpace(r.FormValue("key"))
	value := r.FormValue("value")
	if language != "" && key != "" {
		body, _ := json.Marshal(map[string]string{"lang": language, "key": key, "value": value})
		v2InternalJSON(s, http.MethodPost, "/api/i18n", nil, body)
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_i18n_table", v2I18nViewFrom(s, language), dict, eng)
}

// ---------------------------------------------------------------------------
// Anteprima rinomina (dettaglio serie)
// ---------------------------------------------------------------------------

type v2RenamePreviewItem struct {
	Season    int64
	Episode   int64
	From      string
	To        string
	Error     string
	Discarded bool
}

type v2RenamePreviewView struct {
	Series     string
	Items      []v2RenamePreviewItem
	Episodes   int
	WithPath   int
	AlreadyOK  int
	Renamed    int
	Discarded  int
	ErrorCount int
	Executed   bool
	Force      bool
	Message    string
	Error      bool
}

func v2RenamePreviewFrom(s *AppState, name string, execute, force bool) v2RenamePreviewView {
	view := v2RenamePreviewView{Series: name, Executed: execute, Force: force}
	path := "/api/series/" + url.PathEscape(name) + "/rename-preview"
	if execute {
		path = "/api/series/" + url.PathEscape(name) + "/rename-execute"
	}
	body, _ := json.Marshal(map[string]bool{"force": force, "source_only": false})
	raw, status := v2InternalJSON(s, http.MethodPost, path, nil, body)
	if status >= 400 {
		view.Error = true
		view.Message = v2JSONError(raw)
		return view
	}
	var payload struct {
		Series         string `json:"series"`
		Episodes       int    `json:"episodes"`
		WithPath       int    `json:"with_path"`
		AlreadyOKCount int    `json:"already_ok_count"`
		RenamedCount   int    `json:"renamed_count"`
		DiscardedCount int    `json:"discarded_count"`
		ErrorCount     int    `json:"error_count"`
		Items          []struct {
			Season    int64  `json:"season"`
			Episode   int64  `json:"episode"`
			From      string `json:"from"`
			To        string `json:"to"`
			Error     string `json:"error"`
			Discarded bool   `json:"discarded"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		view.Error = true
		view.Message = "risposta rinomina non valida"
		return view
	}
	if payload.Series != "" {
		view.Series = payload.Series
	}
	view.Episodes = payload.Episodes
	view.WithPath = payload.WithPath
	view.AlreadyOK = payload.AlreadyOKCount
	view.Renamed = payload.RenamedCount
	view.Discarded = payload.DiscardedCount
	view.ErrorCount = payload.ErrorCount
	for _, item := range payload.Items {
		view.Items = append(view.Items, v2RenamePreviewItem{
			Season: item.Season, Episode: item.Episode, From: item.From, To: item.To,
			Error: item.Error, Discarded: item.Discarded,
		})
	}
	if execute {
		view.Message = fmt.Sprintf("Rinomina completata: %d rinominati, %d scartati, %d errori.", view.Renamed, view.Discarded, view.ErrorCount)
	} else if len(view.Items) == 0 {
		view.Message = fmt.Sprintf("Nessun file da rinominare (%d già corretti su %d).", view.AlreadyOK, view.WithPath)
	}
	return view
}

// V2SeriesRenamePreview renders the server-side rename preview panel.
func V2SeriesRenamePreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := strings.TrimSpace(r.FormValue("series"))
	if name == "" {
		http.Error(w, "serie mancante", http.StatusBadRequest)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_rename_preview", v2RenamePreviewFrom(s, name, false, false), dict, eng)
}

// V2SeriesRenameExecute runs the rename and re-renders the panel with the result.
func V2SeriesRenameExecute(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := strings.TrimSpace(r.FormValue("series"))
	if name == "" {
		http.Error(w, "serie mancante", http.StatusBadRequest)
		return
	}
	execute := r.FormValue("preview") != "1"
	force := r.FormValue("force") == "1"
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_rename_preview", v2RenamePreviewFrom(s, name, execute, force), dict, eng)
}

// ---------------------------------------------------------------------------
// Esplora TMDB (calendario, tendenze, ricerca)
// ---------------------------------------------------------------------------

type v2CalendarItem struct {
	Series   string
	Poster   string
	Name     string
	AirDate  string
	Season   int
	Episode  int
	Overview string
}

type v2TMDBItem struct {
	Kind      string
	Name      string
	Year      string
	TvdbID    string
	TvdbURL   string
	Poster    string
	Overview  string
	TmdbID    string
	TmdbURL   string
	Vote      float64
	InLibrary bool
}

func v2TMDBItemFromMap(kind string, item map[string]any) v2TMDBItem {
	out := v2TMDBItem{Kind: kind, TmdbID: v2AnyID(item["id"]), TvdbID: v2AnyID(item["tvdb_id"]), InLibrary: v2Truthy(item["in_library"])}
	tmdbKind, tvdbKind := "movie", "movie"
	if kind != "movie" {
		tmdbKind, tvdbKind = "tv", "series"
	}
	out.TmdbURL = tmdbURL(out.TmdbID, tmdbKind)
	out.TvdbURL = tvdbURL(out.TvdbID, tvdbKind)
	out.Name = v2AnyString(item["name"])
	if out.Name == "" {
		out.Name = v2AnyString(item["title"])
	}
	date := v2AnyString(item["first_air_date"])
	if date == "" {
		date = v2AnyString(item["release_date"])
	}
	if len(date) >= 4 {
		out.Year = date[:4]
	}
	out.Poster = v2AnyString(item["poster"])
	if out.Poster == "" {
		if path := v2AnyString(item["poster_path"]); path != "" {
			out.Poster = "https://image.tmdb.org/t/p/w154" + path
		}
	}
	out.Overview = v2AnyString(item["overview"])
	out.Vote = v2Float(item["vote_average"])
	return out
}

// V2TmdbCalendar renders the upcoming-releases calendar on demand.
func V2TmdbCalendar(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := struct {
		Items []v2CalendarItem
		Error string
	}{}
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/calendar", nil, nil)
	if status >= 400 {
		view.Error = v2JSONError(raw)
	} else {
		var payload struct {
			Items []struct {
				Series  string         `json:"series"`
				Poster  *string        `json:"poster"`
				Episode map[string]any `json:"episode"`
			} `json:"items"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			for _, entry := range payload.Items {
				item := v2CalendarItem{Series: entry.Series}
				if entry.Poster != nil {
					item.Poster = *entry.Poster
				}
				item.Name = v2AnyString(entry.Episode["name"])
				item.AirDate = v2AnyString(entry.Episode["air_date"])
				item.Season = int(v2AnyInt(entry.Episode["season_number"]))
				item.Episode = int(v2AnyInt(entry.Episode["episode_number"]))
				item.Overview = v2AnyString(entry.Episode["overview"])
				view.Items = append(view.Items, item)
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_tmdb_calendar", view, dict, eng)
}

// V2TmdbDiscover renders a trending/popular/category list.
func V2TmdbDiscover(w http.ResponseWriter, r *http.Request, s *AppState) {
	kind := r.FormValue("kind")
	if kind == "" {
		kind = "series"
	}
	mode := r.FormValue("mode")
	if mode == "" {
		mode = "trending"
	}
	window := r.FormValue("window")
	if window == "" {
		window = "week"
	}
	input := map[string]string{"kind": kind, "mode": mode, "window": window}
	body, _ := json.Marshal(input)
	v2TmdbRenderResults(w, s, kind, body)
}

// V2TmdbSearch searches TMDB (with the TVDB fallback) by title.
func V2TmdbSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	kind := r.FormValue("kind")
	if kind == "" {
		kind = "series"
	}
	query := strings.TrimSpace(r.FormValue("query"))
	if query == "" {
		dict, eng := v2Dictionaries(s)
		v2Render(w, http.StatusOK, "v2_tmdb_results", map[string]any{"Empty": "Inserisci un titolo da cercare."}, dict, eng)
		return
	}
	input := map[string]string{"kind": kind, "query": query}
	body, _ := json.Marshal(input)
	v2TmdbRenderResults(w, s, kind, body)
}

func v2TmdbRenderResults(w http.ResponseWriter, s *AppState, kind string, body []byte) {
	view := struct {
		Items []v2TMDBItem
		Error string
		Kind  string
	}{Kind: kind}
	endpoint := "/api/tmdb/discover"
	if strings.Contains(string(body), `"query"`) {
		endpoint = "/api/tmdb/search"
	}
	raw, status := v2InternalJSON(s, http.MethodPost, endpoint, nil, body)
	if status >= 400 {
		view.Error = v2JSONError(raw)
	} else {
		var payload struct {
			Items []map[string]any `json:"items"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			for _, item := range payload.Items {
				view.Items = append(view.Items, v2TMDBItemFromMap(kind, item))
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_tmdb_results", view, dict, eng)
}

// V2TmdbAdd adds a TMDB result to the library.
func V2TmdbAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	kind := strings.ToLower(strings.TrimSpace(r.FormValue("kind")))
	if kind != "movie" && kind != "series" {
		kind = "series"
	}
	payload := TmdbAddInput{
		Kind:        kind,
		Name:        strings.TrimSpace(r.FormValue("name")),
		Year:        strings.TrimSpace(r.FormValue("year")),
		TmdbId:      strings.TrimSpace(r.FormValue("tmdb_id")),
		TvdbId:      strings.TrimSpace(r.FormValue("tvdb_id")),
		Quality:     strings.TrimSpace(r.FormValue("quality")),
		Language:    strings.TrimSpace(r.FormValue("language")),
		Seasons:     strings.TrimSpace(r.FormValue("seasons")),
		ArchivePath: strings.TrimSpace(r.FormValue("archive_path")),
		Exclude:     strings.TrimSpace(r.FormValue("exclude")),
		Subtitle:    strings.TrimSpace(r.FormValue("subtitle")),
		Aliases:     strings.TrimSpace(r.FormValue("aliases")),
	}
	if payload.Kind == "" {
		payload.Kind = "series"
	}
	if payload.Kind != "movie" {
		if payload.Seasons == "" {
			payload.Seasons = "1+"
		}
	}
	if payload.Language == "" {
		payload.Language = "ita"
	}
	body, _ := json.Marshal(payload)
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/tmdb/add", nil, body)
	result := map[string]any{}
	if status >= 400 {
		result["Message"] = v2JSONError(raw)
		result["Error"] = true
	} else {
		result["Message"] = "aggiunto"
		if redirect := strings.TrimSpace(r.FormValue("redirect")); r.Header.Get("HX-Request") != "" && strings.HasPrefix(redirect, "/v2") {
			w.Header().Set("HX-Redirect", redirect)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_tmdb_add_result", result, dict, eng)
}

func V2TmdbManual(w http.ResponseWriter, r *http.Request, s *AppState) {
	kind := strings.ToLower(strings.TrimSpace(r.FormValue("kind")))
	if kind != "movie" {
		kind = "series"
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSpace(r.FormValue("query"))
	}
	view := map[string]string{
		"Kind": kind, "Name": name,
		"Year": strings.TrimSpace(r.FormValue("year")), "TmdbID": strings.TrimSpace(r.FormValue("tmdb_id")),
		"TvdbID": strings.TrimSpace(r.FormValue("tvdb_id")), "Quality": strings.TrimSpace(r.FormValue("quality")),
		"Language": strings.TrimSpace(r.FormValue("language")), "Seasons": "1+",
		"Redirect": strings.TrimSpace(r.FormValue("redirect")),
	}
	if view["Redirect"] == "" {
		view["Redirect"] = "/v2?view=search"
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_tmdb_manual", view, dict, eng)
}

func v2AnyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func v2AnyID(value any) string {
	switch typed := value.(type) {
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return strconv.FormatInt(parsed, 10)
		}
	}
	return v2AnyString(value)
}

func v2AnyInt(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// helper
// ---------------------------------------------------------------------------
// v2JSONError extracts the `error` field of a JSON error response, falling back
// to the raw body so a failure is never silent.
func v2JSONError(raw []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &payload) == nil && strings.TrimSpace(payload.Error) != "" {
		return payload.Error
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "operazione non riuscita"
	}
	if len(text) > 300 {
		text = text[:300]
	}
	return text
}
