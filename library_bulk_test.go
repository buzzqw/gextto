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
