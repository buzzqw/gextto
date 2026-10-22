package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestLibraryBulkEditChangesOnlyTheMarkedFields(t *testing.T) {
	series := []SeriesConfig{
		{Name: "A", Quality: "720p", Language: "ita", Exclude: "CAM", Enabled: true},
		{Name: "B", Quality: "720p", Language: "eng", Enabled: true},
		{Name: "C", Quality: "720p", Language: "ita", Enabled: true},
	}
	movies := []MovieConfig{{ID: 7, Name: "Film", Quality: "720p", Language: "ita", Enabled: true}}
	quality, exclude, upgrades, enabled := "1080p", "", false, false
	edit := LibraryBulkEdit{Series: []string{"a", "B"}, Movies: []int64{7},
		Quality: &quality, Exclude: &exclude, Upgrades: &upgrades, Enabled: &enabled}
	outSeries, outMovies, changed, err := ApplyLibraryBulkEdit(series, movies, edit)
	if err != nil || changed != 3 {
		t.Fatalf("changed = %d, err = %v", changed, err)
	}
	for _, item := range outSeries[:2] {
		if item.Quality != "1080p" || item.Exclude != "" || !item.DisableUpgrades || item.Enabled {
			t.Fatalf("series not edited: %+v", item)
		}
	}
	// The language was not marked: each title keeps its own.
	if outSeries[0].Language != "ita" || outSeries[1].Language != "eng" {
		t.Fatalf("an unmarked field changed: %+v", outSeries)
	}
	if outSeries[2].Quality != "720p" || !outSeries[2].Enabled {
		t.Fatalf("an unselected series changed: %+v", outSeries[2])
	}
	if outMovies[0].Quality != "1080p" || outMovies[0].Enabled {
		t.Fatalf("movie not edited: %+v", outMovies[0])
	}
	// The input is the shared cached configuration: it must stay untouched.
	if series[0].Quality != "720p" || movies[0].Quality != "720p" {
		t.Fatal("the input slices were modified")
	}
	if _, _, _, err := ApplyLibraryBulkEdit(series, movies, LibraryBulkEdit{Series: []string{"A"}}); err == nil {
		t.Fatal("an edit without values must be refused")
	}
	if _, _, _, err := ApplyLibraryBulkEdit(series, movies, LibraryBulkEdit{Quality: &quality}); err == nil {
		t.Fatal("an edit without titles must be refused")
	}
}

func TestLibraryTablesEditTheSelectedRows(t *testing.T) {
	state := v3TestState(t)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	var movieID int64
	for _, movie := range latestConfig(state).Movies {
		movieID = movie.ID
	}
	for view, want := range map[string]string{
		"series": `name="series" value="Show" form="v2-bulk-form-series`,
		"movies": `name="movie" value="` + strconv.FormatInt(movieID, 10) + `" form="v2-bulk-form-movies`,
	} {
		code, body := v2Request(t, server, http.MethodGet, "/?view="+view+"&layout=list", nil)
		if code != 200 || !strings.Contains(body, "Modifica selezionati") || !strings.Contains(body, want) {
			t.Fatalf("%s table without row selection (%d): want %s", view, code, want)
		}
		// The checkboxes, the toggle and the panel must name the same form and
		// panel ids, or ticking a row would submit nothing.
		form := regexp.MustCompile(`form="(v2-bulk-form-[^"]+)"`).FindStringSubmatch(body)
		toggle := regexp.MustCompile(`data-v2-bulk-toggle="([^"]+)"`).FindStringSubmatch(body)
		if form == nil || toggle == nil || !strings.Contains(body, `id="`+form[1]+`"`) || !strings.Contains(body, `id="`+toggle[1]+`"`) {
			t.Fatalf("%s: row checkboxes or toggle point to a missing form/panel (%v %v)", view, form, toggle)
		}
	}
	profiles := ensureQualityProfiles(state)
	var picked QualityProfile
	for _, profile := range profiles {
		if profile.Quality == "2160p" {
			picked = profile
		}
	}
	if picked.ID == 0 {
		t.Fatalf("no 2160p profile in %+v", profiles)
	}
	rec := httptest.NewRecorder()
	V2LibraryBulkProfile(rec, httptest.NewRequest("GET", "/library/bulk/profile?profile="+strconv.Itoa(picked.ID), nil), state)
	if body := rec.Body.String(); !strings.Contains(body, `value="2160p"`) || !strings.Contains(body, "checked") {
		t.Fatalf("profile values = %s", body)
	}
	form := url.Values{"series": {"Show"}, "movie": {strconv.FormatInt(movieID, 10)},
		"set_quality": {"1"}, "quality": {"2160p"}, "language": {"ignored"}, "state": {"paused"}}
	req := httptest.NewRequest("POST", "/library/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	V2LibraryBulkApply(rec, req, state)
	if !strings.Contains(rec.Body.String(), "role=\"status\"") {
		t.Fatalf("apply = %s", rec.Body.String())
	}
	cfg := latestConfig(state)
	for _, series := range cfg.Series {
		switch series.Name {
		case "Show":
			if series.Quality != "2160p" || series.Language != "ita" || series.Enabled {
				t.Fatalf("Show = %+v", series)
			}
		case "Other":
			if series.Quality != "2160p" || !series.Enabled {
				// Other already had 2160p; it must keep it and stay active.
				t.Fatalf("Other changed: %+v", series)
			}
		}
	}
	if cfg.Movies[0].Quality != "2160p" || cfg.Movies[0].Enabled {
		t.Fatalf("movie = %+v", cfg.Movies[0])
	}
}

func TestLibraryBulkEditSetsTheMinimumAudio(t *testing.T) {
	series := []SeriesConfig{{Name: "A", Audio: "standard", AudioChannels: "5.1", Enabled: true}}
	audio := "high-7.1"
	outSeries, _, changed, err := ApplyLibraryBulkEdit(series, nil, LibraryBulkEdit{Series: []string{"A"}, Audio: &audio})
	if err != nil || changed != 1 {
		t.Fatalf("changed = %d, err = %v", changed, err)
	}
	if outSeries[0].Audio != "high-7.1" || outSeries[0].AudioChannels != "" {
		t.Fatalf("audio = %q/%q, want the high-7.1 preset", outSeries[0].Audio, outSeries[0].AudioChannels)
	}
}

func TestBulkProfileFillsAudioAndUpgrades(t *testing.T) {
	state := v3TestState(t)
	body := `{"items":[{"name":"Archivio","quality":"1080p","audio":"lossless-5.1","language":"ita","disable_upgrades":true}]}`
	rec := httptest.NewRecorder()
	SaveQualityProfiles(rec, httptest.NewRequest("POST", "/api/quality-profiles", strings.NewReader(body)), state)
	if rec.Code != 200 {
		t.Fatalf("save -> %d %s", rec.Code, rec.Body.String())
	}
	profiles := ensureQualityProfiles(state)
	if len(profiles) != 1 || profiles[0].Audio != "lossless-5.1" || !profiles[0].DisableUpgrades {
		t.Fatalf("profiles = %+v", profiles)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	code, page := v2Request(t, server, http.MethodGet, "/library/bulk/profile?view=series&profile="+strconv.Itoa(profiles[0].ID), nil)
	if code != http.StatusOK || !strings.Contains(page, `<option value="lossless-5.1" selected>`) || !strings.Contains(page, `<option value="deny" selected>`) {
		t.Fatalf("bulk profile values -> %d: %s", code, page)
	}
}

func TestQualityProfileUpgradesChoiceMapsToDisableUpgrades(t *testing.T) {
	state := v3TestState(t)
	body := `{"items":[{"name":"Fermo","upgrades":"deny"},{"name":"Libero","upgrades":"allow","disable_upgrades":true}]}`
	rec := httptest.NewRecorder()
	SaveQualityProfiles(rec, httptest.NewRequest("POST", "/api/quality-profiles", strings.NewReader(body)), state)
	if rec.Code != 200 {
		t.Fatalf("save -> %d %s", rec.Code, rec.Body.String())
	}
	profiles := ensureQualityProfiles(state)
	if len(profiles) != 2 || !profiles[0].DisableUpgrades || profiles[0].Upgrades != "deny" || profiles[1].DisableUpgrades || profiles[1].Upgrades != "allow" {
		t.Fatalf("profiles = %+v", profiles)
	}
}

// A movie's hard requirements override the language and the subtitles in the
// release check: changing one of them in bulk must carry the requirement with
// it, otherwise the title keeps demanding the old one and nothing matches.
func TestLibraryBulkEditCarriesTheMovieRequirements(t *testing.T) {
	movies := []MovieConfig{{
		ID: 1, Name: "Film", Enabled: true,
		Language: "eng", LanguageRequirements: `[{"language":"eng","required":true}]`,
		Subtitle: "eng", SubtitleRequirements: "eng",
	}}
	language, subtitle := "ita", "ita"
	_, out, changed, err := ApplyLibraryBulkEdit(nil, movies,
		LibraryBulkEdit{Movies: []int64{1}, Language: &language, Subtitle: &subtitle})
	if err != nil || changed != 1 {
		t.Fatalf("changed = %d, err = %v", changed, err)
	}
	if out[0].Language != "ita" || out[0].Subtitle != "ita" {
		t.Fatalf("movie = %+v", out[0])
	}
	if required := parseLanguageRequirements(out[0].LanguageRequirements); len(required) != 1 || required[0] != "ita" {
		t.Fatalf("language requirement = %q, want ita", out[0].LanguageRequirements)
	}
	if out[0].SubtitleRequirements != "ita" {
		t.Fatalf("subtitle requirement = %q, want ita", out[0].SubtitleRequirements)
	}
	// A title without the requirement does not gain one.
	plain := []MovieConfig{{ID: 2, Name: "Altro", Subtitle: "eng", Enabled: true}}
	_, out, _, err = ApplyLibraryBulkEdit(nil, plain, LibraryBulkEdit{Movies: []int64{2}, Subtitle: &subtitle})
	if err != nil || out[0].Subtitle != "ita" || out[0].SubtitleRequirements != "" {
		t.Fatalf("plain movie = %+v, err = %v", out[0], err)
	}
}

// A profile fills the values but leaves the exclusions unticked: they are each
// title's own, and the title page adds the profile's to them instead of
// replacing them. The panel says so, and a profile that no longer exists does
// not fill the fields from another one.
func TestBulkProfileLeavesTheExclusionsUnticked(t *testing.T) {
	state := v3TestState(t)
	body := `{"items":[{"id":9,"name":"Senza cam","quality":"1080p","language":"ita","exclude":"cam"}]}`
	rec := httptest.NewRecorder()
	SaveQualityProfiles(rec, httptest.NewRequest("POST", "/api/quality-profiles", strings.NewReader(body)), state)
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, page := v2Request(t, server, http.MethodGet, "/library/bulk/profile?view=series&profile=9", nil)
	if code != http.StatusOK {
		t.Fatalf("bulk profile -> %d %s", code, page)
	}
	if !strings.Contains(page, `<input type="checkbox" name="set_quality" value="1" checked>`) {
		t.Fatalf("the other fields must be marked: %s", page)
	}
	if strings.Contains(page, `<input type="checkbox" name="set_exclude" value="1" checked>`) {
		t.Fatalf("the exclusions must not be marked by a profile: %s", page)
	}
	if !strings.Contains(page, "Da un profilo le esclusioni non vengono applicate") {
		t.Fatalf("the panel must explain the exclusions: %s", page)
	}

	code, page = v2Request(t, server, http.MethodGet, "/library/bulk/profile?view=series&profile=99", nil)
	if code != http.StatusOK || !strings.Contains(page, "Profilo di qualità non trovato.") {
		t.Fatalf("an unknown profile must be reported: %d %s", code, page)
	}
	if strings.Contains(page, `name="set_quality" value="1" checked`) {
		t.Fatalf("an unknown profile must not fill the fields: %s", page)
	}
}
