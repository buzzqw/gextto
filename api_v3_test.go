package gextto

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	cfg := latestConfig(state)
	profiles := v3Profiles(cfg)
	names := map[string]bool{}
	for _, profile := range profiles {
		names[profile.Name] = true
	}
	if !names["Film 4K"] || !names["Serie 1080"] || !names["Default"] {
		t.Fatalf("profiles = %+v", profiles)
	}
}
