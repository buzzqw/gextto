package gextto

// uiweb_v2_episode_search.go is the inline search of one episode. The 🔍 next to
// an episode fills a frame under its row (on the Missing tab, under the series
// block) with the releases already in the archive and then with the online
// results, saying which sources it is waiting for. It used to open a modal that
// covered the page with a bare "online search in progress".
//
// The search runs beside the request: an indexer fan-out takes up to a minute,
// so the panel polls a small state kept on the AppState instead of holding an
// HTTP request open. The state lives here and not in the JobManager because
// several episode searches run side by side: they must not queue behind each
// other, nor fill the "background operations" panel with user clicks.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	// v2EpisodeSearchRetention keeps a finished search readable for a while: the
	// panel polls every second, a page left open must not lose its results.
	v2EpisodeSearchRetention = 10 * time.Minute
	// v2EpisodeSearchLimit bounds the state kept for finished searches.
	v2EpisodeSearchLimit = 40
)

// v2EpisodeSearch is one manual episode search. The registry mutex guards every
// field: the search goroutine writes them while the panel reads them.
type v2EpisodeSearch struct {
	ID      string
	Key     string
	Series  string
	Season  int64
	Episode int64

	// Phase is "sources" while the indexer fan-out runs, "ed2k" on the fallback.
	Phase string
	Done  int
	Total int

	Rows     []v2SourceRow
	Finished bool
	Error    string
	Started  time.Time
	Ended    time.Time
}

// v2EpisodeSearchRegistry keeps the searches of the open panels.
type v2EpisodeSearchRegistry struct {
	mu       sync.Mutex
	searches map[string]*v2EpisodeSearch
	running  map[string]string // dedup key -> search id
	order    []string
	next     uint64
}

func newV2EpisodeSearchRegistry() *v2EpisodeSearchRegistry {
	return &v2EpisodeSearchRegistry{
		searches: map[string]*v2EpisodeSearch{},
		running:  map[string]string{},
	}
}

// episodeSearches returns the registry of this state, creating it on first use so
// a hand-built state (tests, tools) works too.
func (s *AppState) episodeSearches() *v2EpisodeSearchRegistry {
	if registry := s.episode_search.Load(); registry != nil {
		return registry
	}
	registry := newV2EpisodeSearchRegistry()
	if s.episode_search.CompareAndSwap(nil, registry) {
		return registry
	}
	return s.episode_search.Load()
}

// begin starts a search for one episode, or joins the one already running for the
// same episode. It returns a copy of the state, safe to render.
func (r *v2EpisodeSearchRegistry) begin(parent context.Context, state v2EpisodeSearch, run func(ctx context.Context, id string)) v2EpisodeSearch {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked()
	if id, ok := r.running[state.Key]; ok {
		if current, ok := r.searches[id]; ok && !current.Finished {
			return *current
		}
		delete(r.running, state.Key)
	}
	r.next++
	state.ID = fmt.Sprintf("ep-%d-%d", time.Now().UnixNano(), r.next)
	state.Started = time.Now()
	state.Rows = nil
	state.Finished = false
	state.Error = ""
	state.Phase = ""
	state.Done, state.Total = 0, 0
	r.searches[state.ID] = &state
	r.running[state.Key] = state.ID
	r.order = append(r.order, state.ID)
	snapshot := state
	go run(parent, state.ID)
	return snapshot
}

// get returns a copy of one search.
func (r *v2EpisodeSearchRegistry) get(id string) (v2EpisodeSearch, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.searches[id]
	if !ok {
		return v2EpisodeSearch{}, false
	}
	return *state, true
}

// update edits one search under the registry lock.
func (r *v2EpisodeSearchRegistry) update(id string, edit func(*v2EpisodeSearch)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state, ok := r.searches[id]; ok {
		edit(state)
	}
}

// finish stores the outcome of a search and releases its deduplication key.
func (r *v2EpisodeSearchRegistry) finish(id string, rows []v2SourceRow, searchErr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.searches[id]
	if !ok {
		return
	}
	state.Rows = rows
	state.Error = searchErr
	state.Finished = true
	state.Ended = time.Now()
	if state.Key != "" && r.running[state.Key] == id {
		delete(r.running, state.Key)
	}
}

// pruneLocked drops searches nobody can be looking at anymore. The caller holds
// the lock.
func (r *v2EpisodeSearchRegistry) pruneLocked() {
	cutoff := time.Now().Add(-v2EpisodeSearchRetention)
	kept := r.order[:0]
	finished := 0
	for _, id := range r.order {
		state, ok := r.searches[id]
		switch {
		case !ok:
			continue
		case state.Finished && state.Ended.Before(cutoff):
			delete(r.searches, id)
			continue
		case state.Finished:
			finished++
		}
		kept = append(kept, id)
	}
	r.order = kept
	if finished <= v2EpisodeSearchLimit {
		return
	}
	drop := finished - v2EpisodeSearchLimit
	kept = r.order[:0]
	for _, id := range r.order {
		state, ok := r.searches[id]
		if drop > 0 && ok && state.Finished {
			delete(r.searches, id)
			drop--
			continue
		}
		kept = append(kept, id)
	}
	r.order = kept
}

// v2EpisodeSearchView is the inline panel: the archive releases at once, then
// the online search as a frame the panel follows.
type v2EpisodeSearchView struct {
	// Slot is the id of the element the panel fills: the close button empties it.
	Slot    string
	Label   string
	Archive v2SourcesView
	Online  v2EpisodeOnlineView
	Error   string
}

// v2EpisodeOnlineView is the polled part of the panel: the progress while the
// search runs, then the releases it found.
type v2EpisodeOnlineView struct {
	ID      string
	URL     string
	Running bool
	Phase   string
	Done    int
	Total   int
	Error   string
	Sources v2SourcesView
}

// v2EpisodeSearchSlotID names the container of the panel of one episode.
func v2EpisodeSearchSlotID(season, episode int64) string {
	return fmt.Sprintf("v2-ep-search-%d-%d", season, episode)
}

// v2EpisodeOnlineViewFrom turns the live state into the polled fragment.
func v2EpisodeOnlineViewFrom(state v2EpisodeSearch, redirect string) v2EpisodeOnlineView {
	view := v2EpisodeOnlineView{
		ID:      state.ID,
		URL:     "/series/episode-search-status?search=" + url.QueryEscape(state.ID),
		Running: !state.Finished,
		Phase:   state.Phase,
		Done:    state.Done,
		Total:   state.Total,
		Error:   state.Error,
	}
	if state.Finished {
		view.Sources = v2SourcesView{
			Hint:     "Nessun risultato online per questa puntata.",
			Redirect: redirect,
			Results:  state.Rows,
		}
	}
	return view
}

// V2SeriesEpisodeSearch renders the inline episode-search panel: the releases
// already in the archive, then the online search it follows.
func V2SeriesEpisodeSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	series := strings.TrimSpace(r.FormValue("series"))
	season, seasonErr := strconv.ParseInt(strings.TrimSpace(r.FormValue("season")), 10, 64)
	episode, episodeErr := strconv.ParseInt(strings.TrimSpace(r.FormValue("episode")), 10, 64)
	view := v2EpisodeSearchView{Label: strings.TrimSpace(r.FormValue("season")) + "E" + strings.TrimSpace(r.FormValue("episode"))}
	dict, eng := v2Dictionaries(s)
	// An episode outside the page (a stale tab, a hand-written URL) still gets
	// an answer in its own frame instead of a blank page.
	if series == "" || seasonErr != nil || episodeErr != nil || season < 1 || episode < 1 {
		view.Error = "Stagione o episodio non validi."
		v2Render(w, http.StatusOK, "v2_episode_search_panel", view, dict, eng)
		return
	}
	view.Slot = v2EpisodeSearchSlotID(season, episode)
	view.Label = fmt.Sprintf("S%02dE%02d", season, episode)
	redirect := "/?view=series&series=" + url.QueryEscape(series)
	view.Archive = v2SourcesView{Redirect: redirect, Hint: "Nessuna release in archivio per questa puntata."}
	stored := v2EpisodeResultsView(s, series, r.FormValue("season"), r.FormValue("episode"))
	for _, row := range stored.Results {
		if strings.EqualFold(row.Origin, episodeOriginArchive) {
			view.Archive.Results = append(view.Archive.Results, row)
		}
	}
	view.Archive.Results = v2DedupSources(view.Archive.Results)

	cfg := latestConfig(s)
	if cfg == nil {
		view.Error = "Ricerca non avviata: configurazione non disponibile."
		v2Render(w, http.StatusOK, "v2_episode_search_panel", view, dict, eng)
		return
	}
	ser, found := gh7_find_series(cfg, series)
	if !found {
		view.Error = "Serie non trovata."
		v2Render(w, http.StatusOK, "v2_episode_search_panel", view, dict, eng)
		return
	}
	parent := s.bgContext
	if parent == nil {
		parent = context.Background()
	}
	state := s.episodeSearches().begin(parent, v2EpisodeSearch{
		Key:     fmt.Sprintf("%s|%d|%d", strings.ToLower(ser.Name), season, episode),
		Series:  ser.Name,
		Season:  season,
		Episode: episode,
	}, func(ctx context.Context, id string) {
		v2RunEpisodeSearch(ctx, s, id, ser, season, episode)
	})
	view.Online = v2EpisodeOnlineViewFrom(state, redirect)
	v2Render(w, http.StatusOK, "v2_episode_search_panel", view, dict, eng)
}

// V2SeriesEpisodeSearchStatus renders the polled part of the panel: the live
// progress, or the online releases once the search is over.
func V2SeriesEpisodeSearchStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2EpisodeOnlineView{Error: "Ricerca non più disponibile: aprila di nuovo dalla puntata."}
	if id := strings.TrimSpace(r.FormValue("search")); id != "" {
		if state, ok := s.episodeSearches().get(id); ok {
			view = v2EpisodeOnlineViewFrom(state, "/?view=series&series="+url.QueryEscape(state.Series))
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_episode_search_online", view, dict, eng)
}

// v2RunEpisodeSearch follows one episode search and publishes what it is doing.
// The search itself goes through the JSON API, so the results are still stored
// in the archive and the gap is recorded exactly as with a synchronous search.
func v2RunEpisodeSearch(ctx context.Context, s *AppState, id string, series SeriesConfig, season, episode int64) {
	registry := s.episodeSearches()
	searchCtx := withSearchStages(ctx, func(name string) {
		registry.update(id, func(state *v2EpisodeSearch) {
			state.Phase = name
			if name == "sources" {
				// The fan-out starts with no answers: the count is filled by the
				// progress observer below.
				state.Done, state.Total = 0, 0
			}
		})
	})
	searchCtx = withSearchProgress(searchCtx, func(progress searchProgress) {
		registry.update(id, func(state *v2EpisodeSearch) {
			state.Phase = "sources"
			state.Done, state.Total = progress.Done, progress.Total
		})
	})
	path := "/api/episodes/" + url.PathEscape(series.Name) + "/" +
		strconv.FormatInt(season, 10) + "/" + strconv.FormatInt(episode, 10) + "/search"
	raw, status := v2InternalJSONContext(searchCtx, s, http.MethodPost, path, nil, []byte("{}"))
	if status >= 400 {
		logging.Warn("manual episode search failed",
			"series", series.Name, "season", season, "episode", episode, "status", status)
		registry.finish(id, nil, "Ricerca non riuscita.")
		return
	}
	var payload struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		logging.Warn("manual episode search returned an unreadable answer", "series", series.Name, "error", err)
		registry.finish(id, nil, "Ricerca non riuscita.")
		return
	}
	// The archive section above already lists what came from the archive: a
	// release must not appear twice in the same panel.
	rows := v2SourceRowsFrom(payload.Results)
	archived := map[string]bool{}
	for _, row := range rows {
		if strings.EqualFold(row.Origin, episodeOriginArchive) {
			archived[row.Key] = true
		}
	}
	online := make([]v2SourceRow, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(row.Origin, episodeOriginArchive) || archived[row.Key] {
			continue
		}
		online = append(online, row)
	}
	registry.finish(id, v2DedupSources(online), "")
}
