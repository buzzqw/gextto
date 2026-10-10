package main

// ui_search.go is the standalone indexer search: it queries the configured
// Torznab indexers (Jackett, Prowlarr) and returns the results as JSON for the
// page. In managed mode Gextto owns the search, so it is not available.

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

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
		logf("cannot parse the indexers setting: %v", err)
		return nil
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
	if query == "" || len(indexers) == 0 {
		writeJSON(w, http.StatusOK, []torznab.Item{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	results := make([]torznab.Item, 0, 64)
	for _, indexer := range indexers {
		items, err := indexer.Search(ctx, query)
		if err != nil {
			logf("indexer %s: %v", indexer.Name, err)
			continue
		}
		results = append(results, items...)
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Seeders != results[j].Seeders {
			return results[i].Seeders > results[j].Seeders
		}
		return results[i].Title < results[j].Title
	})
	writeJSON(w, http.StatusOK, results)
}
