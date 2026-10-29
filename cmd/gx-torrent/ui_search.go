package main

// ui_search.go is the standalone indexer search: it queries the configured
// Torznab indexers (Jackett, Prowlarr) and returns the results as JSON for the
// page. In managed mode Gextto owns the search, so it is not available.

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
	"github.com/buzzqw/gextto/internal/torznab"
)

// indexersSettingKey holds the JSON array of Torznab indexers in settings.json.
const indexersSettingKey = "indexers"

// indexers returns the configured Torznab indexers (empty when none/invalid).
func (d *Daemon) indexers() []torznab.Indexer {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(indexersSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []torznab.Indexer
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		logfError("cannot parse the indexers setting: %v", err)
		return nil
	}
	// With FlareSolverr configured, route the indexer requests through it (direct
	// first, FlareSolverr on failure), like Gextto does for external indexers.
	if d.flareEndpoint() != "" {
		for i := range list {
			list[i].Get = d.remoteGet
		}
	}
	return list
}

// handleUISearch queries every indexer and returns the merged results, best
// seeded first.
func (d *Daemon) handleUISearch(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	indexers := d.indexers()
	// The archive page reads this to tell "no indexer configured" from "no
	// result"; the body shape stays a plain array for the other callers.
	w.Header().Set("X-Gx-Indexers", strconv.Itoa(len(indexers)))
	if query == "" || len(indexers) == 0 {
		writeJSON(w, http.StatusOK, []torznab.Item{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	// Indexers often answer with loosely related items (some do an OR or a
	// full-text match); keep only the titles carrying every search term, the
	// same rule the local archive section applies, so the two agree.
	filter := parseArchiveQuery(query, "", false)
	results := make([]torznab.Item, 0, 64)
	for _, indexer := range indexers {
		items, err := indexer.Search(ctx, query)
		if err != nil {
			logf("indexer %s: %v", indexer.Name, err)
			continue
		}
		for _, item := range items {
			if !filter.matchTitle(item.Title) {
				continue
			}
			results = append(results, item)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Seeders != results[j].Seeders {
			return results[i].Seeders > results[j].Seeders
		}
		return results[i].Title < results[j].Title
	})
	// Archive the live results, so the next archive search finds them locally
	// instead of asking the indexers again (the same behaviour as the Wanted
	// live search). The key matches the one the Wanted search uses.
	if len(results) > 0 {
		items := make([]rss.Item, 0, len(results))
		for _, item := range results {
			items = append(items, rssItemFromTorznab(item))
		}
		if _, err := d.archive().record("search:"+strings.ToLower(query), items, time.Now()); err != nil {
			logfError("archive search %q: cannot archive the indexer results: %v", query, err)
		}
	}
	writeJSON(w, http.StatusOK, results)
}
