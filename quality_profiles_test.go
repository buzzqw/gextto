package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A profile matches a title only when the exclusions agree as a set: applying
// a profile adds its exclusions to the title's, so a title that differs would
// change on the next save of an unrelated field.
func TestMatchingQualityProfileComparesExclusions(t *testing.T) {
	profiles := []QualityProfile{{
		ID: 1, Name: "Full HD senza cam", Quality: "1080p", Language: "ita", Exclude: "cam, ts",
	}}
	if id := matchingQualityProfile(profiles, profileTitleValues{
		Quality: "1080p", Language: "ita", Exclude: "spinoff",
	}, "ita"); id != 0 {
		t.Fatalf("a title with different exclusions matched profile %d", id)
	}
	if id := matchingQualityProfile(profiles, profileTitleValues{
		Quality: "1080p", Language: "ita", Exclude: "TS, cam",
	}, "ita"); id != 1 {
		t.Fatalf("the same exclusions in another order must match, got %d", id)
	}
}

func TestSameExclusionsIgnoresOrderCaseAndRepeats(t *testing.T) {
	if !sameExclusions("cam, TS", "ts,cam,cam") {
		t.Fatal("the same exclusions must compare equal")
	}
	if sameExclusions("cam", "cam, ts") {
		t.Fatal("a missing exclusion must not compare equal")
	}
}

// An empty language in a profile means the configured default language, the
// same as an empty language field in the add form.
func TestQualityProfileEmptyLanguageTakesTheDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{
		"default_language": "eng",
		qualityProfilesKey: `[{"id":1,"name":"Default","quality":"1080p"}]`,
	}
	profiles := qualityProfilesFromConfig(&cfg)
	if len(profiles) != 1 || profiles[0].Language != "eng" {
		t.Fatalf("profiles = %+v, want the default language eng", profiles)
	}
	// The title side uses the same rule, so a title with an empty language
	// matches the profile instead of showing «Current values».
	if id := matchingQualityProfile(profiles, profileTitleValues{Quality: "1080p"}, "eng"); id != 1 {
		t.Fatalf("matching profile = %d, want 1", id)
	}
	// The configured default is used for the summary and for the v3 API too.
	if language := qualityProfileLanguage(&cfg, ""); language != "eng" {
		t.Fatalf("language = %q, want eng", language)
	}
}

// The automatic migration must keep the minimum audio and the upgrade switch
// of the titles it derives the profiles from.
func TestSeededProfilesKeepAudioAndUpgrades(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{Name: "Show", Seasons: "1+",
		Quality: "2160p", Audio: "lossless-5.1", Language: "eng", DisableUpgrades: true, Enabled: true}}, nil); err != nil {
		t.Fatal(err)
	}
	profiles := ensureQualityProfiles(state)
	found := false
	for _, profile := range profiles {
		if profile.Quality == "2160p" {
			found = true
			if profile.Audio != "lossless-5.1" || !profile.DisableUpgrades || profile.Language != "eng" || profile.Upgrades != "deny" {
				t.Fatalf("seeded profile = %+v", profile)
			}
		}
	}
	if !found {
		t.Fatalf("no profile was derived from the library: %+v", profiles)
	}
}

func TestSaveQualityProfilesFillsTheDefaultLanguage(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveSetting(state.cfg.DataDir, "default_language", "eng"); err != nil {
		t.Fatal(err)
	}
	TouchConfigGeneration()
	rec := httptest.NewRecorder()
	SaveQualityProfiles(rec, httptest.NewRequest(http.MethodPost, "/api/quality-profiles",
		strings.NewReader(`[{"name":"Senza lingua","quality":"1080p"}]`)), state)
	if rec.Code != 200 {
		t.Fatalf("save -> %d %s", rec.Code, rec.Body.String())
	}
	profiles := ensureQualityProfiles(state)
	if len(profiles) != 1 || profiles[0].Language != "eng" {
		t.Fatalf("profiles = %+v, want language eng", profiles)
	}
}
