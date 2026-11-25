package gextto

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type v2SubtitlesView struct {
	Target      subtitleTarget
	Candidates  []subtitleCandidate
	AppliedPath string
	Message     string
	Error       bool
}

func V2SubtitleSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2SubtitlesView{Candidates: []subtitleCandidate{}}
	query := url.Values{}
	for _, key := range []string{"series", "season", "episode", "movie_id"} {
		if value := strings.TrimSpace(r.FormValue(key)); value != "" {
			query.Set(key, value)
		}
	}
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/subtitles/search?"+query.Encode(), nil, nil)
	if status >= 400 {
		view.Error, view.Message = true, v2JSONError(raw)
		v2Render(w, http.StatusOK, "v2_subtitles_modal", view, s)
		return
	}
	var payload subtitleSearchResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		view.Error, view.Message = true, "risposta sottotitoli non valida"
		v2Render(w, http.StatusOK, "v2_subtitles_modal", view, s)
		return
	}
	view.Target, view.Candidates = payload.Target, payload.Candidates
	v2Render(w, http.StatusOK, "v2_subtitles_modal", view, s)
}

func V2SubtitleApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2SubtitlesView{}
	season, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("season")), 10, 64)
	episode, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("episode")), 10, 64)
	movieID, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("movie_id")), 10, 64)
	fileID, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("file_id")), 10, 64)
	body, _ := json.Marshal(subtitleApplyRequest{
		Series: r.FormValue("series"), Season: season, Episode: episode, MovieID: movieID,
		FileID: fileID, Language: r.FormValue("language"), Forced: r.FormValue("forced") == "true",
		SDH: r.FormValue("sdh") == "true", Replace: r.FormValue("replace") == "true",
	})
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/subtitles/apply", nil, body)
	if status >= 400 {
		view.Error, view.Message = true, v2JSONError(raw)
		v2Render(w, http.StatusOK, "v2_subtitles_modal", view, s)
		return
	}
	var payload struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &payload)
	view.AppliedPath = payload.Path
	view.Message = "Sottotitolo installato."
	v2Render(w, http.StatusOK, "v2_subtitles_modal", view, s)
}
