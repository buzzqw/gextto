package gextto

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type v2ComicExploreItem struct {
	Title       string
	URL         string
	TagURL      string
	CoverURL    string
	Publisher   string
	Description string
	Date        string
}

type v2ComicExploreView struct {
	Items []v2ComicExploreItem
	Error string
}

func v2SectionComicsResult(raw []byte, status int) v2ComicExploreView {
	view := v2ComicExploreView{}
	if status >= 400 {
		view.Error = v2JSONError(raw)
		return view
	}
	var payload struct {
		Items []v2ComicExploreItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		view.Error = "risposta GetComics non valida"
		return view
	}
	view.Items = payload.Items
	return view
}

func V2ComicsExploreDownload(w http.ResponseWriter, r *http.Request, s *AppState) {
	postURL := strings.TrimSpace(r.FormValue("url"))
	title := strings.TrimSpace(r.FormValue("title"))
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/comics/links", nil, mustJSON(ComicLinksInput{Url: postURL}))
	message := "download non avviato"
	if status >= 400 {
		message = v2JSONError(raw)
	} else {
		var payload struct {
			Links ComicLinks `json:"links"`
		}
		if json.Unmarshal(raw, &payload) != nil {
			message = "risposta GetComics non valida"
		} else {
			link := ""
			method := "direct"
			if len(payload.Links.DownloadNow) > 0 {
				link = payload.Links.DownloadNow[0]
				method = "download_now"
			} else if len(payload.Links.Direct) > 0 {
				link = payload.Links.Direct[0]
			}
			if link == "" {
				message = "Download Now non trovato per questo post"
			} else {
				downloadRaw, downloadStatus := v2InternalJSON(s, http.MethodPost, "/api/comics/download", nil, mustJSON(ComicDownloadInput{Url: link, Method: method, Title: "Comic " + title, PostUrl: postURL}))
				status = downloadStatus
				if status < 400 {
					message = "avviato"
				} else {
					message = "download non avviato: " + v2JSONError(downloadRaw)
				}
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_comics_explore_action", map[string]any{"Error": status >= 400, "Message": message}, dict, eng)
}

func V2ComicsExploreSelect(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_comics_explore_select", v2ComicExploreItem{
		Title: r.FormValue("title"), URL: r.FormValue("url"), TagURL: r.FormValue("tag_url"),
		CoverURL: r.FormValue("cover_url"), Publisher: r.FormValue("publisher"),
		Description: r.FormValue("description"), Date: r.FormValue("date"),
	}, dict, eng)
}

func V2ComicsExploreAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	payload := ComicInput{Title: r.FormValue("title"), TagUrl: r.FormValue("tag_url"), PostUrl: r.FormValue("post_url"),
		CoverUrl: r.FormValue("cover_url"), Publisher: r.FormValue("publisher"), Description: r.FormValue("description"),
		FromDate: r.FormValue("from_date"), SavePath: r.FormValue("save_path")}
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/comics", nil, mustJSON(payload))
	message := "fumetto aggiunto"
	if status >= 400 {
		message = v2JSONError(raw)
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_comics_explore_action", map[string]any{"Error": status >= 400, "Message": message}, dict, eng)
}

func mustJSON(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}

// v2ComicsLinksView is deliberately small: the link finder returns grouped
// URLs, while the template turns each one into an explicit download action.
type v2ComicsLinksView struct {
	PostURL     string
	DownloadNow []string
	Direct      []string
	Torrents    []string
	Magnets     []string
	Mega        []string
	Error       string
}

func (v v2ComicsLinksView) Groups() []struct {
	Label  string
	Method string
	Items  []string
} {
	return []struct {
		Label  string
		Method string
		Items  []string
	}{
		{Label: "Download diretto", Method: "download_now", Items: v.DownloadNow},
		{Label: "File diretti", Method: "direct", Items: v.Direct},
		{Label: "Torrent", Method: "torrent", Items: v.Torrents},
		{Label: "Magnet", Method: "magnet", Items: v.Magnets},
		{Label: "Mega", Method: "mega", Items: v.Mega},
	}
}

func V2ComicsLinks(w http.ResponseWriter, r *http.Request, s *AppState) {
	postURL := strings.TrimSpace(r.FormValue("url"))
	view := v2ComicsLinksView{PostURL: postURL}
	if postURL == "" {
		view.Error = "inserisci l'URL del post GetComics"
	} else {
		body, _ := json.Marshal(ComicLinksInput{Url: postURL})
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/comics/links", nil, body)
		if status >= 400 {
			view.Error = v2JSONError(raw)
		} else {
			var payload struct {
				Links ComicLinks `json:"links"`
			}
			if err := json.Unmarshal(raw, &payload); err != nil {
				view.Error = "risposta GetComics non valida"
			} else {
				view.DownloadNow = payload.Links.DownloadNow
				view.Direct = payload.Links.Direct
				view.Torrents = payload.Links.Torrents
				view.Magnets = payload.Links.Magnets
				view.Mega = payload.Links.Mega
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_comics_links_result", view, dict, eng)
}

func V2ComicsDownload(w http.ResponseWriter, r *http.Request, s *AppState) {
	payload := ComicDownloadInput{
		Url: r.FormValue("url"), Method: r.FormValue("method"),
		Title: r.FormValue("title"), PostUrl: r.FormValue("post_url"), SavePath: r.FormValue("save_path"),
	}
	body, _ := json.Marshal(payload)
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/comics/download", nil, body)
	result := map[string]any{"Error": status >= 400}
	if status >= 400 {
		result["Message"] = v2JSONError(raw)
	} else {
		result["Message"] = "avviato"
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_comics_download_result", result, dict, eng)
}

// V2ComicsWeeklyForce queues a Weekly Pack from its recorded magnet or torrent
// link. It is separate from the generic table action so the user sees whether
// the pack was queued or was already present in the torrent client.
func V2ComicsWeeklyForce(w http.ResponseWriter, r *http.Request, s *AppState) {
	date := strings.TrimSpace(r.FormValue("date"))
	magnet := strings.TrimSpace(r.FormValue("magnet"))
	torrent := strings.TrimSpace(r.FormValue("torrent"))
	link := magnet
	method := "magnet"
	if link == "" {
		link = torrent
		method = "torrent"
	}
	result := map[string]any{"Title": "Weekly Pack", "Error": false}
	if date == "" || link == "" {
		result["Error"] = true
		result["Message"] = "link del Weekly Pack non disponibile"
	} else {
		payload := ComicDownloadInput{
			Url: link, Method: method, Title: "Weekly Pack " + date,
			PostUrl: "weekly:" + date,
		}
		raw, status := v2InternalJSON(s, http.MethodPost, "/api/comics/download", nil, mustJSON(payload))
		if status >= 400 {
			result["Error"] = true
			result["Message"] = v2JSONError(raw)
		} else {
			result["Message"] = "Weekly Pack " + date + " accodato via " + method
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_action_notice", result, dict, eng)
}

type v2ComicEditView struct {
	ID          int64
	Title       string
	TagURL      string
	PostURL     string
	FromDate    string
	SavePath    string
	CoverURL    string
	Publisher   string
	Description string
}

func v2ComicEditFrom(s *AppState, id int64) (v2ComicEditView, bool) {
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/comics", nil, nil)
	if status >= 400 {
		return v2ComicEditView{}, false
	}
	var items []ComicMonitored
	if json.Unmarshal(raw, &items) != nil {
		return v2ComicEditView{}, false
	}
	for _, item := range items {
		if item.ID == id {
			return v2ComicEditView{ID: item.ID, Title: item.Title, TagURL: item.TagURL, PostURL: item.PostURL,
				FromDate: item.FromDate, SavePath: item.SavePath, CoverURL: item.CoverURL,
				Publisher: item.Publisher, Description: item.Description}, true
		}
	}
	return v2ComicEditView{}, false
}

func V2ComicsEdit(w http.ResponseWriter, r *http.Request, s *AppState) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil {
		http.Error(w, "fumetto non valido", http.StatusBadRequest)
		return
	}
	view, ok := v2ComicEditFrom(s, id)
	if !ok {
		http.Error(w, "fumetto non trovato", http.StatusNotFound)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_comic_edit_modal", view, dict, eng)
}

func V2ComicsSave(w http.ResponseWriter, r *http.Request, s *AppState) {
	payload := ComicInput{Title: r.FormValue("title"), TagUrl: r.FormValue("tag_url"), PostUrl: r.FormValue("post_url"),
		CoverUrl: r.FormValue("cover_url"), Publisher: r.FormValue("publisher"), Description: r.FormValue("description"),
		FromDate: r.FormValue("from_date"), SavePath: r.FormValue("save_path")}
	body, _ := json.Marshal(payload)
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/comics", nil, body)
	if r.Header.Get("HX-Request") != "" {
		if status >= 400 {
			dict, eng := v2Dictionaries(s)
			v2Render(w, http.StatusOK, "v2_comics_save_result", map[string]any{"Error": true, "Message": v2JSONError(raw)}, dict, eng)
			return
		}
		w.Header().Set("HX-Redirect", "/?view=comics")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/?view=comics", http.StatusSeeOther)
}
