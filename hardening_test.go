package gextto

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

// The settings UI reads these keys from GET /api/config; if the view omits them
// the controls silently fall back to their defaults and saved values are
// invisible. This test pins the read/write round-trip.
func TestConfigViewExposesAcquisitionAndMaintenanceSettings(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	saved := map[string]string{
		"delay_torrent_minutes":                "45",
		"delay_movies_minutes":                 "20",
		"delay_bypass_score":                   "1500",
		"housekeeping_interval_hours":          "12",
		"housekeeping_retain_cycles":           "150",
		"housekeeping_seen_days":               "15",
		"media_info_backfill_interval_minutes": "30",
		"media_info_backfill_batch":            "7",
		"upgrade_min_score_diff":               "321",
		"cleanup_min_score_diff":               "11",
	}
	for key, value := range saved {
		payload, _ := json.Marshal(map[string]string{"key": key, "value": value})
		if status, body := webPostJSON(t, server, "/api/config/settings", string(payload)); status != http.StatusOK {
			t.Fatalf("save %s -> %d: %s", key, status, body)
		}
	}

	code, _, body := webGet(t, server, "/api/config")
	if code != http.StatusOK {
		t.Fatalf("config -> %d", code)
	}
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	for key, value := range saved {
		got, ok := view[key]
		if !ok {
			t.Errorf("config view is missing %q", key)
			continue
		}
		if normalizeJSONScalar(got) != value {
			t.Errorf("config[%s] = %v, want %s", key, got, value)
		}
	}
}

// normalizeJSONScalar renders a decoded JSON scalar the way the settings map
// stores it (strings verbatim, numbers without trailing zeros).
func normalizeJSONScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		encoded, _ := json.Marshal(value)
		return string(encoded)
	}
}

// A token set at runtime must be enforced immediately, without a restart.
func TestApiTokenSetAtRuntimeIsEnforced(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	payload, _ := json.Marshal(map[string]string{"key": "api_token", "value": "s3cret-token"})
	if status, body := webPostJSON(t, server, "/api/config/settings", string(payload)); status != http.StatusOK {
		t.Fatalf("save token -> %d: %s", status, body)
	}

	response, err := http.Get(server.URL + "/api/series")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("protected route without token -> %d, want 401", response.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/series", nil)
	request.Header.Set("x-gextto-token", "s3cret-token")
	authorized, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	authorized.Body.Close()
	if authorized.StatusCode != http.StatusOK {
		t.Fatalf("protected route with token -> %d, want 200", authorized.StatusCode)
	}

	// Public liveness endpoints stay reachable.
	if code, _, _ := webGet(t, server, "/api/health"); code != http.StatusOK {
		t.Fatalf("health should stay public: %d", code)
	}
}

func TestAddOptionsPreallocationAndStopFlags(t *testing.T) {
	opts := AddOptions{Preallocate: true, StopWhenReady: true}
	flags := opts.Flags()
	if flags&(1<<4) == 0 {
		t.Fatalf("preallocate flag bit missing: %b", flags)
	}
	if flags&(1<<5) == 0 {
		t.Fatalf("stop-when-ready flag bit missing: %b", flags)
	}
}

func TestLibtorrentPreallocateDefaultsToTrue(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Settings == nil {
		cfg.Settings = map[string]string{}
	}
	if !cfg.LibtorrentPreallocate() {
		t.Fatal("preallocation must default to true")
	}
	cfg.Settings["libtorrent_preallocate"] = "false"
	if cfg.LibtorrentPreallocate() {
		t.Fatal("explicit false ignored")
	}
	cfg.Settings["libtorrent_preallocate"] = "yes"
	if !cfg.LibtorrentPreallocate() {
		t.Fatal("explicit yes must enable preallocation")
	}
}

func TestCopyTorrentFileHonoursSetting(t *testing.T) {
	dir := t.TempDir()
	client := &LibtorrentClient{configDB: filepath.Join(dir, "gextto_config.db"), stateDir: dir}
	client.torrents = map[string]models.TorrentView{"abc": {Hash: "abc", Name: "My Torrent"}}

	// Without the setting nothing is copied.
	source := filepath.Join(dir, "src.torrent")
	if err := os.WriteFile(source, []byte("torrent"), 0o644); err != nil {
		t.Fatal(err)
	}
	client.copyTorrentFile("abc", source)
	outDir := filepath.Join(dir, "torrents")
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Fatalf("copy happened without the setting: %v", err)
	}

	// With the setting the .torrent is copied under the display name.
	if err := SaveSetting(dir, "libtorrent_torrent_copy_dir", outDir); err != nil {
		t.Fatal(err)
	}
	client.copyTorrentFile("abc", source)
	target := filepath.Join(outDir, "My Torrent.torrent")
	if data, err := os.ReadFile(target); err != nil || string(data) != "torrent" {
		t.Fatalf("torrent not copied to %s: %v %q", target, err, data)
	}
}

func TestLibtorrentSessionStatsEndpoint(t *testing.T) {
	state := newTestAppState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/api/libtorrent/session-stats")
	if code != http.StatusOK {
		t.Fatalf("session-stats -> %d: %s", code, body)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if !result.OK {
		t.Fatalf("session-stats not ok: %s", body)
	}
}

func TestFeedMaxPagesHonoursLegacyThresholdField(t *testing.T) {
	cases := []struct {
		settings map[string]string
		want     int
	}{
		{map[string]string{}, 3},
		{map[string]string{"stop_on_old_page_threshold": "5"}, 5},
		{map[string]string{"stop_on_old_page_threshold": "0.8"}, 3},
		{map[string]string{"stop_on_old_page_threshold": "5", "feed_max_pages": "7"}, 7},
		{map[string]string{"feed_max_pages": "42"}, 10},
	}
	for _, test := range cases {
		cfg := DefaultConfig()
		cfg.Settings = test.settings
		if got := cfg.FeedMaxPages(); got != test.want {
			t.Errorf("FeedMaxPages(%v) = %d, want %d", test.settings, got, test.want)
		}
	}
}
