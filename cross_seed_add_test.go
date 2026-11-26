package gextto

// The cross-seed action must be opt-in per source and must never download the
// payload: the torrent is added paused at metadata, pointed at the verified
// library copy and resumed only when every piece is already there.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

type crossSeedEngine struct {
	nullEngine
	views       map[string]models.TorrentView
	have        int
	total       int
	options     AddOptions
	savePath    string
	associated  string
	resumed     []string
	fileAdds    int
	metadataAdd int
}

func (e *crossSeedEngine) List() []models.TorrentView {
	out := make([]models.TorrentView, 0, len(e.views))
	for _, view := range e.views {
		out = append(out, view)
	}
	return out
}

func (e *crossSeedEngine) AddWithOptions(magnet string, cfg *Config, path *string, options AddOptions) (bool, error) {
	e.metadataAdd++
	e.options = options
	if path != nil {
		e.savePath = *path
	}
	hash := releaseInfoHash(&models.Release{Magnet: magnet})
	e.views[hash] = models.TorrentView{
		Hash: hash, HasMetadata: true, State: "paused",
		PiecesTotal: e.total, PiecesHave: e.have,
	}
	return true, nil
}

func (e *crossSeedEngine) AddTorrentFileWithOptions(string, *Config, *string, AddOptions) (*string, error) {
	e.fileAdds++
	hash := "file"
	return &hash, nil
}

func (e *crossSeedEngine) AssociateStorage(hash, destination string) (bool, error) {
	e.associated = destination
	return true, nil
}

func (e *crossSeedEngine) Resume(hash string) (bool, error) {
	e.resumed = append(e.resumed, hash)
	view := e.views[hash]
	view.IsSeeding = true
	e.views[hash] = view
	return true, nil
}

// crossSeedAddEnv builds a completed target, one exact-hash source and an
// engine whose local piece map is fully under the test's control.
func crossSeedAddEnv(t *testing.T, crossSeed bool, have, total int) (*AppState, *crossSeedEngine, string, string) {
	t.Helper()
	db, err := OpenDatabase(filepath.Join(t.TempDir(), "gextto.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	hash := strings.Repeat("a", 40)
	series := "Example Show"
	season, episode := int64(1), int64(2)
	release := models.Release{
		Title: "Example.Show.S01E02.1080p", Magnet: "magnet:?xt=urn:btih:" + hash,
		Source: "original", Kind: "series", Series: &series, Season: &season, Episode: &episode,
		EpisodeRange: []int64{episode}, Seeders: -1, Peers: -1,
	}
	if err := db.RegisterTorrentScored(&release, 100); err != nil {
		t.Fatal(err)
	}
	library := t.TempDir()
	processed := filepath.Join(library, "Example - S01E02.mkv")
	if err := os.WriteFile(processed, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkTorrentCompleted(hash, processed, 1234); err != nil {
		t.Fatal(err)
	}

	magnet := "magnet:?xt=urn:btih:" + hash + "&tr=https%3A%2F%2Ftracker.example%2Fannounce"
	xmlMagnet := strings.ReplaceAll(magnet, "&", "&amp;")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item>
			<title>Example.Show.S01E02.1080p</title>
			<torznab:attr name="jackettindexer" value="Tracker One"/>
			<torznab:attr name="magneturl" value="`+xmlMagnet+`"/>
		</item></channel></rss>`)
	}))
	t.Cleanup(server.Close)

	rule := `[{"name":"allow","source":"first","cross_seed":` + boolString(crossSeed) + `}]`
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{seedRulesKey: rule}
	cfg.Series = []SeriesConfig{{Name: "Example Show", Enabled: true}}
	cfg.Indexers = []IndexerConfig{{Name: "first", URL: server.URL, Enabled: true, Manager: ManagerJackett}}
	cfg.StateDir = t.TempDir()
	engine := &crossSeedEngine{views: map[string]models.TorrentView{}, have: have, total: total}
	state := &AppState{db: db, cfg: &cfg, torrent_engine: engine}
	return state, engine, hash, "first:Tracker One"
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestCrossSeedStartSeedsFromTheExistingCopy(t *testing.T) {
	state, engine, hash, source := crossSeedAddEnv(t, true, 8, 8)
	result, err := crossSeedStart(context.Background(), state, hash, source, "Example.Show.S01E02.1080p", true)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "seeding" {
		t.Fatalf("status = %v, want seeding", result["status"])
	}
	if engine.metadataAdd != 1 || engine.fileAdds != 0 {
		t.Fatalf("metadata adds %d, file adds %d; want one metadata-only add", engine.metadataAdd, engine.fileAdds)
	}
	if !engine.options.StopAtMetadata || !engine.options.Paused || engine.options.SeedMode {
		t.Fatalf("add options = %+v, want paused at metadata with the hash check on", engine.options)
	}
	if engine.associated == "" || engine.savePath != engine.associated {
		t.Fatalf("associated %q, save path %q; want the library folder", engine.associated, engine.savePath)
	}
	if len(engine.resumed) != 1 || engine.resumed[0] != hash {
		t.Fatalf("resumed %v, want the verified torrent", engine.resumed)
	}
}

func TestCrossSeedStartKeepsAnUnverifiedCopyPaused(t *testing.T) {
	state, engine, hash, source := crossSeedAddEnv(t, true, 3, 8)
	result, err := crossSeedStart(context.Background(), state, hash, source, "Example.Show.S01E02.1080p", true)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "kept_paused" {
		t.Fatalf("status = %v, want kept_paused", result["status"])
	}
	if len(engine.resumed) != 0 {
		t.Fatalf("resumed %v, want no resume for an unverified copy", engine.resumed)
	}
}

func TestCrossSeedStartNeedsAnExplicitOptIn(t *testing.T) {
	state, engine, hash, source := crossSeedAddEnv(t, false, 8, 8)
	_, err := crossSeedStart(context.Background(), state, hash, source, "Example.Show.S01E02.1080p", true)
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("err = %v, want the source to be refused", err)
	}
	if engine.metadataAdd != 0 || engine.fileAdds != 0 {
		t.Fatal("a source that is not opted in must not be added")
	}
}

func TestCrossSeedStartNeedsHnRConfirmationWithFiniteTargets(t *testing.T) {
	state, engine, hash, source := crossSeedAddEnv(t, true, 8, 8)
	state.cfg.Transfer.SeedRatio = 0.5
	state.cfg.Transfer.SeedTimeDays = 365
	_, err := crossSeedStart(context.Background(), state, hash, source, "Example.Show.S01E02.1080p", false)
	if err == nil || !strings.Contains(err.Error(), "hit-and-run") {
		t.Fatalf("err = %v, want the hit-and-run confirmation gate", err)
	}
	if engine.metadataAdd != 0 {
		t.Fatal("nothing may be added before the operator confirms the risk")
	}
}

func TestCrossSeedAllowedChecksHostAndCategory(t *testing.T) {
	cfg := &Config{Settings: map[string]string{seedRulesKey: `[{"name":"host","source":"tracker.example","category":"series","cross_seed":true}]`}}
	match := &crossSeedMatch{Source: "jackett:Some Indexer", TrackerHosts: []string{"tracker.example"}}
	target := &crossSeedTarget{Kind: "series"}
	if !crossSeedAllowed(cfg, match, target) {
		t.Fatal("a rule matching a tracker host must allow the source")
	}
	target.Kind = "movie"
	if crossSeedAllowed(cfg, match, target) {
		t.Fatal("a rule limited to series must not allow a movie")
	}
}
