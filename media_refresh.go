package gextto

// media_refresh.go asks Jellyfin and Plex to rescan only the folders that
// changed instead of the whole library. A full library scan on a NAS reads
// every folder; a targeted one reads only the series or movie folder that
// received a file, so the episode shows up in seconds. When the targeted
// request is not possible (unknown folder, no matching Plex section, an error)
// the full refresh used before is the fallback.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/utils"
)

const mediaRefreshTimeout = 20 * time.Second

// mediaRefreshFolder returns the library folder to rescan after a torrent was
// imported, or "" when it is not known (a full refresh is used then).
func mediaRefreshFolder(db *Database, hash string) string {
	if db == nil {
		return ""
	}
	processed, err := db.TorrentProcessed(hash)
	if err != nil || processed == nil || strings.TrimSpace(*processed) == "" {
		return ""
	}
	path := filepath.Clean(strings.TrimSpace(*processed))
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

// mediaServerPath translates a Gextto path into the media server's view using
// the `<server>_path_mappings` setting (one `gextto_path=server_path` per line).
func mediaServerPath(cfg *Config, server, path string) string {
	mappings, err := ParsePathMappings(cfg.Settings[server+"_path_mappings"])
	if err != nil || len(mappings) == 0 {
		return path
	}
	translated, _ := TranslateGexttoToBackend(path, mappings)
	return translated
}

func uniqueSortedFolders(folders []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(folders))
	for _, folder := range folders {
		folder = strings.TrimSpace(folder)
		if folder == "" || seen[folder] {
			continue
		}
		seen[folder] = true
		out = append(out, folder)
	}
	sort.Strings(out)
	return out
}

func jellyfinHeaders(key string) map[string]string {
	return map[string]string{
		"Authorization": fmt.Sprintf("MediaBrowser Token=\"%s\"", key),
		"X-Emby-Token":  key,
	}
}

func refreshJellyfinFull(url, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), mediaRefreshTimeout)
	defer cancel()
	response, err := HTTPRequest(ctx, http.MethodPost, strings.TrimRight(url, "/")+"/Library/Refresh", jellyfinHeaders(key), nil, "")
	if err != nil {
		logging.Warn("Jellyfin did not accept the library refresh: " + utils.RedactURLSecrets(err.Error()))
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		logging.Warn("Jellyfin did not accept the library refresh: " + response.Status)
		return
	}
	logging.Debug("Jellyfin full library refresh requested")
}

// refreshJellyfinFolders reports the changed folders through
// /Library/Media/Updated; it returns false when Jellyfin refused the request.
func refreshJellyfinFolders(cfg *Config, url, key string, folders []string) bool {
	type update struct {
		Path       string `json:"Path"`
		UpdateType string `json:"UpdateType"`
	}
	updates := make([]update, 0, len(folders))
	paths := make([]string, 0, len(folders))
	for _, folder := range folders {
		path := mediaServerPath(cfg, "jellyfin", folder)
		updates = append(updates, update{Path: path, UpdateType: "Modified"})
		paths = append(paths, path)
	}
	body, err := json.Marshal(map[string]any{"Updates": updates})
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), mediaRefreshTimeout)
	defer cancel()
	response, err := HTTPRequest(ctx, http.MethodPost, strings.TrimRight(url, "/")+"/Library/Media/Updated", jellyfinHeaders(key), body, "application/json")
	if err != nil {
		logging.Debug("Jellyfin targeted refresh failed", "error", utils.RedactURLSecrets(err.Error()))
		return false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		logging.Debug("Jellyfin targeted refresh refused", "status", response.Status)
		return false
	}
	logging.Debug("Jellyfin asked to rescan the changed folders", "folders", strings.Join(paths, ", "))
	return true
}

// plexSection is a library section with the folders it covers.
type plexSection struct {
	Key       string
	Locations []string
}

func plexSections(url, token string) ([]plexSection, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mediaRefreshTimeout)
	defer cancel()
	response, err := HTTPGet(ctx, strings.TrimRight(url, "/")+"/library/sections",
		map[string]string{"X-Plex-Token": token, "Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("status %s", response.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var payload struct {
		MediaContainer struct {
			Directory []struct {
				Key      string `json:"key"`
				Location []struct {
					Path string `json:"path"`
				} `json:"Location"`
			} `json:"Directory"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	sections := make([]plexSection, 0, len(payload.MediaContainer.Directory))
	for _, directory := range payload.MediaContainer.Directory {
		section := plexSection{Key: directory.Key}
		for _, location := range directory.Location {
			if strings.TrimSpace(location.Path) != "" {
				section.Locations = append(section.Locations, filepath.Clean(location.Path))
			}
		}
		if section.Key != "" {
			sections = append(sections, section)
		}
	}
	return sections, nil
}

// plexSectionFor returns the section whose folder contains path (the longest
// matching folder wins).
func plexSectionFor(sections []plexSection, path string) (string, bool) {
	best, bestLength := "", -1
	for _, section := range sections {
		for _, location := range section.Locations {
			if pathWithin(filepath.Clean(path), location) && len(location) > bestLength {
				best, bestLength = section.Key, len(location)
			}
		}
	}
	return best, bestLength >= 0
}

func refreshPlexFull(url, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), mediaRefreshTimeout)
	defer cancel()
	response, err := HTTPGet(ctx, strings.TrimRight(url, "/")+"/library/sections/all/refresh", map[string]string{"X-Plex-Token": token})
	if err != nil {
		logging.Warn("Plex did not accept the library refresh: " + utils.RedactURLSecrets(err.Error()))
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		logging.Warn("Plex did not accept the library refresh: " + response.Status)
		return
	}
	logging.Debug("Plex full library refresh requested")
}

// refreshPlexFolders refreshes each folder inside its own section; it returns
// false when any folder could not be refreshed in a targeted way.
func refreshPlexFolders(cfg *Config, baseURL, token string, folders []string) bool {
	sections, err := plexSections(baseURL, token)
	if err != nil {
		logging.Debug("Plex sections not readable; using a full refresh", "error", utils.RedactURLSecrets(err.Error()))
		return false
	}
	for _, folder := range folders {
		serverPath := mediaServerPath(cfg, "plex", folder)
		key, ok := plexSectionFor(sections, serverPath)
		if !ok {
			logging.Debug("no Plex library contains the folder; using a full refresh", "folder", serverPath)
			return false
		}
		endpoint := strings.TrimRight(baseURL, "/") + "/library/sections/" + url.PathEscape(key) + "/refresh?path=" + url.QueryEscape(serverPath)
		ctx, cancel := context.WithTimeout(context.Background(), mediaRefreshTimeout)
		response, err := HTTPGet(ctx, endpoint, map[string]string{"X-Plex-Token": token})
		cancel()
		if err != nil {
			logging.Debug("Plex targeted refresh failed", "error", utils.RedactURLSecrets(err.Error()))
			return false
		}
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			logging.Debug("Plex targeted refresh refused", "status", response.Status)
			return false
		}
		logging.Debug("Plex asked to rescan a folder", "section", key, "folder", serverPath)
	}
	return true
}

// RefreshMediaLibraries asks Jellyfin and Plex to pick up new files. With
// folders it rescans only those (falling back to a full refresh when that is
// not possible); without folders it refreshes the whole library.
func RefreshMediaLibraries(cfg *Config, folders ...string) {
	folders = uniqueSortedFolders(folders)
	jellyfinURL := strings.TrimSpace(cfg.Settings["jellyfin_url"])
	jellyfinKey := strings.TrimSpace(cfg.Settings["jellyfin_api_key"])
	if jellyfinURL != "" && jellyfinKey != "" {
		if len(folders) == 0 || !refreshJellyfinFolders(cfg, jellyfinURL, jellyfinKey, folders) {
			refreshJellyfinFull(jellyfinURL, jellyfinKey)
		}
	}
	plexURL := strings.TrimSpace(cfg.Settings["plex_url"])
	plexToken := strings.TrimSpace(cfg.Settings["plex_token"])
	if plexURL != "" && plexToken != "" {
		if len(folders) == 0 || !refreshPlexFolders(cfg, plexURL, plexToken, folders) {
			refreshPlexFull(plexURL, plexToken)
		}
	}
}
