package gextto

// uiweb_v2_season_toggle_test.go drives the season chips of the series page
// through the same /section/action round trip the browser uses. A chip carries
// its own JSON body: a wrong key is silently dropped by the API, so the click
// answers "done" while the season stays exactly as it was.

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var v2HiddenInputRE = regexp.MustCompile(`<input type="hidden" name="([a-z]+)" value=(?:"([^"]*)"|'([^']*)')`)

// v2SeasonChipForm returns the hidden fields of one season chip as the page
// sends them, so the test cannot drift from the template.
func v2SeasonChipForm(t *testing.T, page string, season int64) url.Values {
	t.Helper()
	button := ">S" + strconv.FormatInt(season, 10) + "</button>"
	for _, chunk := range strings.Split(page, "<form") {
		form := strings.SplitN(chunk, "</form>", 2)[0]
		if !strings.Contains(form, "/toggle-season") || !strings.Contains(form, button) {
			continue
		}
		fields := url.Values{}
		for _, match := range v2HiddenInputRE.FindAllStringSubmatch(form, -1) {
			value := match[2]
			if value == "" && match[3] != "" {
				value = match[3]
			}
			fields.Set(match[1], html.UnescapeString(value))
		}
		return fields
	}
	t.Fatalf("no toggle form for season %d in the page", season)
	return nil
}

// TestV2SeriesSeasonChipTogglesBothWays checks that clicking S2 while it is
// disabled switches it back on as well as clicking it while enabled switches it
// off. The first direction is the one that used to be broken: the chip sent an
// "ignored" key the API never read.
func TestV2SeriesSeasonChipTogglesBothWays(t *testing.T) {
	state := newTestAppState(t)
	if err := SaveLibrary(state.cfg.DataDir, []SeriesConfig{{
		Name: "Show", Seasons: "1+", Quality: "1080p", Language: "ita",
		Enabled: true, IgnoredSeasons: []int64{2},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	// Keep season 2 in the chip row after it has been enabled: without any
	// metadata the row only lists ignored seasons.
	if _, err := state.db.db.Exec("INSERT INTO series_metadata(series_name,season,episode_count,updated_at) VALUES ('Show',2,4,datetime('now'))"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	clickSeason := func(want string) {
		t.Helper()
		code, page := v2Request(t, server, http.MethodGet, "/?view=series&series=Show", nil)
		if code != http.StatusOK {
			t.Fatalf("series page -> %d", code)
		}
		fields := v2SeasonChipForm(t, page, 2)
		if !strings.Contains(fields.Get("body"), `"enabled":`+want) || strings.Contains(fields.Get("body"), `"ignored"`) {
			t.Fatalf("the season chip carries the wrong body: %q", fields.Get("body"))
		}
		if code, _ := v2Request(t, server, http.MethodPost, "/section/action", fields); code != http.StatusNoContent {
			t.Fatalf("season action -> %d", code)
		}
	}

	clickSeason("true")
	if got := latestConfig(state).Series[0].IgnoredSeasons; len(got) != 0 {
		t.Fatalf("clicking the disabled season did not enable it: %v", got)
	}

	clickSeason("false")
	if got := latestConfig(state).Series[0].IgnoredSeasons; !reflect.DeepEqual(got, []int64{2}) {
		t.Fatalf("clicking the enabled season did not disable it: %v", got)
	}
}
