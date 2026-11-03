package gextto

// Integrations with external services: the Simkl endpoints (settings, PIN
// login, calendar, watchlist, mark as watched, watchlist import), the Jellyfin
// refresh and test, the FlareSolverr test, and the helpers that map a provider
// to its indexer URL and read the mircrew indexer's login state.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/utils"
)

func SaveSimklSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SettingsPatch
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	status, body := saveProviderSettings(s, input, []string{
		"simkl_client_id",
		"simkl_watchlist_status",
		"simkl_calendar_days",
		"simkl_mark_watched",
	})
	jsonStatus(w, status, body)
}

func settingsValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		if typed {
			return "true", true
		}
		return "false", true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case int:
		return strconv.Itoa(typed), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case json.Number:
		return typed.String(), true
	}
	return "", false
}

func saveProviderSettings(s *AppState, input SettingsPatch, allowed []string) (int, map[string]any) {
	keys := make([]string, 0, len(input.Values))
	for key := range input.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	saved := []string{}
	for _, key := range keys {
		value := input.Values[key]
		if value == nil || !gh0ContainsString(allowed, key) {
			continue
		}
		encoded, ok := settingsValue(value)
		if !ok {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid setting value"}
		}
		if len(encoded) > 4096 {
			return http.StatusBadRequest, map[string]any{"ok": false, "error": "setting too long"}
		}
		if err := SaveSetting(s.cfg.DataDir, key, encoded); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
		saved = append(saved, key)
	}
	return http.StatusOK, map[string]any{"ok": true, "saved": saved}
}

func SimklAuthRevoke(w http.ResponseWriter, r *http.Request, s *AppState) {
	status, body := revokeIntegrationTokens(s, []string{"simkl_access_token"})
	jsonStatus(w, status, body)
}

func revokeIntegrationTokens(s *AppState, keys []string) (int, map[string]any) {
	if s.dryRun() {
		return http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not modify integration tokens"}
	}
	for _, key := range keys {
		if err := SaveSetting(s.cfg.DataDir, key, ""); err != nil {
			return http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()}
		}
	}
	return http.StatusOK, map[string]any{"ok": true, "authenticated": false}
}

func JellyfinRefresh(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	url := cfg.Settings["jellyfin_url"]
	key := cfg.Settings["jellyfin_api_key"]
	if strings.TrimSpace(url) == "" || strings.TrimSpace(key) == "" {
		jsonError(w, http.StatusBadRequest, "Jellyfin non configurato")
		return
	}
	endpoint := strings.TrimRight(url, "/") + "/Library/Refresh"
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	headers := map[string]string{
		"Authorization": fmt.Sprintf("MediaBrowser Token=\"%s\"", key),
		"X-Emby-Token":  key,
	}
	response, err := HTTPRequest(ctx, http.MethodPost, endpoint, headers, nil, "")
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	jsonError(w, http.StatusBadGateway, "HTTP "+response.Status)
}

func JellyfinTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	url := cfg.Settings["jellyfin_url"]
	key := cfg.Settings["jellyfin_api_key"]
	if strings.TrimSpace(url) == "" || strings.TrimSpace(key) == "" {
		jsonError(w, http.StatusBadRequest, "Jellyfin non configurato")
		return
	}
	endpoint := strings.TrimRight(url, "/") + "/System/Info"
	client := &http.Client{Timeout: 15 * time.Second}
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	request.Header.Set("Authorization", fmt.Sprintf("MediaBrowser Token=\"%s\"", key))
	request.Header.Set("X-Emby-Token", key)
	response, err := client.Do(request)
	if err != nil {
		logging.Info("jellyfin test", "host", domainOf(url), "ok", false, "error", err.Error())
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		var info map[string]any
		_ = json.NewDecoder(response.Body).Decode(&info)
		server, _ := info["ServerName"].(string)
		version, _ := info["Version"].(string)
		logging.Info("jellyfin test", "host", domainOf(url), "ok", true, "status", response.StatusCode, "server", server)
		jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "server": server, "version": version})
		return
	}
	mediaError := mediaTestError("Jellyfin", response.StatusCode)
	logging.Info("jellyfin test", "host", domainOf(url), "ok", false, "status", response.StatusCode, "error", mediaError)
	jsonStatus(w, http.StatusBadGateway, map[string]any{
		"ok":    false,
		"error": mediaError,
	})
}

func SimklAuthStart(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := new(SimklClient).FromSettings(cfg.Settings)
	if !client.Configured() {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "Simkl non configurato"})
		return
	}
	value, err := client.PinStart(r.Context())
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "data": value})
}

// FlaresolverrTest asks the configured FlareSolverr for its session list and
// reports the status it answers with.
func FlaresolverrTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	configured := ""
	if cfg.FlaresolverrURL != nil && strings.TrimSpace(*cfg.FlaresolverrURL) != "" {
		configured = *cfg.FlaresolverrURL
	}
	if configured == "" {
		jsonError(w, http.StatusBadRequest, "URL FlareSolverr non configurato")
		return
	}
	endpoint := flareSolverrEndpoint(configured)
	host := domainOf(configured)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	body, statusCode, err := HTTPPostJSON(ctx, endpoint, nil, []byte(`{"cmd": "sessions.list"}`))
	if err != nil {
		logging.Info("flaresolverr test", "host", host, "ok", false, "error", err.Error())
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if statusCode >= http.StatusBadRequest {
		err := fmt.Errorf("HTTP %d", statusCode)
		logging.Info("flaresolverr test", "host", host, "ok", false, "error", err.Error())
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		logging.Info("flaresolverr test", "host", host, "ok", false, "error", "invalid response")
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	status, _ := value["status"].(string)
	sessions, ok := value["sessions"]
	if !ok {
		sessions = []any{}
	}
	logging.Info("flaresolverr test", "host", host, "ok", status == "ok", "status", status)
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":       status == "ok",
		"status":   value["status"],
		"sessions": sessions,
	})
}

// SimklCalendar returns the user's Simkl calendar; it needs a configured and
// authenticated client.
func SimklCalendar(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := (&SimklClient{}).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		jsonError(w, http.StatusConflict, "Simkl non configurato")
		return
	}
	value, err := client.Calendar(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonStatus(w, http.StatusOK, value)
}

// SimklScrobble marks the items sent in the request as watched on Simkl.
func SimklScrobble(w http.ResponseWriter, r *http.Request, s *AppState) {
	if s.dryRun() {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "dry-run does not mark watched items"})
		return
	}
	if !(&SimklClient{}).FromSettings(latestConfig(s).Settings).Configured() {
		jsonStatus(w, http.StatusConflict, map[string]any{"ok": false, "error": "Simkl non configurato"})
		return
	}
	var input ScrobbleInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	client := (&SimklClient{}).FromSettings(cfg.Settings)
	value, err := client.MarkWatched(r.Context(), input.Payload)
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	jsonStatus(w, http.StatusOK, value)
}

// SimklSettings returns the Simkl options stored in settings, with their
// defaults, plus whether the client is configured and authenticated.
func SimklSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := new(SimklClient).FromSettings(cfg.Settings)
	clientID, hasClientID := cfg.Settings["simkl_client_id"]
	clientIDConfigured := hasClientID && clientID != ""
	watchlistStatus, ok := cfg.Settings["simkl_watchlist_status"]
	if !ok {
		watchlistStatus = "plantowatch"
	}
	calendarDays, ok := cfg.Settings["simkl_calendar_days"]
	if !ok {
		calendarDays = "7"
	}
	markWatched := false
	if value, ok := cfg.Settings["simkl_mark_watched"]; ok {
		markWatched = settingTruthy(value)
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"configured":           client.Configured(),
		"authenticated":        client.Authenticated(),
		"client_id":            clientID,
		"client_id_configured": clientIDConfigured,
		"watchlist_status":     watchlistStatus,
		"calendar_days":        calendarDays,
		"mark_watched":         markWatched,
	})
}

// SimklStatus reports whether Simkl is configured and authenticated.
func SimklStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := new(SimklClient).FromSettings(cfg.Settings)
	jsonResponse(w, map[string]any{
		"configured":    client.Configured(),
		"authenticated": client.Authenticated(),
	})
}

// SimklWatchlist returns the user's Simkl watchlist; it needs a configured and
// authenticated client.
func SimklWatchlist(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	client := (&SimklClient{}).FromSettings(cfg.Settings)
	if !client.Configured() || !client.Authenticated() {
		jsonError(w, http.StatusConflict, "Simkl non configurato")
		return
	}
	value, err := client.Watchlist(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonResponse(w, value)
}

func collectWatchlistEntries(value any, out *[][2]string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectWatchlistEntries(item, out)
		}
	case map[string]any:
		if raw, ok := typed["title"]; ok {
			if title, ok := raw.(string); ok {
				year := ""
				if y, ok := typed["year"].(float64); ok {
					year = fmt.Sprintf("%d", int64(y))
				} else if y, ok := typed["year"].(int64); ok {
					year = fmt.Sprintf("%d", y)
				}
				*out = append(*out, [2]string{title, year})
				return
			}
		}
		for _, child := range typed {
			collectWatchlistEntries(child, out)
		}
	}
}

func collectWatchlist(value any, out *[][3]string) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			collectWatchlist(item, out)
		}
	case map[string]any:
		typedFound := false
		for key, child := range typed {
			var kind string
			switch key {
			case "show", "shows", "anime", "series", "tv":
				kind = "series"
			case "movie", "movies":
				kind = "movie"
			default:
				continue
			}
			typedFound = true
			entries := [][2]string{}
			collectWatchlistEntries(child, &entries)
			for _, entry := range entries {
				*out = append(*out, [3]string{kind, entry[0], entry[1]})
			}
		}
		if !typedFound {
			for _, child := range typed {
				collectWatchlist(child, out)
			}
		}
	}
}

func applyWatchlistImport(cfg *Config, entries [][3]string) map[string]any {
	seriesAdded := 0
	moviesAdded := 0
	for _, entry := range entries {
		kind, title, year := entry[0], entry[1], entry[2]
		if strings.TrimSpace(title) == "" {
			continue
		}
		if kind == "series" {
			found := false
			for _, item := range cfg.Series {
				if strings.EqualFold(item.Name, title) {
					found = true
					break
				}
			}
			if found {
				continue
			}
			cfg.Series = append(cfg.Series, SeriesConfig{
				Name:     title,
				Seasons:  "1+",
				Language: "ita",
				Enabled:  true,
			})
			seriesAdded++
		} else {
			found := false
			for _, item := range cfg.Movies {
				if strings.EqualFold(item.Name, title) {
					found = true
					break
				}
			}
			if found {
				continue
			}
			cfg.Movies = append(cfg.Movies, MovieConfig{
				Name:     title,
				Year:     year,
				Language: "ita",
				Enabled:  true,
			})
			moviesAdded++
		}
	}
	return map[string]any{
		"series_added": seriesAdded,
		"movies_added": moviesAdded,
		"series_total": len(cfg.Series),
		"movies_total": len(cfg.Movies),
	}
}

// SimklAuthPoll handles POST /api/simkl/auth/poll.
func SimklAuthPoll(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input AuthCode
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.dryRun() {
		jsonError(w, http.StatusConflict, "dry-run does not save integration tokens")
		return
	}
	cfg := latestConfig(s)
	value, err := (&SimklClient{}).FromSettings(cfg.Settings).PinPoll(r.Context(), input.Code)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	access, err := TokenString(value, "access_token")
	if err != nil {
		jsonStatus(w, http.StatusBadGateway, map[string]any{
			"ok":    false,
			"error": "Simkl response did not contain an access token",
			"data":  value,
		})
		return
	}
	if err := saveConfigSetting(cfg.DataDir, "simkl_access_token", access); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "authenticated": true})
}

// SimklWatchlistImport handles POST /api/simkl/watchlist/import.
func SimklWatchlistImport(w http.ResponseWriter, r *http.Request, s *AppState) {
	live := latestConfig(s)
	cfg := *live
	cfg.Series = append([]SeriesConfig(nil), live.Series...)
	cfg.Movies = append([]MovieConfig(nil), live.Movies...)

	client := (&SimklClient{}).FromSettings(cfg.Settings)
	value, err := client.Watchlist(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	entries := [][3]string{}
	collectWatchlist(value, &entries)
	report := applyWatchlistImport(&cfg, entries)
	if err := SaveLibrary(s.cfg.DataDir, cfg.Series, cfg.Movies); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{
		"ok":     true,
		"source": "simkl",
		"found":  len(entries),
		"report": report,
	})
}

// providerURL maps a provider name to the base URL of the matching
// configured indexer, so the health table can link to its own site.
func providerURL(cfg *Config, provider string) string {
	if cfg == nil {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(provider))
	for index := range cfg.Indexers {
		if strings.ToLower(strings.TrimSpace(cfg.Indexers[index].Name)) != name {
			continue
		}
		base := strings.TrimRight(strings.TrimSpace(cfg.Indexers[index].URL), "/")
		base = strings.TrimSuffix(base, "/api")
		return base
	}
	return ""
}

func mirCrewIndexer(cfg *Config) *IndexerConfig {
	if cfg == nil {
		return nil
	}
	for index := range cfg.Indexers {
		if !cfg.Indexers[index].Enabled {
			continue
		}
		name := strings.ToLower(cfg.Indexers[index].Name)
		rawURL := strings.ToLower(cfg.Indexers[index].URL)
		if strings.Contains(name, "mircrew") || strings.Contains(rawURL, "mircrew") ||
			strings.Contains(rawURL, ":9118") {
			return &cfg.Indexers[index]
		}
	}
	return nil
}

// mirCrewLoginProbe reads the /status of the mircrew-indexer service at
// base and reports whether its forum login has failed. reachable is false when
// the service cannot be read at all (down, older version without /status, or a
// malformed reply): that is not a login failure and the caller must not read it
// as a recovery either.
func mirCrewLoginProbe(base string) (loginError string, failed bool, reachable bool) {
	if strings.TrimSpace(base) == "" {
		return "", false, false
	}
	status, reachable, body := servicesProbe(strings.TrimRight(base, "/")+"/status", 5*time.Second)
	if !reachable || status != http.StatusOK {
		return "", false, false
	}
	var payload struct {
		LoggedIn   *bool  `json:"logged_in"`
		LoginError string `json:"login_error"`
		LastRun    string `json:"last_run"`
	}
	if json.Unmarshal([]byte(body), &payload) != nil || payload.LoggedIn == nil {
		return "", false, false
	}
	loginError = strings.TrimSpace(payload.LoginError)
	switch {
	case loginError != "":
		return utils.RedactURLSecrets(loginError), true, true
	case !*payload.LoggedIn && strings.TrimSpace(payload.LastRun) != "":
		// Logged out after a pass had already run: the login was lost.
		return "", true, true
	}
	// Logged in, or still logging in right after the service started.
	return "", false, true
}
