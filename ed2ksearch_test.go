package gextto

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/amule/amuletest"
	"github.com/buzzqw/gextto/internal/models"
)

func ed2kTestRelease() models.Release {
	link := "ed2k://|file|Rare%20Italian%20Movie%201974%20(1080p).avi|700000000|0123456789abcdef0123456789abcdef|/"
	release := ParseEd2kRelease("Rare Italian Movie 1974 (1080p).avi", link, "ed2k:kad", 700000000, 9, time.Now().UTC())
	if release == nil {
		panic("ParseEd2kRelease returned nil")
	}
	return *release
}

func TestParseEd2kRelease(t *testing.T) {
	release := ed2kTestRelease()
	if release.Network != "ed2k" {
		t.Fatalf("network = %q", release.Network)
	}
	if release.Magnet != "" {
		t.Fatalf("magnet must be empty, got %q", release.Magnet)
	}
	if !strings.HasPrefix(release.Ed2kLink, "ed2k://|file|") {
		t.Fatalf("link = %q", release.Ed2kLink)
	}
	if release.SizeBytes != 700000000 || release.Sources != 9 {
		t.Fatalf("size/sources = %d/%d", release.SizeBytes, release.Sources)
	}
	if release.Quality.Resolution != "1080p" {
		t.Fatalf("resolution = %q", release.Quality.Resolution)
	}
	if release.Seeders != -1 {
		t.Fatalf("eD2k sources must not be mapped to Seeders: %d", release.Seeders)
	}
}

func TestReleaseDedupKeyForEd2k(t *testing.T) {
	release := ed2kTestRelease()
	key, ok := releaseDedupKey(&release)
	if !ok || key != "ed2k:0123456789abcdef0123456789abcdef" {
		t.Fatalf("dedup key = %q, %v", key, ok)
	}
}

// ed2kTestCore starts a fake aMule core and returns a configuration that
// points at it, with the integration switched on.
func ed2kTestCore(t *testing.T, extra map[string]string) (*amuletest.Core, *Config) {
	t.Helper()
	core, err := amuletest.Start("secret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Close() })
	host, port, _ := net.SplitHostPort(core.Address())
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{
		"ed2k_enabled": "true", "amule_host": host, "amule_port": port, "amule_password": "secret",
		"ed2k_search_timeout_secs": "5",
	}
	for key, value := range extra {
		cfg.Settings[key] = value
	}
	ed2kInvalidateHealth()
	return core, cfg
}

func TestSearchED2KThroughAmule(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	core.SetResults([]amuletest.Result{{
		Hash: "dddddddddddddddddddddddddddddddd", Name: "The Matrix 1999 1080p.mkv", Size: 7000000000, Sources: 12,
	}}, "")
	releases := SearchED2K(context.Background(), cfg, "The Matrix 1999")
	if len(releases) != 1 {
		t.Fatalf("got %d releases", len(releases))
	}
	want := "ed2k://|file|The%20Matrix%201999%201080p.mkv|7000000000|dddddddddddddddddddddddddddddddd|/"
	if releases[0].Network != "ed2k" || releases[0].Ed2kLink != want || releases[0].Sources != 12 {
		t.Fatalf("release = %+v", releases[0])
	}
	if releases[0].Quality.Resolution != "1080p" {
		t.Fatalf("quality = %+v", releases[0].Quality)
	}
	if got := core.Searches(); len(got) != 1 || got[0] != "The Matrix 1999" {
		t.Fatalf("searches = %v", got)
	}
}

func TestSearchED2KRefusedByCore(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	core.SetResults(nil, "Kad search can't be done if Kad is not running")
	if got := SearchED2K(context.Background(), cfg, "anything"); len(got) != 0 {
		t.Fatalf("a refused search returned %d releases", len(got))
	}
}

func TestSearchED2KDisabledByDefault(t *testing.T) {
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.Settings = map[string]string{}
	if got := SearchED2K(context.Background(), cfg, "anything"); len(got) != 0 {
		t.Fatalf("disabled provider returned %d releases", len(got))
	}
	if got := SearchED2KManual(context.Background(), cfg, "anything"); len(got) != 0 {
		t.Fatalf("a manual search with aMule off returned %d releases", len(got))
	}
}

func TestEd2kFallbackRequiresReachableAmule(t *testing.T) {
	core, cfg := ed2kTestCore(t, map[string]string{"ed2k_fallback": "true"})
	if search, download := ed2kAvailability(cfg); !search || !download {
		t.Fatalf("availability = %v/%v", search, download)
	}
	if !ed2kFallbackEligible(cfg) {
		t.Fatal("fallback should be eligible")
	}
	cfg.Settings["ed2k_fallback"] = "false"
	if ed2kFallbackEligible(cfg) {
		t.Fatal("fallback must be off when the switch is off")
	}
	cfg.Settings["ed2k_fallback"] = "true"
	_ = core.Close()
	ed2kInvalidateHealth()
	if ed2kFallbackEligible(cfg) {
		t.Fatal("fallback must be off while aMule is unreachable")
	}
}

func TestEd2kWrongPasswordIsReported(t *testing.T) {
	_, cfg := ed2kTestCore(t, map[string]string{"amule_password": "wrong"})
	status := Ed2kRuntimeStatus(cfg)
	if status["reachable"] != false || !strings.Contains(status["last_error"].(string), "authentication") {
		t.Fatalf("status = %+v", status)
	}
}

func TestSearchSeriesED2KFiltersAutomaticCandidates(t *testing.T) {
	core, cfg := ed2kTestCore(t, map[string]string{"ed2k_min_sources": "3"})
	core.SetResults([]amuletest.Result{
		{Hash: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Name: "Rare Show S02E03 1080p.mkv", Size: 5000000000, Sources: 1},
		{Hash: "ffffffffffffffffffffffffffffffff", Name: "Rare Show S02E03 720p.mkv", Size: 900000000, Sources: 8},
		{Hash: "11111111111111111111111111111111", Name: "Rare Show S02E04 1080p.mkv", Size: 5000000000, Sources: 9},
	}, "")
	series := &SeriesConfig{Name: "Rare Show"}
	engine := &Engine{}
	got := engine.SearchSeriesED2K(context.Background(), cfg, series, 2, 3, false)
	if len(got) != 1 {
		t.Fatalf("accepted %d candidates, want 1: %+v", len(got), got)
	}
	if got[0].Sources != 8 || got[0].Quality.Resolution != "720p" {
		t.Fatalf("candidate = %+v", got[0])
	}
	if *got[0].Series != "Rare Show" {
		t.Fatalf("series = %q", *got[0].Series)
	}
}

func TestDatabaseStoresEd2kIdentity(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	release := ed2kTestRelease()
	if err := db.RegisterTorrentScored(&release, 10); err != nil {
		t.Fatalf("RegisterTorrentScored: %v", err)
	}
	const hash = "0123456789abcdef0123456789abcdef"
	if got := db.TorrentNetwork(hash); got != "ed2k" {
		t.Fatalf("network = %q", got)
	}
	meta, err := db.TorrentMeta(hash)
	if err != nil || meta == nil {
		t.Fatalf("TorrentMeta: %v, %+v", err, meta)
	}
	if meta.Release.Ed2kLink == "" || meta.Release.Network != "ed2k" {
		t.Fatalf("metadata = %+v", meta.Release)
	}
}

func TestSearchSeriesED2KQueriesAliasesAndDedupes(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	core.SetResults([]amuletest.Result{{
		Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Murdoch Mysteries S13E12 1080p.mkv", Size: 2000000000, Sources: 4,
	}}, "")
	series := &SeriesConfig{Name: "I misteri di Murdoch", Aliases: []string{"Murdoch Mysteries"}}
	engine := &Engine{}
	got := engine.SearchSeriesED2K(context.Background(), cfg, series, 13, 12, true)
	if len(got) != 1 {
		t.Fatalf("aliases returned %d candidates, want 1 after dedup: %+v", len(got), got)
	}
	joined := strings.Join(core.Searches(), "\n")
	for _, want := range []string{"I misteri di Murdoch S13E12", "Murdoch Mysteries S13E12"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("queries missing %q: %v", want, core.Searches())
		}
	}
}

func TestEd2kLinkAddAndDownloadActions(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	const link = "ed2k://|file|Rare%20Show%20S02E03.mkv|900000000|ffffffffffffffffffffffffffffffff|/"
	if err := AddEd2kLink(cfg, link); err != nil {
		t.Fatalf("add: %v", err)
	}
	if added, err := addEd2kRelease(cfg, &models.Release{Ed2kLink: link}); added || err != nil {
		t.Fatalf("a duplicate must be reported as not added: %v, %v", added, err)
	}
	rows := uiEd2kRows(cfg)
	if len(rows) != 1 || rows[0].Name != "Rare Show S02E03.mkv" || rows[0].Size != 900000000 {
		t.Fatalf("rows = %+v", rows)
	}
	if err := Ed2kDownloadAction(cfg, "pause", rows[0].Hash); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if rows = uiEd2kRows(cfg); !rows[0].Paused {
		t.Fatalf("not paused: %+v", rows[0])
	}
	if err := Ed2kDownloadAction(cfg, "remove", rows[0].Hash); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(core.Downloads()) != 0 {
		t.Fatalf("queue after remove = %+v", core.Downloads())
	}
}

func TestEd2kSessionReportsCompletion(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	resetPoll := func() {
		ed2kEventsMu.Lock()
		ed2kLastPoll = time.Time{}
		ed2kEventsMu.Unlock()
	}
	ed2kEventsMu.Lock()
	ed2kLastStatus = map[string]bool{}
	ed2kEventsKey = ""
	ed2kEventsMu.Unlock()
	resetPoll()
	core.SetDownloads([]amuletest.File{{Hash: "ffffffffffffffffffffffffffffffff", Name: "Rare Show S02E03.mkv", Size: 100, Done: 50, Status: 0}})
	session := ed2kSession{cfg: cfg}
	if events := session.PollEvents(); len(events) != 0 {
		t.Fatalf("an incomplete file produced events: %+v", events)
	}
	core.SetDownloads([]amuletest.File{{Hash: "ffffffffffffffffffffffffffffffff", Name: "Rare Show S02E03.mkv", Size: 100, Done: 100, Status: 9}})
	if early := session.PollEvents(); len(early) != 0 {
		t.Fatalf("a poll inside the interval read aMule again: %+v", early)
	}
	resetPoll()
	events := session.PollEvents()
	if len(events) != 1 || events[0].Kind != "torrent_finished" || events[0].SavePath != core.Incoming || events[0].Name != "Rare Show S02E03.mkv" {
		t.Fatalf("events = %+v", events)
	}
	resetPoll()
	if again := session.PollEvents(); len(again) != 0 {
		t.Fatalf("completion reported twice: %+v", again)
	}
	if cfgCopy := ed2kImportConfig(cfg); cfgCopy.Settings["move_episodes"] != "false" || cfg.Settings["move_episodes"] == "false" {
		t.Fatal("eD2k files must be imported by copy without changing the caller's configuration")
	}
}
