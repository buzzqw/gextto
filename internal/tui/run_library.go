package tui

import (
	"context"
	"fmt"
)

// performLibraryAction runs the series, movie and episode actions. After a
// change it reloads the library (and the open series detail) in the same
// call, so the screen shows the result without waiting for the next poll.
func performLibraryAction(ctx context.Context, client *Client, tr *Translator, action Action) actionResult {
	result := actionResult{}
	fail := func(err error) actionResult {
		result.message = tr.Format("msg.libraryfailed", err)
		return result
	}
	// reload refreshes the library lists and, for a series action, the
	// series detail; message is shown once both are applied.
	reload := func(message string) actionResult {
		series, movies, listErr := client.Library(ctx)
		var detail *SeriesDetail
		if action.Text != "" && action.Kind != ActionDeleteSeries {
			if loaded, err := client.SeriesDetail(ctx, action.Text); err == nil {
				detail = &loaded
			}
		}
		result.apply = func(m *Model) {
			if listErr == nil {
				m.SetLibraryData(series, movies)
			}
			if detail != nil {
				m.SetSeriesDetail(*detail)
			}
		}
		result.message = message
		return result
	}
	label := func() string { return episodeLabel(action.Season, action.Episode) }

	switch action.Kind {
	case ActionLoadSeries:
		detail, err := client.SeriesDetail(ctx, action.Text)
		if err != nil {
			result.apply = func(m *Model) {
				if m.SeriesView != nil && m.SeriesView.Data == nil {
					m.SeriesView = nil
				}
				m.PendingEdit = false
			}
			return fail(err)
		}
		result.apply = func(m *Model) { m.SetSeriesDetail(detail) }
		result.clearMessage = true
	case ActionLoadMovie:
		detail, err := client.MovieDetail(ctx, action.ID)
		if err != nil {
			result.apply = func(m *Model) {
				if m.MovieView != nil && m.MovieView.Data == nil {
					m.MovieView = nil
				}
			}
			return fail(err)
		}
		result.apply = func(m *Model) { m.SetMovieDetail(detail) }
		result.clearMessage = true
	case ActionSaveSeries:
		if err := client.UpdateSeries(ctx, action.Text, action.Fields); err != nil {
			return fail(err)
		}
		message := tr.T("msg.seriessaved")
		if enabled, ok := action.Fields["enabled"].(bool); ok && len(action.Fields) == 1 {
			message = tr.Format("msg.seriespaused", action.Text)
			if enabled {
				message = tr.Format("msg.seriesresumed", action.Text)
			}
		}
		return reload(message)
	case ActionDeleteSeries:
		if err := client.DeleteSeries(ctx, action.Text); err != nil {
			return fail(err)
		}
		reload(tr.Format("msg.seriesremoved", action.Text))
		apply := result.apply
		result.apply = func(m *Model) {
			apply(m)
			if m.SeriesView != nil && m.SeriesView.Name == action.Text {
				m.SeriesView = nil
			}
		}
	case ActionSaveMovie:
		if err := client.UpdateMovie(ctx, *action.Movie); err != nil {
			return fail(err)
		}
		movie := *action.Movie
		reload(tr.Format("msg.moviesaved", movie.Name))
		if detail, err := client.MovieDetail(ctx, movie.ID); err == nil {
			apply := result.apply
			result.apply = func(m *Model) {
				apply(m)
				m.SetMovieDetail(detail)
			}
		}
	case ActionDeleteMovie:
		if err := client.DeleteMovie(ctx, action.ID); err != nil {
			return fail(err)
		}
		reload(tr.T("msg.movieremoved"))
		apply := result.apply
		result.apply = func(m *Model) {
			apply(m)
			if m.MovieView != nil && m.MovieView.ID == action.ID {
				m.MovieView = nil
			}
		}
	case ActionAddToLibrary:
		if err := client.TmdbAdd(ctx, action.Fields); err != nil {
			return fail(err)
		}
		return reload(tr.Format("msg.libraryadded", stringValue(action.Fields["name"])))
	case ActionTmdbSearch:
		items, err := client.TmdbSearch(ctx, action.Domain, action.Text)
		if err != nil {
			return fail(err)
		}
		kind := SearchTmdbSeries
		if action.Domain == "movie" {
			kind = SearchTmdbMovies
		}
		result.apply = func(m *Model) { m.SetPickerResults(kind, action.Text, items) }
		result.message = tr.Format("msg.tmdbresults", len(items))
	case ActionToggleSeason:
		if err := client.ToggleSeason(ctx, action.Text, action.Season, action.Flag); err != nil {
			return fail(err)
		}
		if action.Flag {
			return reload(tr.Format("msg.seasonon", action.Season))
		}
		return reload(tr.Format("msg.seasonoff", action.Season))
	case ActionSeriesSearchMissing:
		items, searched, err := client.SeriesSearchMissing(ctx, action.Text)
		if err != nil {
			return fail(err)
		}
		if searched == 0 {
			result.message = tr.T("msg.nomissing")
			return result
		}
		query := tr.Format("search.missing", action.Text, searched)
		result.apply = func(m *Model) { m.SetPickerResults(SearchReleases, query, items) }
		result.message = tr.Format("msg.search", len(items))
	case ActionSeriesMetadata:
		if err := client.SeriesMetadata(ctx, action.Text); err != nil {
			return fail(err)
		}
		return reload(tr.T("msg.metadataupdated"))
	case ActionRenamePreview:
		count, err := client.SeriesRename(ctx, action.Text, false)
		if err != nil {
			return fail(err)
		}
		if count == 0 {
			result.message = tr.T("msg.renamenone")
			return result
		}
		result.apply = func(m *Model) {
			m.Confirm = &confirm{MessageKey: "prompt.rename", Args: []any{count, Shorten(action.Text, 30)}, Action: Action{Kind: ActionRenameExecute, Text: action.Text}}
		}
		result.clearMessage = true
	case ActionRenameExecute:
		count, err := client.SeriesRename(ctx, action.Text, true)
		if err != nil {
			return fail(err)
		}
		return reload(tr.Format("msg.renamed", count))
	case ActionEpisodeSources, ActionEpisodeSearch:
		search := client.EpisodeSources
		if action.Kind == ActionEpisodeSearch {
			search = client.SearchEpisode
		}
		items, err := search(ctx, action.Text, action.Season, action.Episode)
		if err != nil {
			return fail(err)
		}
		query := fmt.Sprintf("%s %s", action.Text, label())
		if action.Kind == ActionEpisodeSources && len(items) == 0 {
			result.message = tr.Format("msg.nosources", label())
			return result
		}
		result.apply = func(m *Model) { m.SetPickerResults(SearchReleases, query, items) }
		result.message = tr.Format("msg.search", len(items))
	case ActionEpisodeIgnore:
		if err := client.IgnoreEpisode(ctx, action.Text, action.Season, action.Episode, action.Flag); err != nil {
			return fail(err)
		}
		message := tr.Format("msg.episodeunignored", label())
		if action.Flag {
			message = tr.Format("msg.episodeignored", label())
		}
		reload(message)
		gaps, gapsErr := client.Gaps(ctx)
		apply := result.apply
		result.apply = func(m *Model) {
			apply(m)
			if gapsErr == nil {
				m.Missing = gaps
				m.MissingSelected = min(m.MissingSelected, max(0, len(gaps)-1))
			}
		}
	case ActionEpisodeRedownload:
		if err := client.RedownloadEpisode(ctx, action.Text, action.Season, action.Episode); err != nil {
			return fail(err)
		}
		return reload(tr.Format("msg.episoderedownload", label()))
	case ActionMovieSearch:
		items, err := client.SearchMovie(ctx, action.ID)
		if err != nil {
			return fail(err)
		}
		result.apply = func(m *Model) { m.SetPickerResults(SearchReleases, action.Domain, items) }
		result.message = tr.Format("msg.search", len(items))
	case ActionMovieRedownload:
		if err := client.RedownloadMovie(ctx, action.ID); err != nil {
			return fail(err)
		}
		result.message = tr.T("msg.movieredownload")
	}
	return result
}
