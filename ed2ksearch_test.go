package gextto

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	ed2kForgetQueue()
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
		{Hash: "44444444444444444444444444444444", Name: "Rare Show S02E03 720p.rar", Size: 900000000, Sources: 20},
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
	// The automatic fallback asks Kad and the eD2k servers.
	if searches := core.Searches(); len(searches) != 2 {
		t.Fatalf("automatic searches = %v, want one per network", searches)
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
	rows := uiEd2kRows(Ed2kDownloads(cfg))
	if len(rows) != 1 || rows[0].Name != "Rare Show S02E03.mkv" || rows[0].Size != 900000000 {
		t.Fatalf("rows = %+v", rows)
	}
	if err := Ed2kDownloadAction(cfg, "pause", rows[0].Hash); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if rows = uiEd2kRows(Ed2kDownloads(cfg)); !rows[0].Paused {
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
		ed2kForgetQueue()
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
}

// TestEd2kSessionFindsFilesThatLeftTheQueue covers aMule 3.x, which drops a
// finished file from the download queue at once: the completion is found
// among the shared files, both for a file seen in the queue and for a file
// the database still lists as pending after a Gextto restart.
func TestEd2kSessionFindsFilesThatLeftTheQueue(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	reset := func() {
		ed2kEventsMu.Lock()
		ed2kLastStatus = map[string]bool{}
		ed2kReported = map[string]time.Time{}
		ed2kEventsKey = ""
		ed2kLastPoll = time.Time{}
		ed2kEventsMu.Unlock()
	}
	poll := func(session ed2kSession) []models.TorrentEvent {
		ed2kEventsMu.Lock()
		ed2kLastPoll = time.Time{}
		ed2kEventsMu.Unlock()
		ed2kForgetQueue()
		return session.PollEvents()
	}
	reset()
	const seen = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const restarted = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	core.SetDownloads([]amuletest.File{
		{Hash: seen, Name: "Seen S01E01.mkv", Size: 100, Done: 10},
		{Hash: restarted, Name: "Restarted S01E02.mkv", Size: 100, Done: 10},
	})
	pending := []string{seen, restarted}
	session := ed2kSession{cfg: cfg, pending: func() ([]string, error) { return pending, nil }}
	if events := poll(session); len(events) != 0 {
		t.Fatalf("events before completion: %+v", events)
	}
	core.Finish(seen, "/data/amule")
	events := poll(session)
	if len(events) != 1 || events[0].Hash != seen || events[0].SavePath != "/data/amule" || events[0].Name != "Seen S01E01.mkv" {
		t.Fatalf("events = %+v", events)
	}
	if again := poll(session); len(again) != 0 {
		t.Fatalf("the completion was reported again at once: %+v", again)
	}
	// The completion handler archived it: the database no longer lists it.
	pending = []string{restarted}

	// Gextto restarts while aMule finishes the second file: only the pending
	// list from the database knows about it.
	reset()
	core.Finish(restarted, "/data/amule")
	events = poll(session)
	if len(events) != 1 || events[0].Hash != restarted {
		t.Fatalf("events after restart = %+v", events)
	}
	pending = nil
	if again := poll(session); len(again) != 0 {
		t.Fatalf("a file no longer pending was reported: %+v", again)
	}
}

// TestMonitorEd2kStalledGivesUpOnAutomaticDownloads checks the give-up rule:
// an automatically chosen download idle for longer than ed2k_stall_days is
// cancelled in aMule and blocklisted; a manual one and a recent one stay.
func TestMonitorEd2kStalledGivesUpOnAutomaticDownloads(t *testing.T) {
	core, cfg := ed2kTestCore(t, map[string]string{"ed2k_stall_days": "7"})
	db, err := OpenDatabase(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	register := func(name, hash, reason string) {
		link := "ed2k://|file|" + name + "|100|" + hash + "|/"
		release := ParseEd2kRelease(name, link, "ed2k:amule", 100, 3, time.Now().UTC())
		if err := db.RegisterTorrentScored(release, 10); err != nil {
			t.Fatal(err)
		}
		_ = db.SetTorrentReason(hash, reason)
	}
	const stale = "11111111111111111111111111111111"
	const manual = "22222222222222222222222222222222"
	const recent = "33333333333333333333333333333333"
	register("Rare Show S01E01 720p.mkv", stale, "gap_filled")
	register("Rare Show S01E02 720p.mkv", manual, "manual")
	register("Rare Show S01E03 720p.mkv", recent, "gap_filled")
	old := time.Now().Add(-10 * 24 * time.Hour).Unix()
	core.SetDownloads([]amuletest.File{
		{Hash: stale, Name: "Rare Show S01E01 720p.mkv", Size: 100, Done: 20, LastRecv: old},
		{Hash: manual, Name: "Rare Show S01E02 720p.mkv", Size: 100, Done: 20, LastRecv: old},
		{Hash: recent, Name: "Rare Show S01E03 720p.mkv", Size: 100, Done: 20, LastRecv: time.Now().Unix()},
	})
	notifierCfg := DefaultConfig()
	MonitorEd2kStalled(cfg, db, FromConfig(&notifierCfg), time.Now())
	left := map[string]bool{}
	for _, file := range core.Downloads() {
		left[file.Hash] = true
	}
	if left[stale] || !left[manual] || !left[recent] {
		t.Fatalf("queue after the check = %v", left)
	}
	if status, _ := db.TorrentStatus(stale); status == nil || *status != "error" {
		t.Fatalf("stale status = %v", status)
	}
	pending, _ := db.PendingEd2kHashes()
	if len(pending) != 2 {
		t.Fatalf("pending = %v", pending)
	}
	cfg.Settings["ed2k_stall_days"] = "0"
	core.SetDownloads([]amuletest.File{{Hash: recent, Name: "x", Size: 100, LastRecv: old}})
	ed2kForgetQueue()
	MonitorEd2kStalled(cfg, db, FromConfig(&notifierCfg), time.Now())
	if len(core.Downloads()) != 1 {
		t.Fatal("0 days must never give up")
	}
}

// TestAddNonVideoEd2kLinkIsNotRegistered keeps a pasted non-video link out of
// the library flow: aMule downloads it, Gextto does not register a release
// that the import would reject (and quarantine).
func TestAddNonVideoEd2kLinkIsNotRegistered(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	state := newTestAppState(t)
	for key, value := range cfg.Settings {
		if err := SaveSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	link := "ed2k://|file|eMule0.70b-Installer.exe|4266968|df0064f8199c5d4aff7359ca0ede089c|/"
	release := ParseEd2kRelease("eMule0.70b-Installer.exe", link, "ed2k:link", 4266968, 0, time.Now().UTC())
	status, value := gh3AddParsedRelease(state, *release)
	payload, _ := value.(map[string]any)
	if status != 202 || payload["registered"] != false {
		t.Fatalf("status %d, payload %+v", status, payload)
	}
	if len(core.Downloads()) != 1 {
		t.Fatalf("aMule queue = %+v", core.Downloads())
	}
	if pending, _ := state.db.PendingEd2kHashes(); len(pending) != 0 {
		t.Fatalf("a non-video link was registered: %v", pending)
	}
}

// TestAddEd2kLinkAlreadyQueuedIsAccepted: aMule refuses a link it already
// has, yet the file is in its queue, so the add (and the registration that
// follows) goes ahead.
func TestAddEd2kLinkAlreadyQueuedIsAccepted(t *testing.T) {
	core, cfg := ed2kTestCore(t, nil)
	const link = "ed2k://|file|Rare%20Show%20S02E03.mkv|900000000|ffffffffffffffffffffffffffffffff|/"
	if err := AddEd2kLink(cfg, link); err != nil {
		t.Fatal(err)
	}
	if err := AddEd2kLink(cfg, link); err != nil {
		t.Fatalf("a link already in aMule's queue must be accepted: %v", err)
	}
	if len(core.Downloads()) != 1 {
		t.Fatalf("queue = %+v", core.Downloads())
	}
	if err := AddEd2kLink(cfg, "ed2k://|file|other.mkv|10|eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee|/"); err != nil {
		t.Fatal(err)
	}
}

// TestEd2kReleaseCompletesInTheDatabase pins the bug found with a real
// download: the completion (and blocklist, rollback, add intents) took the
// release identity from the magnet only and failed on an eD2k release.
func TestEd2kReleaseCompletesInTheDatabase(t *testing.T) {
	db, err := OpenDatabase(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const hash = "f7003a131b7d720c48e9edcd3a11cb62"
	name := "The.Pitt.S02E14.ITA.ENG.1080p.AMZN.WEB-DL.DDP5.1.H.264-MeM.mkv"
	release := ParseEd2kRelease(name, "ed2k://|file|"+name+"|2941579665|"+hash+"|/", "ed2k:link", 2941579665, 11, time.Now().UTC())
	if release == nil || release.Kind != "series" {
		t.Fatalf("release = %+v", release)
	}
	if err := db.BeginTorrentAdd(release, 10); err != nil {
		t.Fatalf("BeginTorrentAdd: %v", err)
	}
	if err := db.RegisterTorrentScored(release, 10); err != nil {
		t.Fatal(err)
	}
	if err := db.EndTorrentAdd(release); err != nil {
		t.Fatalf("EndTorrentAdd: %v", err)
	}
	if err := db.MarkReleaseCompleted(release, "/library/The Pitt/S02E14.mkv", 2941579665); err != nil {
		t.Fatalf("MarkReleaseCompleted: %v", err)
	}
	if status, _ := db.TorrentStatus(hash); status == nil || *status != "completed" {
		t.Fatalf("status = %v", status)
	}
	if err := db.Blocklist(release, "test"); err != nil {
		t.Fatalf("Blocklist: %v", err)
	}
}

func TestEd2kSearchReportsAnotherClientSearching(t *testing.T) {
	core, handler := ed2kTestRouter(t)
	core.SetResults([]amuletest.Result{{
		Hash: "dddddddddddddddddddddddddddddddd", Name: "The.Pitt.S01E01.mkv", Size: 7000000000, Sources: 12,
	}}, "")
	core.SetSearchInterference([]amuletest.Result{{
		Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "Unrelated.Show.S01E01.mkv", Size: 10, Sources: 1,
	}}, 1, 1, 0)

	form := url.Values{"query": {"the pitt"}, "network": {"kad"}}
	request := httptest.NewRequest(http.MethodPost, "/ed2k/search", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	body := recorder.Body.String()
	if strings.Contains(body, "Unrelated") {
		t.Fatal("another client's results were shown as answers to the query")
	}
	if !strings.Contains(body, "sta eseguendo un") || !strings.Contains(body, "riprova tra qualche secondo") {
		t.Fatalf("the busy message is missing from the page: %s", body)
	}
}
