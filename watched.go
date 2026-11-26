package gextto

// watched.go reads what a media server has already played and turns it into a
// compact set of keys, so a cycle does not look for a copy the household has
// already seen. It is an optional, best-effort filter: a network error never
// blocks a download.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/buzzqw/gextto/internal/utils"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const (
	watchedSuppressionKey = "watched_suppression_enabled"
	watchedRequestTimeout = 15 * time.Second
)

func watchedSuppressionEnabled(cfg *Config) bool {
	return cfg != nil && settingTruthy(settingsOr(cfg, watchedSuppressionKey, "false"))
}

// watchedReleaseKey is the same shape for the media server and for a release:
// a movie is identified by title and year, an episode by series, season and
// number. Titles are compared case-insensitively.
func watchedReleaseKey(release *models.Release) string {
	if release == nil {
		return ""
	}
	if release.Kind == "movie" {
		year := ""
		if release.Year != nil {
			year = strconv.FormatInt(*release.Year, 10)
		}
		return "movie\x00" + strings.ToLower(strings.TrimSpace(release.Title)) + "\x00" + year
	}
	if release.Series == nil || release.Season == nil || release.Episode == nil {
		return ""
	}
	return fmt.Sprintf("episode\x00%s\x00%d\x00%d", strings.ToLower(strings.TrimSpace(*release.Series)), *release.Season, *release.Episode)
}

func watchedJSON(ctx context.Context, address string, headers map[string]string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, watchedRequestTimeout)
	defer cancel()
	response, err := HTTPGet(ctx, address, headers)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	return io.ReadAll(io.LimitReader(response.Body, 8<<20))
}

// loadWatchedKeys asks each configured media server for its played items.
// Plex exposes viewCount on library items; Jellyfin exposes IsPlayed.
func loadWatchedKeys(ctx context.Context, cfg *Config) (map[string]bool, error) {
	keys := map[string]bool{}
	if cfg == nil {
		return keys, nil
	}
	plexURL := settingsOr(cfg, "plex_url", "")
	plexToken := settingsOr(cfg, "plex_token", "")
	if strings.TrimSpace(plexURL) != "" && strings.TrimSpace(plexToken) != "" {
		if err := loadPlexWatched(ctx, plexURL, plexToken, keys); err != nil {
			logging.Debug("could not read watched items from Plex", "error", err.Error())
		}
	}
	jellyfinURL := settingsOr(cfg, "jellyfin_url", "")
	jellyfinKey := settingsOr(cfg, "jellyfin_api_key", "")
	if strings.TrimSpace(jellyfinURL) != "" && strings.TrimSpace(jellyfinKey) != "" {
		if err := loadJellyfinWatched(ctx, jellyfinURL, jellyfinKey, keys); err != nil {
			logging.Debug("could not read watched items from Jellyfin", "error", err.Error())
		}
	}
	return keys, nil
}

// WatchedSettings implements GET and POST /api/watched/settings.
func WatchedSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.Method == http.MethodGet {
		jsonResponse(w, map[string]any{"ok": true, "enabled": watchedSuppressionEnabled(latestConfig(s))})
		return
	}
	var input SettingsPatch
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, body := saveProviderSettings(s, input, []string{watchedSuppressionKey})
	if status != http.StatusOK {
		message, _ := body["error"].(string)
		jsonError(w, status, utils.RedactURLSecrets(message))
		return
	}
	jsonResponse(w, body)
}

func loadPlexWatched(ctx context.Context, base, token string, keys map[string]bool) error {
	sections, err := plexSections(base, token)
	if err != nil {
		return err
	}
	headers := map[string]string{"X-Plex-Token": token, "Accept": "application/json"}
	for _, section := range sections {
		for _, kind := range []string{"1", "2", "4"} { // movie, show, episode
			address := strings.TrimRight(base, "/") + "/library/sections/" + section.Key + "/all?type=" + kind
			body, err := watchedJSON(ctx, address, headers)
			if err != nil {
				continue
			}
			var payload struct {
				MediaContainer struct {
					Metadata []struct {
						Type        string `json:"type"`
						Title       string `json:"title"`
						ParentTitle string `json:"parentTitle"`
						Year        int    `json:"year"`
						Season      int    `json:"seasonNumber"`
						Episode     int    `json:"episodeNumber"`
						ViewCount   int    `json:"viewCount"`
					} `json:"Metadata"`
				} `json:"MediaContainer"`
			}
			if json.Unmarshal(body, &payload) != nil {
				continue
			}
			for _, item := range payload.MediaContainer.Metadata {
				if item.ViewCount <= 0 {
					continue
				}
				if item.Type == "episode" || (item.Season > 0 && item.Episode > 0) {
					keys[fmt.Sprintf("episode\x00%s\x00%d\x00%d", strings.ToLower(item.ParentTitle), item.Season, item.Episode)] = true
				} else if item.Year > 0 {
					keys[fmt.Sprintf("movie\x00%s\x00%d", strings.ToLower(item.Title), item.Year)] = true
				}
			}
		}
	}
	return nil
}

func loadJellyfinWatched(ctx context.Context, base, key string, keys map[string]bool) error {
	body, err := watchedJSON(ctx, strings.TrimRight(base, "/")+"/Users", jellyfinHeaders(key))
	if err != nil {
		return err
	}
	var users []struct {
		ID string `json:"Id"`
	}
	if json.Unmarshal(body, &users) != nil || len(users) == 0 {
		return fmt.Errorf("no Jellyfin user found")
	}
	headers := jellyfinHeaders(key)
	address := strings.TrimRight(base, "/") + "/Users/" + users[0].ID + "/Items?IsPlayed=true&Recursive=true"
	body, err = watchedJSON(ctx, address, headers)
	if err != nil {
		return err
	}
	var payload struct {
		Items []struct {
			Type     string            `json:"Type"`
			Name     string            `json:"Name"`
			Series   string            `json:"SeriesName"`
			Year     int               `json:"ProductionYear"`
			Season   int               `json:"ParentIndexNumber"`
			Episode  int               `json:"IndexNumber"`
			Provider map[string]string `json:"ProviderIds"`
		} `json:"Items"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return fmt.Errorf("invalid Jellyfin response")
	}
	for _, item := range payload.Items {
		if item.Type == "Episode" && item.Season > 0 && item.Episode > 0 {
			keys[fmt.Sprintf("episode\x00%s\x00%d\x00%d", strings.ToLower(item.Series), item.Season, item.Episode)] = true
		} else if item.Type == "Movie" && item.Year > 0 {
			keys[fmt.Sprintf("movie\x00%s\x00%d", strings.ToLower(item.Name), item.Year)] = true
		}
	}
	return nil
}
