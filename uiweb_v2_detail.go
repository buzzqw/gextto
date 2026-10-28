package gextto

// uiweb_v2_detail.go implements the v2 series and movie detail pages. They reuse
// the existing Go view-models (uiSeriesDetailFrom / uiMovieDetailFrom) and only
// add v2 templates plus the two write paths the classic client performed by
// hand: saving a series (read-modify-write of the library array) and showing the
// per-episode sources.

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"
)

// v2ContentDetail returns the detail body when the view selects one item.
func v2ContentDetail(s *AppState, r *http.Request, view string) (string, any, bool) {
	switch view {
	case "series":
		if detail, ok := uiSeriesDetailFrom(s, r); ok {
			return "v2_series_detail", detail, true
		}
	case "movies":
		if detail, ok := uiMovieDetailFrom(s, r); ok {
			return "v2_movie_detail", detail, true
		}
	}
	return "", nil, false
}

// V2SeriesSave mirrors the classic series editor: it reads the whole library and
// replaces the edited series, then saves the array exactly like /api/config/series.
func V2SeriesSave(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := strings.TrimSpace(r.FormValue("name"))
	redirect := "/?view=series"
	if name != "" {
		redirect = "/?view=series&series=" + url.QueryEscape(name)
	}
	if name != "" {
		aliases := []string{}
		for _, part := range strings.Split(r.FormValue("aliases"), ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				aliases = append(aliases, trimmed)
			}
		}
		// Only the edited fields: the per-series endpoint keeps everything else.
		fields := map[string]any{
			"seasons":      r.FormValue("seasons"),
			"archive_path": r.FormValue("archive_path"),
			"exclude":      r.FormValue("exclude"),
			"aliases":      aliases,
		}
		// The ids are written only when the page carries them: a tab opened
		// before the TMDB field existed must not clear the stored id.
		for _, key := range []string{"tmdb_id", "tvdb_id"} {
			if _, ok := r.Form[key]; ok {
				fields[key] = r.FormValue(key)
			}
		}
		// Quality, language and subtitles come from the quality profile chosen
		// on the page (0 = keep the current values); they are still accepted
		// when posted directly.
		for _, key := range []string{"quality", "language", "subtitle"} {
			if _, ok := r.Form[key]; ok {
				fields[key] = r.FormValue(key)
			}
		}
		if id, err := strconv.Atoi(strings.TrimSpace(r.FormValue("quality_profile_id"))); err == nil && id > 0 {
			fields["quality_profile_id"] = id
		}
		// Audio minimo is set by the quality profile (and Modifica selezionati):
		// the page has no field for it, so it is sent only when posted.
		if _, ok := r.Form["audio"]; ok {
			fields["audio"] = r.FormValue("audio")
			fields["audio_channels"] = r.FormValue("audio_channels")
		}
		// Checkboxes send nothing when unchecked: the "flags" marker says the
		// form carried them, so an absent box means false.
		if r.FormValue("flags") != "" {
			fields["anime"] = r.FormValue("anime") == "true"
			// «Permetti upgrade» is set by the profile and by Modifica
			// selezionati: sent only when the form carries it.
			if _, ok := r.Form["disable_upgrades"]; ok {
				fields["disable_upgrades"] = r.FormValue("disable_upgrades") == "true"
			}
		}
		body, _ := json.Marshal(fields)
		v2InternalJSON(s, http.MethodPost, "/api/series/"+url.PathEscape(name), nil, body)
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, redirect, http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Redirect", redirect)
	w.WriteHeader(http.StatusNoContent)
}

type v2SourceRow struct {
	Title      string
	Episode    string
	Source     string
	Origin     string
	Score      int64
	SizeBytes  int64
	Magnet     template.URL
	Body       string
	Key        string
	LastResort bool
}

type v2SourcesView struct {
	Label       string
	Hint        string
	OnlineURL   string
	Redirect    string
	ShowEpisode bool
	Results     []v2SourceRow
}

// v2SourceRowFromRelease builds one table row from a release. The same row is
// used by the episode modal and by the whole-series missing search.
func v2SourceRowFromRelease(release *models.Release, origin string, season, episode int64) v2SourceRow {
	link := release.Magnet
	if strings.TrimSpace(link) == "" && release.TorrentURL != nil {
		link = *release.TorrentURL
	}
	ed2kLink := release.Ed2kLink
	sourceLabel := v2SourceLabel(release.Source)
	if release.Network == "ed2k" || ed2kLink != "" {
		// An eD2k result has a network name, not a host: v2SourceLabel would
		// misread it.
		sourceLabel = "eD2k/Kad"
	}
	keyLink := link
	if strings.TrimSpace(keyLink) == "" {
		keyLink = ed2kLink
	}
	body, _ := json.Marshal(map[string]any{
		"items": []map[string]string{{"title": release.Title, "magnet": link, "source": release.Source, "ed2k_link": ed2kLink}},
	})
	row := v2SourceRow{
		Title:      release.Title,
		Source:     sourceLabel,
		Origin:     origin,
		Score:      release.Score,
		SizeBytes:  release.SizeBytes,
		Magnet:     uiMagnetURL(link),
		Key:        v2SourceKey(keyLink, release.Title),
		LastResort: release.LastResort,
	}
	if season > 0 && episode > 0 {
		row.Episode = fmt.Sprintf("S%02dE%02d", season, episode)
	}
	if strings.TrimSpace(link) != "" || strings.TrimSpace(ed2kLink) != "" {
		row.Body = string(body)
	}
	return row
}

// v2SourceRowsFrom turns the JSON `results` of an episode search into rows.
// The episode is filled when the payload carries it (the missing-episodes bulk
// search), so the same table can name the episode each release belongs to.
func v2SourceRowsFrom(entries []map[string]any) []v2SourceRow {
	rows := make([]v2SourceRow, 0, len(entries))
	for _, entry := range entries {
		raw, _ := json.Marshal(entry["release"])
		if raw == nil || string(raw) == "null" {
			raw, _ = json.Marshal(entry)
		}
		var release models.Release
		if json.Unmarshal(raw, &release) != nil {
			continue
		}
		season, episode := int64(v2Float(entry["season"])), int64(v2Float(entry["episode"]))
		row := v2SourceRowFromRelease(&release, v2String(entry["origin"]), season, episode)
		if score := int64(v2Float(entry["score"])); score != 0 {
			row.Score = score
		}
		rows = append(rows, row)
	}
	return rows
}

// v2EpisodeResultsView builds the modal view for one episode, either from the
// sources already collected (live=false) or from a fresh online search
// (live=true).
func v2EpisodeResultsView(s *AppState, series, season, episode string, live bool) v2SourcesView {
	view := v2SourcesView{Label: "S" + season + "E" + episode}
	if series == "" || season == "" || episode == "" {
		return view
	}
	view.Redirect = "/?view=series&series=" + url.QueryEscape(series)
	base := "/api/episodes/" + url.PathEscape(series) + "/" + url.PathEscape(season) + "/" + url.PathEscape(episode)
	path := base + "/sources"
	method := http.MethodGet
	var body []byte
	if live {
		path = base + "/search"
		method = http.MethodPost
		body = []byte("{}")
	}
	raw, status := v2InternalJSON(s, method, path, nil, body)
	if status >= 400 {
		return view
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return view
	}
	view.Results = v2SourceRowsFrom(payload.Results)
	return view
}

// v2SourceKey returns a stable identity for a release so the same release
// coming from different origins (feed, archive, indexer) is listed once.
func v2SourceKey(link, title string) string {
	if value, ok := utils.MagnetHash(link); ok {
		return value
	}
	if strings.TrimSpace(link) != "" {
		return link
	}
	return "title:" + strings.ToLower(strings.TrimSpace(title))
}

// v2SourcePreferred reports whether `candidate` is a better copy of the same
// release than `current` (richer metadata wins, then the origin).
func v2SourcePreferred(candidate, current v2SourceRow) bool {
	rank := func(origin string) int {
		switch {
		case strings.EqualFold(origin, episodeOriginIndexer):
			return 3
		case strings.EqualFold(origin, episodeOriginArchive):
			return 2
		case strings.EqualFold(origin, episodeOriginFeed):
			return 1
		default:
			return 0
		}
	}
	if rank(candidate.Origin) != rank(current.Origin) {
		return rank(candidate.Origin) > rank(current.Origin)
	}
	return candidate.SizeBytes > current.SizeBytes
}

// v2DedupSources keeps one row per release, preserving the original order.
func v2DedupSources(rows []v2SourceRow) []v2SourceRow {
	index := map[string]int{}
	out := make([]v2SourceRow, 0, len(rows))
	for _, row := range rows {
		if at, ok := index[row.Key]; ok {
			if v2SourcePreferred(row, out[at]) {
				out[at] = row
			}
			continue
		}
		index[row.Key] = len(out)
		out = append(out, row)
	}
	return out
}

// V2SeriesSources renders the per-episode sources modal using only the local
// archive. It never triggers an online search (MirCrew / RSS feeds); use the
// search button for that.
func V2SeriesSources(w http.ResponseWriter, r *http.Request, s *AppState) {
	all := v2EpisodeResultsView(s, r.FormValue("series"), r.FormValue("season"), r.FormValue("episode"), false)
	view := v2SourcesView{Label: all.Label, Hint: "Nessuna release in archivio per questa puntata. Usa 🔍 per cercare online.", Redirect: all.Redirect}
	for _, row := range all.Results {
		if strings.EqualFold(row.Origin, episodeOriginArchive) {
			view.Results = append(view.Results, row)
		}
	}
	view.Results = v2DedupSources(view.Results)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_sources_modal", view, dict, eng)
}

// v2SeriesHistoryView is the download story of a series or of one episode.
type v2SeriesHistoryView struct {
	Label  string
	Events []AcquisitionEvent
	Error  string
	// Modal wraps the table in a dialog (episode button); the series page
	// loads the bare table into its own panel.
	Modal bool
}

// V2SeriesHistory renders the acquisition events of a series, or of one
// season/episode, newest first: a modal for an episode, a panel body for the
// whole series. With `hash` it shows the story of one download instead.
func V2SeriesHistory(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := strings.TrimSpace(r.FormValue("series"))
	season, seasonErr := optionalInt64(r.FormValue("season"))
	episode, episodeErr := optionalInt64(r.FormValue("episode"))
	view := v2SeriesHistoryView{Label: series, Modal: r.FormValue("modal") != ""}
	limit := 50
	hash := strings.TrimSpace(r.FormValue("hash"))
	switch {
	case hash != "":
		// One download, oldest first: opened from the dashboard problems.
		events, err := s.db.AcquisitionEvents(hash)
		if err != nil {
			view.Error = "Storia non disponibile: " + err.Error()
		}
		view.Events = events
		view.Label = logging.AcqID(hash)
		if len(events) > 0 && events[0].Title != "" {
			view.Label = events[0].Title
		}
	case seasonErr != nil || episodeErr != nil:
		view.Error = "Stagione o episodio non validi."
	default:
		if season != nil && episode != nil {
			view.Label = fmt.Sprintf("%s S%02dE%02d", series, *season, *episode)
			limit = 200
		}
		events, err := s.db.AcquisitionEventsForTitle(series, season, episode, limit)
		if err != nil {
			view.Error = "Storia non disponibile: " + err.Error()
		}
		view.Events = events
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_series_history", view, dict, eng)
}

// V2SeriesEpisodeSearch opens the manual episode search modal: the local
// archive results are shown immediately, then the (slow) feed/indexer results
// are loaded asynchronously into #v2-sources-online.
func V2SeriesEpisodeSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := r.FormValue("series")
	season := r.FormValue("season")
	episode := r.FormValue("episode")
	stored := v2EpisodeResultsView(s, series, season, episode, false)
	view := v2SourcesView{Label: stored.Label, Redirect: stored.Redirect}
	for _, row := range stored.Results {
		if strings.EqualFold(row.Origin, episodeOriginArchive) {
			view.Results = append(view.Results, row)
		}
	}
	view.Results = v2DedupSources(view.Results)
	view.OnlineURL = "/series/episode-search-online?series=" + url.QueryEscape(series) +
		"&season=" + url.QueryEscape(season) + "&episode=" + url.QueryEscape(episode)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_episode_search_modal", view, dict, eng)
}

// V2SeriesEpisodeSearchOnline runs the online part of the search (feeds and
// indexers) and returns just those rows for the async modal section.
func V2SeriesEpisodeSearchOnline(w http.ResponseWriter, r *http.Request, s *AppState) {
	all := v2EpisodeResultsView(s, r.FormValue("series"), r.FormValue("season"), r.FormValue("episode"), true)
	view := v2SourcesView{Label: all.Label, Hint: "Nessun risultato online per questa puntata.", Redirect: all.Redirect}
	// A release already shown in the archive section must not reappear here.
	archived := map[string]bool{}
	for _, row := range all.Results {
		if strings.EqualFold(row.Origin, episodeOriginArchive) {
			archived[row.Key] = true
		}
	}
	for _, row := range all.Results {
		if strings.EqualFold(row.Origin, episodeOriginArchive) || archived[row.Key] {
			continue
		}
		view.Results = append(view.Results, row)
	}
	view.Results = v2DedupSources(view.Results)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_sources_rows", view, dict, eng)
}
