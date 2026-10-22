package gextto

// library_bulk.go is the bulk editor of the library: the operator ticks
// rows of the Series or Movies table and sets the same quality, language, subtitles,
// exclusions, upgrade policy or state on all of them at once. Only the fields
// the operator marks are written; every other value of each title is kept. A
// quality profile can fill the form as a template: the values are copied into
// the titles, which stay free to diverge afterwards (profiles are templates,
// not rules).

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// LibraryBulkEdit is one bulk change. A nil pointer leaves the field as it is;
// a pointer to "" clears it.
type LibraryBulkEdit struct {
	Series   []string
	Movies   []int64
	Quality  *string
	Audio    *string
	Language *string
	Subtitle *string
	Exclude  *string
	Upgrades *bool
	Enabled  *bool
}

func (e LibraryBulkEdit) changesSomething() bool {
	return e.Quality != nil || e.Audio != nil || e.Language != nil || e.Subtitle != nil || e.Exclude != nil ||
		e.Upgrades != nil || e.Enabled != nil
}

// ApplyLibraryBulkEdit returns copies of series and movies with the edit
// applied to the selected titles, and how many titles it touched. The input
// slices are never modified: they may be the shared cached configuration.
func ApplyLibraryBulkEdit(series []SeriesConfig, movies []MovieConfig, edit LibraryBulkEdit) ([]SeriesConfig, []MovieConfig, int, error) {
	if len(edit.Series) == 0 && len(edit.Movies) == 0 {
		return nil, nil, 0, errors.New("select at least one series or movie")
	}
	if !edit.changesSomething() {
		return nil, nil, 0, errors.New("choose at least one value to change")
	}
	set := func(target *string, value *string) {
		if value != nil {
			*target = strings.TrimSpace(*value)
		}
	}
	pickedSeries := map[string]bool{}
	for _, name := range edit.Series {
		pickedSeries[strings.ToLower(strings.TrimSpace(name))] = true
	}
	pickedMovies := map[int64]bool{}
	for _, id := range edit.Movies {
		pickedMovies[id] = true
	}
	changed := 0
	outSeries := append([]SeriesConfig{}, series...)
	for i := range outSeries {
		item := &outSeries[i]
		if !pickedSeries[strings.ToLower(strings.TrimSpace(item.Name))] {
			continue
		}
		set(&item.Quality, edit.Quality)
		if edit.Audio != nil {
			item.Audio, item.AudioChannels = normalizeAudioRequirement(*edit.Audio), ""
		}
		set(&item.Language, edit.Language)
		set(&item.Subtitle, edit.Subtitle)
		set(&item.Exclude, edit.Exclude)
		if edit.Upgrades != nil {
			item.DisableUpgrades = !*edit.Upgrades
		}
		if edit.Enabled != nil {
			item.Enabled = *edit.Enabled
		}
		changed++
	}
	outMovies := append([]MovieConfig{}, movies...)
	for i := range outMovies {
		item := &outMovies[i]
		if !pickedMovies[item.ID] {
			continue
		}
		set(&item.Quality, edit.Quality)
		if edit.Audio != nil {
			item.Audio, item.AudioChannels = normalizeAudioRequirement(*edit.Audio), ""
		}
		set(&item.Language, edit.Language)
		set(&item.Subtitle, edit.Subtitle)
		set(&item.Exclude, edit.Exclude)
		if edit.Upgrades != nil {
			item.DisableUpgrades = !*edit.Upgrades
		}
		if edit.Enabled != nil {
			item.Enabled = *edit.Enabled
		}
		changed++
	}
	if changed == 0 {
		return nil, nil, 0, errors.New("the selected titles are no longer in the library")
	}
	return outSeries, outMovies, changed, nil
}

// v2BulkPanel is the "Edit selected" panel above the Series and Movies
// tables: the rows are ticked in the table, the panel holds the values.
type v2BulkPanel struct {
	View     string
	Profiles []QualityProfile
	Values   QualityProfile
	Marked   bool
	Message  string
	Error    string
}

// v2BulkPanelFor builds the panel of a library table, or nil for a table
// whose rows cannot be selected.
func v2BulkPanelFor(s *AppState, view string, spec uiTableSpec) *v2BulkPanel {
	if spec.SelectName == "" {
		return nil
	}
	return &v2BulkPanel{View: view, Profiles: ensureQualityProfiles(s)}
}

// V2LibraryBulkProfile is GET /library/bulk/profile?id=…: the value fields
// filled from a quality profile, each marked to be applied.
func V2LibraryBulkProfile(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2BulkPanel{View: strings.TrimSpace(r.FormValue("view"))}
	if id, err := strconv.Atoi(strings.TrimSpace(r.FormValue("profile"))); err == nil && id > 0 {
		view.Values = qualityProfileByID(ensureQualityProfiles(s), id)
		view.Marked = true
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_bulk_values", view, dict, eng)
}

// V2LibraryBulkApply is POST /library/bulk: it applies the marked values to
// the ticked titles and reports how many changed.
func V2LibraryBulkApply(w http.ResponseWriter, r *http.Request, s *AppState) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	edit := LibraryBulkEdit{Series: r.Form["series"]}
	for _, raw := range r.Form["movie"] {
		if id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
			edit.Movies = append(edit.Movies, id)
		}
	}
	text := func(key string) *string {
		if r.FormValue("set_"+key) == "" {
			return nil
		}
		value := r.FormValue(key)
		return &value
	}
	choice := func(key, yes, no string) *bool {
		switch r.FormValue(key) {
		case yes:
			value := true
			return &value
		case no:
			value := false
			return &value
		}
		return nil
	}
	edit.Quality, edit.Audio, edit.Language = text("quality"), text("audio"), text("language")
	edit.Subtitle, edit.Exclude = text("subtitle"), text("exclude")
	edit.Upgrades = choice("upgrades", "allow", "deny")
	edit.Enabled = choice("state", "active", "paused")
	view := v2BulkPanel{}
	cfg := latestConfig(s)
	series, movies, changed, err := ApplyLibraryBulkEdit(cfg.Series, cfg.Movies, edit)
	if err == nil {
		err = SaveLibrary(cfg.DataDir, series, movies)
	}
	if err != nil {
		view.Error = err.Error()
	} else {
		view.Message = strconv.Itoa(changed)
		// The table shows the old values: reload the page so the edited
		// columns are visible at once.
		w.Header().Set("HX-Refresh", "true")
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_bulk_done", view, dict, eng)
}
