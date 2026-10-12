package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestIsLocalURL(t *testing.T) {
	local := []string{
		"http://localhost:9117/api",
		"http://127.0.0.1:9696/api",
		"http://192.168.1.5:8191/v1",
		"http://10.0.0.4/torznab",
	}
	for _, u := range local {
		if !isLocalURL(u) {
			t.Errorf("isLocalURL(%q) = false, want true", u)
		}
	}
	remote := []string{
		"https://indexer.example/api",
		"http://8.8.8.8/x",
		"not a url",
	}
	for _, u := range remote {
		if isLocalURL(u) {
			t.Errorf("isLocalURL(%q) = true, want false", u)
		}
	}
}

// flareTestServer answers every FlareSolverr request with a solved page.
func flareTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"solution": map[string]any{"status": 200, "response": body},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestIndexersUseFlareTransportWhenConfigured: with a FlareSolverr URL the
// configured indexers get the transport hook; without it they keep the default
// HTTP client.
func TestIndexersUseFlareTransportWhenConfigured(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(indexersSettingKey, `[{"name":"fake","url":"https://indexer.example/api","apikey":"k"}]`)
	if got := d.indexers(); len(got) != 1 || got[0].Get != nil {
		t.Fatalf("without FlareSolverr the hook must stay nil: %+v", got)
	}
	d.opts.Settings.Set(flareURLKey, "http://127.0.0.1:8191")
	if got := d.indexers(); len(got) != 1 || got[0].Get == nil {
		t.Fatalf("with FlareSolverr the hook must be set: %+v", got)
	}
}

// TestRemoteGetFallsBackToFlareSolverr: a remote host that fails directly is
// retried through FlareSolverr.
func TestRemoteGetFallsBackToFlareSolverr(t *testing.T) {
	flare := flareTestServer(t, "<rss>solved</rss>")
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(flareURLKey, flare.URL)

	body, err := d.remoteGet(context.Background(), "https://target.invalid/feed")
	if err != nil {
		t.Fatalf("remoteGet: %v", err)
	}
	if string(body) != "<rss>solved</rss>" {
		t.Fatalf("body = %q", body)
	}
}

// TestRemoteGetSkipsFlareForLocalHost: a LAN service that fails is not retried
// through FlareSolverr (it cannot help a local network error).
func TestRemoteGetSkipsFlareForLocalHost(t *testing.T) {
	flare := flareTestServer(t, "<rss>solved</rss>")
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer target.Close()

	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(flareURLKey, flare.URL)
	if _, err := d.remoteGet(context.Background(), target.URL+"/feed"); err == nil {
		t.Fatal("a failing local host must not be solved by FlareSolverr")
	}
}

// TestUIRssFlareTestHandler: the Test button probes the URL typed in the field.
func TestUIRssFlareTestHandler(t *testing.T) {
	flare := flareTestServer(t, "ok")
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/ui/rss-flaresolverr-test", url.Values{"url": {flare.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		MS    int64  `json:"ms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.OK {
		t.Fatalf("test endpoint = %+v, want ok", res)
	}

	// A bad URL is reported, not a 500.
	resp2, err := http.PostForm(srv.URL+"/ui/rss-flaresolverr-test", url.Values{"url": {"http://127.0.0.1:1/"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var bad struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&bad); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if bad.OK || bad.Error == "" {
		t.Fatalf("a bad endpoint must be reported: %+v", bad)
	}
}
