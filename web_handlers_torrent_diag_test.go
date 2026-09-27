package gextto

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/qbittorrent"
)

func TestSelectivePriorities(t *testing.T) {
	files := []models.FileView{
		{Path: "Movie.2020.mkv", Size: 1000},
		{Path: "Movie.2020.srt", Size: 1},
		{Path: "sample.mkv", Size: 10},
		{Path: "info.nfo", Size: 1},
	}
	cases := map[string][]int32{
		"video":       {6, 1, 0, 0},
		"all":         {1, 1, 1, 1},
		"skip_extras": {1, 1, 0, 0},
	}
	for profile, want := range cases {
		got := selectivePriorities(files, profile)
		if len(got) != len(want) {
			t.Fatalf("%s: len = %d, want %d", profile, len(got), len(want))
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("%s[%d] = %d, want %d (%+v)", profile, index, got[index], want[index], got)
			}
		}
	}
	if selectivePriorities(files, "wat") != nil {
		t.Fatal("unknown profile must return nil")
	}
}

func TestTorrentPiecesUnsupportedBackend(t *testing.T) {
	state := newTestAppState(t)
	api := httptest.NewServer(Router(state))
	defer api.Close()
	code, _, _ := webGet(t, api, "/api/torrents/"+repeatHash("a")+"/pieces")
	if code != http.StatusConflict {
		t.Fatalf("pieces on embedded -> %d, want 409", code)
	}
}

func TestTorrentSelectiveEndpoint(t *testing.T) {
	state := newTestAppState(t)
	fake := newFakeQB()
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	fake.setTorrents([]qbittorrent.Torrent{{
		Hash: "abc", Name: "movie", State: "downloading", Progress: 0.5, Size: 100,
		SavePath: state.cfg.LibtorrentDir,
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
	code, body := webPostJSON(t, api, "/api/torrents/abc/selective", `{"profile":"video"}`)
	if code >= 400 {
		t.Fatalf("selective -> %d: %s", code, body)
	}
	// The single fake file is the main video, so it is set to priority 6.
	if got := fake.forms["/api/v2/torrents/filePrio"].Get("priority"); got != "6" {
		t.Fatalf("filePrio priority = %q, want 6", got)
	}
}

func repeatHash(character string) string {
	out := ""
	for i := 0; i < 40; i++ {
		out += character
	}
	return out
}
