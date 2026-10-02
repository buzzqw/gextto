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
	switch op {
	case "unpin":
		path = "/api/torrents/unpin"
		body = []byte(`{}`)
	case "clear_completed":
		path = "/api/torrents/remove_completed"
		body, _ = json.Marshal(RemoveCompletedInput{DeleteFiles: false})
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
	if status >= 400 {
		message = v2JSONError(raw)
	} else if op == "clear_completed" {
		var reply struct {
			Removed int `json:"removed"`
			Skipped int `json:"skipped"`
		}
		if err := json.Unmarshal(raw, &reply); err == nil {
			if reply.Removed > 0 {
				if reply.Removed == 1 {
					message = "1 torrent completato rimosso dalla sessione"
				} else {
					message = fmt.Sprintf("%d torrent completati rimossi dalla sessione", reply.Removed)
				}
			} else if reply.Skipped > 0 {
				message = fmt.Sprintf("Nessun torrent rimosso (%d ancora in seed)", reply.Skipped)
			} else {
				message = "Nessun torrent completato da rimuovere"
			}
		}
	}
	if r.Header.Get("HX-Request") == "" {
		query := url.Values{"view": {"downloads"}, "msg": {message}}
		http.Redirect(w, r, "/v2?"+query.Encode(), http.StatusSeeOther)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_torrents_wrap", v2TorrentsViewFrom(s, r, message, status >= 400), dict, eng)
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
