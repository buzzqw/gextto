package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

func V2DownloadsSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	op := strings.TrimSpace(r.FormValue("op"))
	var path string
	var body []byte
	torrentNames := map[string]string{}
	// The temporary limit being applied, for the confirmation message.
	var tempDL, tempUL, tempMinutes int64
	switch op {
	case "clear_completed":
		path = "/api/torrents/remove_completed"
		body, _ = json.Marshal(RemoveCompletedInput{DeleteFiles: false})
		for _, torrent := range s.activeEngine().List() {
			torrentNames[strings.ToLower(strings.TrimSpace(torrent.Hash))] = strings.TrimSpace(torrent.Name)
		}
	case "temp_apply":
		tempDL = v2ParseInt(r.FormValue("download_kib"), 0)
		tempUL = v2ParseInt(r.FormValue("upload_kib"), 0)
		tempMinutes = v2ParseInt(r.FormValue("minutes"), 0)
		path = "/api/torrents/temp-limits"
		body, _ = json.Marshal(TempLimitsInput{DownloadKib: tempDL, UploadKib: tempUL, Minutes: tempMinutes})
	case "temp_set":
		// A quick choice of the bandwidth menu changes one value (download,
		// upload or duration) of the temporary limit and keeps the others.
		tempDL, tempUL, tempMinutes = v2QuickLimitBase(s)
		if value := r.FormValue("dl"); value != "" {
			tempDL = v2ParseInt(value, tempDL)
		}
		if value := r.FormValue("ul"); value != "" {
			tempUL = v2ParseInt(value, tempUL)
		}
		if value := r.FormValue("minutes"); value != "" {
			tempMinutes = v2ParseInt(value, tempMinutes)
		}
		if settingsBool(latestConfig(s), "torrent_temp_auto", false) {
			// Auto would beat the new limit, and an all-zero limit does not
			// turn it off by itself.
			off := false
			autoBody, _ := json.Marshal(TempLimitsInput{Auto: &off})
			v2InternalJSON(s, http.MethodPost, "/api/torrents/temp-limits", nil, autoBody)
		}
		path = "/api/torrents/temp-limits"
		body, _ = json.Marshal(TempLimitsInput{DownloadKib: tempDL, UploadKib: tempUL, Minutes: tempMinutes})
	case "temp_clear":
		path = "/api/torrents/temp-limits"
		body, _ = json.Marshal(TempLimitsInput{Clear: true})
	case "temp_auto":
		// The temporary override's Auto mode: no cap now, the line to the tuning.
		autoSpeed := r.FormValue("value") == "true"
		path = "/api/torrents/temp-limits"
		body, _ = json.Marshal(TempLimitsInput{Auto: &autoSpeed})
	default:
		http.Error(w, "impostazione download non valida", http.StatusBadRequest)
		return
	}
	raw, status := v2InternalJSON(s, http.MethodPost, path, nil, body)
	message := "Impostazione salvata"
	var clearReply struct {
		Removed int      `json:"removed"`
		Skipped int      `json:"skipped"`
		Items   []string `json:"items"`
	}
	if status >= 400 {
		message = v2JSONError(raw)
	} else if op == "clear_completed" {
		if err := json.Unmarshal(raw, &clearReply); err == nil {
			if clearReply.Removed > 0 {
				names := make([]string, 0, len(clearReply.Items))
				for _, hash := range clearReply.Items {
					name := torrentNames[strings.ToLower(strings.TrimSpace(hash))]
					if name == "" {
						name = hash
					}
					names = append(names, name)
				}
				list := strings.Join(names, " · ")
				if clearReply.Removed == 1 {
					message = "1 torrent completato rimosso dalla sessione: " + list
				} else {
					message = fmt.Sprintf("%d torrent completati rimossi dalla sessione: %s", clearReply.Removed, list)
				}
			} else if clearReply.Skipped > 0 {
				message = fmt.Sprintf("Nessun torrent rimosso (%d ancora in seed)", clearReply.Skipped)
			} else {
				message = "Nessun torrent completato da rimuovere"
			}
		}
	} else if (op == "temp_apply" || op == "temp_set") && status < 400 {
		if tempMinutes > 0 {
			message = fmt.Sprintf("Limite temporaneo applicato: %d KiB/s DL · %d KiB/s UL per %d min", tempDL, tempUL, tempMinutes)
		} else {
			message = fmt.Sprintf("Limite temporaneo applicato: %d KiB/s DL · %d KiB/s UL (fino a rimozione)", tempDL, tempUL)
		}
	} else if op == "temp_clear" && status < 400 {
		message = "Limite temporaneo rimosso (ripristinati limiti normali)"
	} else if op == "temp_auto" && status < 400 {
		if r.FormValue("value") == "true" {
			message = "Auto speed attivo: nessun tetto, la linea la misura l'auto-tune"
		} else {
			message = "Auto speed disattivato (ripristinati i limiti normali)"
		}
	}
	if r.Header.Get("HX-Request") == "" {
		query := url.Values{"view": {"downloads"}, "msg": {message}}
		http.Redirect(w, r, "/?"+query.Encode(), http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	view := v2TorrentsViewFrom(s, r, message, status >= 400)
	view.Partial = true
	if op == "clear_completed" && len(clearReply.Items) > 0 {
		removedMap := make(map[string]struct{}, len(clearReply.Items))
		for _, item := range clearReply.Items {
			removedMap[strings.ToLower(strings.TrimSpace(item))] = struct{}{}
		}
		filteredRows := make([]uiTorrentRow, 0, len(view.Rows))
		for _, row := range view.Rows {
			if _, ok := removedMap[strings.ToLower(strings.TrimSpace(row.Hash))]; !ok {
				filteredRows = append(filteredRows, row)
			}
		}
		view.Rows = filteredRows
		view.Count = len(view.Rows)
	}
	if op == "clear_completed" || op == "temp_apply" || op == "temp_set" || op == "temp_clear" || op == "temp_auto" {
		title := "Limiti di velocità"
		if op == "clear_completed" {
			title = "Pulisci completati"
		}
		v2Render(w, http.StatusOK, "v2_downloads_settings_result", map[string]any{
			"View":   view,
			"Notice": map[string]any{"Title": title, "Message": message, "Error": status >= 400},
		}, dict, eng)
		return
	}
	v2Render(w, http.StatusOK, "v2_torrents_wrap", view, dict, eng)
}

// v2QuickLimitBase is where a quick choice of the bandwidth menu starts from:
// the temporary limit in force, or else the limits applied now with no expiry.
// While a schedule window is active a limit without expiry would be beaten by
// it, so a new quick limit lasts one hour there.
func v2QuickLimitBase(s *AppState) (download, upload, minutes int64) {
	page := uiDownloadsPageFor(s)
	if page.TempActive {
		return page.TempDL, page.TempUL, page.TempMinutes
	}
	cfg := latestConfig(s)
	download, upload, _, _ = speedPolicy(cfg)
	if download < 0 {
		download = 0
	}
	if upload < 0 {
		upload = 0
	}
	if page.SchedActive {
		minutes = 60
	}
	return download, upload, minutes
}

func V2HTTPDownloadDetail(w http.ResponseWriter, r *http.Request, s *AppState) {
	id := strings.TrimSpace(r.FormValue("id"))
	var found *ComicDownload
	for _, download := range HTTPDownloads() {
		if download.ID == id {
			copy := download
			found = &copy
			break
		}
	}
	if found == nil {
		http.Error(w, "download HTTP non trovato", http.StatusNotFound)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_http_download_detail", found, dict, eng)
}

// v2OrphansView is the data of the guided orphan-cleanup modal.
type v2OrphansView struct {
	Items      []OrphanDownload
	TotalBytes int64
	SafeBytes  int64
	Message    string
	Error      bool
}

func v2OrphansViewFrom(s *AppState) v2OrphansView {
	view := v2OrphansView{}
	if s == nil || s.db == nil {
		return view
	}
	view.Items = FindOrphanedDownloads(latestConfig(s), s.db, s.activeEngine())
	for _, item := range view.Items {
		view.TotalBytes += item.SizeBytes
		if item.Safe {
			view.SafeBytes += item.SizeBytes
		}
	}
	return view
}

// V2DownloadsOrphansModal lists the download-folder leftovers that no live
// torrent owns, with the verdict of the library check.
func V2DownloadsOrphansModal(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_orphans_modal", v2OrphansViewFrom(s), dict, eng)
}

// V2DownloadsOrphansClean moves the safe leftovers to the trash in a
// background job: a cross-filesystem copy can be tens of gigabytes and must
// not block the HTTP request. The modal says the job started; progress and the
// final count are in the jobs panel.
func V2DownloadsOrphansClean(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2OrphansViewFrom(s)
	cfg := latestConfig(s)
	torrents := s.activeEngine()
	job, created := s.jobs.Create("orphan-cleanup", "orphan-cleanup")
	if !created {
		view.Message = "Una pulizia dei file orfani è già in corso"
	} else if err := s.jobs.Start(job.ID, func(ctx context.Context, jobID string) {
		moved, movedBytes, err := CleanOrphanedDownloads(cfg, s.db, torrents)
		if err != nil {
			s.jobs.Fail(jobID, err)
			return
		}
		s.jobs.SetResult(jobID, map[string]any{"moved": moved, "bytes": movedBytes})
		s.jobs.SetProgress(jobID, 1, fmt.Sprintf("%d elementi · %s", moved, logging.HumanBytesI64(movedBytes)))
	}); err != nil {
		view.Message = err.Error()
		view.Error = true
	} else {
		view.Message = "Pulizia avviata: lo spostamento nel cestino può richiedere qualche minuto, controlla Operazioni"
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_orphans_modal", view, dict, eng)
}
