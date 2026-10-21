package gextto

// ed2ksearch.go connects Gextto to the eD2k and Kad networks through aMule.
// amuled does the networking (servers, Kad, sources, transfers); Gextto talks
// to it over the External Connections protocol (internal/amule) to search,
// queue ed2k:// links, follow the downloads and show the connection state.
// Nothing here is required for torrents: when aMule is off or unreachable the
// eD2k features are simply unavailable.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const (
	defaultAmuleHost         = "127.0.0.1"
	defaultAmulePort         = 4712
	defaultEd2kSearchTimeout = 20
	ed2kStatusCacheTTL       = 10 * time.Second
	// ed2kDownBackoff keeps the UI and the background loop from waiting on a
	// dial timeout at every refresh while amuled is down.
	ed2kDownBackoff = 15 * time.Second
	ed2kRequestTime = 8 * time.Second
)

// Networks a file can be downloaded from.
const (
	networkTorrent = "torrent"
	networkEd2k    = "ed2k"
)

// stopRetiredGxEd2k stops the gx-ed2k daemon an older Gextto started in a
// systemd scope. The scope outlives Gextto restarts on purpose, so without
// this it would keep running (and keep UDP 4672, aMule's Kad port) after the
// update that removed it.
func stopRetiredGxEd2k() {
	defer recoverGoroutine("stop retired gx-ed2k")
	if !gxCanUseScope() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "systemctl", "--user", "stop", "gextto-gx-ed2k-*.scope").Run(); err != nil {
		logging.Debug("no retired gx-ed2k scope to stop", "error", err)
	}
}

// ed2kSettings is the resolved aMule configuration.
type ed2kSettings struct {
	Address  string
	Password string
	Network  uint8
	Timeout  time.Duration
}

func ed2kSettingsFromConfig(cfg *Config) (ed2kSettings, error) {
	if cfg == nil {
		return ed2kSettings{}, errors.New("eD2k configuration is unavailable")
	}
	host := strings.TrimSpace(cfg.Settings["amule_host"])
	if host == "" {
		host = defaultAmuleHost
	}
	port := defaultAmulePort
	if raw := strings.TrimSpace(cfg.Settings["amule_port"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			return ed2kSettings{}, errors.New("amule_port must be between 1 and 65535")
		}
		port = parsed
	}
	timeout := defaultEd2kSearchTimeout
	if raw := strings.TrimSpace(cfg.Settings["ed2k_search_timeout_secs"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 5 || parsed > 120 {
			return ed2kSettings{}, errors.New("ed2k_search_timeout_secs must be between 5 and 120")
		}
		timeout = parsed
	}
	return ed2kSettings{
		Address:  net.JoinHostPort(host, strconv.Itoa(port)),
		Password: cfg.Settings["amule_password"],
		Network:  ed2kSearchNetwork(cfg.Settings["ed2k_search_network"]),
		Timeout:  time.Duration(timeout) * time.Second,
	}, nil
}

// ed2kSearchBoth searches Kad and then every eD2k server (global search) and
// merges the two result lists; it is Gextto's, not an aMule search type.
const ed2kSearchBoth uint8 = 0xFF

// ed2kSearchNetwork maps the setting to an aMule search type. Kad is the
// default: it does not depend on the server the core is connected to.
func ed2kSearchNetwork(value string) uint8 {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "global":
		return amule.SearchGlobal
	case "local", "server":
		return amule.SearchLocal
	case "both":
		return ed2kSearchBoth
	}
	return amule.SearchKad
}

// ed2kEnabled reports whether the aMule integration is switched on. The
// default is off: a fresh installation must not contact a new network.
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

// ed2kState is the shared client and the last known state of the core.
var ed2kState struct {
	mu       sync.Mutex
	key      string
	client   *amule.Client
	status   amule.Status
	checked  time.Time
	err      string
	failedAt time.Time
	incoming string
}

// ed2kSearchMu serialises searches: amuled keeps a single result list, so a
// second search would replace the first one's results.
var ed2kSearchMu sync.Mutex

// ed2kClient returns the client for the configured core, reusing the open
// session while the address and password stay the same.
func ed2kClient(cfg *Config) (*amule.Client, error) {
	if !ed2kEnabled(cfg) {
		return nil, errors.New("eD2k/aMule is disabled")
	}
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	key := settings.Address + "\x00" + settings.Password
	ed2kState.mu.Lock()
	defer ed2kState.mu.Unlock()
	if ed2kState.client == nil || ed2kState.key != key {
		if ed2kState.client != nil {
			_ = ed2kState.client.Close()
		}
		ed2kState.client = amule.New(amule.Config{
			Address:       settings.Address,
			Password:      settings.Password,
			ClientName:    "Gextto",
			ClientVersion: constants.AppVersion(),
			Timeout:       ed2kRequestTime,
		})
		ed2kState.key = key
		ed2kState.checked = time.Time{}
		ed2kState.failedAt = time.Time{}
		ed2kState.incoming = ""
	}
	return ed2kState.client, nil
}

// ed2kRememberIncoming updates the cached incoming folder from a preferences
// read, so a change made in aMule is picked up without restarting Gextto once
// the settings or eD2k page has been opened.
func ed2kRememberIncoming(dir string) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	ed2kState.mu.Lock()
	ed2kState.incoming = dir
	ed2kState.mu.Unlock()
}

// ed2kPreferences reads aMule's directories and options.
func ed2kPreferences(cfg *Config) (amule.Preferences, error) {
	var prefs amule.Preferences
	err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		value, callErr := client.Preferences(ctx)
		prefs = value
		return callErr
	})
	return prefs, err
}

// ed2kCall runs one request against the core. While the core is known to be
// down it fails fast instead of waiting on a dial timeout.
func ed2kCall(cfg *Config, call func(ctx context.Context, client *amule.Client) error) error {
	client, err := ed2kClient(cfg)
	if err != nil {
		return err
	}
	ed2kState.mu.Lock()
	failedAt, lastErr := ed2kState.failedAt, ed2kState.err
	ed2kState.mu.Unlock()
	if !failedAt.IsZero() && time.Since(failedAt) < ed2kDownBackoff {
		return fmt.Errorf("aMule unreachable: %s", lastErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), ed2kRequestTime)
	defer cancel()
	err = call(ctx, client)
	ed2kState.mu.Lock()
	defer ed2kState.mu.Unlock()
	if err != nil && ed2kTransportError(err) {
		ed2kState.failedAt = time.Now()
		ed2kState.err = err.Error()
		ed2kState.checked = time.Time{}
	} else if err == nil {
		ed2kState.failedAt = time.Time{}
	}
	return err
}

// ed2kTransportError tells a dead or refusing core apart from a command the
// core answered with an error (which says nothing about reachability).
func ed2kTransportError(err error) bool {
	if errors.Is(err, amule.ErrAuth) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "amule: connect") || strings.Contains(message, "amule: send") ||
		strings.Contains(message, "amule: receive")
}

// ed2kInvalidateHealth drops the cached state so a settings change takes
// effect immediately.
func ed2kInvalidateHealth() {
	ed2kState.mu.Lock()
	ed2kState.checked = time.Time{}
	ed2kState.failedAt = time.Time{}
	ed2kState.incoming = ""
	ed2kState.mu.Unlock()
}

// ed2kStatus returns the core state, cached for a few seconds.
func ed2kStatus(cfg *Config) (amule.Status, error) {
	if !ed2kEnabled(cfg) {
		return amule.Status{}, errors.New("eD2k/aMule is disabled")
	}
	ed2kState.mu.Lock()
	if !ed2kState.checked.IsZero() && time.Since(ed2kState.checked) < ed2kStatusCacheTTL {
		status, lastErr := ed2kState.status, ed2kState.err
		ed2kState.mu.Unlock()
		if lastErr != "" {
			return status, errors.New(lastErr)
		}
		return status, nil
	}
	ed2kState.mu.Unlock()
	var status amule.Status
	err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		status, callErr = client.Status(ctx)
		return callErr
	})
	ed2kState.mu.Lock()
	ed2kState.status = status
	ed2kState.checked = time.Now()
	if err != nil {
		ed2kState.err = err.Error()
	} else {
		ed2kState.err = ""
	}
	ed2kState.mu.Unlock()
	return status, err
}

// ed2kStatusCached returns the last state without contacting the core, for
// pages that must render even when amuled hangs.
func ed2kStatusCached() (amule.Status, string, bool) {
	ed2kState.mu.Lock()
	defer ed2kState.mu.Unlock()
	return ed2kState.status, ed2kState.err, !ed2kState.checked.IsZero()
}

// ed2kIncomingDir is where amuled puts completed files (cached: it only
// changes when the core's preferences change).
func ed2kIncomingDir(cfg *Config) string {
	ed2kState.mu.Lock()
	cached := ed2kState.incoming
	ed2kState.mu.Unlock()
	if cached != "" {
		return cached
	}
	var prefs amule.Preferences
	if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		prefs, callErr = client.Preferences(ctx)
		return callErr
	}); err != nil {
		return ""
	}
	incoming := prefs.IncomingDir
	// A remote amuled (another host or container) reports its own path: the
	// mapping brings it into Gextto's view before the import reads it.
	if mappings, err := ParsePathMappings(cfg.Settings["amule_path_mappings"]); err == nil {
		if mapped, ok := TranslateBackendToGextto(incoming, mappings); ok {
			incoming = mapped
		}
	}
	ed2kState.mu.Lock()
	ed2kState.incoming = incoming
	ed2kState.mu.Unlock()
	return incoming
}

// ed2kAvailability reports whether eD2k can be searched and downloaded: both
// need a reachable, authenticated aMule core.
func ed2kAvailability(cfg *Config) (search bool, download bool) {
	if !ed2kEnabled(cfg) {
		return false, false
	}
	_, err := ed2kStatus(cfg)
	return err == nil, err == nil
}

// ed2kFallbackEligible is the single gate for the automatic gap-filling
// fallback: enabled, explicitly allowed and aMule reachable.
func ed2kFallbackEligible(cfg *Config) bool {
	if !ed2kFallbackEnabled(cfg) {
		return false
	}
	_, download := ed2kAvailability(cfg)
	return download
}

// ed2kManualEpisodeFallback is the manual-search counterpart of the automatic
// fallback: when the torrent plane found nothing for an episode, it asks eD2k
// for it too. It shares the same explicit switch (ed2k_fallback), uses the
// network chosen in the settings and keeps every candidate (no source floor):
// a manual search only shows results, it does not start anything by itself.
// The canonical series name is set on every release so the caller can keep its
// usual matching and dedup rules.
func ed2kManualEpisodeFallback(ctx context.Context, engine *Engine, cfg *Config, series *SeriesConfig, season, episode int64) []models.Release {
	if engine == nil || series == nil || !ed2kFallbackEligible(cfg) {
		return nil
	}
	started := time.Now()
	candidates := engine.SearchSeriesED2K(ctx, cfg, series, season, episode, true)
	accepted := make([]models.Release, 0, len(candidates))
	for index := range candidates {
		release := candidates[index]
		// Match on the parsed name first: a different show with the same
		// episode number ("Lost Girl" for "Lost") must not pass as this one.
		if !releaseMatchesConfiguredEpisode(&release, series, season, episode) {
			continue
		}
		name := series.Name
		release.Series = &name
		accepted = append(accepted, release)
	}
	if len(accepted) > 0 || time.Since(started) > 2*time.Second {
		logging.Info("manual eD2k fallback",
			"series", series.Name,
			"season", season,
			"episode", episode,
			"candidates", len(accepted),
			"elapsed_ms", time.Since(started).Milliseconds())
	}
	return accepted
}

// SearchED2K searches eD2k/Kad through aMule and normalizes the results. It
// returns nil when the integration is disabled, unreachable or finds nothing:
// an eD2k failure must never fail the torrent search that called it.
func SearchED2K(ctx context.Context, cfg *Config, query string) []models.Release {
	if !ed2kEnabled(cfg) {
		return nil
	}
	results, err := searchAmule(ctx, cfg, query, nil)
	if err != nil {
		logging.SourceFail("ed2k", "aMule", err.Error())
		return nil
	}
	releases := ed2kReleases(results, "ed2k:amule")
	logging.SourceOK("ed2k", "aMule", len(releases))
	return releases
}

// SearchED2KManual is the search behind the eD2k checkbox of the manual
// search. It needs the integration switched on, like every eD2k feature.
func SearchED2KManual(ctx context.Context, cfg *Config, query string) []models.Release {
	return SearchED2K(ctx, cfg, query)
}

// searchAmule runs one search on the configured network. network overrides
// the setting when it is not nil.
func searchAmule(ctx context.Context, cfg *Config, query string, network *uint8) ([]amule.SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	client, err := ed2kClient(cfg)
	if err != nil {
		return nil, err
	}
	searchType := settings.Network
	if network != nil {
		searchType = *network
	}
	if searchType == ed2kSearchBoth {
		// Kad and the servers index different files: ask both and keep each
		// file once, with the larger source count.
		kad, kadErr := searchAmuleOnce(ctx, settings, client, query, amule.SearchKad)
		global, globalErr := searchAmuleOnce(ctx, settings, client, query, amule.SearchGlobal)
		if kadErr != nil && globalErr != nil {
			return nil, kadErr
		}
		return mergeEd2kResults(kad, global), nil
	}
	return searchAmuleOnce(ctx, settings, client, query, searchType)
}

// mergeEd2kResults joins result lists by MD4 hash, keeping the larger source
// count, sorted by sources.
func mergeEd2kResults(groups ...[]amule.SearchResult) []amule.SearchResult {
	byHash := map[string]int{}
	var out []amule.SearchResult
	for _, group := range groups {
		for _, result := range group {
			if index, seen := byHash[result.Hash]; seen {
				if result.Sources > out[index].Sources {
					out[index].Sources = result.Sources
				}
				if result.CompleteSources > out[index].CompleteSources {
					out[index].CompleteSources = result.CompleteSources
				}
				continue
			}
			byHash[result.Hash] = len(out)
			out = append(out, result)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sources > out[j].Sources })
	return out
}

func searchAmuleOnce(ctx context.Context, settings ed2kSettings, client *amule.Client, query string, searchType uint8) ([]amule.SearchResult, error) {
	wait := settings.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline) - time.Second; remaining < wait {
			wait = remaining
		}
	}
	if wait < 2*time.Second {
		wait = 2 * time.Second
	}
	ed2kSearchMu.Lock()
	defer ed2kSearchMu.Unlock()
	searchCtx, cancel := context.WithTimeout(ctx, wait+ed2kRequestTime)
	defer cancel()
	return client.Search(searchCtx, query, amule.SearchOptions{Type: searchType, Wait: wait})
}

func ed2kReleases(results []amule.SearchResult, source string) []models.Release {
	now := time.Now().UTC()
	releases := make([]models.Release, 0, len(results))
	for _, item := range results {
		release := ParseEd2kRelease(strings.TrimSpace(item.Name), item.Link(), source, item.Size, item.Sources, now)
		if release == nil {
			continue
		}
		releases = append(releases, *release)
	}
	return releases
}

// Ed2kRuntimeStatus is the state shown by diagnostics and the settings page.
func Ed2kRuntimeStatus(cfg *Config) map[string]any {
	enabled := ed2kEnabled(cfg)
	out := map[string]any{"enabled": enabled, "reachable": false, "download": false}
	if !enabled {
		return out
	}
	status, err := ed2kStatus(cfg)
	if err != nil {
		out["last_error"] = err.Error()
		return out
	}
	out["reachable"] = true
	out["download"] = true
	out["status"] = status
	return out
}

// Ed2kStatus implements `GET /api/ed2k/status`.
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
	// The automatic fallback asks both Kad and the eD2k servers (global
	// search): they index different files, and a gap is worth the extra wait.
	// A manual search uses the network chosen in the settings.
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return nil
	}
	network := settings.Network
	if !manual {
		network = ed2kSearchBoth
	}
	candidates := e.searchED2KSeriesQueries(ctx, cfg, series, network, func(name string) string {
		return fmt.Sprintf("%s S%02dE%02d", name, season, episode)
	})
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
		if link, err := ed2k.ParseFileLink(release.Ed2kLink); err != nil || !isVideoExtension(link.Name) {
			// An archive or a sample in another format cannot be imported.
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

// SearchSeasonED2K looks up a whole season with one query ("Name Sxx"). The
// manual series missing search uses it so aMule runs one search per season
// instead of one per episode, which keeps the wait bounded.
func (e *Engine) SearchSeasonED2K(ctx context.Context, cfg *Config, series *SeriesConfig, season int64) []models.Release {
	if series == nil || season < 1 || !ed2kEnabled(cfg) {
		return nil
	}
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return nil
	}
	return e.searchED2KSeriesQueries(ctx, cfg, series, settings.Network, func(name string) string {
		return fmt.Sprintf("%s S%02d", name, season)
	})
}

// searchED2KSeriesQueries runs one eD2k search per configured name and alias
// and merges the results by release identity. A library often uses the
// localized title while eD2k files carry the original one ("I misteri di
// Murdoch" vs "Murdoch Mysteries"). aMule runs one search at a time, so the
// names are searched one after the other.
func (e *Engine) searchED2KSeriesQueries(ctx context.Context, cfg *Config, series *SeriesConfig, network uint8, query func(name string) string) []models.Release {
	names := make([]string, 0, 1+len(series.Aliases))
	seenName := map[string]struct{}{}
	for _, name := range append([]string{series.Name}, series.Aliases...) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seenName[key]; exists {
			continue
		}
		seenName[key] = struct{}{}
		names = append(names, name)
	}
	candidates := make([]models.Release, 0)
	seenRelease := map[string]struct{}{}
	for _, name := range names {
		if ctx.Err() != nil {
			break
		}
		results, err := searchAmule(ctx, cfg, query(name), &network)
		if err != nil {
			logging.SourceFail("ed2k", "aMule", err.Error())
			continue
		}
		for _, release := range ed2kReleases(results, "ed2k:amule") {
			key, ok := releaseDedupKey(&release)
			if !ok {
				continue
			}
			if _, exists := seenRelease[key]; exists {
				continue
			}
			seenRelease[key] = struct{}{}
			candidates = append(candidates, release)
		}
	}
	return candidates
}

// releaseMatchesConfiguredSeason reports whether the release belongs to the
// series/alias and to the requested season.
func releaseMatchesConfiguredSeason(release *models.Release, series *SeriesConfig, season int64) bool {
	if release == nil || series == nil || release.Kind != "series" || release.Series == nil || release.Season == nil {
		return false
	}
	if *release.Season != season {
		return false
	}
	if SeriesNamesMatch(series.Name, *release.Series) {
		return true
	}
	for _, alias := range series.Aliases {
		if SeriesNamesMatch(alias, *release.Series) {
			return true
		}
	}
	return false
}

// ed2kManualSeasonFallback is the batch version of ed2kManualEpisodeFallback
// for the series missing search: one query per season, then the caller
// distributes the results to the gaps. It is gated by the same explicit switch.
func ed2kManualSeasonFallback(ctx context.Context, engine *Engine, cfg *Config, series *SeriesConfig, season int64) []models.Release {
	if engine == nil || series == nil || !ed2kFallbackEligible(cfg) {
		return nil
	}
	started := time.Now()
	candidates := engine.SearchSeasonED2K(ctx, cfg, series, season)
	accepted := make([]models.Release, 0, len(candidates))
	for index := range candidates {
		release := candidates[index]
		if !releaseMatchesConfiguredSeason(&release, series, season) {
			continue
		}
		name := series.Name
		release.Series = &name
		accepted = append(accepted, release)
	}
	if len(accepted) > 0 || time.Since(started) > 2*time.Second {
		logging.Info("manual eD2k season fallback",
			"series", series.Name,
			"season", season,
			"candidates", len(accepted),
			"elapsed_ms", time.Since(started).Milliseconds())
	}
	return accepted
}
