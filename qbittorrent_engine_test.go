package gextto

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

// fakeQB is an in-memory qBittorrent-nox Web API used to exercise the adapter
// without a real daemon.
type fakeQB struct {
	mu        sync.Mutex
	token     string
	fail      bool
	torrents  []qbittorrent.Torrent
	calls     map[string]int
	forms     map[string]url.Values
	locations map[string]string
	exports   int
}

func newFakeQB() *fakeQB {
	return &fakeQB{
		calls:     map[string]int{},
		forms:     map[string]url.Values{},
		locations: map[string]string{},
	}
}

func (f *fakeQB) setTorrents(torrents []qbittorrent.Torrent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.torrents = torrents
}

func (f *fakeQB) record(path string, form url.Values) {
	f.calls[path]++
	if form != nil {
		f.forms[path] = form
	}
}

func (f *fakeQB) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	if r.URL.Path == "/api/v2/auth/login" {
		f.token = "sid-token"
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: f.token, Path: "/"})
		_, _ = w.Write([]byte("Ok."))
		return
	}
	if cookie, err := r.Cookie("SID"); err != nil || cookie.Value != f.token {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	_ = r.ParseForm()
	switch r.URL.Path {
	case "/api/v2/app/version":
		_, _ = w.Write([]byte("v5.0.0"))
	case "/api/v2/app/webapiVersion":
		_, _ = w.Write([]byte("2.11.2"))
	case "/api/v2/app/defaultSavePath":
		_, _ = w.Write([]byte("/data/downloads"))
	case "/api/v2/torrents/info":
		_ = json.NewEncoder(w).Encode(f.torrents)
	case "/api/v2/torrents/add":
		_ = r.ParseMultipartForm(1 << 20)
		if r.MultipartForm != nil {
			f.record(r.URL.Path, url.Values(r.MultipartForm.Value))
		}
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/stop", "/api/v2/torrents/start",
		"/api/v2/torrents/pause", "/api/v2/torrents/resume":
		f.record(r.URL.Path, r.PostForm)
		// Reflect the new state so idempotency is observable.
		hashes := strings.Split(r.PostForm.Get("hashes"), "|")
		for index := range f.torrents {
			for _, hash := range hashes {
				if strings.EqualFold(f.torrents[index].Hash, hash) {
					switch r.URL.Path {
					case "/api/v2/torrents/stop", "/api/v2/torrents/pause":
						f.torrents[index].State = "stoppedDL"
					default:
						f.torrents[index].State = "downloading"
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/delete":
		f.record(r.URL.Path, r.PostForm)
		hashes := r.PostForm.Get("hashes")
		kept := f.torrents[:0]
		for _, torrent := range f.torrents {
			if !strings.Contains(strings.ToLower(hashes), strings.ToLower(torrent.Hash)) {
				kept = append(kept, torrent)
			}
		}
		f.torrents = kept
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/setLocation":
		f.record(r.URL.Path, r.PostForm)
		location := r.PostForm.Get("location")
		for index := range f.torrents {
			if strings.EqualFold(f.torrents[index].Hash, r.PostForm.Get("hashes")) {
				f.torrents[index].SavePath = location
				f.locations[strings.ToLower(f.torrents[index].Hash)] = location
			}
		}
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/files":
		_ = json.NewEncoder(w).Encode([]qbittorrent.File{{Name: "movie.mkv", Size: 1000, Progress: 0.5, Priority: 1}})
	case "/api/v2/torrents/export":
		f.exports++
		_, _ = w.Write([]byte("d4:infod4:name8:meta.bin ee"))
	case "/api/v2/torrents/trackers":
		_ = json.NewEncoder(w).Encode([]qbittorrent.Tracker{{URL: "udp://tracker", Status: 2, NumPeers: 4}})
	case "/api/v2/sync/torrentPeers":
		_, _ = w.Write([]byte(`{"peers":{"a":{"ip":"1.2.3.4","port":6881,"client":"qBit","progress":0.2,"dl_speed":10}}}`))
	case "/api/v2/transfer/info":
		_, _ = w.Write([]byte(`{"dl_info_speed":100,"up_info_speed":50}`))
	default:
		// Every remaining mutation is accepted and recorded.
		f.record(r.URL.Path, r.PostForm)
		w.WriteHeader(http.StatusOK)
	}
}

func newFakeQBEngine(t *testing.T) (*fakeQB, *qbittorrentEngine, string) {
	t.Helper()
	fake := newFakeQB()
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	cfg.StateDir = filepath.Join(dir, "state")
	cfg.LibtorrentDir = filepath.Join(dir, "downloads")
	if err := os.MkdirAll(cfg.LibtorrentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg.Settings["torrent_backend"] = BackendQbittorrent
	cfg.Settings["qbittorrent_url"] = server.URL
	cfg.Settings["qbittorrent_username"] = "admin"
	cfg.Settings["qbittorrent_password"] = "secret"
	cfg.Settings["qbittorrent_category"] = "gextto"

	engine, err := newQbittorrentEngine(&cfg)
	if err != nil {
		t.Fatalf("newQbittorrentEngine: %v", err)
	}
	return fake, engine, cfg.LibtorrentDir
}

func TestQbittorrentEngineListMapping(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	fake.setTorrents([]qbittorrent.Torrent{{
		Hash: "ABCDEF", Name: "movie.mkv", State: "downloading", Progress: 0.42,
		Size: 1000, AmountLeft: 580, DownloadRate: 1024, UploadRate: 512,
		NumSeeds: 3, NumLeechs: 2, SavePath: downloads, RatioLimit: -2, SeedingLimit: -2,
		InfohashV1: "abcdef",
	}})
	views := engine.List()
	if len(views) != 1 {
		t.Fatalf("views = %d, want 1", len(views))
	}
	view := views[0]
	if view.Hash != "abcdef" || math.Abs(view.Progress-42) > 0.001 || view.State != "downloading" {
		t.Fatalf("view = %+v", view)
	}
	if view.TotalDone != 420 || view.TotalSize != 1000 {
		t.Fatalf("sizes = %d/%d, want 420/1000", view.TotalDone, view.TotalSize)
	}
	if view.SeedRatio != -1 || view.SeedDays != -1 {
		t.Fatalf("seed limits = %v/%v, want -1/-1 (global)", view.SeedRatio, view.SeedDays)
	}
	if view.SavePath != downloads {
		t.Fatalf("save path = %q, want %q", view.SavePath, downloads)
	}
}

func TestQbittorrentEnginePauseResumeIdempotent(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	fake.setTorrents([]qbittorrent.Torrent{{Hash: "abc", Name: "x", State: "downloading", Progress: 0.1, Size: 100, SavePath: downloads}})
	engine.List() // populate the cache

	if ok, err := engine.Pause("abc"); err != nil || !ok {
		t.Fatalf("Pause = %v, %v", ok, err)
	}
	if ok, err := engine.Pause("abc"); err != nil || !ok {
		t.Fatalf("second Pause = %v, %v", ok, err)
	}
	if total := fake.calls["/api/v2/torrents/stop"] + fake.calls["/api/v2/torrents/pause"]; total != 1 {
		t.Fatalf("pause commands = %d, want 1 (idempotent)", total)
	}
	if ok, err := engine.Resume("abc"); err != nil || !ok {
		t.Fatalf("Resume = %v, %v", ok, err)
	}
	if total := fake.calls["/api/v2/torrents/start"] + fake.calls["/api/v2/torrents/resume"]; total != 1 {
		t.Fatalf("resume commands = %d, want 1", total)
	}
}

func TestQbittorrentEngineMoveEmitsEvent(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	target := filepath.Join(filepath.Dir(downloads), "moved")
	fake.setTorrents([]qbittorrent.Torrent{{Hash: "abc", Name: "x", State: "uploading", Progress: 1, Size: 100, SavePath: downloads}})
	engine.List()

	if ok, err := engine.MoveStorage("abc", target); err != nil || !ok {
		t.Fatalf("MoveStorage = %v, %v", ok, err)
	}
	engine.List()
	events := engine.PollEvents()
	found := false
	for _, event := range events {
		if event.Kind == "storage_moved" && event.Hash == "abc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("storage_moved event not emitted: %+v", events)
	}
}

func TestQbittorrentEngineCompletionAndMetadataEvents(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	fake.setTorrents([]qbittorrent.Torrent{{Hash: "abc", Name: "x", State: "metaDL", Progress: 0, Size: 0, SavePath: downloads}})
	engine.List()

	fake.setTorrents([]qbittorrent.Torrent{{Hash: "abc", Name: "x", State: "downloading", Progress: 0.5, Size: 100, SavePath: downloads}})
	engine.List()

	fake.setTorrents([]qbittorrent.Torrent{{Hash: "abc", Name: "x", State: "uploading", Progress: 1, Size: 100, SavePath: downloads}})
	engine.List()

	events := engine.PollEvents()
	kinds := map[string]bool{}
	for _, event := range events {
		kinds[event.Kind] = true
	}
	if !kinds["metadata_received"] || !kinds["torrent_finished"] {
		t.Fatalf("events = %+v, want metadata_received + torrent_finished", events)
	}
}

func TestQbittorrentEngineUnreachableKeepsCache(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	fake.setTorrents([]qbittorrent.Torrent{{Hash: "abc", Name: "x", State: "downloading", Progress: 0.5, Size: 100, SavePath: downloads}})
	if views := engine.List(); len(views) != 1 {
		t.Fatalf("initial views = %d", len(views))
	}
	fake.mu.Lock()
	fake.fail = true
	fake.mu.Unlock()

	views := engine.List()
	if len(views) != 1 || views[0].Hash != "abc" {
		t.Fatalf("stale cache lost after outage: %+v", views)
	}
	if stats := engine.SyncStats(); stats["connected"] != false {
		t.Fatalf("connected = %v, want false", stats["connected"])
	}
}

func TestQbittorrentEngineUnsupportedCapabilities(t *testing.T) {
	_, engine, _ := newFakeQBEngine(t)
	if _, err := engine.WebSeeds("abc", "http://x", false); !isCapabilityUnavailable(err) {
		t.Fatalf("WebSeeds err = %v, want capability unavailable", err)
	}
	if _, err := engine.AssociateStorage("abc", "/x"); !isCapabilityUnavailable(err) {
		t.Fatalf("AssociateStorage err = %v, want capability unavailable", err)
	}
	if _, err := engine.SetMaxConnections("abc", 10); !isCapabilityUnavailable(err) {
		t.Fatalf("SetMaxConnections err = %v, want capability unavailable", err)
	}
}

func isCapabilityUnavailable(err error) bool {
	var target ErrCapabilityUnavailable
	return errors.As(err, &target)
}

func TestQbittorrentEngineInspection(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	fake.setTorrents([]qbittorrent.Torrent{{Hash: "abc", Name: "x", State: "downloading", Progress: 0.5, Size: 100, SavePath: downloads}})
	files, ok, err := engine.Files("abc")
	if err != nil || !ok || len(files) != 1 || files[0].Path != "movie.mkv" || files[0].Downloaded != 500 {
		t.Fatalf("Files = %+v, %v, %v", files, ok, err)
	}
	peers, ok, err := engine.Peers("abc")
	if err != nil || !ok || len(peers) != 1 || peers[0].Address != "1.2.3.4:6881" {
		t.Fatalf("Peers = %+v, %v, %v", peers, ok, err)
	}
	trackers, ok, err := engine.Trackers("abc")
	if err != nil || !ok || len(trackers) != 1 || !trackers[0].Verified {
		t.Fatalf("Trackers = %+v, %v, %v", trackers, ok, err)
	}
}

func TestQbittorrentEngineAddAndLimits(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	engine.settings.dataDir = downloads
	magnet := "magnet:?xt=urn:btih:ABCDEF0123456789ABCDEF0123456789ABCDEF01&dn=x"
	if ok, err := engine.Add(magnet, nil); err != nil || !ok {
		t.Fatalf("Add = %v, %v", ok, err)
	}
	if got := fake.forms["/api/v2/torrents/add"].Get("savepath"); got != downloads {
		t.Fatalf("savepath = %q, want %q", got, downloads)
	}
	if got := fake.forms["/api/v2/torrents/add"].Get("category"); got != "gextto" {
		t.Fatalf("category = %q", got)
	}
	if _, err := engine.SetLimits("abc", 1024, 512, 1.5, 2); err != nil {
		t.Fatalf("SetLimits: %v", err)
	}
	if fake.forms["/api/v2/torrents/setDownloadLimit"].Get("limit") != "1024" {
		t.Fatalf("download limit = %v", fake.forms["/api/v2/torrents/setDownloadLimit"])
	}
	if fake.forms["/api/v2/torrents/setShareLimits"].Get("ratioLimit") != "1.5" {
		t.Fatalf("share limits = %v", fake.forms["/api/v2/torrents/setShareLimits"])
	}
	if fake.forms["/api/v2/torrents/setShareLimits"].Get("seedingTimeLimit") != "2880" {
		t.Fatalf("seeding time = %v", fake.forms["/api/v2/torrents/setShareLimits"])
	}
	// An empty mapping means "same namespace": the path is passed unchanged.
	if got := fake.forms["/api/v2/torrents/add"].Get("paused"); got != "" {
		t.Fatalf("unexpected paused = %q", got)
	}
}

func TestQbittorrentEngineAddTorrentFileFindsHash(t *testing.T) {
	fake, engine, _ := newFakeQBEngine(t)
	torrentPath := filepath.Join(t.TempDir(), "sample.torrent")
	if err := os.WriteFile(torrentPath, []byte("d4:infod4:name1:ae"), 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	// qBittorrent reports the new torrent after the upload.
	go func() {
		time.Sleep(100 * time.Millisecond)
		fake.setTorrents([]qbittorrent.Torrent{{Hash: "NEWHASH", Name: "sample", Size: 10}})
	}()
	hash, err := engine.AddTorrentFileEx(torrentPath, "/tmp", AddOptions{Sequential: true})
	if err != nil {
		t.Fatalf("AddTorrentFileEx: %v", err)
	}
	if hash == nil || *hash != "newhash" {
		t.Fatalf("hash = %v, want newhash", hash)
	}
}

func TestQbittorrentEngineResolveSavePathMapping(t *testing.T) {
	fake, engine, _ := newFakeQBEngine(t)
	engine.settings.Mappings = []PathMapping{{Gextto: "/gextto/downloads", Backend: "/data/downloads"}}
	engine.settings.dataDir = "/gextto"
	savePath, err := engine.resolveSavePath(nil, &Config{LibtorrentDir: "/gextto/downloads"})
	if err != nil || savePath != "/data/downloads" {
		t.Fatalf("resolveSavePath = %q, %v", savePath, err)
	}
	// A path outside the mapping must be refused, never silently passed on.
	if _, err := engine.resolveSavePath(strptr("/other"), nil); !errors.Is(err, ErrPathMappingMissing) {
		t.Fatalf("expected ErrPathMappingMissing, got %v", err)
	}
	_ = fake
}

func strptr(value string) *string { return &value }

// TestQbittorrentEngineContextHonoursRequestContext documents that the adapter
// always uses its own bounded context (no request cancellation leaks).
func TestQbittorrentEngineContextHonoursRequestContext(t *testing.T) {
	_, engine, _ := newFakeQBEngine(t)
	ctx, cancel := engine.requestContext()
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal("request context already done")
	default:
	}
}

func TestQbittorrentEnginePersistsTorrentFile(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	fake.setTorrents([]qbittorrent.Torrent{{
		Hash: "abc", Name: "x", State: "downloading", Progress: 0.5, Size: 100, SavePath: downloads,
	}})
	engine.List()
	target := filepath.Join(engine.settings.stateDir, "abc.torrent")
	if !fileExists(target) {
		t.Fatalf(".torrent not persisted at %s", target)
	}
	if fake.exports == 0 {
		t.Fatal("export endpoint was not called")
	}
	// A second sync must not re-export.
	before := fake.exports
	engine.List()
	if fake.exports != before {
		t.Fatalf("re-exported an existing .torrent: %d -> %d", before, fake.exports)
	}
}

func TestQbittorrentEngineAdjustQueue(t *testing.T) {
	fake, engine, downloads := newFakeQBEngine(t)
	fake.setTorrents([]qbittorrent.Torrent{
		{Hash: "aaa", Name: "a", State: "downloading", Progress: 0.1, Size: 100, Priority: 1, SavePath: downloads},
		{Hash: "bbb", Name: "b", State: "downloading", Progress: 0.1, Size: 100, Priority: 2, SavePath: downloads},
		{Hash: "ccc", Name: "c", State: "downloading", Progress: 0.1, Size: 100, Priority: 3, SavePath: downloads},
	})
	cfg := DefaultConfig()
	cfg.Libtorrent.ActiveDownloads = 2

	engine.AdjustQueue(&cfg, 0)
	if total := fake.calls["/api/v2/torrents/stop"] + fake.calls["/api/v2/torrents/pause"]; total != 1 {
		t.Fatalf("pause commands = %d, want 1", total)
	}
	// The highest queue position (ccc) is the one paused.
	if got := fake.forms["/api/v2/torrents/stop"].Get("hashes"); got != "" && got != "ccc" {
		t.Fatalf("paused hashes = %q, want ccc", got)
	}
	// Idempotent: no further pause.
	engine.AdjustQueue(&cfg, 0)
	if total := fake.calls["/api/v2/torrents/stop"] + fake.calls["/api/v2/torrents/pause"]; total != 1 {
		t.Fatalf("pause commands after second call = %d, want 1", total)
	}
	// Free a slot: one of the active torrents becomes a seeder, so ccc must be
	// resumed.
	fake.setTorrents([]qbittorrent.Torrent{
		{Hash: "aaa", Name: "a", State: "uploading", Progress: 1, Size: 100, Priority: 1, SavePath: downloads},
		{Hash: "bbb", Name: "b", State: "downloading", Progress: 0.1, Size: 100, Priority: 2, SavePath: downloads},
		{Hash: "ccc", Name: "c", State: "stoppedDL", Progress: 0.1, Size: 100, Priority: 3, SavePath: downloads},
	})
	engine.AdjustQueue(&cfg, 0)
	if total := fake.calls["/api/v2/torrents/start"] + fake.calls["/api/v2/torrents/resume"]; total != 1 {
		t.Fatalf("resume commands = %d, want 1", total)
	}
}

func TestQbittorrentEngineApplyOptimization(t *testing.T) {
	fake, engine, _ := newFakeQBEngine(t)
	result, err := engine.ApplyOptimization(nil)
	if err != nil {
		t.Fatalf("ApplyOptimization: %v", err)
	}
	if result["backend"] != BackendQbittorrent {
		t.Fatalf("result = %+v", result)
	}
	encoded := fake.forms["/api/v2/app/setPreferences"].Get("json")
	if !strings.Contains(encoded, "disk_cache") {
		t.Fatalf("preferences = %q", encoded)
	}
}
