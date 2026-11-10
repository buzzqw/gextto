package gextto

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/buzzqw/gextto/internal/logging"
)

// SeriesUpdateInput is the input of `POST /api/series/{name}`. Every field is
// optional: only the ones present are changed, so a client never has to send
// (and possibly lose) fields it does not know about.
type SeriesUpdateInput struct {
	// Name renames the series in the library. Episodes and their history stay
	// linked; the archive folder is not moved.
	Name             *string   `json:"name"`
	Seasons          *string   `json:"seasons"`
	Quality          *string   `json:"quality"`
	Audio            *string   `json:"audio"`
	AudioChannels    *string   `json:"audio_channels"`
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
	Anime            *bool     `json:"anime"`
	Timeframe        *int64    `json:"timeframe"`
	// QualityProfileID, when above 0, copies that profile's quality, audio,
	// language, subtitles and upgrade switch into the series and adds its
	// exclusions to the series' own (the profile is a template).
	QualityProfileID *int `json:"quality_profile_id"`
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

// validateSeriesName refuses names that cannot be stored or used as a URL
// path segment. It is deliberately looser than a folder name: the rename
// changes the library title, not the archive folder.
func validateSeriesName(name string) error {
	if name == "" {
		return fmt.Errorf("the series name must not be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("the series name is not valid")
	}
	if len([]rune(name)) > 200 {
		return fmt.Errorf("the series name is too long")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("the series name must not contain a slash")
	}
	for _, char := range name {
		if char < 0x20 || char == 0x7f {
			return fmt.Errorf("the series name contains a control character")
		}
	}
	return nil
}

// seriesNameTaken reports whether another series in list already uses name as
// its title or as one of its aliases.
func seriesNameTaken(list []SeriesConfig, skip int, name string) bool {
	for index := range list {
		if index == skip {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(list[index].Name), name) {
			return true
		}
		for _, alias := range list[index].Aliases {
			if strings.EqualFold(strings.TrimSpace(alias), name) {
				return true
			}
		}
	}
	return false
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
	// The rename is the only change that moves the stored state to a new key,
	// so it is applied before the other fields and migrated after the save.
	renamedFrom := ""
	if input.Name != nil {
		newName := strings.TrimSpace(*input.Name)
		if err := validateSeriesName(newName); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if newName != series.Name {
			if seriesNameTaken(list, index, newName) {
				jsonError(w, http.StatusConflict, "a series with this name already exists")
				return
			}
			previous := *series
			renamedFrom = previous.Name
			series.Name = newName
			// Keep new episodes in the folder the library was already using:
			// an auto-detected path would no longer match the new name and
			// would fall back to the archive root.
			if strings.TrimSpace(series.ArchivePath) == "" {
				if resolved := cfg.ResolveArchivePath(&previous); resolved != nil {
					series.ArchivePath = *resolved
				}
			}
		}
	}
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
	if input.Audio != nil {
		series.Audio = normalizeAudioRequirement(*input.Audio)
	}
	if input.AudioChannels != nil {
		series.AudioChannels = normalizeAudioChannels(*input.AudioChannels)
	}
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
	if input.Anime != nil {
		series.Anime = *input.Anime
	}
	if input.Timeframe != nil {
		series.Timeframe = max(*input.Timeframe, 0)
	}
	if input.QualityProfileID != nil && *input.QualityProfileID > 0 {
		profile, ok := qualityProfileExact(ensureQualityProfiles(s), *input.QualityProfileID)
		if !ok {
			jsonError(w, http.StatusBadRequest, "quality profile not found")
			return
		}
		series.Quality, series.Language, series.Subtitle = profile.Quality, profile.Language, profile.Subtitle
		series.Audio, series.AudioChannels = profile.Audio, ""
		series.DisableUpgrades = profile.DisableUpgrades
		series.Exclude = mergeExclusions(profile.Exclude, series.Exclude)
	}
	updated := *series
	if err := SaveLibrary(s.cfg.DataDir, list, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if renamedFrom != "" {
		if err := s.db.RenameSeries(renamedFrom, updated.Name); err != nil {
			// Never leave the library under a name whose episodes and history
			// still answer to the old one.
			if restoreErr := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); restoreErr != nil {
				logging.Error("cannot restore the series name after a failed rename", "error", restoreErr, "series", renamedFrom)
			}
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
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
	gh6PurgeRemovedLibrary(s.db, cfg.Series, cfg.Movies, list, cfg.Movies)
	jsonResponse(w, map[string]any{"ok": true, "removed": cfg.Series[index].Name})
}
