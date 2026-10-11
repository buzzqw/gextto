package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestUISettingsSavesLimitsAndSchedule checks the settings page writes the
// global limits (state.json) and the scheduler window (settings.json).
func TestUISettingsSavesLimitsAndSchedule(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	page, err := http.Get(srv.URL + "/ui/settings")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	if page.StatusCode != http.StatusOK || !strings.Contains(string(body), `name="speed-download"`) || !strings.Contains(string(body), `name="schedule-start"`) {
		t.Fatalf("settings page: status=%d", page.StatusCode)
	}

	resp, err := http.PostForm(srv.URL+"/ui/settings", url.Values{
		"speed-download":    {"2048"},
		"speed-upload":      {"512"},
		"schedule-enabled":  {"1"},
		"schedule-start":    {"01:00"},
		"schedule-end":      {"08:00"},
		"schedule-download": {"128"},
		"schedule-upload":   {"64"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cfg, _, _ := d.config()
	if cfg.SpeedLimitDownload != 2048 || cfg.SpeedLimitUpload != 512 {
		t.Fatalf("global limits = %d/%d, want 2048/512", cfg.SpeedLimitDownload, cfg.SpeedLimitUpload)
	}
	store := d.opts.Settings
	if store.Get(scheduleEnabledKey, "") != "true" || store.Get(scheduleStartKey, "") != "01:00" || store.Get(scheduleEndKey, "") != "08:00" {
		t.Fatalf("schedule window not saved: %q %q %q", store.Get(scheduleEnabledKey, ""), store.Get(scheduleStartKey, ""), store.Get(scheduleEndKey, ""))
	}
	if store.Get(scheduleDLKey, "") != "128" || store.Get(scheduleULKey, "") != "64" {
		t.Fatalf("scheduled limits = %q/%q", store.Get(scheduleDLKey, ""), store.Get(scheduleULKey, ""))
	}

	// Inside the window the scheduled limits are the effective ones; at 03:00
	// with a 01:00-08:00 window they are 128/64.
	inside := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)
	if dl, ul, active := d.scheduledSpeedLimits(inside); !active || dl != 128 || ul != 64 {
		t.Fatalf("inside the window: %d/%d active=%v", dl, ul, active)
	}
	outside := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if _, _, active := d.scheduledSpeedLimits(outside); active {
		t.Fatal("the schedule must not be active outside the window")
	}
}

// TestUISettingsRejectsBadWindow checks an invalid or empty window is refused.
func TestUISettingsRejectsBadWindow(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	for _, form := range []url.Values{
		{"schedule-enabled": {"1"}, "schedule-start": {"25:00"}, "schedule-end": {"08:00"}},
		{"schedule-enabled": {"1"}, "schedule-start": {"01:00"}, "schedule-end": {"01:00"}},
	} {
		resp, err := http.PostForm(srv.URL+"/ui/settings", form)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "err") {
			t.Fatalf("an invalid window must be reported: status=%d body=%s", resp.StatusCode, body)
		}
	}
	if d.opts.Settings.Get(scheduleEnabledKey, "") == "true" {
		t.Fatal("an invalid window must not be saved")
	}
}

// TestUISettingsSavesClientOptions checks the standalone settings page writes the
// folders, network, protocols and queue/cache options.
func TestUISettingsSavesClientOptions(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/ui/settings", url.Values{
		"download-dir":       {"/srv/media"},
		"temp-dir":           {"/srv/tmp"},
		"listen":             {"0.0.0.0:8899"},
		"peer-ports":         {"51400-51500"},
		"listen-interface":   {"eth0"},
		"outgoing-interface": {"wg0"},
		"proxy":              {"socks5://127.0.0.1:9050"},
		"token":              {"secret"},
		"ipfilter":           {"/etc/ipfilter.dat"},
		"ipfilter-trackers":  {"1"},
		"encryption":         {"2"},
		"dht":                {"1"},
		"dht-bootstrap":      {"router.example:6881"},
		"lang":               {"it"},
		"notify-url":         {"https://example/hook"},
		"active-downloads":   {"5"},
		"active-seeds":       {"2"},
		"active-limit":       {"8"},
		"cache-mb":           {"128"},
		"preallocate":        {"1"},
		"sequential":         {"1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	store := d.opts.Settings
	want := map[string]string{
		"download-dir": "/srv/media", "temp-dir": "/srv/tmp", "listen": "0.0.0.0:8899",
		"peer-ports": "51400-51500", "listen-interface": "eth0", "outgoing-interface": "wg0",
		"proxy": "socks5://127.0.0.1:9050", "token": "secret", "ipfilter": "/etc/ipfilter.dat",
		"ipfilter-trackers": "true", "encryption": "2", "dht-bootstrap": "router.example:6881",
		"lang": "it", "notify-url": "https://example/hook",
		"no-dht": "false", "no-pex": "true", "no-utp": "true",
	}
	for key, value := range want {
		if got := store.Get(key, ""); got != value {
			t.Errorf("setting %q = %q, want %q", key, got, value)
		}
	}
	cfg, _, _ := d.config()
	if cfg.ActiveDownloads != 5 || cfg.ActiveSeeds != 2 || cfg.ActiveLimit != 8 || cfg.CacheMB != 128 || !cfg.Preallocate || !cfg.Sequential {
		t.Fatalf("queue config = %+v", cfg)
	}

	// The page shows the saved values.
	page, err := http.Get(srv.URL + "/ui/settings")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(page.Body)
	page.Body.Close()
	for _, fragment := range []string{
		`name="download-dir" value="/srv/media"`,
		`name="listen" value="0.0.0.0:8899"`,
		`name="peer-ports" value="51400-51500"`,
	} {
		if !strings.Contains(string(body), fragment) {
			t.Fatalf("settings page missing %q", fragment)
		}
	}
}
