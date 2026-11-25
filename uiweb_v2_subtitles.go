package gextto

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	subtitleprovider "github.com/buzzqw/gextto/internal/subtitles"
)

type v2SubtitlesView struct {
	Target      subtitleTarget
	Candidates  []subtitleCandidate
	AppliedPath string
	Message     string
	Error       bool
}

type v2SubtitleTestView struct {
	OK      bool
	Message string
}

func subtitleSettingsInputFromForm(r *http.Request) subtitleSettingsInput {
	return subtitleSettingsInput{
		Enabled: r.FormValue("enabled"), ProviderURL: r.FormValue("provider_url"),
		APIKey: r.FormValue("api_key"), Username: r.FormValue("username"), Password: r.FormValue("password"),
		Forced: r.FormValue("forced"), SDH: r.FormValue("sdh"),
		Collision: r.FormValue("collision_policy"),
	}
}

func subtitleFriendlyError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "HTTP 401"), strings.Contains(message, "HTTP 403"), strings.Contains(message, "API key is required"):
		return "API key o credenziali non valide."
	case strings.Contains(message, "HTTP 429"), strings.Contains(message, "HTTP 503"), strings.Contains(message, "HTTP 502"), strings.Contains(message, "HTTP 504"):
		return "Provider temporaneamente non disponibile o limite di richieste raggiunto. Riprova tra poco."
	case strings.Contains(message, "no such host"), strings.Contains(message, "connection refused"), strings.Contains(message, "timeout"):
		return "Provider non raggiungibile. Controlla URL e rete."
	default:
		return message
	}
}

// V2SubtitleProviderTest checks the values currently in the form without
// saving them, so a typo is caught before the operator presses Save.
func V2SubtitleProviderTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2SubtitleTestView{}
	cfg := latestConfig(s)
	values, err := subtitleSettingsValues(cfg, subtitleSettingsInputFromForm(r))
	if err != nil {
		view.Message = subtitleFriendlyError(err)
		v2Render(w, http.StatusOK, "v2_subtitle_test_result", view, s)
		return
	}
	temporary := *cfg
	temporary.Settings = copyStringMap(cfg.Settings)
	for key, value := range values {
		temporary.Settings[key] = value
	}
	// Testing is allowed before the master switch is turned on.
	temporary.Settings[subtitleSettingEnabled] = "true"
	client, _, err := subtitleProviderClient(&temporary)
	if err == nil {
		_, err = client.Search(r.Context(), subtitleprovider.SearchQuery{Language: "en", Kind: "movie", Title: "test"})
	}
	if err != nil {
		view.Message = subtitleFriendlyError(err)
		v2Render(w, http.StatusOK, "v2_subtitle_test_result", view, s)
		return
	}
	view.OK = true
	v2Render(w, http.StatusOK, "v2_subtitle_test_result", view, s)
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
