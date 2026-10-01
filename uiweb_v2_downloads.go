package gextto

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func V2DownloadsSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	op := strings.TrimSpace(r.FormValue("op"))
	var path string
	var body []byte
	switch op {
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
