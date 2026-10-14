package main

// api_qbit_search.go implements the qBittorrent search API (/api/v2/search/*)
// on top of the daemon's Torznab indexers. qBittorrent's "plugins" are the
// configured indexers; a search job runs in the background and its results are
// kept in memory until the client deletes it. Registered only in standalone
// mode, like the rest of /api/v2.

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/torznab"
)

// qbitSearchResult is one entry of the qBittorrent search results, the shape
// the WebUI alternatives parse.
type qbitSearchResult struct {
	DescrLink  string `json:"descrLink"`
	FileName   string `json:"fileName"`
	FileSize   int64  `json:"fileSize"`
	FileURL    string `json:"fileUrl"`
	NbLeechers int    `json:"nbLeechers"`
	NbSeeders  int    `json:"nbSeeders"`
	SiteURL    string `json:"siteUrl"`
	PubDate    string `json:"pubDate"`
	EngineName string `json:"engineName"`
}

// qbitSearchJob is an in-memory search: its status is "Running" while the
// indexers are queried, "Stopped" once done (or stopped by the client).
type qbitSearchJob struct {
	pattern string
	status  string
	results []qbitSearchResult
	cancel  context.CancelFunc
}

// qbitSearchTimeout bounds one search job (the indexers are queried in
// parallel by the caller only by convention; here sequentially with a shared
// deadline).
const qbitSearchTimeout = 90 * time.Second

func (d *Daemon) handleQbitSearchStart(w http.ResponseWriter, r *http.Request) {
	pattern := strings.TrimSpace(r.FormValue("pattern"))
	plugins := qbitList(r.FormValue("plugins"))
	ctx, cancel := context.WithTimeout(context.Background(), qbitSearchTimeout)

	d.qbitSearchMu.Lock()
	if d.qbitSearch == nil {
		d.qbitSearch = map[int]*qbitSearchJob{}
	}
	d.qbitSearchSeq++
	id := d.qbitSearchSeq
	d.qbitSearch[id] = &qbitSearchJob{pattern: pattern, status: "Running", cancel: cancel}
	d.qbitSearchMu.Unlock()

	go d.qbitRunSearch(ctx, id, pattern, plugins)
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// qbitRunSearch queries the selected indexers and stores the results on the job.
func (d *Daemon) qbitRunSearch(ctx context.Context, id int, pattern string, plugins []string) {
	defer func() {
		// Release the context once the search is over (runs also when stopped).
		if c := d.qbitSearchCancel(id); c != nil {
			c()
		}
	}()
	results := make([]qbitSearchResult, 0, 64)
	if pattern != "" {
		filter := parseArchiveQuery(pattern, "", false)
		for _, indexer := range d.qbitSearchIndexers(plugins) {
			if ctx.Err() != nil {
				break
			}
			items, err := indexer.Search(ctx, pattern)
			if err != nil {
				logf("qbit search indexer %s: %v", indexer.Name, err)
				continue
			}
			for _, item := range items {
				if !filter.matchTitle(item.Title) {
					continue
				}
				results = append(results, qbitSearchResult{
					DescrLink:  item.Download(),
					FileName:   item.Title,
					FileSize:   item.Size,
					FileURL:    item.Download(),
					NbLeechers: item.Leechers,
					NbSeeders:  item.Seeders,
					PubDate:    qbitSearchPubDate(item.Published),
					EngineName: item.Indexer,
				})
			}
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].NbSeeders != results[j].NbSeeders {
			return results[i].NbSeeders > results[j].NbSeeders
		}
		return results[i].FileName < results[j].FileName
	})

	d.qbitSearchMu.Lock()
	if job := d.qbitSearch[id]; job != nil {
		job.results = results
		job.status = "Stopped"
		job.cancel = nil
	}
	d.qbitSearchMu.Unlock()
}

func (d *Daemon) qbitSearchCancel(id int) context.CancelFunc {
	d.qbitSearchMu.Lock()
	defer d.qbitSearchMu.Unlock()
	if job := d.qbitSearch[id]; job != nil {
		return job.cancel
	}
	return nil
}

// qbitSearchIndexers returns the indexers a job should query: all of them when
// "plugins" is empty or "all", otherwise only the named ones.
func (d *Daemon) qbitSearchIndexers(plugins []string) []torznab.Indexer {
	all := d.indexers()
	wanted := map[string]struct{}{}
	for _, name := range plugins {
		name = strings.TrimSpace(name)
		if name == "" || strings.EqualFold(name, "all") {
			return all
		}
		wanted[name] = struct{}{}
	}
	if len(wanted) == 0 {
		return all
	}
	out := make([]torznab.Indexer, 0, len(all))
	for _, idx := range all {
		if _, ok := wanted[idx.Name]; ok {
			out = append(out, idx)
		}
	}
	return out
}

func qbitSearchPubDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (d *Daemon) handleQbitSearchStatus(w http.ResponseWriter, r *http.Request) {
	only := qbitSearchID(r)
	type entry struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		Total  int    `json:"total"`
	}
	d.qbitSearchMu.Lock()
	ids := make([]int, 0, len(d.qbitSearch))
	for id := range d.qbitSearch {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]entry, 0, len(ids))
	for _, id := range ids {
		if only > 0 && id != only {
			continue
		}
		job := d.qbitSearch[id]
		out = append(out, entry{ID: id, Status: job.status, Total: len(job.results)})
	}
	d.qbitSearchMu.Unlock()
	writeJSON(w, http.StatusOK, out)
}

func (d *Daemon) handleQbitSearchResults(w http.ResponseWriter, r *http.Request) {
	id := qbitSearchID(r)
	d.qbitSearchMu.Lock()
	job := d.qbitSearch[id]
	status, total := "", 0
	var results []qbitSearchResult
	if job != nil {
		status = job.status
		total = len(job.results)
		results = append([]qbitSearchResult(nil), job.results...)
	}
	d.qbitSearchMu.Unlock()
	results = pageSlice(results, r.URL.Query().Get("limit"), r.URL.Query().Get("offset"))
	if results == nil {
		results = []qbitSearchResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  status,
		"total":   total,
		"results": results,
	})
}

func (d *Daemon) handleQbitSearchStop(w http.ResponseWriter, r *http.Request) {
	id := qbitSearchID(r)
	d.qbitSearchMu.Lock()
	if job := d.qbitSearch[id]; job != nil {
		job.status = "Stopped"
		if job.cancel != nil {
			job.cancel()
		}
	}
	d.qbitSearchMu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitSearchDelete(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.FormValue("id"))
	if raw == "" || strings.EqualFold(raw, "all") {
		d.qbitSearchMu.Lock()
		for _, job := range d.qbitSearch {
			if job.cancel != nil {
				job.cancel()
			}
		}
		d.qbitSearch = map[int]*qbitSearchJob{}
		d.qbitSearchMu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	}
	d.qbitSearchMu.Lock()
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '|' }) {
		if id, err := strconv.Atoi(strings.TrimSpace(field)); err == nil {
			if job := d.qbitSearch[id]; job != nil {
				if job.cancel != nil {
					job.cancel()
				}
				delete(d.qbitSearch, id)
			}
		}
	}
	d.qbitSearchMu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// qbitSearchID parses the "id" parameter of the search endpoints (0 = none).
func qbitSearchID(r *http.Request) int {
	raw := strings.TrimSpace(r.FormValue("id"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	id, _ := strconv.Atoi(raw)
	return id
}

// qbitSearchPlugin is one entry of search/plugins: the configured indexers.
type qbitSearchPlugin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

func (d *Daemon) handleQbitSearchPlugins(w http.ResponseWriter, _ *http.Request) {
	out := []qbitSearchPlugin{}
	for _, idx := range d.indexers() {
		out = append(out, qbitSearchPlugin{Name: idx.Name, Version: "1.0", Enabled: true, URL: idx.URL})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleQbitSearchPluginManage accepts installPlugin/uninstallPlugin/
// enablePlugin/updatePlugins. The daemon has no plugin model: the indexers are
// configured from the page (/ui/rss) or in settings.json, so these are
// accepted and ignored (200), like qBittorrent apps that only call them to keep
// their UI happy.
func (d *Daemon) handleQbitSearchPluginManage(w http.ResponseWriter, r *http.Request) {
	logf("qbit %s: indexers are configured from /ui/rss", r.URL.Path)
	w.WriteHeader(http.StatusOK)
}
