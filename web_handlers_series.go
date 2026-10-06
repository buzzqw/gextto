package gextto

import (
	"net/http"
	"strings"
)

// SeriesUpdateInput is the input of `POST /api/series/{name}`. Every field is
// optional: only the ones present are changed, so a client never has to send
// (and possibly lose) fields it does not know about.
type SeriesUpdateInput struct {
	Seasons          *string   `json:"seasons"`
	Quality          *string   `json:"quality"`
	Language         *string   `json:"language"`
	Subtitle         *string   `json:"subtitle"`
	Exclude          *string   `json:"exclude"`
	ArchivePath      *string   `json:"archive_path"`
	TmdbID           *string   `json:"tmdb_id"`
	TvdbID           *string   `json:"tvdb_id"`
	Aliases          *[]string `json:"aliases"`
	Enabled          *bool     `json:"enabled"`
	SeasonSubfolders *bool     `json:"season_subfolders"`
	DisableUpgrades  *bool     `json:"disable_upgrades"`
	Timeframe        *int64    `json:"timeframe"`
}

// seriesIndex returns the position of the series called name (or with that
// alias) in list, or -1.
func seriesIndex(list []SeriesConfig, name string) int {
	for index := range list {
		if list[index].Name == name {
			return index
		}
	}
	for index := range list {
		for _, alias := range list[index].Aliases {
			if alias == name {
				return index
			}
		}
	}
	return -1
}

// UpdateSeries changes the given fields of one monitored series.
func UpdateSeries(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	var input SeriesUpdateInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	list := append([]SeriesConfig(nil), cfg.Series...)
	index := seriesIndex(list, name)
	if index < 0 {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	series := &list[index]
	text := func(target *string, value *string) {
		if value != nil {
			*target = strings.TrimSpace(*value)
		}
	}
	text(&series.Seasons, input.Seasons)
	text(&series.Quality, input.Quality)
	text(&series.Language, input.Language)
	text(&series.Subtitle, input.Subtitle)
	text(&series.Exclude, input.Exclude)
	text(&series.ArchivePath, input.ArchivePath)
	text(&series.TmdbID, input.TmdbID)
	text(&series.TvdbID, input.TvdbID)
	if strings.TrimSpace(series.Seasons) == "" {
		jsonError(w, http.StatusBadRequest, "seasons must not be empty")
		return
	}
	if input.Aliases != nil {
		aliases := []string{}
		for _, alias := range *input.Aliases {
			if trimmed := strings.TrimSpace(alias); trimmed != "" {
				aliases = append(aliases, trimmed)
			}
		}
		series.Aliases = aliases
	}
	if input.Enabled != nil {
		series.Enabled = *input.Enabled
	}
	if input.SeasonSubfolders != nil {
		series.SeasonSubfolders = *input.SeasonSubfolders
	}
	if input.DisableUpgrades != nil {
		series.DisableUpgrades = *input.DisableUpgrades
	}
	if input.Timeframe != nil {
		series.Timeframe = max(*input.Timeframe, 0)
	}
	updated := *series
	if err := SaveLibrary(s.cfg.DataDir, list, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "series": updated})
}

// DeleteSeries removes one series from the monitored library.
func DeleteSeries(w http.ResponseWriter, r *http.Request, s *AppState) {
	name := pathParam(r, "name")
	cfg := latestConfig(s)
	index := seriesIndex(cfg.Series, name)
	if index < 0 {
		jsonError(w, http.StatusNotFound, "series not found")
		return
	}
	list := make([]SeriesConfig, 0, len(cfg.Series)-1)
	list = append(list, cfg.Series[:index]...)
	list = append(list, cfg.Series[index+1:]...)
	if err := SaveLibrary(s.cfg.DataDir, list, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gh6_purgeRemovedLibrary(s.db, cfg.Series, cfg.Movies, list, cfg.Movies)
	jsonResponse(w, map[string]any{"ok": true, "removed": cfg.Series[index].Name})
}
