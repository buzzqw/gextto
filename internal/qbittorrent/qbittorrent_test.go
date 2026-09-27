package qbittorrent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeQbit is a minimal in-memory qBittorrent Web API used to exercise the
// client without a real daemon.
type fakeQbit struct {
	mu            sync.Mutex
	token         string
	logins        int
	stopSupported bool
	lastAdd       url.Values
	lastDelete    url.Values
	lastLocation  url.Values
	lastPause     url.Values
	torrents      []Torrent

	lastFilePath    url.Values
	lastAddTracker  url.Values
	lastShareLimits url.Values
	lastForceStart  url.Values
	lastCategory    url.Values
	lastPrefs       url.Values
	lastEdit        url.Values
	lastTags        url.Values
}

func (f *fakeQbit) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.URL.Path == loginPath {
		f.logins++
		f.token = "sid-" + strings.Repeat("x", f.logins)
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: f.token, Path: "/"})
		_, _ = w.Write([]byte("Ok."))
		return
	}
	cookie, err := r.Cookie("SID")
	if err != nil || cookie.Value != f.token {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	switch r.URL.Path {
	case "/api/v2/app/version":
		_, _ = w.Write([]byte("v5.0.0"))
	case "/api/v2/app/webapiVersion":
		_, _ = w.Write([]byte("2.11.2"))
	case "/api/v2/torrents/info":
		_ = json.NewEncoder(w).Encode(f.torrents)
	case "/api/v2/torrents/add":
		_ = r.ParseMultipartForm(1 << 20)
		f.lastAdd = url.Values(r.MultipartForm.Value)
		if len(r.MultipartForm.File["torrents"]) > 0 {
			f.lastAdd.Set("torrents", r.MultipartForm.File["torrents"][0].Filename)
		}
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/stop", "/api/v2/torrents/start":
		if !f.stopSupported {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = r.ParseForm()
		f.lastPause = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/pause", "/api/v2/torrents/resume":
		_ = r.ParseForm()
		f.lastPause = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/delete":
		_ = r.ParseForm()
		f.lastDelete = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/recheck", "/api/v2/torrents/reannounce",
		"/api/v2/torrents/setDownloadLimit", "/api/v2/torrents/setUploadLimit",
		"/api/v2/transfer/setDownloadLimit", "/api/v2/transfer/setUploadLimit":
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/setLocation":
		_ = r.ParseForm()
		f.lastLocation = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/filePrio":
		_ = r.ParseForm()
		f.lastFilePath = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/addTrackers":
		_ = r.ParseForm()
		f.lastAddTracker = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/setShareLimits":
		_ = r.ParseForm()
		f.lastShareLimits = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/setForceStart":
		_ = r.ParseForm()
		f.lastForceStart = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/setCategory", "/api/v2/torrents/createCategory":
		_ = r.ParseForm()
		f.lastCategory = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/editTracker":
		_ = r.ParseForm()
		f.lastEdit = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/addTags", "/api/v2/torrents/removeTags":
		_ = r.ParseForm()
		f.lastTags = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/setAutoManagement", "/api/v2/torrents/setSuperSeeding":
		_ = r.ParseForm()
		w.WriteHeader(http.StatusOK)
	case "/api/v2/torrents/toggleSequentialDownload":
		_ = r.ParseForm()
		f.lastPause = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/app/defaultSavePath":
		_, _ = w.Write([]byte("/data/downloads"))
	case "/api/v2/app/preferences":
		_, _ = w.Write([]byte(`{"save_path":"/data/downloads","max_connec":200}`))
	case "/api/v2/app/setPreferences":
		_ = r.ParseForm()
		f.lastPrefs = r.PostForm
		w.WriteHeader(http.StatusOK)
	case "/api/v2/transfer/downloadLimit", "/api/v2/transfer/uploadLimit":
		_, _ = w.Write([]byte("1024"))
	case "/api/v2/torrents/files":
		_ = json.NewEncoder(w).Encode([]File{{Name: "movie.mkv", Size: 100, Progress: 0.5, Priority: 1}})
	case "/api/v2/torrents/trackers":
		_ = json.NewEncoder(w).Encode([]Tracker{{URL: "udp://tracker", Status: 2, NumPeers: 3}})
	case "/api/v2/sync/torrentPeers":
		_, _ = w.Write([]byte(`{"peers":{"a":{"ip":"1.2.3.4","port":6881,"client":"x","progress":0.2}}}`))
	case "/api/v2/sync/maindata":
		_, _ = w.Write([]byte(`{"rid":7,"full_update":true,"torrents":{"abc":{"hash":"abc","state":"downloading","progress":0.42}},"torrents_removed":["dead"]}`))
	case "/api/v2/transfer/info":
		_, _ = w.Write([]byte(`{"dl_info_speed":100,"up_info_speed":50}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newFakeServer(t *testing.T) (*fakeQbit, *Client) {
	t.Helper()
	fake := &fakeQbit{stopSupported: true}
	server := httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	client, err := New(Config{BaseURL: server.URL, Username: "admin", Password: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return fake, client
}

func TestLoginAndVersion(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if fake.logins != 1 {
		t.Fatalf("logins = %d, want 1", fake.logins)
	}
	app, err := client.AppVersion(ctx)
	if err != nil || app != "v5.0.0" {
		t.Fatalf("AppVersion = %q, %v", app, err)
	}
	api, err := client.WebAPIVersion(ctx)
	if err != nil || api != "2.11.2" {
		t.Fatalf("WebAPIVersion = %q, %v", api, err)
	}
}

func TestSessionRefreshOnForbidden(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	if err := client.Login(ctx); err != nil {
		t.Fatalf("Login: %v", err)
	}
	// Invalidate the session server-side: the next call must re-login and retry.
	fake.mu.Lock()
	fake.token = "invalidated"
	fake.mu.Unlock()

	if _, err := client.Torrents(ctx); err != nil {
		t.Fatalf("Torrents after expiry: %v", err)
	}
	if fake.logins != 2 {
		t.Fatalf("logins = %d, want 2 (one refresh)", fake.logins)
	}
}

func TestAddMagnetSendsFields(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	hash, err := client.AddMagnet(ctx, "magnet:?xt=urn:btih:ABCDEF0123456789ABCDEF0123456789ABCDEF01&dn=x", AddOptions{
		SavePath:   "/data/completed",
		Category:   "gextto",
		Paused:     true,
		Sequential: true,
	})
	if err != nil {
		t.Fatalf("AddMagnet: %v", err)
	}
	if hash != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("hash = %q", hash)
	}
	if fake.lastAdd.Get("savepath") != "/data/completed" || fake.lastAdd.Get("category") != "gextto" {
		t.Fatalf("add fields = %v", fake.lastAdd)
	}
	if fake.lastAdd.Get("paused") != "true" || fake.lastAdd.Get("sequentialDownload") != "true" {
		t.Fatalf("add flags = %v", fake.lastAdd)
	}
}

func TestPauseFallsBackToLegacyEndpoint(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	fake.stopSupported = false // qBittorrent 4.x: only pause/resume exist
	if err := client.Pause(ctx, "ABC"); err != nil {
		t.Fatalf("Pause fallback: %v", err)
	}
	if fake.lastPause.Get("hashes") != "abc" {
		t.Fatalf("pause hashes = %q", fake.lastPause.Get("hashes"))
	}
}

func TestDeleteRecheckAndLocation(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	if err := client.Delete(ctx, true, "ABC"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if fake.lastDelete.Get("deleteFiles") != "true" || fake.lastDelete.Get("hashes") != "abc" {
		t.Fatalf("delete form = %v", fake.lastDelete)
	}
	if err := client.Recheck(ctx, "abc"); err != nil {
		t.Fatalf("Recheck: %v", err)
	}
	if err := client.SetLocation(ctx, "/data/moved", "abc"); err != nil {
		t.Fatalf("SetLocation: %v", err)
	}
	if fake.lastLocation.Get("location") != "/data/moved" {
		t.Fatalf("location = %q", fake.lastLocation.Get("location"))
	}
}

func TestInspectAndSync(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	fake.torrents = []Torrent{{Hash: "abc", Name: "x", State: "stalledDL", Progress: 0.3}}

	torrents, err := client.Torrents(ctx)
	if err != nil || len(torrents) != 1 {
		t.Fatalf("Torrents = %v, %v", torrents, err)
	}
	files, err := client.Files(ctx, "abc")
	if err != nil || len(files) != 1 {
		t.Fatalf("Files = %v, %v", files, err)
	}
	trackers, err := client.Trackers(ctx, "abc")
	if err != nil || len(trackers) != 1 {
		t.Fatalf("Trackers = %v, %v", trackers, err)
	}
	peers, err := client.Peers(ctx, "abc")
	if err != nil || len(peers) != 1 {
		t.Fatalf("Peers = %v, %v", peers, err)
	}
	sync, err := client.Sync(ctx, 0)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if sync.RID != 7 || sync.Torrents["abc"].State != "downloading" || len(sync.Removed) != 1 {
		t.Fatalf("sync = %+v", sync)
	}
}

func TestNormalizeState(t *testing.T) {
	cases := map[string]string{
		"downloading":        "downloading",
		"forcedDL":           "downloading",
		"metaDL":             "downloading_metadata",
		"stalledDL":          "stalled",
		"queuedDL":           "queued",
		"pausedDL":           "paused",
		"stoppedUP":          "paused",
		"checkingResumeData": "checking_files",
		"allocating":         "checking_files",
		"moving":             "moving",
		"uploading":          "seeding",
		"stalledUP":          "seeding",
		"error":              "error",
		"missingFiles":       "error",
		"wat":                "unknown",
	}
	for in, want := range cases {
		if got := NormalizeState(in); got != want {
			t.Errorf("NormalizeState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMagnetHash(t *testing.T) {
	if got := magnetHash("magnet:?xt=urn:btih:ABCDEF0123456789ABCDEF0123456789ABCDEF01&dn=x"); got != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Fatalf("magnetHash = %q", got)
	}
	if got := magnetHash("http://example/x.torrent"); got != "" {
		t.Fatalf("magnetHash(non-magnet) = %q", got)
	}
}

func TestFilePrioritiesAndTrackers(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	if err := client.SetFilePriorities(ctx, "ABC", []int{0, 2, 5}, FilePriorityMaximal); err != nil {
		t.Fatalf("SetFilePriorities: %v", err)
	}
	if fake.lastFilePath.Get("hash") != "abc" || fake.lastFilePath.Get("id") != "0|2|5" || fake.lastFilePath.Get("priority") != "7" {
		t.Fatalf("filePrio form = %v", fake.lastFilePath)
	}
	if err := client.AddTrackers(ctx, "abc", []string{"udp://a", "udp://b"}); err != nil {
		t.Fatalf("AddTrackers: %v", err)
	}
	if fake.lastAddTracker.Get("urls") != "udp://a\nudp://b" {
		t.Fatalf("addTrackers urls = %q", fake.lastAddTracker.Get("urls"))
	}
	if err := client.EditTracker(ctx, "abc", "udp://old", "udp://new"); err != nil {
		t.Fatalf("EditTracker: %v", err)
	}
	if fake.lastEdit.Get("origUrl") != "udp://old" || fake.lastEdit.Get("newUrl") != "udp://new" {
		t.Fatalf("editTracker form = %v", fake.lastEdit)
	}
}

func TestShareLimitsForceStartAndPreferences(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	if err := client.SetShareLimits(ctx, 1.5, 1440, "abc"); err != nil {
		t.Fatalf("SetShareLimits: %v", err)
	}
	if fake.lastShareLimits.Get("ratioLimit") != "1.5" || fake.lastShareLimits.Get("seedingTimeLimit") != "1440" {
		t.Fatalf("share limits form = %v", fake.lastShareLimits)
	}
	if err := client.SetForceStart(ctx, true, "abc"); err != nil {
		t.Fatalf("SetForceStart: %v", err)
	}
	if fake.lastForceStart.Get("value") != "true" {
		t.Fatalf("force start form = %v", fake.lastForceStart)
	}
	if err := client.SetAutoManagement(ctx, false, "abc"); err != nil {
		t.Fatalf("SetAutoManagement: %v", err)
	}
	if err := client.CreateCategory(ctx, "gextto", "/data"); err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if fake.lastCategory.Get("category") != "gextto" || fake.lastCategory.Get("savePath") != "/data" {
		t.Fatalf("category form = %v", fake.lastCategory)
	}
	if err := client.SetPreferences(ctx, map[string]any{"max_connec": 200}); err != nil {
		t.Fatalf("SetPreferences: %v", err)
	}
	if !strings.Contains(fake.lastPrefs.Get("json"), "max_connec") {
		t.Fatalf("prefs form = %v", fake.lastPrefs)
	}
	if limit, err := client.GlobalDownloadLimit(ctx); err != nil || limit != 1024 {
		t.Fatalf("GlobalDownloadLimit = %d, %v", limit, err)
	}
	if path, err := client.DefaultSavePath(ctx); err != nil || path != "/data/downloads" {
		t.Fatalf("DefaultSavePath = %q, %v", path, err)
	}
}

func TestSetSequentialDownloadOnlyTogglesDifferences(t *testing.T) {
	fake, client := newFakeServer(t)
	ctx := context.Background()
	fake.torrents = []Torrent{
		{Hash: "aaa", Sequential: true},
		{Hash: "bbb", Sequential: false},
	}
	if err := client.SetSequentialDownload(ctx, true, "aaa", "bbb"); err != nil {
		t.Fatalf("SetSequentialDownload: %v", err)
	}
	// Only bbb differs and must be toggled.
	if fake.lastPause.Get("hashes") != "bbb" {
		t.Fatalf("toggled hashes = %q, want bbb", fake.lastPause.Get("hashes"))
	}
}
