package gextto

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func v3TestState(t *testing.T) *AppState {
	t.Helper()
	state := newTestAppState(t)
	archive := state.cfg.DataDir + "/library"
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{
		{Name: "Show", Seasons: "1+", Quality: "1080p", Language: "ita", ArchivePath: archive, Enabled: true},
		{Name: "Other", Seasons: "1+", Quality: "2160p", Language: "eng", ArchivePath: archive, Enabled: true},
	}, []MovieConfig{{Name: "Film", Year: "2020", Quality: "1080p", Language: "ita", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	root := archive
	state.cfg.ArchiveRoot = &root
	TouchConfigGeneration()
	return state
}

func TestV3ProfilesAreGeneratedFromTheLibrary(t *testing.T) {
	state := v3TestState(t)
	rec := httptest.NewRecorder()
	V3QualityProfiles(rec, httptest.NewRequest("GET", "/api/v3/qualityprofile", nil), state)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var profiles []v3QualityProfile
	if err := json.Unmarshal(rec.Body.Bytes(), &profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) < 3 || profiles[0].Name != "Default" {
		t.Fatalf("profiles = %+v", profiles)
	}
	for _, profile := range profiles[1:] {
		if profile.Name == "" {
			t.Fatalf("a generated profile has no name: %+v", profiles)
		}
	}
}

func TestV3RootFoldersAndStatus(t *testing.T) {
	state := v3TestState(t)
	rec := httptest.NewRecorder()
	V3RootFolders(rec, httptest.NewRequest("GET", "/api/v3/rootfolder", nil), state)
	var folders []v3RootFolder
	if err := json.Unmarshal(rec.Body.Bytes(), &folders); err != nil || len(folders) == 0 {
		t.Fatalf("root folders = %s, %v", rec.Body.String(), err)
	}
	if !folders[0].Accessible || folders[0].Path == "" {
		t.Fatalf("folder = %+v", folders[0])
	}
	rec = httptest.NewRecorder()
	V3SystemStatus(rec, httptest.NewRequest("GET", "/api/v3/system/status", nil), state)
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"appName":"Gextto"`)) {
		t.Fatalf("status = %s", rec.Body.String())
	}
}

func TestV3AddSeriesUsesTheProfile(t *testing.T) {
	state := v3TestState(t)
	body := `{"title":"New Show","tvdbId":123,"rootFolderPath":"` + state.cfg.DataDir + `/library","qualityProfileId":1,"monitored":true}`
	rec := httptest.NewRecorder()
	V3SeriesAdd(rec, httptest.NewRequest("POST", "/api/v3/series", bytes.NewBufferString(body)), state)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add -> %d %s", rec.Code, rec.Body.String())
	}
	TouchConfigGeneration()
	cfg := latestConfig(state)
	found := false
	for _, series := range cfg.Series {
		if series.Name == "New Show" {
			found = true
			if !series.Enabled || series.Seasons != "1+" {
				t.Fatalf("series = %+v", series)
			}
		}
	}
	if !found {
		t.Fatal("the series was not saved")
	}
	// A duplicate is refused.
	rec = httptest.NewRecorder()
	V3SeriesAdd(rec, httptest.NewRequest("POST", "/api/v3/series", bytes.NewBufferString(body)), state)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate -> %d %s", rec.Code, rec.Body.String())
	}
}

func TestV3AddMovie(t *testing.T) {
	state := v3TestState(t)
	body := `{"title":"New Film","tmdbId":456,"year":2024,"monitored":true}`
	rec := httptest.NewRecorder()
	V3MovieAdd(rec, httptest.NewRequest("POST", "/api/v3/movie", bytes.NewBufferString(body)), state)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add -> %d %s", rec.Code, rec.Body.String())
	}
	TouchConfigGeneration()
	found := false
	for _, movie := range latestConfig(state).Movies {
		if movie.Name == "New Film" && movie.Year == "2024" {
			found = true
		}
	}
	if !found {
		t.Fatal("the movie was not saved")
	}
}

func TestQualityProfilesSaveAndFeedTheV3API(t *testing.T) {
	state := v3TestState(t)
	body := `{"items":[{"name":"Film 4K","quality":"2160p","language":"ita"},{"name":"Serie 1080","quality":"1080p","language":"eng"}]}`
	rec := httptest.NewRecorder()
	SaveQualityProfiles(rec, httptest.NewRequest("POST", "/api/quality-profiles", bytes.NewBufferString(body)), state)
	if rec.Code != 200 {
		t.Fatalf("save -> %d %s", rec.Code, rec.Body.String())
	}
	profiles := ensureQualityProfiles(state)
	names := map[string]bool{}
	for _, profile := range profiles {
		names[profile.Name] = true
	}
	// The saved list is the whole list: a profile the operator removed (here
	// the generated Default) does not come back.
	if !names["Film 4K"] || !names["Serie 1080"] || names["Default"] {
		t.Fatalf("profiles = %+v", profiles)
	}
}

func TestQualityProfileIDsSurviveARemoval(t *testing.T) {
	state := v3TestState(t)
	save := func(body string) []QualityProfile {
		t.Helper()
		rec := httptest.NewRecorder()
		SaveQualityProfiles(rec, httptest.NewRequest("POST", "/api/quality-profiles", bytes.NewBufferString(body)), state)
		if rec.Code != 200 {
			t.Fatalf("save -> %d %s", rec.Code, rec.Body.String())
		}
		return ensureQualityProfiles(state)
	}
	first := save(`[{"name":"A","quality":"720p"},{"name":"B","quality":"1080p"},{"name":"C","quality":"2160p"}]`)
	ids := map[string]int{}
	for _, profile := range first {
		ids[profile.Name] = profile.ID
	}
	// The page sends rows without ids: removing A must not shift B and C.
	second := save(`[{"name":"B","quality":"1080p"},{"name":"C","quality":"2160p"},{"name":"D"}]`)
	for _, profile := range second {
		if want, ok := ids[profile.Name]; ok && profile.ID != want {
			t.Fatalf("%s changed id %d -> %d", profile.Name, want, profile.ID)
		}
		if profile.Name == "D" && profile.ID <= ids["C"] {
			t.Fatalf("a new profile reused an id: %+v", second)
		}
	}
	// Adding with the id of C applies C, not the profile now in position 2.
	body := `{"title":"Profiled","tvdbId":77,"qualityProfileId":` + strconv.Itoa(ids["C"]) + `}`
	rec := httptest.NewRecorder()
	V3SeriesAdd(rec, httptest.NewRequest("POST", "/api/v3/series", bytes.NewBufferString(body)), state)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add -> %d %s", rec.Code, rec.Body.String())
	}
	for _, series := range latestConfig(state).Series {
		if series.Name == "Profiled" && series.Quality != "2160p" {
			t.Fatalf("quality = %q, want the 2160p of profile C", series.Quality)
		}
	}
}

// Jellyseerr looks a series up as "tvdb:<id>", adds it with every season
// listed and only the requested ones monitored, and updates it with PUT when
// the lookup returns an id.
func TestV3SeriesFollowsTheJellyseerrFlow(t *testing.T) {
	state := v3TestState(t)
	lookup := func() []v3Series {
		t.Helper()
		rec := httptest.NewRecorder()
		V3SeriesLookup(rec, httptest.NewRequest("GET", "/api/v3/series/lookup?term=tvdb:4242", nil), state)
		var out []v3Series
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 1 {
			t.Fatalf("lookup = %s, %v", rec.Body.String(), err)
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"seasons":[`)) {
			t.Fatalf("lookup has no seasons array: %s", rec.Body.String())
		}
		return out
	}
	if got := lookup(); got[0].ID != 0 {
		t.Fatalf("an unknown series must have no id: %+v", got[0])
	}
	body := `{"title":"Requested","tvdbId":4242,"qualityProfileId":1,"monitored":true,
		"seasons":[{"seasonNumber":1,"monitored":false},{"seasonNumber":2,"monitored":true},{"seasonNumber":3,"monitored":false}]}`
	rec := httptest.NewRecorder()
	V3SeriesAdd(rec, httptest.NewRequest("POST", "/api/v3/series", bytes.NewBufferString(body)), state)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add -> %d %s", rec.Code, rec.Body.String())
	}
	series := v3FindSeries(t, state, "Requested")
	if series.Seasons != "2" {
		t.Fatalf("seasons = %q, want only the requested season 2", series.Seasons)
	}
	if got := lookup(); got[0].ID == 0 || got[0].Title != "Requested" {
		t.Fatalf("a configured series must come back with its id: %+v", got[0])
	}
	body = `{"title":"Requested","tvdbId":4242,"monitored":true,"seasons":[{"seasonNumber":2,"monitored":true},{"seasonNumber":3,"monitored":true}]}`
	rec = httptest.NewRecorder()
	V3SeriesUpdate(rec, httptest.NewRequest("PUT", "/api/v3/series", bytes.NewBufferString(body)), state)
	if rec.Code != http.StatusAccepted || !bytes.Contains(rec.Body.Bytes(), []byte(`"id":`)) {
		t.Fatalf("update -> %d %s", rec.Code, rec.Body.String())
	}
	if series := v3FindSeries(t, state, "Requested"); series.Seasons != "2,3" {
		t.Fatalf("seasons after update = %q, want 2,3", series.Seasons)
	}
}

func v3FindSeries(t *testing.T, state *AppState, name string) SeriesConfig {
	t.Helper()
	for _, series := range latestConfig(state).Series {
		if series.Name == name {
			return series
		}
	}
	t.Fatalf("series %q not saved", name)
	return SeriesConfig{}
}

func TestV3MovieAddReturnsTheLibraryID(t *testing.T) {
	state := v3TestState(t)
	body := `{"title":"With Id","tmdbId":909,"year":2023,"monitored":true}`
	rec := httptest.NewRecorder()
	V3MovieAdd(rec, httptest.NewRequest("POST", "/api/v3/movie", bytes.NewBufferString(body)), state)
	var added v3Movie
	if err := json.Unmarshal(rec.Body.Bytes(), &added); err != nil || rec.Code != http.StatusCreated || added.ID <= 0 {
		t.Fatalf("add -> %d %s (Jellyseerr needs a non-zero id)", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	V3MovieLookup(rec, httptest.NewRequest("GET", "/api/v3/movie/lookup?term=tmdb:909", nil), state)
	var found []v3Movie
	if err := json.Unmarshal(rec.Body.Bytes(), &found); err != nil || len(found) != 1 || found[0].ID != added.ID || !found[0].Monitored {
		t.Fatalf("lookup = %s", rec.Body.String())
	}
	body = `{"id":` + strconv.Itoa(added.ID) + `,"tmdbId":909,"monitored":false}`
	rec = httptest.NewRecorder()
	V3MovieUpdate(rec, httptest.NewRequest("PUT", "/api/v3/movie", bytes.NewBufferString(body)), state)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("update -> %d %s", rec.Code, rec.Body.String())
	}
	for _, movie := range latestConfig(state).Movies {
		if movie.Name == "With Id" && movie.Enabled {
			t.Fatal("the update must apply monitored=false")
		}
	}
}

func TestV3RoutesUseTheJellyseerrSpelling(t *testing.T) {
	state := newTestAppState(t)
	router := Router(state)
	for _, path := range []string{"/api/v3/qualityProfile", "/api/v3/qualityprofile"} {
		req := httptest.NewRequest("GET", path, nil)
		if _, pattern := router.Handler(req); pattern == "" {
			t.Fatalf("%s is not routed", path)
		}
	}
}

func TestV3ProfilesIgnoreTitleExclusionsAndAny(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{qualityProfilesKey: `[{"id":1,"name":"Default","language":"ita"}]`}
	cfg.Series = []SeriesConfig{{Name: "A", Quality: "any", Language: "ita"}}
	cfg.Movies = []MovieConfig{
		{Name: "Hope", Quality: "720p+", Language: "ita", Exclude: "without"},
		{Name: "Other", Quality: "720p+", Language: "ita"},
	}
	profiles := v3Profiles(&cfg)
	// Default, plus one 720p+ · ita: "any" is the Default and a title's own
	// exclusion is not a profile.
	if len(profiles) != 2 || profiles[1].Quality != "720p+" || profiles[1].Exclude != "" {
		t.Fatalf("profiles = %+v", profiles)
	}
}
