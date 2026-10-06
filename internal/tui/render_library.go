package tui

import (
	"fmt"
	"strings"
	"time"
)

func (m *Model) stateWord(enabled bool) string {
	if enabled {
		return m.Tr.T("library.active")
	}
	return m.Tr.T("library.paused")
}

func (m *Model) renderSeriesView(width, contentHeight int) []Line {
	view := m.SeriesView
	if view.Data == nil {
		return []Line{{Text: view.Name, Style: StyleHeader}, {Text: m.Tr.T("msg.loading"), Style: StyleMuted}}
	}
	series := view.Data.Series
	downloaded, total := 0, 0
	for _, episode := range view.Data.Episodes {
		if episode.Ignored {
			continue
		}
		total++
		if episodeDone(episode) {
			downloaded++
		}
	}
	headerStyle := StyleHeader
	if !series.Enabled {
		headerStyle = StyleWarn
	}
	lines := []Line{
		{Text: fmt.Sprintf("%s · %s · %s", series.Name, m.stateWord(series.Enabled), m.Tr.Format("library.episodes", downloaded, total)), Style: headerStyle},
		{Text: joinNonEmpty(
			m.Tr.Format("label.seasonsfield", series.Seasons),
			series.Quality, series.Language,
			labelled(m.Tr.T("field.subtitle"), series.Subtitle),
			labelled("TMDB", series.TmdbID), labelled("TVDB", series.TvdbID),
			labelled(m.Tr.T("field.archive_path"), series.ArchivePath),
		), Style: StyleMuted, Wrap: true, Indent: 2},
		{Text: m.seasonStrip(width), Style: StyleNormal},
	}
	now := time.Now()
	episodes := view.SeasonEpisodes()
	switch {
	case view.SeasonIgnored(view.Season):
		lines = append(lines, Line{Text: m.Tr.Format("series.seasonoff", view.Season), Style: StyleWarn, Wrap: true})
		return lines
	case view.SeasonExcluded(view.Season):
		lines = append(lines, Line{Text: m.Tr.Format("series.seasonexcluded", view.Season, series.Seasons), Style: StyleWarn, Wrap: true})
		return lines
	case len(episodes) == 0:
		lines = append(lines, Line{Text: m.Tr.T("series.noepisodes"), Style: StyleMuted, Wrap: true})
		return lines
	}
	texts := make([]string, len(episodes))
	styles := make([]Style, len(episodes))
	for index, episode := range episodes {
		texts[index], styles[index] = m.episodeRow(episode, now)
	}
	expanded := expandLines(lines, width)
	return append(expanded, styledRows(texts, styles, view.Selected, &view.Scroll, contentHeight-len(expanded), width)...)
}

func labelled(label, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return label + " " + value
}

// seasonStrip renders "S1 10/10  [S2 3/8]  S3 off", windowed around the
// selected season when it does not fit.
func (m *Model) seasonStrip(width int) string {
	view := m.SeriesView
	seasons := view.Seasons()
	if len(seasons) == 0 {
		return m.Tr.T("series.noseasons")
	}
	parts := make([]string, len(seasons))
	selected := 0
	for index, season := range seasons {
		label := ""
		switch {
		case view.SeasonIgnored(season):
			label = fmt.Sprintf("S%d %s", season, m.Tr.T("series.off"))
		case view.SeasonExcluded(season):
			label = fmt.Sprintf("S%d -", season)
		default:
			done, total := 0, 0
			for _, episode := range view.episodesOf(season) {
				if episode.Ignored {
					continue
				}
				total++
				if episodeDone(episode) {
					done++
				}
			}
			label = fmt.Sprintf("S%d %d/%d", season, done, total)
		}
		if season == view.Season {
			label = "[" + label + "]"
			selected = index
		} else {
			label = " " + label + " "
		}
		parts[index] = label
	}
	prefix := m.Tr.T("series.seasons") + " ←→ "
	budget := max(10, width-StringWidth(prefix))
	start, end := selected, selected+1
	used := StringWidth(parts[selected])
	for {
		grown := false
		if end < len(parts) && used+StringWidth(parts[end])+2 <= budget {
			used += StringWidth(parts[end])
			end++
			grown = true
		}
		if start > 0 && used+StringWidth(parts[start-1])+2 <= budget {
			start--
			used += StringWidth(parts[start])
			grown = true
		}
		if !grown {
			break
		}
	}
	text := strings.Join(parts[start:end], "")
	if start > 0 {
		text = "…" + text
	}
	if end < len(parts) {
		text += "…"
	}
	return prefix + text
}

// episodeRow describes one episode and picks its style.
func (m *Model) episodeRow(episode Episode, now time.Time) (string, Style) {
	date := firstNonEmpty(episode.AirDate, "----------")
	if len(date) > 10 {
		date = date[:10]
	}
	title := firstNonEmpty(episode.RenamedTitle, episode.Title)
	glyph, style, detail := "✗", StyleWarn, m.Tr.T("episode.missing")
	switch {
	case episode.Ignored:
		glyph, style, detail = "-", StyleMuted, m.Tr.T("episode.ignored")
	case episodeDone(episode):
		glyph, style = "✓", StyleOK
		detail = joinNonEmpty(humanSize(episode.SizeBytes), m.Tr.Format("label.score", episode.QualityScore))
	case episode.Error != "" || strings.Contains(strings.ToLower(episode.Status), "error") || strings.Contains(strings.ToLower(episode.Status), "fail"):
		glyph, style, detail = "!", StyleErr, joinNonEmpty(m.Tr.StateLabel(episode.Status), episode.Error)
	case episode.Status != "" && episode.Status != "missing":
		glyph, style, detail = "↓", StyleNormal, m.Tr.StateLabel(episode.Status)
	case !episodeAired(episode, now):
		glyph, style, detail = "·", StyleMuted, m.Tr.T("episode.upcoming")
	}
	return fmt.Sprintf("%s E%02d %s %s", glyph, episode.Episode, date, joinNonEmpty(title, detail)), style
}

func humanSize(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	return HumanBytes(float64(bytes))
}

func (m *Model) renderMovieView(width, contentHeight int) []Line {
	view := m.MovieView
	if view.Data == nil {
		name := ""
		if movie := m.movieByID(view.ID); movie != nil {
			name = movie.Name
		}
		return []Line{{Text: name, Style: StyleHeader}, {Text: m.Tr.T("msg.loading"), Style: StyleMuted}}
	}
	movie := view.Data.Movie
	headerStyle := StyleHeader
	if !movie.Enabled {
		headerStyle = StyleWarn
	}
	title := movie.Name
	if movie.Year != "" {
		title += " (" + movie.Year + ")"
	}
	lines := []Line{
		{Text: title + " · " + m.stateWord(movie.Enabled), Style: headerStyle, Wrap: true},
		{Text: joinNonEmpty(movie.Quality, movie.Language,
			labelled(m.Tr.T("field.subtitle"), movie.Subtitle),
			labelled(m.Tr.T("field.exclude"), movie.Exclude),
			labelled("TMDB", movie.TmdbID), labelled("TVDB", movie.TvdbID)), Style: StyleMuted, Wrap: true, Indent: 2},
	}
	overview := firstNonEmpty(stringValue(view.Data.Metadata["overview"]), movie.Overview)
	if overview != "" {
		lines = append(lines, Line{Text: Shorten(overview, max(40, width*3)), Style: StyleNormal, Wrap: true})
	}
	if len(view.Data.History) == 0 {
		lines = append(lines, Line{Text: m.Tr.T("movie.nohistory"), Style: StyleMuted})
	} else {
		lines = append(lines, Line{Text: m.Tr.T("movie.history"), Style: StyleHeader})
		for index, item := range view.Data.History {
			if index == 3 {
				break
			}
			date := ""
			if item.DownloadedAt != nil && len(*item.DownloadedAt) >= 10 {
				date = (*item.DownloadedAt)[:10]
			}
			lines = append(lines, Line{Text: "  ✓ " + joinNonEmpty(date, item.Title, humanSize(item.SizeBytes), m.Tr.Format("label.score", item.QualityScore)), Style: StyleOK})
		}
	}
	expanded := expandLines(lines, width)
	if len(view.Data.Matches) == 0 {
		return append(expanded, Line{Text: m.Tr.T("movie.nomatches"), Style: StyleMuted})
	}
	expanded = append(expanded, Line{Text: m.Tr.Format("movie.matches", len(view.Data.Matches)), Style: StyleHeader})
	texts := make([]string, len(view.Data.Matches))
	for index, match := range view.Data.Matches {
		texts[index] = joinNonEmpty(match.Title, match.Source)
	}
	return append(expanded, catalogRows(texts, view.Selected, &view.Scroll, contentHeight-len(expanded), width)...)
}

func (m *Model) renderForm(width, contentHeight int) []Line {
	form := m.Form
	lines := []Line{{Text: form.Title, Style: StyleHeader, Wrap: true}}
	labelWidth := 0
	for _, field := range form.Fields {
		labelWidth = max(labelWidth, StringWidth(field.Label))
	}
	labelWidth = min(labelWidth, max(8, width/3))
	for index, field := range form.Fields {
		value := field.Value
		if field.Bool {
			value = boolWord(m.Tr, field.Value == "true")
		}
		if value == "" {
			value = "-"
		}
		marker, changed := "  ", " "
		if index == form.Selected {
			marker = "> "
		}
		if field.Value != field.Initial {
			changed = "*"
		}
		style := StyleNormal
		if index == form.Selected {
			style = StyleSelected
		}
		lines = append(lines, Line{Text: marker + changed + PadRight(Shorten(field.Label, labelWidth), labelWidth) + "  " + value, Style: style, Wrap: index == form.Selected, Indent: labelWidth + 5})
	}
	if hint := form.Fields[form.Selected].Hint; hint != "" {
		lines = append(lines, Line{}, Line{Text: hint, Style: StyleMuted, Wrap: true})
	}
	return lines
}

// tmdbResultText describes one TMDB/TVDB search result.
func (m *Model) tmdbResultText(item map[string]any) string {
	name := firstNonEmpty(stringValue(item["name"]), stringValue(item["title"]))
	year := firstNonEmpty(stringValue(item["first_air_date"]), stringValue(item["release_date"]))
	if len(year) > 4 {
		year = year[:4]
	}
	if year != "" {
		name += " (" + year + ")"
	}
	parts := []string{name}
	if vote := numberValue(item["vote_average"]); vote > 0 {
		parts = append(parts, m.Tr.Format("label.vote", vote))
	}
	if truthy(item["in_library"]) {
		parts = append(parts, m.Tr.T("label.inlibrary"))
	}
	if source := stringValue(item["external"]); source == "tvdb" {
		parts = append(parts, "TVDB")
	}
	parts = append(parts, Shorten(stringValue(item["overview"]), 120))
	return joinNonEmpty(parts...)
}
