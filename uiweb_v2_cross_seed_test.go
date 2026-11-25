package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestV2CrossSeedActionAppearsOnlyWithARealHash(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Example Show", Seasons: "1+", Enabled: true}}, nil); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(t.TempDir(), "Example.Show.S01E01.mkv")
	if err := state.db.SyncArchiveFileScored("Example Show", 1, 1, "Example.Show.S01E01", video, 1, 1); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("d", 40)
	if _, err := state.db.db.Exec("UPDATE episodes SET magnet_hash=?1 WHERE series_id=(SELECT id FROM series WHERE name='Example Show') AND season=1 AND episode=1", hash); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, page := v2Request(t, server, http.MethodGet, "/?view=series&series=Example+Show", nil)
	if code != http.StatusOK {
		t.Fatalf("series page -> %d", code)
	}
	if !strings.Contains(page, `hx-post="/library/cross-seed"`) || !strings.Contains(page, `name="hash" value="`+hash+`"`) {
		t.Fatalf("series page has no cross-seed action for the archived episode")
	}

	code, modal := v2Request(t, server, http.MethodPost, "/library/cross-seed", url.Values{"hash": {"invalid"}})
	if code != http.StatusOK || !strings.Contains(modal, "Infohash non valido") {
		t.Fatalf("cross-seed modal -> %d: %s", code, modal)
	}
}
