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

func TestUiPartialsRequireTokenButShellIsPublic(t *testing.T) {
	state := newTestAppState(t)
	token := "ui-secret-token"
	if err := SaveSetting(state.cfg.DataDir, "api_token", token); err != nil {
		t.Fatalf("save token: %v", err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	// The shell contains no data and stays public, like the legacy SPA shell.
	if code, _, _ := webGet(t, server, "/ui"); code != http.StatusOK {
		t.Fatalf("shell should be public: %d", code)
	}
	// A data partial must require the token.
	response, err := http.Get(server.URL + "/ui/partial/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("partial without token -> %d, want 401", response.StatusCode)
	}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/ui/partial/dashboard", nil)
	request.Header.Set("x-gextto-token", token)
	authorized, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	authorized.Body.Close()
	if authorized.StatusCode != http.StatusOK {
		t.Fatalf("partial with token -> %d, want 200", authorized.StatusCode)
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
