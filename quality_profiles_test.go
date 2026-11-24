package gextto

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
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

// A repeated name is a mistake the operator has to see: the row must not
// disappear silently from the stored list.
func TestSaveQualityProfilesRefusesADuplicateName(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveSetting(state.cfg.DataDir, qualityProfilesKey,
		`[{"id":1,"name":"Default","language":"ita"}]`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	body := `[{"id":1,"name":"Default"},{"id":2,"name":"default","quality":"2160p"}]`
	SaveQualityProfiles(rec, httptest.NewRequest(http.MethodPost, "/api/quality-profiles", strings.NewReader(body)), state)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "duplicate profile name") {
		t.Fatalf("save -> %d %s, want a 400 naming the duplicate", rec.Code, rec.Body.String())
	}
	// The stored list is untouched: a rejected save changes nothing.
	if profiles := ensureQualityProfiles(state); len(profiles) != 1 || profiles[0].Quality != "" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestSaveQualityProfilesRefusesARunawayList(t *testing.T) {
	state := newTestAppState(t)
	items := make([]string, 0, maxQualityProfiles+1)
	for index := 0; index <= maxQualityProfiles; index++ {
		items = append(items, fmt.Sprintf(`{"name":"P%d"}`, index))
	}
	rec := httptest.NewRecorder()
	SaveQualityProfiles(rec, httptest.NewRequest(http.MethodPost, "/api/quality-profiles",
		strings.NewReader("["+strings.Join(items, ",")+"]")), state)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("save -> %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// The exact match folds the quality case and the language codes, as the
// release check does: a title stored as "1080P" with "it" is the profile of
// "1080p" and "ita", and the order of a list does not make a new policy.
func TestMatchingQualityProfileFoldsCaseAndLanguageCodes(t *testing.T) {
	profiles := []QualityProfile{{
		ID: 2, Name: "HD", Quality: "1080p", Language: "ita,eng", Subtitle: "ita",
	}}
	if id := matchingQualityProfile(profiles, profileTitleValues{
		Quality: "1080P", Language: "it,en", Subtitle: "ITA",
	}, "ita"); id != 2 {
		t.Fatalf("matching profile = %d, want 2", id)
	}
	if id := matchingQualityProfile(profiles, profileTitleValues{
		Quality: "1080p", Language: "eng,ita", Subtitle: "ita",
	}, "ita"); id != 2 {
		t.Fatalf("the list order must not make a different policy, got %d", id)
	}
	if id := matchingQualityProfile(profiles, profileTitleValues{
		Quality: "2160p", Language: "ita,eng", Subtitle: "ita",
	}, "ita"); id != 0 {
		t.Fatalf("a different quality must not match, got %d", id)
	}
}

// The exclusions of a profile are stored as a canonical list, so saving the
// editor twice does not grow the value with duplicates and spaces.
// A value the release filters cannot understand is a widened profile, not a
// harmless typo: "1080pp" used to be stored and then read as "any resolution".
func TestSaveQualityProfilesRefusesInvalidValues(t *testing.T) {
	for name, body := range map[string]string{
		"quality":  `[{"name":"P","quality":"1080pp"}]`,
		"audio":    `[{"name":"P","audio":"loud"}]`,
		"language": `[{"name":"P","language":"italianoo"}]`,
		"subtitle": `[{"name":"P","subtitle":"ita,italianoo"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			state := newTestAppState(t)
			rec := httptest.NewRecorder()
			SaveQualityProfiles(rec, httptest.NewRequest(http.MethodPost, "/api/quality-profiles", strings.NewReader(body)), state)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid quality profile") {
				t.Fatalf("save -> %d %s, want a 400 with the reason", rec.Code, rec.Body.String())
			}
			for _, profile := range ensureQualityProfiles(state) {
				if profile.Name == "P" {
					t.Fatalf("a refused save stored the profile: %+v", profile)
				}
			}
		})
	}
}

func TestSaveQualityProfilesAcceptsLegacySpellings(t *testing.T) {
	state := newTestAppState(t)
	body := `[{"name":"P","quality":"<1080p","audio":"lossless","language":"english","subtitle":"any"}]`
	rec := httptest.NewRecorder()
	SaveQualityProfiles(rec, httptest.NewRequest(http.MethodPost, "/api/quality-profiles", strings.NewReader(body)), state)
	if rec.Code != http.StatusOK {
		t.Fatalf("save -> %d %s", rec.Code, rec.Body.String())
	}
	profiles := ensureQualityProfiles(state)
	if len(profiles) != 1 || profiles[0].Quality != "<1080p" || profiles[0].Audio != "lossless" || profiles[0].Language != "english" || profiles[0].Subtitle != "any" {
		t.Fatalf("legacy values were changed: %+v", profiles)
	}
}

// Applying a profile to an already monitored film must clear the old hard
// subtitle requirement too, as the add form does: otherwise the film keeps
// asking for the previous language while the profile says another one.
func TestMovieProfileClearsSubtitleRequirements(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveSetting(state.cfg.DataDir, qualityProfilesKey,
		`[{"id":7,"name":"HD","quality":"1080p","language":"eng","subtitle":"eng"}]`); err != nil {
		t.Fatal(err)
	}
	if err := SaveLibrary(state.cfg.DataDir, nil, []MovieConfig{{
		ID: 3, Name: "Example Movie", Year: "2020", Quality: "720p", Language: "ita",
		Subtitle: "ita", SubtitleRequirements: "ita", Enabled: true,
	}}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	form := url.Values{"view": {"movies"}, "path": {"/api/movies/3"}, "name": {"Example Movie"}, "year": {"2020"},
		"subtitle": {"ita"}, "quality_profile_id": {"7"}, "_v2_kind_quality_profile_id": {"number"}}
	code, body := v2Request(t, server, http.MethodPost, "/section/form", form)
	if code >= 400 {
		t.Fatalf("movie apply -> %d %s", code, body)
	}
	movie := latestConfig(state).Movies[0]
	if movie.Language != "eng" || movie.Subtitle != "eng" || movie.SubtitleRequirements != "" {
		t.Fatalf("movie after profile = %+v, want the old mandatory subtitle cleared", movie)
	}
}

// The explicit qualities list is the migration of the old single expression:
// read from an existing saved profile, written by the editor as a string.
func TestQualityProfileQualitiesMigration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{
		qualityProfilesKey: `[{"id":1,"name":"Min","quality":"1080p+"},{"id":2,"name":"Range","quality":"720p-1080p"}]`,
	}
	profiles := qualityProfilesFromConfig(&cfg)
	if len(profiles) != 2 {
		t.Fatalf("profiles = %+v", profiles)
	}
	if got := strings.Join(profiles[0].Qualities, ","); got != "1080p,2160p" {
		t.Fatalf("minimum -> %q, want 1080p,2160p", got)
	}
	if got := strings.Join(profiles[1].Qualities, ","); got != "720p,1080p" {
		t.Fatalf("range -> %q, want 720p,1080p", got)
	}

	state := newTestAppState(t)
	rec := httptest.NewRecorder()
	body := `[{"name":"HD","qualities":"720p, 1080p, 2160p","cutoff":"1080p","language":"ita"},{"name":"4K","qualities":["2160p"],"cutoff":"720p","language":"ita"}]`
	SaveQualityProfiles(rec, httptest.NewRequest(http.MethodPost, "/api/quality-profiles", strings.NewReader(body)), state)
	if rec.Code != http.StatusOK {
		t.Fatalf("save -> %d %s", rec.Code, rec.Body.String())
	}
	saved := qualityProfilesFromConfig(latestConfig(state))
	if len(saved) != 2 {
		t.Fatalf("saved = %+v", saved)
	}
	// An old cutoff becomes the accepted ceiling: the resolutions above it
	// are dropped and the separate rule disappears.
	if got := strings.Join(saved[0].Qualities, ","); got != "720p,1080p" || saved[0].Cutoff != "" || saved[0].Quality != "720p-1080p" || saved[0].Upgrade != profileUpgradeImprove {
		t.Fatalf("saved HD = %+v", saved[0])
	}
	if got := strings.Join(saved[1].Qualities, ","); got != "2160p" || saved[1].Cutoff != "" {
		t.Fatalf("cutoff below the whole list was not migrated: %+v", saved[1])
	}
}

func TestResolutionInList(t *testing.T) {
	list := []string{"720p", "2160p"}
	if !resolutionInList(&models.Quality{Resolution: "720p"}, list, "") ||
		!resolutionInList(&models.Quality{Resolution: "2160p"}, list, "") {
		t.Fatal("a listed resolution must be accepted")
	}
	if resolutionInList(&models.Quality{Resolution: "1080p"}, list, "") ||
		resolutionInList(&models.Quality{Resolution: "unknown"}, list, "") {
		t.Fatal("a resolution outside the list must be refused")
	}
	if !resolutionInList(&models.Quality{Resolution: "1080p"}, nil, "1080p+") ||
		resolutionInList(&models.Quality{Resolution: "720p"}, nil, "1080p+") {
		t.Fatal("without a list the legacy expression still decides")
	}
}

// The list on a title is a filter, not a preference: a release outside it is
// refused even when the legacy single-value field would have accepted it.
func TestTitleQualitiesFilterReleases(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Series = []SeriesConfig{{Name: "Show", Seasons: "1+", Quality: "720p+",
		Qualities: []string{"720p", "2160p"}, Enabled: true}}
	cfg.Movies = []MovieConfig{{Name: "Example Movie", Year: "2020", Quality: "720p+",
		Qualities: []string{"720p", "2160p"}, Enabled: true}}
	for name, quality := range map[string]string{"720p": "720p", "2160p": "2160p"} {
		if !cfg.SeriesReleaseAllowed(&cfg.Series[0], &models.Quality{Resolution: quality}, "release") {
			t.Fatalf("series refused %s", name)
		}
		if !cfg.MovieReleaseAllowed(&cfg.Movies[0], &models.Quality{Resolution: quality}) {
			t.Fatalf("movie refused %s", name)
		}
	}
	if cfg.SeriesReleaseAllowed(&cfg.Series[0], &models.Quality{Resolution: "1080p"}, "release") ||
		cfg.MovieReleaseAllowed(&cfg.Movies[0], &models.Quality{Resolution: "1080p"}) {
		t.Fatal("a resolution outside the accepted list was admitted")
	}
}

// Titles saved with a cutoff are migrated on load: the old ceiling becomes a
// narrower accepted list, so no invisible rule is left behind.
func TestOldTitleCutoffMigratesToNarrowedQualities(t *testing.T) {
	got := migrateTitleCutoff([]string{"720p", "1080p", "2160p"}, "720p+", "1080p")
	if joined := strings.Join(got, ","); joined != "720p,1080p" {
		t.Fatalf("qualities = %q, want 720p,1080p", joined)
	}
	got = migrateTitleCutoff(nil, "1080p+", "1080p")
	if joined := strings.Join(got, ","); joined != "1080p" {
		t.Fatalf("legacy expression = %q, want 1080p", joined)
	}
	got = migrateTitleCutoff([]string{"2160p"}, "2160p", "720p")
	if joined := strings.Join(got, ","); joined != "2160p" {
		t.Fatalf("cutoff below the list = %q, want 2160p", joined)
	}
}

// The v3 profile list now carries real items and a cutoff, so a request
// manager can show what a profile accepts instead of an empty list.
func TestV3QualityProfilesExposeItemsAndCutoff(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveSetting(state.cfg.DataDir, qualityProfilesKey,
		`[{"id":1,"name":"HD","qualities":"720p,1080p,2160p","upgrade":"improve","language":"ita"}]`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	V3QualityProfiles(rec, httptest.NewRequest(http.MethodGet, "/api/v3/qualityprofile", nil), state)
	if rec.Code != http.StatusOK {
		t.Fatalf("v3 profiles -> %d %s", rec.Code, rec.Body.String())
	}
	var profiles []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profiles = %+v", profiles)
	}
	// A *arr client reads a real cutoff id even though the profile no longer
	// stores one: it is the highest accepted resolution.
	if cutoff, _ := profiles[0]["cutoff"].(float64); cutoff != 6 {
		t.Fatalf("cutoff = %v, want the id of 2160p (6)", profiles[0]["cutoff"])
	}
	items, _ := profiles[0]["items"].([]any)
	if len(items) != len(profileQualityLabels) {
		t.Fatalf("items = %d, want %d", len(items), len(profileQualityLabels))
	}
	first := items[0].(map[string]any)
	if first["allowed"] != false || first["quality"].(map[string]any)["name"] != "360p" {
		t.Fatalf("first item = %+v", first)
	}
	last := items[len(items)-1].(map[string]any)
	if last["allowed"] != true {
		t.Fatalf("last item = %+v", last)
	}
}

func TestSaveQualityProfilesNormalizesExclusions(t *testing.T) {
	state := newTestAppState(t)
	rec := httptest.NewRecorder()
	SaveQualityProfiles(rec, httptest.NewRequest(http.MethodPost, "/api/quality-profiles",
		strings.NewReader(`[{"name":"Senza cam","exclude":" Cam , ts,CAM "}]`)), state)
	if rec.Code != 200 {
		t.Fatalf("save -> %d %s", rec.Code, rec.Body.String())
	}
	if profiles := ensureQualityProfiles(state); len(profiles) != 1 || profiles[0].Exclude != "Cam, ts" {
		t.Fatalf("profiles = %+v", profiles)
	}
}
