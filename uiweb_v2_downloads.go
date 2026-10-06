package gextto

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func V2DownloadsSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	op := strings.TrimSpace(r.FormValue("op"))
	var path string
	var body []byte
	torrentNames := map[string]string{}
	switch op {
	case "unpin":
		path = "/api/torrents/unpin"
		body = []byte(`{}`)
	case "clear_completed":
		path = "/api/torrents/remove_completed"
		body, _ = json.Marshal(RemoveCompletedInput{DeleteFiles: false})
		for _, torrent := range s.activeEngine().List() {
			torrentNames[strings.ToLower(strings.TrimSpace(torrent.Hash))] = strings.TrimSpace(torrent.Name)
		}
	case "auto_remove":
		path = "/api/config/settings"
		body, _ = json.Marshal(SettingInput{Key: "auto_remove_completed", Value: r.FormValue("value")})
	case "temp_apply":
		path = "/api/torrents/temp-limits"
		body, _ = json.Marshal(TempLimitsInput{
			DownloadKib: v2ParseInt(r.FormValue("download_kib"), 0),
			UploadKib:   v2ParseInt(r.FormValue("upload_kib"), 0),
			Minutes:     v2ParseInt(r.FormValue("minutes"), 0),
		})
	case "temp_clear":
		path = "/api/torrents/temp-limits"
		body, _ = json.Marshal(TempLimitsInput{Clear: true})
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
	} else if op == "temp_apply" && status < 400 {
		dl := v2ParseInt(r.FormValue("download_kib"), 0)
		ul := v2ParseInt(r.FormValue("upload_kib"), 0)
		mins := v2ParseInt(r.FormValue("minutes"), 0)
		if mins > 0 {
			message = fmt.Sprintf("Limite temporaneo applicato: %d KiB/s DL · %d KiB/s UL per %d min", dl, ul, mins)
		} else {
			message = fmt.Sprintf("Limite temporaneo applicato: %d KiB/s DL · %d KiB/s UL (fino a rimozione)", dl, ul)
		}
	} else if op == "temp_clear" && status < 400 {
		message = "Limite temporaneo rimosso (ripristinati limiti normali)"
	}
	if r.Header.Get("HX-Request") == "" {
		query := url.Values{"view": {"downloads"}, "msg": {message}}
		http.Redirect(w, r, "/?"+query.Encode(), http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	view := v2TorrentsViewFrom(s, r, message, status >= 400)
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
	if op == "clear_completed" || op == "temp_apply" || op == "temp_clear" {
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
