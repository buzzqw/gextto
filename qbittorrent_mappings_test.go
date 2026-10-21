package gextto

// qbittorrent_mappings_test.go exercises the «Proponi» action that reads
// qBittorrent's own folders and fills the path mapping.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeQbitFolders answers the two calls the proposal makes: login and the
// application preferences.
func fakeQbitFolders(t *testing.T, savePath, tempPath string, tempEnabled bool) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "sid", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/preferences":
			if _, err := r.Cookie("SID"); err != nil {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"save_path":"` + savePath + `","temp_path":"` + tempPath +
				`","temp_path_enabled":` + map[bool]string{true: "true", false: "false"}[tempEnabled] + `}`))
		case "/api/v2/app/defaultSavePath":
			_, _ = w.Write([]byte(savePath))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func qbitTestConfig(url, downloadDir, tempDir string) *Config {
	temp := tempDir
	return &Config{
		DownloadDir:     downloadDir,
		DownloadTempDir: &temp,
		Settings: map[string]string{
			"qbittorrent_url":      url,
			"qbittorrent_username": "admin",
			"qbittorrent_password": "secret",
		},
	}
}

func TestProposeQbittorrentMappingsMapsTheBackendFolders(t *testing.T) {
	server := fakeQbitFolders(t, "/downloads", "/downloads/temp", true)
	cfg := qbitTestConfig(server.URL, "/home/andres/trasferimento", "/mnt/temp")
	proposal, added, err := proposeQbittorrentMappings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 {
		t.Fatalf("added %d mappings, want 2: %q", added, proposal)
	}
	for _, want := range []string{
		"/home/andres/trasferimento=/downloads",
		"/mnt/temp=/downloads/temp",
	} {
		if !strings.Contains(proposal, want) {
			t.Fatalf("proposal lacks %q: %q", want, proposal)
		}
	}
}

func TestProposeQbittorrentMappingsSkipsIdenticalFolders(t *testing.T) {
	server := fakeQbitFolders(t, "/home/andres/trasferimento", "", false)
	cfg := qbitTestConfig(server.URL, "/home/andres/trasferimento", "/mnt/temp")
	proposal, added, err := proposeQbittorrentMappings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || proposal != "" {
		t.Fatalf("identical folder proposed a mapping: added=%d %q", added, proposal)
	}
}
