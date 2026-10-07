package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SeriesView is the open series detail: one season at a time, with its
// episodes as a selectable list.
type SeriesView struct {
	Name     string
	Data     *SeriesDetail
	Season   int64
	Selected int
	Scroll   int
	// Focus, when set, selects this episode once the detail arrives (used
	// when the view is opened from the Missing tab).
	Focus [2]int64
}

// MovieView is the open movie detail; the archive matches are selectable.
type MovieView struct {
	ID       int64
	Data     *MovieDetail
	Selected int
	Scroll   int
}

// FormKind identifies what a form saves.
type FormKind int

const (
	FormSeriesEdit FormKind = iota
	FormMovieEdit
	FormSeriesAdd
	FormMovieAdd
)

// FormField is one editable line of a form. A Bool field toggles with Enter.
type FormField struct {
	Key     string
	Label   string
	Value   string
	Initial string
	Bool    bool
	Hint    string
}

// Form is a small modal editor: ↑↓ choose a field, Enter edits it on the
// prompt line (or toggles a yes/no field), s saves, Esc discards.
type Form struct {
	Kind     FormKind
	Title    string
	Target   string
	Movie    MovieConfig
	Fields   []FormField
	Selected int
}

// SearchKind tells the search overlay what its results are.
type SearchKind int

const (
	SearchReleases SearchKind = iota
	SearchTmdbSeries
	SearchTmdbMovies
)

// SetLibraryData replaces the monitored series and movies.
func (m *Model) SetLibraryData(series []SeriesLibraryItem, movies []MovieConfig) {
	selected := m.selectedLibraryKey()
	m.Series = append([]SeriesLibraryItem(nil), series...)
	m.Movies = append([]MovieConfig(nil), movies...)
	m.reselectLibrary(selected)
}

func (m *Model) selectedLibraryKey() string {
	rows := m.VisibleLibraryRows()
	if m.LibrarySelected >= 0 && m.LibrarySelected < len(rows) {
		return rows[m.LibrarySelected].Key
	}
	return ""
}

func (m *Model) reselectLibrary(key string) {
	rows := m.VisibleLibraryRows()
	if key != "" {
		for index, row := range rows {
			if row.Key == key {
				m.LibrarySelected = index
				return
			}
		}
	}
	m.LibrarySelected = min(m.LibrarySelected, max(0, len(rows)-1))
}

// SelectedSeries returns the highlighted series of the library list.
func (m *Model) SelectedSeries() *SeriesLibraryItem {
	rows := m.VisibleLibraryRows()
	if m.Library != LibrarySeries || m.LibrarySelected < 0 || m.LibrarySelected >= len(rows) {
		return nil
	}
	for index := range m.Series {
		if m.Series[index].Name == rows[m.LibrarySelected].Key {
			item := m.Series[index]
			return &item
		}
	}
	return nil
}

// SelectedMovie returns the highlighted movie of the library list.
func (m *Model) SelectedMovie() *MovieConfig {
	rows := m.VisibleLibraryRows()
	if m.Library != LibraryMovies || m.LibrarySelected < 0 || m.LibrarySelected >= len(rows) {
		return nil
	}
	return m.movieByKey(rows[m.LibrarySelected].Key)
}

func (m *Model) movieByKey(key string) *MovieConfig {
	for index := range m.Movies {
		if movieKey(m.Movies[index].ID) == key {
			item := m.Movies[index]
			return &item
		}
	}
	return nil
}

func (m *Model) movieByID(id int64) *MovieConfig { return m.movieByKey(movieKey(id)) }

func movieKey(id int64) string { return "movie:" + strconv.FormatInt(id, 10) }

// OpenSeries opens the detail of a series; the runner loads its episodes.
func (m *Model) OpenSeries(name string) Action {
	m.Tab = TabLibrary
	m.Library = LibrarySeries
	m.MovieView = nil
	m.SeriesView = &SeriesView{Name: name}
	return Action{Kind: ActionLoadSeries, Text: name}
}

// OpenMovie opens the detail of a movie; the runner loads it.
func (m *Model) OpenMovie(id int64) Action {
	m.SeriesView = nil
	m.MovieView = &MovieView{ID: id}
	return Action{Kind: ActionLoadMovie, ID: id}
}

// SetSeriesDetail stores the loaded series detail, keeping the season and
// episode the user is on when it is a reload.
func (m *Model) SetSeriesDetail(detail SeriesDetail) {
	view := m.SeriesView
	if view == nil || view.Name != detail.Series.Name && !containsString(detail.Series.Aliases, view.Name) {
		return
	}
	reload := view.Data != nil
	previous := view.selectedEpisode()
	view.Name = detail.Series.Name
	view.Data = &detail
	seasons := view.Seasons()
	switch {
	case view.Focus != [2]int64{}:
		view.Season = view.Focus[0]
	case !reload || !containsInt64(seasons, view.Season):
		view.Season = view.firstSeason()
	}
	if m.PendingEdit {
		m.PendingEdit = false
		m.Form = seriesEditForm(m.Tr, detail.Series)
	}
	episodes := view.SeasonEpisodes()
	target := previous
	if view.Focus != [2]int64{} {
		target = &Episode{Season: view.Focus[0], Episode: view.Focus[1]}
		view.Focus = [2]int64{}
	}
	if target != nil {
		for index, episode := range episodes {
			if episode.Season == target.Season && episode.Episode == target.Episode {
				view.Selected = index
				return
			}
		}
	}
	if !reload {
		view.Selected = 0
	}
	view.Selected = min(max(view.Selected, 0), max(0, len(episodes)-1))
}

// SetMovieDetail stores the loaded movie detail.
func (m *Model) SetMovieDetail(detail MovieDetail) {
	if m.MovieView == nil || m.MovieView.ID != detail.Movie.ID {
		return
	}
	m.MovieView.Data = &detail
	m.MovieView.Selected = min(m.MovieView.Selected, max(0, len(detail.Matches)-1))
}

// Seasons lists every known season: expected by TMDB, with episodes, or
// switched off.
func (v *SeriesView) Seasons() []int64 {
	if v.Data == nil {
		return nil
	}
	set := map[int64]bool{}
	for _, item := range v.Data.Metadata {
		set[item.Season] = true
	}
	for _, episode := range v.Data.Episodes {
		set[episode.Season] = true
	}
	for _, season := range v.Data.Series.IgnoredSeasons {
		set[season] = true
	}
	seasons := make([]int64, 0, len(set))
	for season := range set {
		seasons = append(seasons, season)
	}
	sort.Slice(seasons, func(i, j int) bool { return seasons[i] < seasons[j] })
	return seasons
}

// firstSeason opens on the first monitored season with episodes, skipping
// specials (season 0) when there is anything else.
func (v *SeriesView) firstSeason() int64 {
	seasons := v.Seasons()
	for _, season := range seasons {
		if season > 0 && len(v.episodesOf(season)) > 0 {
			return season
		}
	}
	for _, season := range seasons {
		if season > 0 {
			return season
		}
	}
	if len(seasons) > 0 {
		return seasons[0]
	}
	return 1
}

func (v *SeriesView) episodesOf(season int64) []Episode {
	if v.Data == nil {
		return nil
	}
	episodes := []Episode{}
	for _, episode := range v.Data.Episodes {
		if episode.Season == season {
			episodes = append(episodes, episode)
		}
	}
	return episodes
}

// SeasonEpisodes returns the episodes of the selected season.
func (v *SeriesView) SeasonEpisodes() []Episode { return v.episodesOf(v.Season) }

// SeasonIgnored reports whether the season was switched off explicitly.
func (v *SeriesView) SeasonIgnored(season int64) bool {
	return v.Data != nil && containsInt64(v.Data.Series.IgnoredSeasons, season)
}

// SeasonExcluded reports a season TMDB knows about but the "seasons" field
// leaves out: the daemon returns no episodes for it.
func (v *SeriesView) SeasonExcluded(season int64) bool {
	if v.Data == nil || v.SeasonIgnored(season) || len(v.episodesOf(season)) > 0 {
		return false
	}
	for _, item := range v.Data.Metadata {
		if item.Season == season && item.Count > 0 {
			return true
		}
	}
	return false
}

func (v *SeriesView) selectedEpisode() *Episode {
	episodes := v.SeasonEpisodes()
	if v.Selected < 0 || v.Selected >= len(episodes) {
		return nil
	}
	episode := episodes[v.Selected]
	return &episode
}

// episodeDone reports an episode already in the library.
func episodeDone(episode Episode) bool {
	return episode.Status == "downloaded" || (episode.ArchivePath != nil && *episode.ArchivePath != "")
}

// episodeAired reports whether the episode has been broadcast (unknown dates
// count as aired, like the daemon's missing search does).
func episodeAired(episode Episode, now time.Time) bool {
	if len(episode.AirDate) < 10 {
		return true
	}
	date, err := time.Parse("2006-01-02", episode.AirDate[:10])
	return err != nil || !date.After(now)
}

// updateTabContext handles the keys of the library list, of the series and
// movie details and of the Missing tab before the global shortcuts, so that
// a/e/s mean "add", "edit", "search" there. It reports whether it consumed k.
func (m *Model) updateTabContext(k Key) (Action, bool) {
	if m.Tab == TabMissing && k.Kind == KeyRune && k.Rune == 's' {
		if m.MissingSelected >= 0 && m.MissingSelected < len(m.Missing) {
			gap := m.Missing[m.MissingSelected]
			return Action{Kind: ActionEpisodeSearch, Text: gap.Series, Season: gap.Season, Episode: gap.Episode}, true
		}
		return Action{}, true
	}
	if m.Tab != TabLibrary {
		return Action{}, false
	}
	if m.SeriesView != nil {
		return m.updateSeriesView(k)
	}
	if m.MovieView != nil {
		return m.updateMovieView(k)
	}
	if k.Kind != KeyRune || m.Library == LibraryComics {
		return Action{}, false
	}
	switch k.Rune {
	case 'a':
		kind := PromptTmdbSeries
		if m.Library == LibraryMovies {
			kind = PromptTmdbMovie
		}
		m.Prompt = newPrompt(kind, "")
		return Action{}, true
	case 'e':
		if series := m.SelectedSeries(); series != nil {
			// The list carries every editable field but tvdb_id and
			// disable_upgrades: load the detail and edit from there.
			action := m.OpenSeries(series.Name)
			m.SeriesView.Focus = [2]int64{}
			m.PendingEdit = true
			return action, true
		}
		if movie := m.SelectedMovie(); movie != nil {
			m.Form = movieEditForm(m.Tr, *movie)
			return Action{}, true
		}
		return Action{}, true
	}
	return Action{}, false
}

// updateLibrary handles the remaining library list keys.
func (m *Model) updateLibrary(k Key) Action {
	switch {
	// The digits always switch tab, as everywhere else: series, movies and
	// comics are chosen with ←→.
	case k.Kind == KeyLeft || k.Kind == KeyRight:
		step := 1
		if k.Kind == KeyLeft {
			step = 2
		}
		m.Library = LibraryKind((int(m.Library) + step) % 3)
		m.LibrarySelected, m.LibraryScroll = 0, 0
	case k.Kind == KeyRune && (k.Rune == 's' || k.Rune == '/'):
		m.Prompt = newPrompt(PromptSearch, m.LibraryFilter)
	case k.Kind == KeyUp, k.Kind == KeyDown, k.Kind == KeyPgUp, k.Kind == KeyPgDn, k.Kind == KeyHome, k.Kind == KeyEnd:
		m.LibrarySelected = moveSelection(m.LibrarySelected, len(m.VisibleLibraryRows()), k.Kind)
	case k.Kind == KeyEnter:
		if series := m.SelectedSeries(); series != nil {
			return m.OpenSeries(series.Name)
		}
		if movie := m.SelectedMovie(); movie != nil {
			return m.OpenMovie(movie.ID)
		}
	case k.Kind == KeyRune && k.Rune == 'p':
		if series := m.SelectedSeries(); series != nil {
			return Action{Kind: ActionSaveSeries, Text: series.Name, Fields: map[string]any{"enabled": !series.Enabled}}
		}
		if movie := m.SelectedMovie(); movie != nil {
			movie.Enabled = !movie.Enabled
			return Action{Kind: ActionSaveMovie, Movie: movie}
		}
	case k.Kind == KeyRune && k.Rune == 'd':
		if series := m.SelectedSeries(); series != nil {
			m.Confirm = &confirm{MessageKey: "prompt.seriesremove", Args: []any{Shorten(series.Name, 40)}, Action: Action{Kind: ActionDeleteSeries, Text: series.Name}}
		}
		if movie := m.SelectedMovie(); movie != nil {
			m.Confirm = &confirm{MessageKey: "prompt.movieremove", Args: []any{Shorten(movie.Name, 40)}, Action: Action{Kind: ActionDeleteMovie, ID: movie.ID}}
		}
	case k.Kind == KeyRune && k.Rune == 'm':
		if series := m.SelectedSeries(); series != nil {
			return Action{Kind: ActionSeriesSearchMissing, Text: series.Name}
		}
		if movie := m.SelectedMovie(); movie != nil {
			return Action{Kind: ActionMovieSearch, ID: movie.ID, Domain: movie.Name}
		}
	case k.Kind == KeyEsc:
		if m.LibraryFilter != "" {
			m.LibraryFilter = ""
			m.LibrarySelected, m.LibraryScroll = 0, 0
			return Action{}
		}
		m.Tab = TabStatus
		return m.loadTabAction()
	}
	return Action{}
}

func (m *Model) updateSeriesView(k Key) (Action, bool) {
	view := m.SeriesView
	episodes := view.SeasonEpisodes()
	episode := view.selectedEpisode()
	name := view.Name
	switch k.Kind {
	case KeyEsc, KeyBackspace:
		m.SeriesView = nil
		return Action{}, true
	case KeyUp, KeyDown, KeyPgUp, KeyPgDn, KeyHome, KeyEnd:
		view.Selected = moveSelection(view.Selected, len(episodes), k.Kind)
		return Action{}, true
	case KeyLeft, KeyRight:
		seasons := view.Seasons()
		for index, season := range seasons {
			if season != view.Season {
				continue
			}
			if k.Kind == KeyLeft && index > 0 {
				view.Season = seasons[index-1]
			} else if k.Kind == KeyRight && index < len(seasons)-1 {
				view.Season = seasons[index+1]
			}
			break
		}
		view.Selected, view.Scroll = 0, 0
		return Action{}, true
	case KeyEnter:
		if episode != nil {
			return Action{Kind: ActionEpisodeSources, Text: name, Season: episode.Season, Episode: episode.Episode}, true
		}
		return Action{}, true
	case KeyRune:
	default:
		return Action{}, false
	}
	if view.Data == nil && k.Rune != 'r' {
		// Still loading: only q/? (handled before) and r make sense.
		return Action{}, k.Rune != 'q' && k.Rune != '?'
	}
	switch k.Rune {
	case ' ':
		if view.SeasonExcluded(view.Season) {
			m.Message = m.Tr.Format("msg.seasonexcluded", view.Season)
			return Action{}, true
		}
		return Action{Kind: ActionToggleSeason, Text: name, Season: view.Season, Flag: view.SeasonIgnored(view.Season)}, true
	case 's':
		if episode != nil {
			return Action{Kind: ActionEpisodeSearch, Text: name, Season: episode.Season, Episode: episode.Episode}, true
		}
	case 'i':
		if episode != nil {
			return Action{Kind: ActionEpisodeIgnore, Text: name, Season: episode.Season, Episode: episode.Episode, Flag: !episode.Ignored}, true
		}
	case 'R':
		if episode != nil {
			m.Confirm = &confirm{MessageKey: "prompt.episoderedownload", Args: []any{episode.Season, episode.Episode}, Action: Action{Kind: ActionEpisodeRedownload, Text: name, Season: episode.Season, Episode: episode.Episode}}
		}
	case 'y':
		if episode != nil && episode.MagnetLink != nil && *episode.MagnetLink != "" {
			return Action{Kind: ActionCopy, Text: *episode.MagnetLink}, true
		}
	case 'm':
		return Action{Kind: ActionSeriesSearchMissing, Text: name}, true
	case 'M':
		return Action{Kind: ActionSeriesMetadata, Text: name}, true
	case 'n':
		return Action{Kind: ActionRenamePreview, Text: name}, true
	case 'e':
		m.Form = seriesEditForm(m.Tr, view.Data.Series)
	case 'p':
		return Action{Kind: ActionSaveSeries, Text: name, Fields: map[string]any{"enabled": !view.Data.Series.Enabled}}, true
	case 'd':
		m.Confirm = &confirm{MessageKey: "prompt.seriesremove", Args: []any{Shorten(name, 40)}, Action: Action{Kind: ActionDeleteSeries, Text: name}}
	case 'r':
		return Action{Kind: ActionLoadSeries, Text: name}, true
	default:
		// Digits would switch the library list behind the detail.
		return Action{}, k.Rune >= '0' && k.Rune <= '9'
	}
	return Action{}, true
}

func (m *Model) updateMovieView(k Key) (Action, bool) {
	view := m.MovieView
	var matches []ArchiveMatch
	if view.Data != nil {
		matches = view.Data.Matches
	}
	switch k.Kind {
	case KeyEsc, KeyBackspace:
		m.MovieView = nil
		return Action{}, true
	case KeyUp, KeyDown, KeyPgUp, KeyPgDn, KeyHome, KeyEnd:
		view.Selected = moveSelection(view.Selected, len(matches), k.Kind)
		return Action{}, true
	case KeyEnter:
		if view.Selected >= 0 && view.Selected < len(matches) && matches[view.Selected].Magnet != "" {
			return Action{Kind: ActionAddMagnet, Text: matches[view.Selected].Magnet}, true
		}
		return Action{}, true
	case KeyRune:
	default:
		return Action{}, false
	}
	if view.Data == nil && k.Rune != 'r' {
		return Action{}, k.Rune != 'q' && k.Rune != '?'
	}
	movie := MovieConfig{ID: view.ID}
	if view.Data != nil {
		movie = view.Data.Movie
	}
	switch k.Rune {
	case 's':
		return Action{Kind: ActionMovieSearch, ID: view.ID, Domain: movie.Name}, true
	case 'R':
		m.Confirm = &confirm{MessageKey: "prompt.movieredownload", Args: []any{Shorten(movie.Name, 40)}, Action: Action{Kind: ActionMovieRedownload, ID: view.ID}}
	case 'e':
		m.Form = movieEditForm(m.Tr, movie)
	case 'p':
		movie.Enabled = !movie.Enabled
		return Action{Kind: ActionSaveMovie, Movie: &movie}, true
	case 'd':
		m.Confirm = &confirm{MessageKey: "prompt.movieremove", Args: []any{Shorten(movie.Name, 40)}, Action: Action{Kind: ActionDeleteMovie, ID: view.ID}}
	case 'y':
		if view.Selected >= 0 && view.Selected < len(matches) && matches[view.Selected].Magnet != "" {
			return Action{Kind: ActionCopy, Text: matches[view.Selected].Magnet}, true
		}
	case 'r':
		return Action{Kind: ActionLoadMovie, ID: view.ID}, true
	default:
		return Action{}, k.Rune >= '0' && k.Rune <= '9'
	}
	return Action{}, true
}

// --- forms ----------------------------------------------------------------

func textField(tr *Translator, key, value string) FormField {
	return FormField{Key: key, Label: tr.T("field." + key), Value: value, Initial: value, Hint: tr.T("fieldhint." + key)}
}

func boolField(tr *Translator, key string, value bool) FormField {
	text := strconv.FormatBool(value)
	return FormField{Key: key, Label: tr.T("field." + key), Value: text, Initial: text, Bool: true, Hint: tr.T("fieldhint." + key)}
}

func seriesEditForm(tr *Translator, series SeriesConfig) *Form {
	return &Form{Kind: FormSeriesEdit, Title: tr.Format("form.seriesedit", series.Name), Target: series.Name, Fields: []FormField{
		textField(tr, "seasons", series.Seasons),
		textField(tr, "quality", series.Quality),
		textField(tr, "language", series.Language),
		textField(tr, "subtitle", series.Subtitle),
		textField(tr, "exclude", series.Exclude),
		textField(tr, "aliases", strings.Join(series.Aliases, ", ")),
		textField(tr, "archive_path", series.ArchivePath),
		textField(tr, "tmdb_id", series.TmdbID),
		textField(tr, "tvdb_id", series.TvdbID),
		boolField(tr, "season_subfolders", series.SeasonSubfolders),
		boolField(tr, "disable_upgrades", series.DisableUpgrades),
	}}
}

func movieEditForm(tr *Translator, movie MovieConfig) *Form {
	return &Form{Kind: FormMovieEdit, Title: tr.Format("form.movieedit", movie.Name), Movie: movie, Fields: []FormField{
		textField(tr, "name", movie.Name),
		textField(tr, "year", movie.Year),
		textField(tr, "quality", movie.Quality),
		textField(tr, "language", movie.Language),
		textField(tr, "subtitle", movie.Subtitle),
		textField(tr, "exclude", movie.Exclude),
		textField(tr, "tmdb_id", movie.TmdbID),
		textField(tr, "tvdb_id", movie.TvdbID),
	}}
}

// addForm prefills the "add to library" form from a TMDB search result.
func addForm(tr *Translator, kind SearchKind, item map[string]any) *Form {
	name := firstNonEmpty(stringValue(item["name"]), stringValue(item["title"]))
	year := firstNonEmpty(stringValue(item["first_air_date"]), stringValue(item["release_date"]))
	if len(year) > 4 {
		year = year[:4]
	}
	tmdbID := ""
	if id := numberValue(item["id"]); id > 0 {
		tmdbID = strconv.FormatInt(int64(id), 10)
	}
	tvdbID := stringValue(item["tvdb_id"])
	if kind == SearchTmdbMovies {
		return &Form{Kind: FormMovieAdd, Title: tr.Format("form.movieadd", name), Fields: []FormField{
			textField(tr, "name", name),
			textField(tr, "year", year),
			textField(tr, "quality", ""),
			textField(tr, "language", "ita"),
			textField(tr, "subtitle", ""),
			textField(tr, "exclude", ""),
			textField(tr, "tmdb_id", tmdbID),
		}}
	}
	return &Form{Kind: FormSeriesAdd, Title: tr.Format("form.seriesadd", name), Fields: []FormField{
		textField(tr, "name", name),
		textField(tr, "seasons", "1+"),
		textField(tr, "quality", ""),
		textField(tr, "language", "ita"),
		textField(tr, "subtitle", ""),
		textField(tr, "exclude", ""),
		textField(tr, "aliases", ""),
		textField(tr, "archive_path", ""),
		textField(tr, "tmdb_id", tmdbID),
		textField(tr, "tvdb_id", tvdbID),
	}}
}

func (f *Form) value(key string) string {
	for _, field := range f.Fields {
		if field.Key == key {
			return strings.TrimSpace(field.Value)
		}
	}
	return ""
}

func splitList(value string) []string {
	items := []string{}
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

func (m *Model) updateForm(k Key) Action {
	form := m.Form
	switch {
	case k.Kind == KeyEsc:
		m.Form = nil
		m.Message = m.Tr.T("msg.cancelled")
	case k.Kind == KeyUp, k.Kind == KeyDown, k.Kind == KeyHome, k.Kind == KeyEnd, k.Kind == KeyPgUp, k.Kind == KeyPgDn:
		form.Selected = moveSelection(form.Selected, len(form.Fields), k.Kind)
	case k.Kind == KeyEnter || (k.Kind == KeyRune && k.Rune == ' '):
		field := &form.Fields[form.Selected]
		if field.Bool {
			field.Value = strconv.FormatBool(field.Value != "true")
			return Action{}
		}
		if k.Kind == KeyEnter {
			m.Prompt = newPrompt(PromptFormField, field.Value)
		}
	case k.Kind == KeyRune && (k.Rune == 's' || k.Rune == 'S'):
		return m.submitForm()
	}
	return Action{}
}

// submitForm validates the form and turns it into the save action.
func (m *Model) submitForm() Action {
	form := m.Form
	switch form.Kind {
	case FormSeriesEdit:
		fields := map[string]any{}
		for _, field := range form.Fields {
			if field.Value == field.Initial {
				continue
			}
			switch {
			case field.Bool:
				fields[field.Key] = field.Value == "true"
			case field.Key == "aliases":
				fields[field.Key] = splitList(field.Value)
			default:
				fields[field.Key] = strings.TrimSpace(field.Value)
			}
		}
		if seasons, ok := fields["seasons"]; ok && seasons == "" {
			m.Message = m.Tr.T("msg.seasonsrequired")
			return Action{}
		}
		m.Form = nil
		if len(fields) == 0 {
			m.Message = m.Tr.T("msg.nochanges")
			return Action{}
		}
		return Action{Kind: ActionSaveSeries, Text: form.Target, Fields: fields}
	case FormMovieEdit:
		movie := form.Movie
		movie.Name = form.value("name")
		movie.Year = form.value("year")
		movie.Quality = form.value("quality")
		movie.Language = form.value("language")
		movie.Subtitle = form.value("subtitle")
		movie.Exclude = form.value("exclude")
		movie.TmdbID = form.value("tmdb_id")
		movie.TvdbID = form.value("tvdb_id")
		if movie.Name == "" {
			m.Message = m.Tr.T("msg.namerequired")
			return Action{}
		}
		m.Form = nil
		return Action{Kind: ActionSaveMovie, Movie: &movie}
	default:
		fields := map[string]any{"kind": "series"}
		if form.Kind == FormMovieAdd {
			fields["kind"] = "movie"
		}
		for _, field := range form.Fields {
			fields[field.Key] = strings.TrimSpace(field.Value)
		}
		if fields["name"] == "" {
			m.Message = m.Tr.T("msg.namerequired")
			return Action{}
		}
		if fields["tmdb_id"] == "" {
			m.Message = m.Tr.T("msg.tmdbrequired")
			return Action{}
		}
		if form.Kind == FormSeriesAdd && fields["seasons"] == "" {
			m.Message = m.Tr.T("msg.seasonsrequired")
			return Action{}
		}
		m.Form = nil
		return Action{Kind: ActionAddToLibrary, Fields: fields}
	}
}

// --- Missing tab ------------------------------------------------------------

func (m *Model) updateMissing(k Key) Action {
	switch {
	case k.Kind == KeyUp || k.Kind == KeyDown || k.Kind == KeyPgUp || k.Kind == KeyPgDn || k.Kind == KeyHome || k.Kind == KeyEnd:
		m.MissingSelected = moveSelection(m.MissingSelected, len(m.Missing), k.Kind)
		return Action{}
	}
	if m.MissingSelected < 0 || m.MissingSelected >= len(m.Missing) {
		return Action{}
	}
	gap := m.Missing[m.MissingSelected]
	switch {
	case k.Kind == KeyEnter:
		action := m.OpenSeries(gap.Series)
		m.SeriesView.Focus = [2]int64{gap.Season, gap.Episode}
		return action
	case k.Kind == KeyRune && k.Rune == 'i':
		m.Confirm = &confirm{MessageKey: "prompt.episodeignore", Args: []any{Shorten(gap.Series, 30), gap.Season, gap.Episode}, Action: Action{Kind: ActionEpisodeIgnore, Text: gap.Series, Season: gap.Season, Episode: gap.Episode, Flag: true}}
	}
	return Action{}
}

func containsInt64(values []int64, value int64) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// episodeLabel formats S01E02.
func episodeLabel(season, episode int64) string { return fmt.Sprintf("S%02dE%02d", season, episode) }
