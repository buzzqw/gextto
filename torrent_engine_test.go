package gextto

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

func TestParsePathMappings(t *testing.T) {
	mappings, err := ParsePathMappings(`
# comment
/gextto/downloads=/data/downloads
/gextto/tmp = /data/tmp
`)
	if err != nil {
		t.Fatalf("ParsePathMappings: %v", err)
	}
	if len(mappings) != 2 || mappings[0].Gextto != "/gextto/downloads" || mappings[0].Backend != "/data/downloads" {
		t.Fatalf("mappings = %+v", mappings)
	}
	if _, err := ParsePathMappings("/broken"); err == nil {
		t.Fatal("expected error for a line without '='")
	}
	if _, err := ParsePathMappings("=/data"); err == nil {
		t.Fatal("expected error for an empty path")
	}
}

func TestTranslatePathLongestMatch(t *testing.T) {
	mappings := []PathMapping{
		{Gextto: "/gextto/downloads", Backend: "/data/downloads"},
		{Gextto: "/gextto/downloads/tv", Backend: "/data/tv"},
	}
	got, ok := TranslateGexttoToBackend("/gextto/downloads/tv/show", mappings)
	if !ok || got != "/data/tv/show" {
		t.Fatalf("translate = %q, %v; want /data/tv/show", got, ok)
	}
	got, ok = TranslateGexttoToBackend("/gextto/downloads/movie", mappings)
	if !ok || got != "/data/downloads/movie" {
		t.Fatalf("translate = %q, %v; want /data/downloads/movie", got, ok)
	}
	if _, ok := TranslateGexttoToBackend("/elsewhere/x", mappings); ok {
		t.Fatal("unmapped path must not translate")
	}
	reverse, ok := TranslateBackendToGextto("/data/tv/show", mappings)
	if !ok || reverse != "/gextto/downloads/tv/show" {
		t.Fatalf("reverse = %q, %v", reverse, ok)
	}
}

func TestValidatePathMappings(t *testing.T) {
	required := []string{"/gextto/downloads"}
	if err := ValidatePathMappings(nil, required); err == nil {
		t.Fatal("empty mappings with a required path must fail")
	}
	mappings := []PathMapping{{Gextto: "/gextto", Backend: "/data"}}
	if err := ValidatePathMappings(mappings, required); err != nil {
		t.Fatalf("covered path rejected: %v", err)
	}
	overlap := []PathMapping{
		{Gextto: "/gextto", Backend: "/data"},
		{Gextto: "/gextto/downloads", Backend: "/mnt"},
	}
	if err := ValidatePathMappings(overlap, required); err == nil {
		t.Fatal("overlapping mappings must be refused")
	}
}

func TestValidateBackendMappingsSharedNamespace(t *testing.T) {
	dir := t.TempDir()
	if err := validateBackendMappings(nil, []string{dir}); err != nil {
		t.Fatalf("existing shared path rejected: %v", err)
	}
	if err := validateBackendMappings(nil, []string{filepath.Join(dir, "missing")}); err == nil {
		t.Fatal("missing shared path must be refused")
	}
	mappings := []PathMapping{{Gextto: dir, Backend: "/data"}}
	if err := validateBackendMappings(mappings, []string{dir}); err != nil {
		t.Fatalf("mapped path rejected: %v", err)
	}
}

func TestCapabilityParityIsComplete(t *testing.T) {
	parity := CapabilityParity()
	if len(parity) == 0 {
		t.Fatal("empty capability parity")
	}
	for capability, row := range parity {
		for _, backend := range []string{BackendEmbedded, BackendQbittorrent} {
			if strings.TrimSpace(row[backend]) == "" {
				t.Fatalf("capability %q missing backend %q", capability, backend)
			}
		}
	}
	if CapabilityMatrix(BackendEmbedded)["list"] != "full" {
		t.Fatal("embedded list must be full")
	}
	if CapabilityMatrix(BackendQbittorrent)["sync"] != "full" {
		t.Fatal("qbittorrent sync must be full")
	}
}

func TestSelectTorrentEngineBackends(t *testing.T) {
	// embedded -> nil engine (the adapter is built on demand).
	cfg := DefaultConfig()
	cfg.Settings["torrent_backend"] = BackendEmbedded
	if engine, _, err := selectTorrentEngine(&cfg); err != nil || engine != nil {
		t.Fatalf("embedded select = %v, %v", engine, err)
	}
	// A fresh installation with no saved backend uses gx-torrent.
	cfgDefault := DefaultConfig()
	if got := TorrentBackendName(&cfgDefault); got != BackendGxTorrent {
		t.Fatalf("fresh install default backend = %q, want %q", got, BackendGxTorrent)
	}
	// qbittorrent without a URL is refused.
	cfg.Settings["torrent_backend"] = BackendQbittorrent
	if _, _, err := selectTorrentEngine(&cfg); err == nil {
		t.Fatal("qbittorrent without url must be refused")
	}
	// Removed backend values fall back to the embedded implementation.
	cfg2 := DefaultConfig()
	cfg2.Settings["torrent_backend"] = "anacrolix"
	engine, _, err := selectTorrentEngine(&cfg2)
	if err != nil || engine != nil || TorrentBackendName(&cfg2) != BackendEmbedded {
		t.Fatalf("legacy anacrolix selection must fall back to embedded: engine=%v err=%v", engine, err)
	}
}

func TestAlternativeBackendSuppressesEmbeddedSession(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	cfg.StateDir = filepath.Join(dir, "state")
	cfg.LibtorrentDir = filepath.Join(dir, "downloads")
	cfg.LibtorrentTempDir = nil
	if err := os.MkdirAll(cfg.LibtorrentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg.DryRun = false
	cfg.LibtorrentEnabled = true
	cfg.Settings["torrent_backend"] = BackendEmbedded

	if alternativeBackendActive(&cfg) {
		t.Fatal("embedded backend must not suppress the libtorrent session")
	}
	cfg.Settings["torrent_backend"] = BackendQbittorrent
	cfg.Settings["qbittorrent_url"] = "http://127.0.0.1:1"
	if !alternativeBackendActive(&cfg) {
		t.Fatal("expected the qbittorrent backend to suppress the embedded session")
	}
	client, err := NewLibtorrentClient(&cfg)
	if err != nil {
		t.Fatalf("NewLibtorrentClient: %v", err)
	}
	defer client.Shutdown(&cfg)
	if !client.DryRun {
		t.Fatal("the embedded libtorrent session must be session-less with an alternative backend")
	}
}

func TestPreflightQbittorrentUnreachable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings["qbittorrent_url"] = "http://127.0.0.1:1"
	result := PreflightQbittorrent(&cfg)
	if result.OK || result.Connected {
		t.Fatalf("preflight unexpectedly succeeded: %+v", result)
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected a connectivity error")
	}
}

func TestPreflightQbittorrentWithFakeServer(t *testing.T) {
	fake := newFakeQB()
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	cfg := DefaultConfig()
	cfg.Settings["qbittorrent_url"] = server.URL
	result := PreflightQbittorrent(&cfg)
	if !result.OK || !result.Connected {
		t.Fatalf("preflight = %+v", result)
	}
	if result.AppVersion != "v5.0.0" {
		t.Fatalf("app version = %q", result.AppVersion)
	}
}

// TestTorrentBackendAndListUseActiveEngine installs a qBittorrent engine in a
// fully wired test AppState and checks that the read path and the status
// endpoint report it without touching libtorrent.
func TestTorrentBackendAndListUseActiveEngine(t *testing.T) {
	state := newTestAppState(t)
	fake := newFakeQB()
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	fake.setTorrents([]qbittorrent.Torrent{{
		Hash: "feedbeef", Name: "engine.mkv", State: "downloading",
		Progress: 0.5, Size: 100, SavePath: state.cfg.LibtorrentDir,
	}})

	cfg := state.cfg
	cfg.Settings["torrent_backend"] = BackendQbittorrent
	cfg.Settings["qbittorrent_url"] = server.URL
	engine, err := newQbittorrentEngine(cfg)
	if err != nil {
		t.Fatalf("newQbittorrentEngine: %v", err)
	}
	state.setActiveEngine(engine)

	api := httptest.NewServer(Router(state))
	defer api.Close()
	code, _, body := webGet(t, api, "/api/torrents")
	if code != 200 || !strings.Contains(string(body), "feedbeef") {
		t.Fatalf("GET /api/torrents -> %d: %s", code, body)
	}
	code, _, body = webGet(t, api, "/api/torrent-backend")
	if code != 200 || !strings.Contains(string(body), `"backend":"qbittorrent"`) {
		t.Fatalf("GET /api/torrent-backend -> %d: %s", code, body)
	}
	if !strings.Contains(string(body), "capability_matrix") {
		t.Fatalf("capability matrix missing: %s", body)
	}
}
