package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// TestLiveEd2kCoverage measures how many missing episodes the automatic eD2k
// fallback would close, with the fallback's own rules (configured name and
// aliases, Kad + eD2k servers, exact episode, recognised quality, video file,
// ed2k_min_sources). It runs only against a live installation:
//
//	GEXTTO_ED2K_MEASURE=/path/to/data GEXTTO_ED2K_MEASURE_GAPS=http://127.0.0.1:5000 \
//	  go test . -run TestLiveEd2kCoverage -v -timeout 90m
//
// One gap per series is sampled (the earliest missing episode), searches are
// spaced by GEXTTO_ED2K_MEASURE_SPACING (default 20s), and the per-gap detail
// is written to GEXTTO_ED2K_MEASURE_OUT when set.
func TestLiveEd2kCoverage(t *testing.T) {
	dataDir := os.Getenv("GEXTTO_ED2K_MEASURE")
	if dataDir == "" {
		t.Skip("GEXTTO_ED2K_MEASURE not set")
	}
	cfgValue := DefaultConfig()
	cfgValue.DataDir = dataDir
	if err := cfgValue.loadConfigDB(); err != nil {
		t.Fatal(err)
	}
	cfg := &cfgValue
	if !ed2kEnabled(cfg) {
		t.Fatal("eD2k/aMule is not enabled in this installation")
	}
	spacing := 20 * time.Second
	if raw := os.Getenv("GEXTTO_ED2K_MEASURE_SPACING"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			spacing = parsed
		}
	}
	base := os.Getenv("GEXTTO_ED2K_MEASURE_GAPS")
	if base == "" {
		base = "http://127.0.0.1:5000"
	}
	response, err := http.Get(base + "/api/gaps")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items []struct {
			Series  string `json:"series"`
			Season  int64  `json:"season"`
			Episode int64  `json:"episode"`
		} `json:"items"`
	}
	err = json.NewDecoder(response.Body).Decode(&payload)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	first := map[string]int{}
	for index, gap := range payload.Items {
		previous, seen := first[gap.Series]
		if !seen || gap.Season < payload.Items[previous].Season ||
			(gap.Season == payload.Items[previous].Season && gap.Episode < payload.Items[previous].Episode) {
			first[gap.Series] = index
		}
	}
	names := make([]string, 0, len(first))
	for name := range first {
		names = append(names, name)
	}
	sort.Strings(names)

	type row struct {
		Series        string   `json:"series"`
		Episode       string   `json:"episode"`
		Queries       []string `json:"queries"`
		Results       int      `json:"results"`
		Exact         int      `json:"exact_episode"`
		ExactQuality  int      `json:"exact_with_quality"`
		ExactVideo    int      `json:"exact_quality_video"`
		Accepted      int      `json:"accepted"`
		BestName      string   `json:"best_name,omitempty"`
		BestSources   int64    `json:"best_sources,omitempty"`
		SearchSeconds float64  `json:"search_seconds"`
	}
	floor := ed2kMinSources(cfg)
	var rows []row
	both := ed2kSearchBoth
	for index, name := range names {
		gap := payload.Items[first[name]]
		season := gap.Season
		series := cfg.FindSeriesMatch(gap.Series, &season)
		if series == nil {
			continue
		}
		if index > 0 {
			time.Sleep(spacing)
		}
		item := row{Series: series.Name, Episode: fmt.Sprintf("S%02dE%02d", gap.Season, gap.Episode)}
		queries := []string{series.Name}
		for _, alias := range series.Aliases {
			if alias = strings.TrimSpace(alias); alias != "" && !strings.EqualFold(alias, series.Name) {
				queries = append(queries, alias)
			}
		}
		started := time.Now()
		seen := map[string]struct{}{}
		for _, query := range queries {
			text := fmt.Sprintf("%s %s", query, item.Episode)
			item.Queries = append(item.Queries, text)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			results, searchErr := searchAmule(ctx, cfg, text, &both)
			cancel()
			if searchErr != nil {
				t.Logf("%s: %v", text, searchErr)
			}
			for _, release := range ed2kReleases(results, "ed2k:amule") {
				link, err := ed2k.ParseFileLink(release.Ed2kLink)
				if err != nil {
					continue
				}
				if _, dup := seen[link.Hash]; dup {
					continue
				}
				seen[link.Hash] = struct{}{}
				item.Results++
				if !releaseMatchesConfiguredEpisode(&release, series, gap.Season, gap.Episode) {
					continue
				}
				item.Exact++
				if release.Quality.Resolution == "" || strings.EqualFold(release.Quality.Resolution, "unknown") {
					continue
				}
				item.ExactQuality++
				if !isVideoExtension(link.Name) {
					continue
				}
				item.ExactVideo++
				if release.Sources >= floor {
					item.Accepted++
				}
				if release.Sources > item.BestSources {
					item.BestSources, item.BestName = release.Sources, link.Name
				}
			}
		}
		item.SearchSeconds = time.Since(started).Seconds()
		t.Logf("%-40s %s results=%d exact=%d quality=%d video=%d accepted=%d best=%d %s",
			item.Series, item.Episode, item.Results, item.Exact, item.ExactQuality, item.ExactVideo, item.Accepted, item.BestSources, item.BestName)
		rows = append(rows, item)
	}
	covered, exact := 0, 0
	for _, item := range rows {
		if item.Accepted > 0 {
			covered++
		}
		if item.ExactVideo > 0 {
			exact++
		}
	}
	t.Logf("SUMMARY: %d gaps, %d with an exact video episode with quality, %d accepted by the fallback (>= %d sources)", len(rows), exact, covered, floor)
	if out := os.Getenv("GEXTTO_ED2K_MEASURE_OUT"); out != "" {
		raw, _ := json.MarshalIndent(map[string]any{"floor": floor, "rows": rows, "covered": covered, "exact": exact}, "", "  ")
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
