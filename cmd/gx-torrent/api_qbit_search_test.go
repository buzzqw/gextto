package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// TestQbitSearchAPI covers the qBittorrent search API mapped onto a fake
// Torznab indexer: plugins, start, status, results, stop and delete.
func TestQbitSearchAPI(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)

	var hits int32
	idxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel>
<item>
  <title>The.Movie.2020.1080p</title>
  <size>1000</size>
  <link>http://example.test/1.torrent</link>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <torznab:attr name="seeders" value="10"/>
  <torznab:attr name="leechers" value="2"/>
</item>
</channel></rss>`)
	}))
	defer idxSrv.Close()

	raw, _ := json.Marshal([]map[string]string{{"name": "fake", "url": idxSrv.URL}})
	d.opts.Settings.Set(indexersSettingKey, string(raw))

	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	var plugins []qbitSearchPlugin
	getJSONInto(t, srv.URL+"/api/v2/search/plugins", &plugins)
	if len(plugins) != 1 || plugins[0].Name != "fake" || !plugins[0].Enabled {
		t.Fatalf("plugins = %+v", plugins)
	}

	var started struct {
		ID int `json:"id"`
	}
	if code := postFormJSON(t, srv.URL+"/api/v2/search/start", url.Values{"pattern": {"Movie"}, "plugins": {"all"}}, &started); code != http.StatusOK || started.ID == 0 {
		t.Fatalf("search/start: %d %+v", code, started)
	}

	// Wait for the job to stop (the fake indexer answers immediately).
	var status []struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		Total  int    `json:"total"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		status = nil
		getJSONInto(t, srv.URL+"/api/v2/search/status", &status)
		if len(status) == 1 && status[0].Status == "Stopped" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("search did not stop: %+v", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status[0].Total != 1 {
		t.Fatalf("status total = %d, want 1", status[0].Total)
	}

	var results struct {
		Status  string             `json:"status"`
		Total   int                `json:"total"`
		Results []qbitSearchResult `json:"results"`
	}
	getJSONInto(t, srv.URL+"/api/v2/search/results?id="+strconv.Itoa(started.ID), &results)
	if results.Total != 1 || len(results.Results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	got := results.Results[0]
	if got.FileName != "The.Movie.2020.1080p" || got.NbSeeders != 10 || got.NbLeechers != 2 || got.FileURL == "" {
		t.Fatalf("result mapping = %+v", got)
	}
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("the indexer was not queried")
	}

	// The management endpoints are accepted (no plugin model) without error.
	for _, path := range []string{"installPlugin", "uninstallPlugin", "enablePlugin", "updatePlugins"} {
		if code := postFormJSON(t, srv.URL+"/api/v2/search/"+path, url.Values{"source": {"x"}}, nil); code != http.StatusOK {
			t.Fatalf("%s: %d", path, code)
		}
	}

	// delete removes the job.
	if code := postFormJSON(t, srv.URL+"/api/v2/search/delete", url.Values{"id": {strconv.Itoa(started.ID)}}, nil); code != http.StatusOK {
		t.Fatalf("search/delete: %d", code)
	}
	status = nil
	getJSONInto(t, srv.URL+"/api/v2/search/status", &status)
	if len(status) != 0 {
		t.Fatalf("job was not deleted: %+v", status)
	}
}

func postFormJSON(t *testing.T, rawurl string, form url.Values, out any) int {
	t.Helper()
	resp, err := http.PostForm(rawurl, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("POST %s: decode: %v", rawurl, err)
		}
	}
	return resp.StatusCode
}
