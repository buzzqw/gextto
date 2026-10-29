package gextto

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// qbitRecordingEngine records the calls the compatibility API makes, so the
// tests do not need a daemon.
type qbitRecordingEngine struct {
	nullEngine

	mu        sync.Mutex
	adds      []string
	files     [][]byte
	paused    []string
	resumed   []string
	rechecked []string
	removed   []string
	listed    []models.TorrentView
	accept    bool
}

func (e *qbitRecordingEngine) Name() string { return "test" }

func (e *qbitRecordingEngine) List() []models.TorrentView {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]models.TorrentView(nil), e.listed...)
}

func (e *qbitRecordingEngine) AddWithOptions(link string, _ *Config, _ *string, _ AddOptions) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.adds = append(e.adds, link)
	return e.accept, nil
}

func (e *qbitRecordingEngine) AddTorrentFileWithOptions(path string, _ *Config, _ *string, _ AddOptions) (*string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	e.files = append(e.files, payload)
	if !e.accept {
		return nil, nil
	}
	hash := "0123456789abcdef"
	return &hash, nil
}

func (e *qbitRecordingEngine) Pause(hash string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.paused = append(e.paused, hash)
	return true, nil
}

func (e *qbitRecordingEngine) Resume(hash string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resumed = append(e.resumed, hash)
	return true, nil
}

func (e *qbitRecordingEngine) ForceRecheck(hash string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rechecked = append(e.rechecked, hash)
	return true, nil
}

func (e *qbitRecordingEngine) Remove(hash string, _ bool) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removed = append(e.removed, hash)
	return true, nil
}

// newQbitState returns a state with the recording engine and a completed setup,
// which the add route requires.
func newQbitState(t *testing.T) (*AppState, *qbitRecordingEngine) {
	t.Helper()
	state := newTestAppState(t)
	engine := &qbitRecordingEngine{accept: true}
	state.setActiveEngine(engine)
	if err := CompleteSetup(state.cfg); err != nil {
		t.Fatalf("CompleteSetup: %v", err)
	}
	return state, engine
}

// qbitCall performs a request against the real router.
func qbitCall(t *testing.T, handler http.Handler, method, target, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:5555"
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func qbitFormCall(t *testing.T, handler http.Handler, target string, values url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return qbitCall(t, handler, http.MethodPost, target, "application/x-www-form-urlencoded", []byte(values.Encode()))
}

func TestQbitCompatInformationalRoutes(t *testing.T) {
	state, engine := newQbitState(t)
	engine.listed = []models.TorrentView{{Hash: "AA", DownloadRate: 100, UploadRate: 7}}
	handler := Router(state)

	login := qbitCall(t, handler, http.MethodPost, "/api/v2/auth/login", "application/x-www-form-urlencoded", []byte("username=&password="))
	if login.Code != http.StatusOK || login.Body.String() != qbitOKBody {
		t.Fatalf("login without access control: %d %q", login.Code, login.Body.String())
	}
	logout := qbitCall(t, handler, http.MethodPost, "/api/v2/auth/logout", "", nil)
	if logout.Code != http.StatusOK || logout.Body.String() != qbitOKBody {
		t.Fatalf("logout: %d %q", logout.Code, logout.Body.String())
	}
	version := qbitCall(t, handler, http.MethodGet, "/api/v2/app/version", "", nil)
	if version.Code != http.StatusOK || !strings.HasPrefix(version.Body.String(), "v") {
		t.Fatalf("app/version: %d %q", version.Code, version.Body.String())
	}
	webAPI := qbitCall(t, handler, http.MethodGet, "/api/v2/app/webapiVersion", "", nil)
	if webAPI.Code != http.StatusOK || webAPI.Body.String() != qbitWebAPIVersion {
		t.Fatalf("app/webapiVersion: %d %q", webAPI.Code, webAPI.Body.String())
	}
	savePath := qbitCall(t, handler, http.MethodGet, "/api/v2/app/defaultSavePath", "", nil)
	if savePath.Body.String() != latestConfig(state).DownloadDir {
		t.Fatalf("app/defaultSavePath = %q", savePath.Body.String())
	}

	transfer := qbitCall(t, handler, http.MethodGet, "/api/v2/transfer/info", "", nil)
	var info map[string]any
	if err := json.Unmarshal(transfer.Body.Bytes(), &info); err != nil {
		t.Fatalf("transfer/info: %v (%s)", err, transfer.Body.String())
	}
	if info["dl_info_speed"] != float64(100) || info["up_info_speed"] != float64(7) {
		t.Fatalf("transfer/info speeds = %v", info)
	}
}

func TestQbitCompatAddMagnetAndTorrent(t *testing.T) {
	state, engine := newQbitState(t)
	handler := Router(state)

	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x"
	added := qbitFormCall(t, handler, "/api/v2/torrents/add", url.Values{"urls": {magnet}})
	if added.Code != http.StatusOK || added.Body.String() != qbitOKBody {
		t.Fatalf("add magnet: %d %q", added.Code, added.Body.String())
	}
	if len(engine.adds) != 1 || engine.adds[0] != magnet {
		t.Fatalf("engine adds = %v", engine.adds)
	}

	// The same torrent again: the engine reports it as already present and the
	// extension expects 409.
	engine.accept = false
	duplicate := qbitFormCall(t, handler, "/api/v2/torrents/add", url.Values{"urls": {magnet}})
	if duplicate.Code != http.StatusConflict || duplicate.Body.String() != qbitFailBody {
		t.Fatalf("duplicate add: %d %q", duplicate.Code, duplicate.Body.String())
	}

	// A .torrent uploaded in the field the extensions use.
	engine.accept = true
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("fileselect", "movie.torrent")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("d4:infod4:name5:movieee")
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploaded := qbitCall(t, handler, http.MethodPost, "/api/v2/torrents/add", writer.FormDataContentType(), body.Bytes())
	if uploaded.Code != http.StatusOK || uploaded.Body.String() != qbitOKBody {
		t.Fatalf("add upload: %d %q", uploaded.Code, uploaded.Body.String())
	}
	if len(engine.files) != 1 || !bytes.Equal(engine.files[0], payload) {
		t.Fatalf("engine received %v", engine.files)
	}
}

func TestQbitCompatAddFetchesAnHTTPTorrent(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write([]byte("d4:infod4:name5:movieee"))
	}))
	defer source.Close()

	state, engine := newQbitState(t)
	// The compatibility API does not download .torrent files in dry-run, like
	// the browser handlers: turn it off for this test.
	if err := saveConfigSetting(state.cfg.DataDir, "dry_run", "false"); err != nil {
		t.Fatal(err)
	}
	handler := Router(state)
	recorder := qbitFormCall(t, handler, "/api/v2/torrents/add", url.Values{"urls": {source.URL + "/movie.torrent"}})
	if recorder.Code != http.StatusOK || recorder.Body.String() != qbitOKBody {
		t.Fatalf("add url: %d %q", recorder.Code, recorder.Body.String())
	}
	if len(engine.files) != 1 || !bytes.Contains(engine.files[0], []byte("movie")) {
		t.Fatalf("engine received %v", engine.files)
	}
}

func TestQbitCompatAddRefusesOptionsItCannotHonour(t *testing.T) {
	state, engine := newQbitState(t)
	handler := Router(state)
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"

	for name, values := range map[string]url.Values{
		"category":      {"urls": {magnet}, "category": {"movies"}},
		"tags":          {"urls": {magnet}, "tags": {"nas"}},
		"contentLayout": {"urls": {magnet}, "contentLayout": {"Subfolder"}},
	} {
		recorder := qbitFormCall(t, handler, "/api/v2/torrents/add", values)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s was not refused: %d", name, recorder.Code)
		}
	}
	if len(engine.adds) != 0 {
		t.Fatalf("a refused option still added %v", engine.adds)
	}

	// The default layout the extensions send is accepted.
	original := qbitFormCall(t, handler, "/api/v2/torrents/add", url.Values{"urls": {magnet}, "contentLayout": {"Original"}})
	if original.Code != http.StatusOK {
		t.Fatalf("contentLayout=Original: %d %q", original.Code, original.Body.String())
	}
}

func TestQbitCompatAddRequiresTheSetup(t *testing.T) {
	state := newTestAppState(t)
	state.setActiveEngine(&qbitRecordingEngine{accept: true})
	handler := Router(state)
	recorder := qbitFormCall(t, handler, "/api/v2/torrents/add", url.Values{"urls": {"magnet:?xt=urn:btih:aa"}})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("add before the setup: %d", recorder.Code)
	}
}

func TestQbitCompatTorrentsInfoMapsTheStates(t *testing.T) {
	state, engine := newQbitState(t)
	engine.listed = []models.TorrentView{
		// Gextto reports the completion in percent; qBittorrent wants 0..1.
		{Hash: "AA", Name: "downloading", State: "downloading", Progress: 50, TotalSize: 100, TotalDone: 50, DownloadRate: 10, NumSeeds: 3, NumPeers: 4},
		{Hash: "BB", Name: "seeding", State: "seeding", Progress: 100, IsSeeding: true, TotalSize: 100, TotalDone: 100, UploadRate: 5},
		{Hash: "CC", Name: "stopped", State: "paused", Progress: 25},
	}
	handler := Router(state)

	recorder := qbitCall(t, handler, http.MethodGet, "/api/v2/torrents/info", "", nil)
	var torrents []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &torrents); err != nil {
		t.Fatalf("torrents/info: %v (%s)", err, recorder.Body.String())
	}
	if len(torrents) != 3 {
		t.Fatalf("listed %d torrents", len(torrents))
	}
	first := torrents[0]
	if first["hash"] != "aa" || first["state"] != "downloading" || first["progress"] != 0.5 {
		t.Fatalf("first torrent = %v", first)
	}
	if first["amount_left"] != float64(50) || first["dlspeed"] != float64(10) {
		t.Fatalf("first torrent sizes = %v", first)
	}
	if torrents[1]["state"] != "seeding" || torrents[2]["state"] != "pausedDL" {
		t.Fatalf("states = %v %v", torrents[1]["state"], torrents[2]["state"])
	}

	filtered := qbitCall(t, handler, http.MethodGet, "/api/v2/torrents/info?hashes=bb", "", nil)
	var only []map[string]any
	if err := json.Unmarshal(filtered.Body.Bytes(), &only); err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0]["hash"] != "bb" {
		t.Fatalf("hashes filter = %v", only)
	}
	seeding := qbitCall(t, handler, http.MethodGet, "/api/v2/torrents/info?filter=seeding", "", nil)
	if err := json.Unmarshal(seeding.Body.Bytes(), &only); err != nil {
		t.Fatal(err)
	}
	if len(only) != 1 || only[0]["hash"] != "bb" {
		t.Fatalf("seeding filter = %v", only)
	}
}

func TestQbitCompatTorrentActions(t *testing.T) {
	state, engine := newQbitState(t)
	engine.listed = []models.TorrentView{{Hash: "aa"}, {Hash: "bb"}}
	handler := Router(state)

	if recorder := qbitFormCall(t, handler, "/api/v2/torrents/pause", url.Values{"hashes": {"aa|bb"}}); recorder.Code != http.StatusOK {
		t.Fatalf("pause: %d", recorder.Code)
	}
	if len(engine.paused) != 2 {
		t.Fatalf("paused = %v", engine.paused)
	}
	qbitFormCall(t, handler, "/api/v2/torrents/stop", url.Values{"hashes": {"aa"}})
	qbitFormCall(t, handler, "/api/v2/torrents/resume", url.Values{"hashes": {"aa"}})
	qbitFormCall(t, handler, "/api/v2/torrents/start", url.Values{"hashes": {}})
	if len(engine.resumed) != 3 {
		t.Fatalf("resumed = %v", engine.resumed)
	}
	qbitFormCall(t, handler, "/api/v2/torrents/recheck", url.Values{"hashes": {"aa"}})
	if len(engine.rechecked) != 1 {
		t.Fatalf("rechecked = %v", engine.rechecked)
	}
	if recorder := qbitFormCall(t, handler, "/api/v2/torrents/delete", url.Values{"hashes": {"aa"}, "deleteFiles": {"false"}}); recorder.Code != http.StatusOK {
		t.Fatalf("delete: %d %q", recorder.Code, recorder.Body.String())
	}
	if len(engine.removed) != 1 || engine.removed[0] != "aa" {
		t.Fatalf("removed = %v", engine.removed)
	}
}

func TestQbitCompatSessionOpensOnlyTheCompatAPI(t *testing.T) {
	state := newTestAppState(t)
	state.setActiveEngine(&qbitRecordingEngine{accept: true})
	for key, value := range map[string]string{
		"auth_enabled":      "true",
		"auth_password":     "s3cret!",
		"auth_local_bypass": "false",
	} {
		if err := saveConfigSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	handler := AuthMiddleware(state, Router(state))
	call := func(method, target, contentType string, body []byte, mutate func(*http.Request)) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, target, bytes.NewReader(body))
		request.RemoteAddr = "198.51.100.7:4000"
		if contentType != "" {
			request.Header.Set("Content-Type", contentType)
		}
		if mutate != nil {
			mutate(request)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	// The login route must be reachable before any session exists.
	bad := call(http.MethodPost, "/api/v2/auth/login", "application/x-www-form-urlencoded", []byte("username=admin&password=wrong"), nil)
	if bad.Code != http.StatusOK || bad.Body.String() != qbitFailBody {
		t.Fatalf("failed login: %d %q", bad.Code, bad.Body.String())
	}
	good := call(http.MethodPost, "/api/v2/auth/login", "application/x-www-form-urlencoded", []byte("username=admin&password="+url.QueryEscape("s3cret!")), nil)
	if good.Code != http.StatusOK || good.Body.String() != qbitOKBody {
		t.Fatalf("login: %d %q", good.Code, good.Body.String())
	}
	var sid *http.Cookie
	for _, cookie := range good.Result().Cookies() {
		if cookie.Name == qbitSIDCookie {
			sid = cookie
		}
	}
	if sid == nil || sid.Value == "" {
		t.Fatal("login did not set the SID cookie")
	}

	list := call(http.MethodGet, "/api/v2/torrents/info", "", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: qbitSIDCookie, Value: sid.Value})
	})
	if list.Code != http.StatusOK {
		t.Fatalf("list with the SID cookie: %d %q", list.Code, list.Body.String())
	}
	// The same cookie must not open the rest of the interface.
	other := call(http.MethodGet, "/api/health", "", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: qbitSIDCookie, Value: sid.Value})
	})
	if other.Code != http.StatusUnauthorized {
		t.Fatalf("the SID cookie opened %s: %d", "/api/health", other.Code)
	}
	if anonymous := call(http.MethodGet, "/api/v2/torrents/info", "", nil, nil); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("list without a session: %d", anonymous.Code)
	}
}

func TestQbitCompatLoginAcceptsLocalClientsWithTheBypass(t *testing.T) {
	state := newTestAppState(t)
	for key, value := range map[string]string{"auth_enabled": "true", "auth_password": "s3cret!"} {
		if err := saveConfigSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	handler := AuthMiddleware(state, Router(state))
	request := httptest.NewRequest(http.MethodPost, "/api/v2/auth/login", strings.NewReader("username=&password="))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.RemoteAddr = "127.0.0.1:5555"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Body.String() != qbitOKBody {
		t.Fatalf("local login: %d %q", recorder.Code, recorder.Body.String())
	}
}
