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

// TestUIRssFollowCreatesFeedAndRule checks the one-click "follow a title".
func TestUIRssFollowCreatesFeedAndRule(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/ui/rss-follow", url.Values{
		"title": {"Serie S01"}, "kind": {"series"}, "include": {"1080p"},
		"savepath": {"/srv/tv"}, "category": {"tv"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	feeds := d.feeds()
	if len(feeds) != 1 || feeds[0].Name != "Serie S01" || feeds[0].Search != "Serie S01" {
		t.Fatalf("feeds = %+v", feeds)
	}
	rules := d.rules()
	if len(rules) != 1 || rules[0].Name != "Serie S01" || len(rules[0].Feeds) != 1 || rules[0].Feeds[0] != "Serie S01" {
		t.Fatalf("rules = %+v", rules)
	}
	if !rules[0].Match.RequireEpisode || !rules[0].Match.SmartEpisode {
		t.Fatalf("a followed series must require and track episodes: %+v", rules[0].Match)
	}
	if rules[0].Action.SavePath != "/srv/tv" || rules[0].Action.Category != "tv" {
		t.Fatalf("action = %+v", rules[0].Action)
	}

	// A movie follow does not force the episode marker.
	resp, err = http.PostForm(srv.URL+"/ui/rss-follow", url.Values{"title": {"Big Movie"}, "kind": {"movie"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, rule := range d.rules() {
		if rule.Name == "Big Movie" && (rule.Match.RequireEpisode || rule.Match.SmartEpisode) {
			t.Fatalf("a followed movie must not require episodes: %+v", rule.Match)
		}
	}
}
