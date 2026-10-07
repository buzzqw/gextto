package gextto

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGxDaemon is a scripted gx-torrent API.
type fakeGxDaemon struct {
	mu       sync.Mutex
	items    []gxTorrentItem
	down     bool
	requests []string
	configs  []string
	tokens   []string
}

func (f *fakeGxDaemon) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tokens = append(f.tokens, r.Header.Get("X-Gx-Token"))
		if f.down {
			http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
			return
		}
		_ = r.ParseForm()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path+"?"+r.Form.Encode())
		switch {
		case r.URL.Path == "/api/v1/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.URL.Path == "/api/v1/torrents":
			_ = json.NewEncoder(w).Encode(f.items)
		case r.URL.Path == "/api/v1/config":
			body, _ := io.ReadAll(r.Body)
			f.configs = append(f.configs, string(body))
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case r.URL.Path == "/api/v1/add":
			_ = json.NewEncoder(w).Encode(map[string]any{"hash": "abc", "existing": r.Form.Get("magnet") == "magnet:?dup"})
		case strings.HasSuffix(r.URL.Path, "/torrent-file"):
			http.Error(w, `{"error":"no"}`, http.StatusNotFound)
		case strings.HasPrefix(r.URL.Path, "/api/v1/torrents/missing"):
			http.Error(w, `{"error":"torrent not found"}`, http.StatusNotFound)
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	})
}

func (f *fakeGxDaemon) set(items ...gxTorrentItem) {
	f.mu.Lock()
	f.items = items
	f.mu.Unlock()
}

func (f *fakeGxDaemon) seen(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, request := range f.requests {
		if strings.HasPrefix(request, prefix) {
			return true
		}
	}
	return false
}

func newTestGxEngine(t *testing.T, token string) (*gxTorrentEngine, *fakeGxDaemon) {
	t.Helper()
	fake := &fakeGxDaemon{}
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	cfg := &Config{
		DataDir:  t.TempDir(),
		StateDir: t.TempDir(),
		Settings: map[string]string{
			"gxtorrent_url":              server.URL,
			"gxtorrent_token":            token,
			"gxtorrent_managed":          "false",
			"gxtorrent_poll_interval_ms": "0",
		},
	}
	engine, err := newGxTorrentEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return engine, fake
}

func TestGxEngineMapsStatesAndEmitsEvents(t *testing.T) {
	engine, fake := newTestGxEngine(t, "")
	fake.set(gxTorrentItem{Hash: "AA", Name: "Show", State: "downloading_metadata", SavePath: "/dl", SeedRatio: -1, SeedDays: -1})
	views := engine.List()
	if len(views) != 1 || views[0].Hash != "aa" || views[0].State != "downloading_metadata" || views[0].HasMetadata {
		t.Fatalf("unexpected view %+v", views)
	}

	fake.set(gxTorrentItem{Hash: "aa", Name: "Show", State: "downloading", SavePath: "/dl", Progress: 40,
		TotalSize: 100, TotalDone: 40, HasMetadata: true, AutoManaged: true})
	fake.set(gxTorrentItem{Hash: "aa", Name: "Show", State: "seeding", SavePath: "/dl", Progress: 100,
		TotalSize: 100, TotalDone: 100, HasMetadata: true, AutoManaged: true})
	events := engine.PollEvents()
	kinds := map[string]bool{}
	for _, event := range events {
		kinds[event.Kind] = true
		if event.SavePath != "/dl" || event.Hash != "aa" {
			t.Fatalf("event without save path/hash: %+v", event)
		}
	}
	if !kinds["metadata_received"] || !kinds["torrent_finished"] {
		t.Fatalf("missing lifecycle events: %+v", events)
	}
	view := engine.List()[0]
	if !view.IsSeeding || view.Progress != 100 || view.Diagnosis != "seeding" {
		t.Fatalf("unexpected seeding view %+v", view)
	}
}

func TestGxEngineKeepsStaleCacheDuringOutage(t *testing.T) {
	engine, fake := newTestGxEngine(t, "")
	fake.set(gxTorrentItem{Hash: "aa", Name: "Show", State: "downloading"})
	if len(engine.List()) != 1 || !engine.SessionHealthy() {
		t.Fatal("expected a healthy snapshot")
	}
	fake.mu.Lock()
	fake.down = true
	fake.mu.Unlock()
	if len(engine.List()) != 1 {
		t.Fatal("an outage must keep the stale snapshot")
	}
	if engine.SessionHealthy() {
		t.Fatal("an outage must mark the session unhealthy")
	}
}

func TestGxEngineStalledFlowUsesParkAndProbe(t *testing.T) {
	engine, fake := newTestGxEngine(t, "")
	fake.set(gxTorrentItem{Hash: "aa", Name: "Show", State: "downloading", Progress: 10, TotalSize: 100})
	engine.List()
	if ok, err := engine.MarkStalled("AA"); err != nil || !ok {
		t.Fatalf("park: %v %v", ok, err)
	}
	if !fake.seen("POST /api/v1/torrents/aa/park") {
		t.Fatal("MarkStalled must park the torrent in the daemon")
	}
	if ok, err := engine.Restart("aa"); err != nil || !ok {
		t.Fatalf("restart: %v %v", ok, err)
	}
	if !fake.seen("POST /api/v1/torrents/aa/restart") {
		t.Fatal("Restart must start a probe")
	}
	engine.ClearStalled("aa")
	if !fake.seen("POST /api/v1/torrents/aa/unpark") {
		t.Fatal("ClearStalled must unpark")
	}
	engine.ClearStalled("missing") // a removed torrent is not an error
}

func TestGxEngineAdjustQueuePushesPolicyOnce(t *testing.T) {
	engine, fake := newTestGxEngine(t, "")
	cfg := &Config{}
	cfg.Libtorrent.ActiveDownloads = 4
	cfg.Libtorrent.ActiveSeeds = 2
	cfg.Libtorrent.ActiveLimit = 8
	cfg.Libtorrent.DontCountSlowTorrents = true
	engine.AdjustQueue(cfg, 0)
	engine.AdjustQueue(cfg, 0)
	fake.mu.Lock()
	pushed := append([]string(nil), fake.configs...)
	fake.mu.Unlock()
	if len(pushed) != 1 {
		t.Fatalf("the policy must be pushed once, got %v", pushed)
	}
	var policy map[string]any
	if err := json.Unmarshal([]byte(pushed[0]), &policy); err != nil {
		t.Fatal(err)
	}
	if policy["active_downloads"] != float64(4) || policy["active_limit"] != float64(8) || policy["dont_count_slow"] != true {
		t.Fatalf("unexpected policy %v", policy)
	}
	cfg.Libtorrent.ActiveDownloads = 5
	engine.AdjustQueue(cfg, 0)
	if ok, err := engine.SetGlobalSpeedLimits(300, 50); err != nil || !ok {
		t.Fatalf("speed limits: %v %v", ok, err)
	}
	fake.mu.Lock()
	count := len(fake.configs)
	last := fake.configs[len(fake.configs)-1]
	fake.mu.Unlock()
	if count != 3 || !strings.Contains(last, `"speed_limit_download":300`) {
		t.Fatalf("changes must be pushed: %d %s", count, last)
	}
}

func TestGxEngineOperations(t *testing.T) {
	engine, fake := newTestGxEngine(t, "s3cret")
	fake.set(gxTorrentItem{Hash: "aa", Name: "Show", State: "seeding", SavePath: "/dl", Progress: 100, TotalSize: 10, TotalDone: 10, HasMetadata: true})
	engine.List()

	if ok, err := engine.Remove("aa", true); err != nil || !ok {
		t.Fatalf("remove: %v %v", ok, err)
	}
	if !fake.seen("DELETE /api/v1/torrents/aa?delete_files=1") {
		t.Fatal("deleteFiles must reach the daemon")
	}
	if ok, _ := engine.AddWithOptions("magnet:?dup", &Config{LibtorrentDir: "/dl"}, nil, AddOptions{}); ok {
		t.Fatal("an existing torrent must not report a new start")
	}
	if ok, err := engine.AddWithOptions("magnet:?new", &Config{LibtorrentDir: "/dl"}, nil, AddOptions{QueueTop: true}); err != nil || !ok {
		t.Fatalf("add: %v %v", ok, err)
	}
	if !fake.seen("POST /api/v1/add?destination=%2Fdl&magnet=magnet%3A%3Fnew&top=1") {
		t.Fatalf("add options not sent: %v", fake.requests)
	}
	if _, err := engine.SetLimits("aa", 1000, -1, 2.0, -1); err == nil {
		t.Fatal("per-torrent rate limits are not supported and must say so")
	}
	if ok, err := engine.SetLimits("aa", -1, -1, 2.0, 7); err != nil || !ok {
		t.Fatalf("seed limits: %v %v", ok, err)
	}
	if !fake.seen("POST /api/v1/torrents/aa/seed-limits?seed_days=7&seed_ratio=2") {
		t.Fatal("seed limits not sent")
	}
	if ok, err := engine.SetFilePriorities("aa", []int32{4, 0, 7}); err != nil || !ok {
		t.Fatalf("file priorities: %v %v", ok, err)
	}
	if !fake.seen("POST /api/v1/torrents/aa/file-priorities?priorities=4%2C0%2C7") {
		t.Fatal("file priorities not sent")
	}
	fake.mu.Lock()
	for _, token := range fake.tokens {
		if token != "s3cret" {
			fake.mu.Unlock()
			t.Fatalf("request without the token: %q", token)
		}
	}
	fake.mu.Unlock()
}

func TestGxEngineMoveEmitsStorageMoved(t *testing.T) {
	engine, fake := newTestGxEngine(t, "")
	fake.set(gxTorrentItem{Hash: "aa", Name: "Show", State: "seeding", SavePath: "/dl", Progress: 100, TotalSize: 10, TotalDone: 10, HasMetadata: true})
	engine.List()
	if ok, err := engine.MoveStorage("aa", "/library"); err != nil || !ok {
		t.Fatalf("move: %v %v", ok, err)
	}
	fake.set(gxTorrentItem{Hash: "aa", Name: "Show", State: "moving", SavePath: "/dl", Progress: 100, TotalSize: 10, TotalDone: 10, HasMetadata: true})
	if events := engine.PollEvents(); len(events) != 0 {
		t.Fatalf("no event while moving: %+v", events)
	}
	fake.set(gxTorrentItem{Hash: "aa", Name: "Show", State: "seeding", SavePath: "/library", Progress: 100, TotalSize: 10, TotalDone: 10, HasMetadata: true})
	events := engine.PollEvents()
	if len(events) != 1 || events[0].Kind != "storage_moved" || events[0].SavePath != "/library" {
		t.Fatalf("expected storage_moved, got %+v", events)
	}
	if ok, _ := engine.MoveStorage("aa", "/library"); ok {
		t.Fatal("a move to the current path is a no-op")
	}
}

func TestGxEngineV2Error(t *testing.T) {
	err := error(gxAPIError{Status: 400, Message: "v2_unsupported: BitTorrent v2-only torrent"})
	if !errors.Is(err, ErrTorrentV2Unsupported) {
		t.Fatal("the v2 refusal must be recognizable")
	}
	if errors.Is(gxAPIError{Status: 400, Message: "other"}, ErrTorrentV2Unsupported) {
		t.Fatal("other errors are not v2 refusals")
	}
}

func TestTorrentBackendNameAcceptsGxTorrent(t *testing.T) {
	cfg := &Config{Settings: map[string]string{"torrent_backend": "gx-torrent"}}
	if got := TorrentBackendName(cfg); got != BackendGxTorrent {
		t.Fatalf("gx-torrent must be selectable, got %q", got)
	}
}

func TestUIEngineLabelsMatchBackend(t *testing.T) {
	// The label must follow the engine: a gx-torrent install used to be shown
	// as "libtorrent integrato" in the dashboard and in the "Non attivo con il
	// motore «…»" notes.
	cases := map[string]string{
		BackendGxTorrent:   "gx-torrent",
		BackendQbittorrent: "qBittorrent-nox",
		BackendEmbedded:    "libtorrent integrato",
	}
	for backend, want := range cases {
		if got := uiBackendLabel(backend); got != want {
			t.Fatalf("uiBackendLabel(%q) = %q, want %q", backend, got, want)
		}
	}
}

func TestGxNetworkArgs(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir()}
	cfg.Libtorrent.PortMin = 6881
	cfg.Libtorrent.PortMax = 6891
	cfg.Libtorrent.ListenInterfaces = "wg0:51413"
	cfg.Libtorrent.OutgoingInterface = "wg0"
	cfg.Libtorrent.Encryption = 2
	cfg.Libtorrent.Dht = true
	cfg.Libtorrent.Pex = false
	cfg.Libtorrent.Utp = true
	cfg.Libtorrent.Lsd = false
	cfg.Libtorrent.Upnp = false
	cfg.Libtorrent.Natpmp = true
	cfg.Libtorrent.ApplyIpFilter = true
	got := strings.Join(gxNetworkArgs(cfg), " ")
	want := "-peer-ports 51413 -listen-interface wg0 -outgoing-interface wg0 -encryption 2 -no-pex -no-lsd -no-upnp -ipfilter-trackers=true"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	cfg.Libtorrent.ListenInterfaces = "0.0.0.0:6881-6891"
	if host, ports := gxListenInterface(cfg.Libtorrent.ListenInterfaces); host != "" || ports != "6881-6891" {
		t.Fatalf("all interfaces: %q %q", host, ports)
	}
}

func TestGxManagedListen(t *testing.T) {
	// Default: the daemon listens on the loopback of gxtorrent_url.
	got, err := gxManagedListen(gxTorrentSettings{BaseURL: "http://127.0.0.1:8890"})
	if err != nil || got != "127.0.0.1:8890" {
		t.Fatalf("loopback listen = %q, %v", got, err)
	}
	// gxtorrent_listen overrides it, to expose the read-only page on the LAN.
	got, err = gxManagedListen(gxTorrentSettings{BaseURL: "http://127.0.0.1:8890", Listen: "0.0.0.0:8890"})
	if err != nil || got != "0.0.0.0:8890" {
		t.Fatalf("listen override = %q, %v", got, err)
	}
	if _, err := gxManagedListen(gxTorrentSettings{Listen: "senza-porta"}); err == nil {
		t.Fatal("a listen value without a port must be refused")
	}
	// A non-loopback URL is only allowed with an explicit listen address.
	if _, err := gxManagedListen(gxTorrentSettings{BaseURL: "http://192.168.1.10:8890"}); err == nil {
		t.Fatal("a non-loopback URL without gxtorrent_listen must be refused")
	}
	// The listen port is aligned with gxtorrent_url: Gextto reaches the daemon
	// there, so a mismatched port would make the managed daemon unreachable.
	got, err = gxManagedListen(gxTorrentSettings{BaseURL: "http://127.0.0.1:8890", Listen: "0.0.0.0:9000"})
	if err != nil || got != "0.0.0.0:8890" {
		t.Fatalf("listen port must follow the URL port, got %q, %v", got, err)
	}
	if listenIsLoopback("0.0.0.0:8890") || listenIsLoopback("192.168.1.10:8890") || listenIsLoopback(":8890") {
		t.Fatal("non-loopback addresses detected as loopback")
	}
	if !listenIsLoopback("127.0.0.1:8890") || !listenIsLoopback("localhost:8890") || !listenIsLoopback("[::1]:8890") {
		t.Fatal("loopback addresses not detected")
	}
}

func TestV2DetailCapsPerEngine(t *testing.T) {
	gx := v2DetailCapsFor(BackendGxTorrent)
	if gx.SuperSeeding || gx.WebSeeds || gx.RateLimits || gx.Connections || gx.FileLevels || gx.TrackerNote == "" {
		t.Fatalf("gx-torrent must hide what it cannot do: %+v", gx)
	}
	qb := v2DetailCapsFor(BackendQbittorrent)
	if !qb.SuperSeeding || qb.WebSeeds || !qb.RateLimits || qb.Connections || !qb.FileLevels {
		t.Fatalf("qBittorrent capabilities: %+v", qb)
	}
	lt := v2DetailCapsFor(BackendEmbedded)
	if !lt.SuperSeeding || !lt.WebSeeds || !lt.RateLimits || !lt.Connections || !lt.FileLevels {
		t.Fatalf("libtorrent supports everything: %+v", lt)
	}
	if v2SwarmLabel(-1, -1) != "n/d" || v2SwarmLabel(3, 7) != "3 / 7" {
		t.Fatal("swarm label")
	}
}

func TestGxCrashBudgetFallsBackAfterTheWindowBudgetIsSpent(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	budget := gxCrashBudget{window: 10 * time.Minute, max: 3}
	if budget.register(base) {
		t.Fatal("first failure must not trigger the fallback")
	}
	if budget.register(base.Add(time.Minute)) {
		t.Fatal("second failure must not trigger the fallback")
	}
	if !budget.register(base.Add(2 * time.Minute)) {
		t.Fatal("third failure inside the window must fall back to libtorrent")
	}
	if budget.len() != 3 {
		t.Fatalf("budget = %d, want 3", budget.len())
	}
}

func TestGxCrashBudgetForgetsFailuresOutsideTheWindow(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	budget := gxCrashBudget{window: 10 * time.Minute, max: 3}
	budget.register(base)
	budget.register(base.Add(time.Minute))
	// A long healthy run (here: a gap longer than the window) earns back the
	// previous failures instead of falling back.
	if budget.register(base.Add(20 * time.Minute)) {
		t.Fatal("failures outside the window must not count")
	}
	if budget.len() != 1 {
		t.Fatalf("budget = %d, want 1", budget.len())
	}
}

func TestGxManagedShutdownGrace(t *testing.T) {
	if managedShutdownGrace != 45*time.Second {
		t.Fatalf("managed shutdown grace = %s, want 45s", managedShutdownGrace)
	}
}

func TestGxPeerLimitsSplitConnections(t *testing.T) {
	dial, accept := gxPeerLimits(200)
	if dial != 160 || accept != 40 {
		t.Fatalf("gxPeerLimits(200) = %d/%d, want 160/40", dial, accept)
	}
	if dial, accept := gxPeerLimits(0); dial != 0 || accept != 0 {
		t.Fatalf("no limit must leave rain's defaults, got %d/%d", dial, accept)
	}
	if dial, accept := gxPeerLimits(1); dial < 1 || accept < 1 {
		t.Fatalf("gxPeerLimits(1) = %d/%d, want at least 1 each", dial, accept)
	}
}

func TestGxTorrentVersionNormalizes(t *testing.T) {
	for input, want := range map[string]string{"": "v1", "v1": "v1", "hybrid": "hybrid", "HYBRID": "hybrid", "junk": "v1"} {
		if got := gxTorrentVersion(input); got != want {
			t.Fatalf("gxTorrentVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGxProxyValidation(t *testing.T) {
	for _, valid := range []string{"", "socks5://127.0.0.1:1080", "socks5h://user:pass@host:1080", "http://host:3128"} {
		if err := validateGxProxyURL(valid); err != nil {
			t.Fatalf("proxy %q must be accepted: %v", valid, err)
		}
	}
	for _, invalid := range []string{"host:1080", "ftp://host:1080", "socks5://host", "socks5://:1080"} {
		if err := validateGxProxyURL(invalid); err == nil {
			t.Fatalf("proxy %q must be refused", invalid)
		}
	}
}

func TestGxEnsureIPFilterForcesAtBoot(t *testing.T) {
	body := "001.002.003.000 - 001.002.003.255 , 000 , test\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "ipfilter.dat")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DataDir: dir}
	cfg.Libtorrent.IpFilterPath = server.URL

	// Not forced: a recent file is reused instead of downloading again.
	gxEnsureIPFilter(cfg, false)
	if got, _ := os.ReadFile(path); string(got) != "old\n" {
		t.Fatalf("fresh cache must be reused, got %q", got)
	}

	// Forced (service boot): the file is replaced with the downloaded list.
	gxEnsureIPFilter(cfg, true)
	if got, _ := os.ReadFile(path); string(got) != body {
		t.Fatalf("boot refresh must replace the file, got %q", got)
	}
}

func TestGxAllowedRootsValidation(t *testing.T) {
	for _, valid := range []string{"", "/srv/media", "/srv/media,/data", " /srv/media , /data "} {
		if err := validateGxAllowedRoots(valid); err != nil {
			t.Fatalf("allowed roots %q must be accepted: %v", valid, err)
		}
	}
	if err := validateGxAllowedRoots("relative/path"); err == nil {
		t.Fatal("a relative allowed root must be refused")
	}
}

func TestGxManagedListenSettingDefaultsToLAN(t *testing.T) {
	if got := gxManagedListenSetting(&Config{Settings: map[string]string{}}); got != "0.0.0.0:8890" {
		t.Fatalf("default listen = %q, want 0.0.0.0:8890", got)
	}
	if got := gxManagedListenSetting(&Config{Settings: map[string]string{"gxtorrent_listen": "127.0.0.1:8890"}}); got != "127.0.0.1:8890" {
		t.Fatalf("explicit listen = %q", got)
	}
}

func TestGxCacheMB(t *testing.T) {
	if gxCacheMB(-1) != -1 || gxCacheMB(0) != -1 {
		t.Fatal("automatic")
	}
	if gxCacheMB(65536) != 1024 { // 65536 blocks of 16 KiB = 1 GiB
		t.Fatalf("got %d", gxCacheMB(65536))
	}
	if gxCacheMB(1) != 1 {
		t.Fatal("at least 1 MiB")
	}
}
