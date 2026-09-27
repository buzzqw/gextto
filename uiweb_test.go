package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

func TestUiShellRendersNavigationParity(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/ui")
	if code != http.StatusOK {
		t.Fatalf("GET /ui -> %d", code)
	}
	html := string(body)
	if !strings.Contains(html, `data-view="dashboard"`) {
		t.Fatalf("shell missing page marker")
	}
	// Every page of the legacy navigation must be reachable from the new shell.
	for _, label := range []string{"Dashboard", "Scarico", "Serie TV", "Film", "Mancanti", "Esplora", "Archivio", "Fumetti", "Configurazione", "Integrazioni", "Manutenzione", "Salute", "Log", "Blocklist", "Manuale", "Licenza"} {
		if !strings.Contains(html, ">"+label+"<") {
			t.Fatalf("navigation missing %q", label)
		}
	}
	if !strings.Contains(html, "/ui/static/gextto-ui.js") {
		t.Fatal("shell does not load the new UI script")
	}
}

func TestUiPartialDashboardRenders(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, _, body := webGet(t, server, "/ui/partial/dashboard")
	if code != http.StatusOK || !strings.Contains(string(body), "Stato daemon") {
		t.Fatalf("dashboard partial -> %d: %s", code, body)
	}
}

func TestUiPartialTorrentsEscapesTorrentName(t *testing.T) {
	state := newTestAppState(t)
	fake := newFakeQB()
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	fake.setTorrents([]qbittorrent.Torrent{{
		Hash:     "abc",
		Name:     `<script>alert(1)</script>`,
		State:    "downloading",
		Progress: 0.5,
		Size:     1000,
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
	t.Cleanup(api.Close)
	code, _, body := webGet(t, api, "/ui/partial/torrents")
	if code != http.StatusOK {
		t.Fatalf("torrents partial -> %d", code)
	}
	html := string(body)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatalf("torrent name was not escaped: %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Fatalf("escaped torrent name missing: %s", html)
	}
	if !strings.Contains(html, "In scarico") {
		t.Fatalf("state label missing: %s", html)
	}
}

func TestUiPagesArePublicOnLan(t *testing.T) {
	state := newTestAppState(t)
	// A token may be configured, but the new UI is meant for a trusted LAN and
	// does not gate its pages or partials.
	if err := SaveSetting(state.cfg.DataDir, "api_token", "lan-token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	for _, path := range []string{"/ui", "/ui/partial/dashboard", "/ui/partial/torrents"} {
		if code, _, _ := webGet(t, server, path); code != http.StatusOK {
			t.Fatalf("GET %s -> %d, want 200", path, code)
		}
	}
}

func TestUiServerRenderedPages(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	cases := map[string]string{
		"dashboard": "Stato daemon",
		"downloads": "Scarico",
		"health":    "Percorsi",
		"logs":      "Log",
		"manual":    "Manuale",
		"license":   "Licenza",
	}
	for view, marker := range cases {
		code, _, body := webGet(t, server, "/ui?view="+view)
		if code != http.StatusOK || !strings.Contains(string(body), marker) {
			t.Fatalf("GET /ui?view=%s -> %d, missing %q", view, code, marker)
		}
	}
}

func TestUiListAndActionPages(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	cases := map[string]string{
		"series":       `data-endpoint="/api/series"`,
		"movies":       `data-endpoint="/api/movies"`,
		"gaps":         `data-endpoint="/api/gaps"`,
		"archive":      `data-endpoint="/api/archive"`,
		"blocklist":    `/api/blocklist/{hash}/remove`,
		"maintenance":  `/api/database/rescore`,
		"integrations": `/api/jellyfin/test`,
	}
	for view, marker := range cases {
		code, _, body := webGet(t, server, "/ui?view="+view)
		if code != http.StatusOK || !strings.Contains(string(body), marker) {
			t.Fatalf("GET /ui?view=%s -> %d, missing %q", view, code, marker)
		}
	}
	// Settings/Esplora/Fumetti still point to the legacy UI (no functionality lost).
	for _, view := range []string{"settings", "search", "comics"} {
		code, _, body := webGet(t, server, "/ui?view="+view)
		if code != http.StatusOK || !strings.Contains(string(body), "Apri la UI classica") {
			t.Fatalf("GET /ui?view=%s -> %d, missing legacy fallback", view, code)
		}
	}
}

func TestUiStateLabelParity(t *testing.T) {
	cases := map[string]string{
		"downloading":          "In scarico",
		"downloading_metadata": "Metadata",
		"stalled":              "In attesa di seed",
		"seeding":              "In seed",
		"finished":             "Completato",
		"checking_files":       "Verifica file",
		"paused":               "In pausa",
		"moving":               "Spostamento",
	}
	for state, want := range cases {
		if got := uiStateLabel(state); got != want {
			t.Fatalf("uiStateLabel(%q) = %q, want %q", state, got, want)
		}
	}
}
