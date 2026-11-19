package gextto

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestSeriesIndexerTitlesUsesAliases(t *testing.T) {
	series := &SeriesConfig{Name: " Titolo Localizzato ", Aliases: []string{
		"", "titolo localizzato", "Original Title", "Another Title", "Third", "Fourth", "Too Many",
	}}
	got := seriesIndexerTitles(series)
	want := []string{"Titolo Localizzato", "Original Title", "Another Title", "Third", "Fourth"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("titles = %q, want %q", got, want)
	}
	if single := seriesIndexerTitles(&SeriesConfig{Name: "Solo"}); len(single) != 1 || single[0] != "Solo" {
		t.Fatalf("titles without aliases = %q, want [Solo]", single)
	}
	if none := seriesIndexerTitles(nil); none != nil {
		t.Fatalf("titles of no series = %q, want nil", none)
	}
}

// seriesAliasIndexer is a direct Torznab that answers every title search with
// the same release, so the caller is expected to merge the batches into one.
func seriesAliasIndexer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	queries := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		query := strings.TrimSpace(values.Get("q"))
		if query == "" {
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0"?><caps><searching>`+
				`<search available="yes" supportedParams="q"/>`+
				`<tv-search available="yes" supportedParams="q,season,ep"/></searching></caps>`)
			return
		}
		mu.Lock()
		queries = append(queries, query)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>`+
			`<item><title>%s.1080p.WEB-DL.x264-GRUPPO</title><size>2000000000</size>`+
			`<torznab:attr name="seeders" value="50"/>`+
			`<torznab:attr name="category" value="5000"/>`+
			`<torznab:attr name="infohash" value="0123456789012345678901234567890123456789"/>`+
			`</item></channel></rss>`, strings.ReplaceAll(query, " ", "."))
	}))
	t.Cleanup(server.Close)
	return server, &queries
}

func TestSeriesEpisodeSearchAsksEveryAlias(t *testing.T) {
	server, queries := seriesAliasIndexer(t)
	cfg := DefaultConfig()
	cfg.WebsearchEngines = nil
	cfg.Indexers = []IndexerConfig{{Name: "direct", URL: server.URL + "/api", APIKey: "k", Enabled: true}}
	series := &SeriesConfig{Name: "Titolo Localizzato", Aliases: []string{"Original Title"}, Enabled: true}

	releases := NewEngine().SearchSeriesEpisode(context.Background(), &cfg, series, 3, 2, true)
	if len(releases) != 1 {
		t.Fatalf("releases = %+v, want one after the merge", releases)
	}
	got := append([]string(nil), *queries...)
	sort.Strings(got)
	want := []string{"Original Title S03E02", "Titolo Localizzato S03E02"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("queries = %q, want %q", got, want)
	}
}

func TestSeriesSeasonSearchAsksEveryAlias(t *testing.T) {
	server, queries := seriesAliasIndexer(t)
	cfg := DefaultConfig()
	cfg.WebsearchEngines = nil
	cfg.Indexers = []IndexerConfig{{Name: "direct", URL: server.URL + "/api", APIKey: "k", Enabled: true}}
	series := &SeriesConfig{Name: "Titolo Localizzato", Aliases: []string{"Original Title"}, Enabled: true}

	if releases := NewEngine().SearchSeriesSeason(context.Background(), &cfg, series, 3, true); len(releases) != 1 {
		t.Fatalf("releases = %+v, want one after the merge", releases)
	}
	got := append([]string(nil), *queries...)
	sort.Strings(got)
	want := []string{"Original Title S03", "Titolo Localizzato S03"}
	if strings.Join(got, " | ") != strings.Join(want, " | ") {
		t.Fatalf("queries = %q, want %q", got, want)
	}
}

// A series searched under one name only must keep the single query it always
// sent: the alias fan-out must not add requests when there are no aliases.
func TestSeriesEpisodeSearchWithoutAliasesKeepsOneQuery(t *testing.T) {
	server, queries := seriesAliasIndexer(t)
	cfg := DefaultConfig()
	cfg.WebsearchEngines = nil
	cfg.Indexers = []IndexerConfig{{Name: "direct", URL: server.URL + "/api", APIKey: "k", Enabled: true}}
	series := &SeriesConfig{Name: "Example Show", Enabled: true}

	NewEngine().SearchSeriesEpisode(context.Background(), &cfg, series, 1, 2, false)
	if len(*queries) != 1 || (*queries)[0] != "Example Show S01E02" {
		t.Fatalf("queries = %q, want one title query", *queries)
	}
}
