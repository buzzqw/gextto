package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeQualityProfile(t *testing.T) {
	got := normalizeQualityProfile(qualityProfile{
		Name:      "  HD ",
		Qualities: []string{"1080p", "720P", "bogus", "1080p"},
		Cutoff:    "720p",
	})
	if got.Name != "HD" || len(got.Qualities) != 2 || got.Qualities[0] != "720p" || got.Qualities[1] != "1080p" {
		t.Fatalf("normalized = %+v", got)
	}
	if got.Cutoff != "720p" {
		t.Fatalf("cutoff = %q", got.Cutoff)
	}
	if got := normalizeQualityProfile(qualityProfile{Name: "x", Qualities: []string{"720p"}, Cutoff: "2160p"}); got.Cutoff != "" {
		t.Fatalf("a cutoff outside the profile must be removed: %+v", got)
	}
}

func TestQualityProfileAllowsAndCutoff(t *testing.T) {
	profile := qualityProfile{Name: "HD", Qualities: []string{"720p", "1080p"}, Cutoff: "1080p"}
	if !profile.allows("Show S01E01 1080p") || profile.allows("Show S01E01 2160p") {
		t.Fatal("the profile must allow only its qualities")
	}
	if profile.cutoffRank() != 5 || profile.maxRank() != 5 {
		t.Fatalf("ranks = %d/%d", profile.cutoffRank(), profile.maxRank())
	}
}

func TestWantedProfileMatchAndUpgrade(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(qualityProfilesKey, `[{"name":"HD","qualities":["720p","1080p"],"cutoff":"1080p"}]`)
	w := wantedEntry{Name: "Show", Kind: "series", Profile: "HD"}
	w.profile, _ = d.qualityProfileByName("HD")
	if !w.qualityMatch("Show S01E01 1080p") || w.qualityMatch("Show S01E01 2160p") {
		t.Fatal("the profile must drive the quality match")
	}
	// Taken at 720p: 1080p is an upgrade, 2160p is beyond the cutoff.
	d.state.WantedTakenQuality = map[string]smartEpisodes{
		wantedKey(w.Name, 1): {"1": 4},
	}
	if !d.wantedCanUpgrade(w, 1, 1, "Show S01E01 1080p") {
		t.Fatal("1080p must be an upgrade of 720p")
	}
	if d.wantedCanUpgrade(w, 1, 1, "Show S01E01 2160p") {
		t.Fatal("2160p is beyond the cutoff")
	}
	// Taken at 1080p: the cutoff is reached, nothing upgrades.
	d.state.WantedTakenQuality[wantedKey(w.Name, 1)]["1"] = 5
	if d.wantedCanUpgrade(w, 1, 1, "Show S01E01 2160p") {
		t.Fatal("the cutoff must stop upgrades")
	}
	// No profile: no upgrade logic.
	w.Profile, w.profile = "", qualityProfile{}
	if d.wantedCanUpgrade(w, 1, 1, "Show S01E01 1080p") {
		t.Fatal("a title without a profile must not upgrade")
	}
}

func TestQualityProfileSettingsHandler(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	post := func(values url.Values) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/quality-profile", strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		d.handleUIQualityProfile(rec, req)
		return rec
	}
	if rec := post(url.Values{"op": {"add"}, "name": {"4K"}, "qualities": {"1080p, 2160p"}, "cutoff": {"2160p"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("add status = %d", rec.Code)
	}
	if p, ok := d.qualityProfileByName("4K"); !ok || p.Cutoff != "2160p" {
		t.Fatalf("profile = %+v ok=%v", p, ok)
	}
	if rec := post(url.Values{"op": {"edit"}, "name": {"4K"}, "qualities": {"720p,1080p"}, "cutoff": {"1080p"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("edit status = %d", rec.Code)
	}
	if p, _ := d.qualityProfileByName("4K"); len(p.Qualities) != 2 || p.Cutoff != "1080p" {
		t.Fatalf("edited profile = %+v", p)
	}
	if rec := post(url.Values{"op": {"remove"}, "name": {"4K"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("remove status = %d", rec.Code)
	}
	if _, ok := d.qualityProfileByName("4K"); ok {
		t.Fatal("profile not removed")
	}
	// The settings page shows the table and the add form.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/settings", nil)
	d.renderSettings(rec, req, "", false)
	if body := rec.Body.String(); !strings.Contains(body, `id="quality-profiles"`) || !strings.Contains(body, `action="/ui/quality-profile"`) {
		t.Fatal("settings page does not show the quality profiles")
	}
}
