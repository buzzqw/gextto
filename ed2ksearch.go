package gextto

// ed2ksearch.go is the search-plane half of the eD2k integration. It talks to
// the supervised gx-ed2k daemon (the same pattern as gxtorrent_engine.go) and
// turns its results into models.Release values. The transfer plane is separate
// and deliberately not connected while gx-ed2k reports "download": false: a
// release that cannot be downloaded must never enter the acquisition queue.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const (
	defaultEd2kURL            = "http://127.0.0.1:8899"
	defaultEd2kSearchTimeout  = 20
	ed2kHealthCacheTTL        = 30 * time.Second
	ed2kHealthProbeTimeout    = 2 * time.Second
	ed2kSearchResponseMaxBody = 8 << 20
)

// ed2kSettings is the resolved adapter configuration.
type ed2kSettings struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

func ed2kSettingsFromConfig(cfg *Config) (ed2kSettings, error) {
	if cfg == nil {
		return ed2kSettings{}, errors.New("eD2k configuration is unavailable")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Settings["ed2k_url"]), "/")
	if baseURL == "" {
		baseURL = defaultEd2kURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ed2kSettings{}, errors.New("ed2k_url must be a complete http(s) URL")
	}
	timeout := defaultEd2kSearchTimeout
	if raw := strings.TrimSpace(cfg.Settings["ed2k_search_timeout_secs"]); raw != "" {
		parsedTimeout, err := strconv.Atoi(raw)
		if err != nil || parsedTimeout < 5 || parsedTimeout > 120 {
			return ed2kSettings{}, errors.New("ed2k_search_timeout_secs must be between 5 and 120")
		}
		timeout = parsedTimeout
	}
	return ed2kSettings{
		BaseURL: baseURL,
		Token:   strings.TrimSpace(cfg.Settings["ed2k_token"]),
		Timeout: time.Duration(timeout) * time.Second,
	}, nil
}

// ed2kEnabled reports whether the search provider is switched on. The default
// is off: a fresh installation must not contact a new network on its own.
func ed2kEnabled(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	return settingIsTrue(cfg.Settings["ed2k_enabled"])
}

// ed2kFallbackEnabled is the automatic gap-filling switch. It is separate from
// ed2k_enabled and defaults to off, so eD2k can be searched by hand without
// ever changing which downloads Gextto starts by itself.
func ed2kFallbackEnabled(cfg *Config) bool {
	if cfg == nil || !ed2kEnabled(cfg) {
		return false
	}
	return settingIsTrue(cfg.Settings["ed2k_fallback"])
}

// ed2kUPnPEnabled reports whether the managed daemon should ask the router to
// forward the Kad UDP port. It defaults to on.
func ed2kUPnPEnabled(cfg *Config) bool {
	if cfg == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Settings["ed2k_upnp"])) {
	case "false", "no", "0", "off":
		return false
	}
	return true
}

// ed2kMinSources is the source floor the automatic fallback requires. A manual
// search shows every result; an automatic acquisition is more conservative.
func ed2kMinSources(cfg *Config) int64 {
	if cfg == nil {
		return 3
	}
	raw := strings.TrimSpace(cfg.Settings["ed2k_min_sources"])
	if raw == "" {
		return 3
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 || value > 1000 {
		return 3
	}
	return value
}

func settingIsTrue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "si", "sì":
		return true
	}
	return false
}

var (
	ed2kHealthMu    sync.Mutex
	ed2kHealthCache = map[string]ed2kHealthEntry{}
	ed2kHealthHTTP  = &http.Client{Timeout: ed2kHealthProbeTimeout}
)

type ed2kHealthEntry struct {
	checkedAt    time.Time
	healthy      bool
	download     bool
	version      string
	reachability string
	reachDetail  string
	lowID        bool
	upnp         map[string]any
	err          string
}

// ed2kHealth probes gx-ed2k's /api/v1/health, caching the answer briefly so a
// search never waits on a daemon that is down.
func ed2kHealth(cfg *Config) ed2kHealthEntry {
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return ed2kHealthEntry{checkedAt: time.Now(), err: err.Error()}
	}
	ed2kHealthMu.Lock()
	if cached, ok := ed2kHealthCache[settings.BaseURL]; ok && time.Since(cached.checkedAt) < ed2kHealthCacheTTL {
		ed2kHealthMu.Unlock()
		return cached
	}
	ed2kHealthMu.Unlock()

	entry := ed2kHealthEntry{checkedAt: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), ed2kHealthProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, settings.BaseURL+"/api/v1/health", nil)
	if err != nil {
		entry.err = err.Error()
	} else {
		if settings.Token != "" {
			request.Header.Set("X-Gx-Token", settings.Token)
		}
		response, err := ed2kHealthHTTP.Do(request)
		if err != nil {
			entry.err = err.Error()
		} else {
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				entry.err = fmt.Sprintf("health: HTTP %d", response.StatusCode)
			} else {
				var payload struct {
					OK           bool           `json:"ok"`
					Version      string         `json:"version"`
					Download     bool           `json:"download"`
					Reachability string         `json:"reachability"`
					LowID        bool           `json:"low_id"`
					UPnP         map[string]any `json:"upnp"`
				}
				if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
					entry.err = err.Error()
				} else {
					entry.healthy = payload.OK
					entry.download = payload.Download
					entry.version = payload.Version
					entry.reachability = payload.Reachability
					entry.lowID = payload.LowID
					entry.upnp = payload.UPnP
				}
			}
		}
	}
	ed2kHealthMu.Lock()
	ed2kHealthCache[settings.BaseURL] = entry
	ed2kHealthMu.Unlock()
	return entry
}

// ed2kHealthCached returns the last probe without waiting on the network. The
// settings page uses it so rendering never blocks on a daemon that is down.
func ed2kHealthCached(cfg *Config) (ed2kHealthEntry, bool) {
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return ed2kHealthEntry{}, false
	}
	ed2kHealthMu.Lock()
	entry, ok := ed2kHealthCache[settings.BaseURL]
	ed2kHealthMu.Unlock()
	return entry, ok
}

// ed2kInvalidateHealth drops the cached probe so a settings change takes
// effect immediately instead of after the cache TTL.
func ed2kInvalidateHealth() {
	ed2kHealthMu.Lock()
	ed2kHealthCache = map[string]ed2kHealthEntry{}
	ed2kHealthMu.Unlock()
}

// ed2kAvailability reports whether eD2k can be used for a manual search and
// whether it can also complete a download. The two answers are separate on
// purpose: search works long before the transfer engine exists.
func ed2kAvailability(cfg *Config) (search bool, download bool) {
	if !ed2kEnabled(cfg) {
		return false, false
	}
	health := ed2kHealth(cfg)
	return health.healthy, health.healthy && health.download
}

// ed2kFallbackEligible is the single gate for the automatic gap-filling
// fallback: enabled, explicitly allowed, and able to finish the download. If
// any part is false the deep pass never asks eD2k for a release.
func ed2kFallbackEligible(cfg *Config) bool {
	if !ed2kFallbackEnabled(cfg) {
		return false
	}
	_, download := ed2kAvailability(cfg)
	return download
}

// ed2kAPIFile is gx-ed2k's result shape.
type ed2kAPIFile struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Hash      string `json:"hash"`
	Link      string `json:"link"`
	Sources   int64  `json:"sources"`
	Origin    string `json:"origin"`
}

// SearchED2K queries gx-ed2k and normalizes its results. It returns nil when
// the provider is disabled, unreachable or finds nothing: a search-plane
// failure must never fail the torrent search that called it.
func SearchED2K(ctx context.Context, cfg *Config, query string) []models.Release {
	if cfg == nil || !ed2kEnabled(cfg) {
		return nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		logging.SourceFail("ed2k", "gx-ed2k", err.Error())
		return nil
	}
	timeout := settings.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	if timeout < time.Second {
		timeout = time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := settings.BaseURL + "/api/v1/search?q=" + url.QueryEscape(query) + "&timeout_ms=" + strconv.FormatInt(timeout.Milliseconds(), 10)
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		logging.SourceFail("ed2k", "gx-ed2k", err.Error())
		return nil
	}
	if settings.Token != "" {
		request.Header.Set("X-Gx-Token", settings.Token)
	}
	client := &http.Client{Timeout: timeout + 2*time.Second}
	response, err := client.Do(request)
	if err != nil {
		logging.SourceFail("ed2k", "gx-ed2k", err.Error())
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		logging.SourceFail("ed2k", "gx-ed2k", fmt.Sprintf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body))))
		return nil
	}
	var payload struct {
		OK      bool          `json:"ok"`
		Results []ed2kAPIFile `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, ed2kSearchResponseMaxBody)).Decode(&payload); err != nil {
		logging.SourceFail("ed2k", "gx-ed2k", err.Error())
		return nil
	}
	if !payload.OK {
		logging.SourceFail("ed2k", "gx-ed2k", "daemon answered ok=false")
		return nil
	}
	now := time.Now().UTC()
	releases := make([]models.Release, 0, len(payload.Results))
	for _, item := range payload.Results {
		name := strings.TrimSpace(item.Name)
		link := strings.TrimSpace(item.Link)
		if link == "" && ed2k.ValidHash(item.Hash) {
			link = ed2k.FileLink{Name: name, Size: item.SizeBytes, Hash: item.Hash}.String()
		}
		release := ParseEd2kRelease(name, link, "ed2k:"+strings.TrimSpace(item.Origin), item.SizeBytes, item.Sources, now)
		if release == nil {
			continue
		}
		releases = append(releases, *release)
	}
	logging.SourceOK("ed2k", "gx-ed2k", len(releases))
	return releases
}

// Ed2kStatus implements `GET /api/ed2k/status`: the eD2k provider state used
// by the settings page and by diagnostics.
func Ed2kStatus(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	jsonResponse(w, map[string]any{"ok": true, "provider": Ed2kRuntimeStatus(cfg)})
}

// SearchSeriesED2K is the gap-filling entry point. It builds the same
// `Name SxxExx` query as SearchSeriesEpisode, then keeps only the results that
// match the episode, the configured quality and the source floor. The caller
// (the deep gap pass) has already established that the torrent plane found
// nothing.
func (e *Engine) SearchSeriesED2K(ctx context.Context, cfg *Config, series *SeriesConfig, season, episode int64, manual bool) []models.Release {
	if series == nil || season < 1 || episode < 1 || !ed2kEnabled(cfg) {
		return nil
	}
	query := fmt.Sprintf("%s S%02dE%02d", series.Name, season, episode)
	candidates := SearchED2K(ctx, cfg, query)
	if manual {
		return candidates
	}
	minSources := ed2kMinSources(cfg)
	accepted := make([]models.Release, 0, len(candidates))
	for index := range candidates {
		release := candidates[index]
		if !releaseMatchesConfiguredEpisode(&release, series, season, episode) {
			continue
		}
		if release.Quality.Resolution == "" || strings.EqualFold(release.Quality.Resolution, "unknown") {
			continue
		}
		if release.Sources < minSources {
			continue
		}
		canonicalName := series.Name
		release.Series = &canonicalName
		accepted = append(accepted, release)
	}
	return accepted
}
