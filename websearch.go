package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/utils"

	"golang.org/x/net/html"
)

var websearchTrackers = [3]string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

// webEngineLimiter must be shared by *all* title searches. A limiter allocated
// per query only caps one query; with several scheduled queries it still permits
// dozens of simultaneous HTML fetches and parsers.
var webEngineLimiter = make(chan struct{}, 4)

var (
	engineFailuresMu sync.Mutex
	engineFailures   = map[string]int{}
)

// EngineFailure is one drained per-cycle web-engine failure aggregate.
type EngineFailure struct {
	Engine string `json:"engine"`
	Count  int    `json:"count"`
}

// TakeEngineFailures drains the accumulated web-engine failures
// (`engine -> count`), most frequent first.
func TakeEngineFailures() []EngineFailure {
	engineFailuresMu.Lock()
	failures := engineFailures
	engineFailures = map[string]int{}
	engineFailuresMu.Unlock()
	result := make([]EngineFailure, 0, len(failures))
	for engine, count := range failures {
		result = append(result, EngineFailure{Engine: engine, Count: count})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Count > result[j].Count })
	return result
}

var (
	engineCooldownMu sync.Mutex
	engineCooldown   = map[string]time.Time{}
)

func engineInCooldown(engine string) bool {
	now := time.Now()
	engineCooldownMu.Lock()
	defer engineCooldownMu.Unlock()
	until, ok := engineCooldown[engine]
	if !ok {
		return false
	}
	if until.After(now) {
		return true
	}
	delete(engineCooldown, engine)
	return false
}

func setEngineCooldown(engine, errorText string) {
	// Rate-limit: pausa più lunga. Altri errori (parsing/timeout): pausa breve.
	duration := 15 * time.Minute
	if strings.Contains(errorText, "429") {
		duration = 30 * time.Minute
	}
	engineCooldownMu.Lock()
	engineCooldown[engine] = time.Now().Add(duration)
	engineCooldownMu.Unlock()
}

// webResult is the raw `(title, magnet, source)` tuple returned by an engine.
type webResult struct {
	title  string
	magnet string
	source string
}

// Search runs a web search over the configured engines with no overall budget.
func Search(ctx context.Context, cfg *Config, query string) []models.Release {
	return SearchWithTimeout(ctx, cfg, query, nil)
}

// SearchWithTimeout runs a web search with an optional per-source budget: each
// engine gets that long once it starts, so a source queued behind the shared
// limiter is not cut off before it has had its turn. Answers already received
// are always kept.
func SearchWithTimeout(ctx context.Context, cfg *Config, query string, timeout *time.Duration) []models.Release {
	raw := searchWithTimeoutRaw(ctx, cfg, query, timeout)
	var releases []models.Release
	for _, item := range raw {
		if release := ParseRelease(item.title, item.magnet, item.source); release != nil {
			releases = append(releases, *release)
		}
	}
	return releases
}

func knownWebEngine(engine string) bool {
	switch engine {
	case "bitsearch", "tpb", "thepiratebay", "knaben", "nyaa", "eztv", "btdig",
		"torrentscsv", "limetorrents", "torrentz2", "bt4g", "1337x", "1337":
		return true
	default:
		return false
	}
}

func searchWithTimeoutRaw(ctx context.Context, cfg *Config, query string, timeout *time.Duration) []webResult {
	var engines []string
	var flaresolverr *string
	if cfg != nil {
		ConfigureCloudflareState(cfg.DataDir)
		engines = cfg.WebsearchEngines
		if cfg.FlaresolverrURL != nil && strings.TrimSpace(*cfg.FlaresolverrURL) != "" {
			flaresolverr = cfg.FlaresolverrURL
		}
	}
	type outcome struct {
		found   []webResult
		failure string
	}
	resultsChannel := make(chan outcome, len(engines))
	active := 0
	for _, rawEngine := range engines {
		engine := strings.ToLower(rawEngine)
		if !knownWebEngine(engine) {
			continue
		}
		// Motore in raffreddamento dopo errori recenti: salta per non ripetere
		// la stessa richiesta a vuoto ad ogni query.
		if engineInCooldown(engine) {
			continue
		}
		active++
		go func(engine string) {
			// The collector waits for one outcome per engine: a panic must still
			// deliver one (the channel is buffered for every engine).
			defer func() {
				if r := recover(); r != nil {
					logging.Error("web engine search panicked; recovered",
						"engine", engine, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
					resultsChannel <- outcome{failure: engine}
				}
			}()
			select {
			case webEngineLimiter <- struct{}{}:
			case <-ctx.Done():
				resultsChannel <- outcome{}
				return
			}
			defer func() { <-webEngineLimiter }()
			engineCtx := ctx
			if timeout != nil {
				var cancel context.CancelFunc
				engineCtx, cancel = context.WithTimeout(ctx, *timeout)
				defer cancel()
			}
			found, err := runWebEngine(engineCtx, engine, query, flaresolverr)
			if err != nil {
				if engineCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
					// This source did not answer within its own budget. Keep the
					// answers from the faster ones, and do not put a healthy
					// source in cooldown because it was merely slow.
					logging.Debug("web engine search timed out", "engine", engine, "query", query, "budget", logDuration(*timeout))
					resultsChannel <- outcome{}
					return
				}
				redacted := utils.RedactURLSecrets(err.Error())
				logging.SourceFail("web", engine, redacted)
				// A cancelled or expired parent context is not the engine's
				// fault: never put a healthy engine in cooldown for it.
				if ctx.Err() == nil {
					setEngineCooldown(engine, redacted)
				}
				logging.Debug("web engine search failed", "engine", engine, "query", query, "error", redacted)
				resultsChannel <- outcome{failure: engine}
				return
			}
			logging.SourceOK("web", engine, len(found))
			logging.Debug("web engine search completed", "engine", engine, "query", query, "results", len(found))
			resultsChannel <- outcome{found: found}
		}(engine)
	}
	var results []webResult
collect:
	for completed := 0; completed < active; completed++ {
		select {
		case item := <-resultsChannel:
			results = append(results, item.found...)
			if item.failure != "" {
				// Aggregate per cycle instead of logging once per query.
				engineFailuresMu.Lock()
				engineFailures[item.failure]++
				engineFailuresMu.Unlock()
			}
		case <-ctx.Done():
			// The caller gave up: return what is already here. Every engine
			// still sends into the buffered channel, so none is left blocked.
			break collect
		}
	}
	seen := map[string]struct{}{}
	kept := results[:0]
	for _, item := range results {
		hash, ok := utils.MagnetHash(item.magnet)
		if !ok {
			continue
		}
		if _, exists := seen[hash]; exists {
			continue
		}
		seen[hash] = struct{}{}
		kept = append(kept, item)
	}
	return kept
}

func runWebEngine(ctx context.Context, engine, query string, flaresolverr *string) ([]webResult, error) {
	switch engine {
	case "bitsearch":
		return searchBitsearch(ctx, query)
	case "tpb", "thepiratebay":
		return searchTpb(ctx, query)
	case "knaben":
		return searchKnaben(ctx, query)
	case "nyaa":
		return searchNyaa(ctx, query, flaresolverr)
	case "eztv":
		return searchEztv(ctx, query)
	case "btdig":
		return searchBtdig(ctx, query, flaresolverr)
	case "torrentscsv":
		return searchTorrentscsv(ctx, query)
	case "limetorrents":
		return searchLimetorrents(ctx, query, flaresolverr)
	case "torrentz2":
		return searchTorrentz2(ctx, query, flaresolverr)
	case "bt4g":
		return searchBt4g(ctx, query, flaresolverr)
	case "1337x", "1337":
		return search1337x(ctx, query, flaresolverr)
	default:
		return nil, nil
	}
}

// fetchJSON performs a GET and decodes a JSON body (numbers preserved).
func fetchJSON(ctx context.Context, rawURL string) (any, error) {
	response, err := httpDo(ctx, defaultHTTPClient, http.MethodGet, rawURL, nil, nil, "")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// fetchJSONPost performs a POST with a JSON body and decodes the response.
func fetchJSONPost(ctx context.Context, rawURL string, payload []byte) (any, error) {
	response, err := httpDo(ctx, defaultHTTPClient, http.MethodPost, rawURL, nil, payload, "application/json")
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func arrayValue(value any) []any {
	items, _ := value.([]any)
	return items
}

func objectValue(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

// encodeQueryPairs form-urlencodes ordered key/value pairs (space -> `+`).
func encodeQueryPairs(pairs [][2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		parts = append(parts, url.QueryEscape(pair[0])+"="+url.QueryEscape(pair[1]))
	}
	return strings.Join(parts, "&")
}

func webContainsInt64(values []int64, needle int64) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func searchBitsearch(ctx context.Context, query string) ([]webResult, error) {
	rawURL := "https://bitsearch.to/api/v1/search?" + encodeQueryPairs([][2]string{
		{"q", query}, {"fuv", "yes"}, {"limit", "20"},
	})
	value, err := fetchJSON(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var results []webResult
	for _, entry := range arrayValue(objectValue(value)["results"]) {
		item := objectValue(entry)
		title := strings.TrimSpace(jsonStringValue(item["title"]))
		hash := strings.TrimSpace(jsonStringValue(item["infohash"]))
		if title == "" || len(hash) != 40 {
			continue
		}
		if magnet := buildMagnet(hash, title); magnet != "" {
			results = append(results, webResult{title: title, magnet: magnet, source: "BitSearch"})
		}
	}
	return results, nil
}

func searchTpb(ctx context.Context, query string) ([]webResult, error) {
	rawURL := "https://apibay.org/q.php?" + encodeQueryPairs([][2]string{
		{"q", query}, {"cat", "0"},
	})
	value, err := fetchJSON(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var results []webResult
	for _, entry := range arrayValue(value) {
		item := objectValue(entry)
		title := strings.TrimSpace(jsonStringValue(item["name"]))
		hash := strings.TrimSpace(jsonStringValue(item["info_hash"]))
		if title == "" || len(hash) != 40 || allZeros(hash) {
			continue
		}
		if magnet := buildMagnet(hash, title); magnet != "" {
			results = append(results, webResult{title: title, magnet: magnet, source: "ThePirateBay"})
		}
	}
	return results, nil
}

func allZeros(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] != '0' {
			return false
		}
	}
	return true
}

func searchKnaben(ctx context.Context, query string) ([]webResult, error) {
	payload, err := json.Marshal(map[string]any{
		"query":          query,
		"from":           0,
		"size":           20,
		"hideNsfw":       true,
		"orderBy":        "seeders",
		"orderDirection": "desc",
	})
	if err != nil {
		return nil, err
	}
	value, err := fetchJSONPost(ctx, "https://api.knaben.org/v1", payload)
	if err != nil {
		return nil, err
	}
	var results []webResult
	for _, entry := range arrayValue(objectValue(value)["hits"]) {
		item := objectValue(entry)
		title := strings.TrimSpace(jsonStringValue(item["title"]))
		magnet := strings.TrimSpace(jsonStringValue(item["magnetUrl"]))
		if title == "" {
			continue
		}
		if _, ok := utils.SanitizeMagnet(magnet, &title); ok {
			results = append(results, webResult{title: title, magnet: magnet, source: "Knaben"})
		}
	}
	return results, nil
}

func searchNyaa(ctx context.Context, query string, flaresolverr *string) ([]webResult, error) {
	rawURL := "https://nyaa.si/?page=rss&q=" + url.QueryEscape(query)
	releases, err := FetchFeed(ctx, rawURL, flaresolverr, 3, 0, 0.8)
	if err != nil {
		return nil, err
	}
	results := make([]webResult, 0, len(releases))
	for _, release := range releases {
		results = append(results, webResult{title: release.Title, magnet: release.Magnet, source: "Nyaa"})
	}
	return results, nil
}

func searchEztv(ctx context.Context, query string) ([]webResult, error) {
	regex := utils.MustCachedRegex(`(?i)^(.+?)\s+s(\d{1,2})e(\d{1,3})\b`)
	captures := regex.FindStringSubmatch(query)
	if captures == nil {
		return nil, nil
	}
	expectedSeries := strings.TrimSpace(captures[1])
	expectedSeason, err := strconv.ParseInt(captures[2], 10, 64)
	if err != nil {
		return nil, nil
	}
	expectedEpisode, err := strconv.ParseInt(captures[3], 10, 64)
	if err != nil {
		return nil, nil
	}
	rawURL := "https://eztvx.to/api/get-torrents?" + encodeQueryPairs([][2]string{
		{"limit", "100"}, {"page", "1"}, {"keywords", query},
	})
	value, err := fetchJSON(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var output []webResult
	for _, entry := range arrayValue(objectValue(value)["torrents"]) {
		item := objectValue(entry)
		title := strings.TrimSpace(jsonStringValue(item["title"]))
		if title == "" {
			title = strings.TrimSpace(jsonStringValue(item["filename"]))
		}
		hash := strings.TrimSpace(jsonStringValue(item["hash"]))
		magnet := strings.TrimSpace(jsonStringValue(item["magnet_url"]))
		if magnet == "" && len(hash) == 40 {
			magnet = buildMagnet(hash, title)
		}
		if title != "" {
			if magnet == "" {
				continue
			}
			accepted := false
			if release := ParseRelease(title, magnet, "EZTV"); release != nil {
				accepted = release.Kind == "series" &&
					release.Season != nil && *release.Season == expectedSeason &&
					webContainsInt64(release.EpisodeRange, expectedEpisode) &&
					release.Series != nil && SeriesNamesMatch(expectedSeries, *release.Series)
			}
			if accepted {
				output = append(output, webResult{title: title, magnet: magnet, source: "EZTV"})
			}
		}
		if len(output) >= 20 {
			break
		}
	}
	return output, nil
}

func searchBtdig(ctx context.Context, query string, flaresolverr *string) ([]webResult, error) {
	rawURL := "https://btdig.com/search?q=" + url.QueryEscape(query) + "&order=0&p=0"
	body, err := fetchHtml(ctx, rawURL, flaresolverr)
	if err != nil {
		return nil, err
	}
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	var output []webResult
	for _, link := range htmlSelectAll(document, "a[href^='magnet:']") {
		magnet, _ := htmlAttr(link, "href")
		title := strings.TrimSpace(htmlText(link))
		// Alcuni link BTDigg hanno come testo un magnet troncato
		// (`magnet:?xt=…`): non è un titolo, salta la voce.
		if strings.HasPrefix(strings.ToLower(title), "magnet:") {
			continue
		}
		if title != "" {
			if _, ok := utils.SanitizeMagnet(magnet, &title); ok {
				output = append(output, webResult{title: title, magnet: magnet, source: "BTDigg"})
			}
		}
		if len(output) >= 20 {
			break
		}
	}
	return output, nil
}

func fetchHtml(ctx context.Context, rawURL string, flaresolverr *string) (string, error) {
	useFlareSolverr := flaresolverr != nil && strings.TrimSpace(*flaresolverr) != ""
	flareTried := false
	if useFlareSolverr && cfDomainNeedsFlareSolverr(rawURL) {
		_, hasSession := sessionFor(rawURL)
		if !hasSession {
			flareTried = true
			if body, err := fetchWithFlaresolverr(ctx, defaultHTTPClient, *flaresolverr, rawURL); err == nil {
				return body, nil
			}
		}
	}

	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	headers := map[string]string{}
	if session, ok := sessionFor(rawURL); ok {
		if session.userAgent != "" {
			headers["User-Agent"] = session.userAgent
		}
		if session.cookie != "" {
			headers["Cookie"] = session.cookie
		}
	}
	response, err := httpDo(requestCtx, defaultHTTPClient, http.MethodGet, rawURL, headers, nil, "")
	if err != nil {
		cancel()
		if useFlareSolverr && !flareTried {
			flareTried = true
			if body, flareErr := fetchWithFlaresolverr(ctx, defaultHTTPClient, *flaresolverr, rawURL); flareErr == nil {
				return body, nil
			}
		}
		return "", err
	}

	status := response.StatusCode
	payload, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	cancel()
	if readErr != nil {
		if useFlareSolverr && !flareTried {
			flareTried = true
			if body, flareErr := fetchWithFlaresolverr(ctx, defaultHTTPClient, *flaresolverr, rawURL); flareErr == nil {
				return body, nil
			}
		}
		return "", readErr
	}
	body := string(payload)
	if status >= 200 && status < 300 && !isCloudflareChallenge(body) {
		return body, nil
	}

	directErr := fmt.Errorf("HTTP %d", status)
	cloudflare := cloudflareBlocked(status) || isCloudflareChallenge(body)
	if !cloudflare {
		return "", directErr
	}
	if !useFlareSolverr || flareTried {
		if isCloudflareChallenge(body) && status >= 200 && status < 300 {
			return "", fmt.Errorf("Cloudflare challenge")
		}
		return "", directErr
	}
	return fetchWithFlaresolverr(ctx, defaultHTTPClient, *flaresolverr, rawURL)
}

func searchTorrentscsv(ctx context.Context, query string) ([]webResult, error) {
	rawURL := "https://torrents-csv.com/service/search?" + encodeQueryPairs([][2]string{
		{"q", query}, {"size", "20"},
	})
	value, err := fetchJSON(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	var output []webResult
	for _, entry := range arrayValue(objectValue(value)["torrents"]) {
		item := objectValue(entry)
		title := strings.TrimSpace(jsonStringValue(item["name"]))
		hash := strings.TrimSpace(jsonStringValue(item["infohash"]))
		if title == "" || len(hash) != 40 {
			continue
		}
		if magnet := buildMagnet(hash, title); magnet != "" {
			output = append(output, webResult{title: title, magnet: magnet, source: "TorrentsCSV"})
		}
	}
	return output, nil
}

// searchDetailLinks pairs a title with an absolute detail URL.
type searchDetailLink struct {
	title string
	url   string
}

// magnetFromDetail fetches a detail page and returns its raw magnet match.
// LimeTorrents/Torrentz2 push the raw match, matching the behaviour.
func magnetFromDetail(ctx context.Context, rawURL string, flaresolverr *string) (string, error) {
	body, err := fetchHtml(ctx, rawURL, flaresolverr)
	if err != nil {
		return "", err
	}
	return rawMagnet(body), nil
}

// sanitizedMagnetFromDetail fetches and sanitizes the first magnet (BT4G/1337x).
func sanitizedMagnetFromDetail(ctx context.Context, rawURL string, flaresolverr *string) (string, error) {
	body, err := fetchHtml(ctx, rawURL, flaresolverr)
	if err != nil {
		return "", err
	}
	return firstMagnet(body), nil
}

// limetorrentsMirrors are the hosts of LimeTorrents, tried in order until one
// gives results. The site moves between them and they do not all speak the same
// URL: limetorrent.store and limetorrent.net answer /search.php (the path form
// /search/all/<query>/seeds/1/ loops between http and https on .net, which made
// every cycle report the engine unreachable), while www.limetorrents.fun serves
// the older path form and 404s on /search.php. Only .store and .fun carry the
// magnet in the detail page: .net hands it to .store, so it goes last.
var limetorrentsMirrors = []limetorrentsMirror{
	{host: "https://limetorrent.store"},
	{host: "https://www.limetorrents.fun", pathForm: true},
	{host: "https://limetorrent.net"},
}

// limetorrentsMirror is one host of the site and the search URL it speaks.
type limetorrentsMirror struct {
	host string
	// pathForm is the older /search/all/<query>/seeds/1/ instead of
	// /search.php?q=<query>.
	pathForm bool
}

// limetorrentsSearchURL is the search page of one mirror in its own form.
func limetorrentsSearchURL(mirror string, pathForm bool, query string) string {
	host := strings.TrimRight(mirror, "/")
	if pathForm {
		slug := strings.ReplaceAll(strings.TrimSpace(query), " ", "-")
		return host + "/search/all/" + url.PathEscape(slug) + "/seeds/1/"
	}
	return host + "/search.php?" + url.Values{
		"orderby": {"DESC"},
		"order":   {"seeders"},
		"q":       {query},
	}.Encode()
}

// limetorrentsDetails collects the result rows of a search page. Every row
// links the title and the magnet to the same detail page, so the duplicates
// are dropped before the cap: eight distinct torrents, not four.
func limetorrentsDetails(body string, base *url.URL) ([]searchDetailLink, error) {
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, 8)
	details := make([]searchDetailLink, 0, 8)
	for _, link := range htmlSelectAll(document, "table.table2 a[href$='.html']") {
		title := strings.TrimSpace(htmlText(link))
		href, _ := htmlAttr(link, "href")
		if title == "" {
			continue
		}
		detail, err := base.Parse(href)
		if err != nil || seen[detail.String()] {
			continue
		}
		seen[detail.String()] = true
		details = append(details, searchDetailLink{title: title, url: detail.String()})
		if len(details) == 8 {
			break
		}
	}
	return details, nil
}

// searchLimetorrents tries each mirror until one gives results: a host that
// answers but serves nothing (a search page without magnets, a block page) is
// as useless as one that fails, so both move on to the next.
func searchLimetorrents(ctx context.Context, query string, flaresolverr *string) ([]webResult, error) {
	var firstErr error
	for _, mirror := range limetorrentsMirrors {
		results, err := searchLimetorrentsOn(ctx, mirror.host, mirror.pathForm, query, flaresolverr)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	// With nothing from any mirror, a failure is still a failure: the engine is
	// reported unreachable instead of quietly finding nothing.
	return nil, firstErr
}

// searchLimetorrentsOn runs the search of one mirror: the results page, then
// the detail page of each torrent for its magnet.
func searchLimetorrentsOn(ctx context.Context, mirror string, pathForm bool, query string, flaresolverr *string) ([]webResult, error) {
	rawURL := limetorrentsSearchURL(mirror, pathForm, query)
	body, err := fetchHtml(ctx, rawURL, flaresolverr)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	details, err := limetorrentsDetails(body, base)
	if err != nil {
		return nil, err
	}
	var output []webResult
	for _, detail := range details {
		magnet, err := magnetFromDetail(ctx, detail.url, flaresolverr)
		if err != nil {
			return nil, err
		}
		if magnet != "" {
			output = append(output, webResult{title: detail.title, magnet: magnet, source: "LimeTorrents"})
		}
	}
	return output, nil
}

func searchTorrentz2(ctx context.Context, query string, flaresolverr *string) ([]webResult, error) {
	rawURL := "https://torrentz2.nz/search?q=" + url.QueryEscape(query)
	body, err := fetchHtml(ctx, rawURL, flaresolverr)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	nodes := htmlSelectAll(document, "div.results a[href^='/torrent/']")
	if len(nodes) > 8 {
		nodes = nodes[:8]
	}
	var details []searchDetailLink
	for _, link := range nodes {
		title := strings.TrimSpace(htmlText(link))
		href, _ := htmlAttr(link, "href")
		if title == "" {
			continue
		}
		if detail, err := base.Parse(href); err == nil {
			details = append(details, searchDetailLink{title: title, url: detail.String()})
		}
	}
	var output []webResult
	for _, detail := range details {
		magnet, err := magnetFromDetail(ctx, detail.url, flaresolverr)
		if err != nil {
			return nil, err
		}
		if magnet != "" {
			output = append(output, webResult{title: detail.title, magnet: magnet, source: "Torrentz2"})
		}
	}
	return output, nil
}

func searchBt4g(ctx context.Context, query string, flaresolverr *string) ([]webResult, error) {
	encoded := url.QueryEscape(query)
	candidates := []string{
		"https://bt4gprx.com/search?q=" + encoded,
		"https://bt4g.org/search?q=" + encoded,
	}
	var selected string
	var body string
	var lastErr error
	for _, candidate := range candidates {
		if value, err := fetchHtml(ctx, candidate, flaresolverr); err == nil {
			selected = candidate
			body = value
			break
		} else {
			lastErr = err
		}
	}
	if selected == "" {
		if lastErr != nil {
			return nil, fmt.Errorf("BT4G unavailable: %w", lastErr)
		}
		return nil, fmt.Errorf("invalid BT4G base URL")
	}
	if body == "" {
		return nil, fmt.Errorf("BT4G returned no HTML")
	}
	base, err := url.Parse(selected)
	if err != nil {
		return nil, fmt.Errorf("invalid BT4G base URL: %w", err)
	}
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	nodes := htmlSelectAll(document, "a[href*='/torrent/'], a[href*='/detail/']")
	if len(nodes) > 20 {
		nodes = nodes[:20]
	}
	var details []searchDetailLink
	for _, link := range nodes {
		title := strings.TrimSpace(htmlText(link))
		href, _ := htmlAttr(link, "href")
		if title == "" || href == "" {
			continue
		}
		details = append(details, searchDetailLink{title: title, url: href})
	}
	var output []webResult
	for _, detail := range details {
		joined, err := base.Parse(detail.url)
		if err != nil {
			return nil, fmt.Errorf("invalid BT4G detail URL: %w", err)
		}
		magnet, err := sanitizedMagnetFromDetail(ctx, joined.String(), flaresolverr)
		if err != nil {
			return nil, err
		}
		if magnet != "" {
			output = append(output, webResult{title: detail.title, magnet: magnet, source: "BT4G"})
		}
		if len(output) >= 20 {
			break
		}
	}
	return output, nil
}

func search1337x(ctx context.Context, query string, flaresolverr *string) ([]webResult, error) {
	slug := strings.Join(strings.Fields(query), "-")
	rawURL := fmt.Sprintf("https://1337x.to/search/%s/1/", slug)
	body, err := fetchHtml(ctx, rawURL, flaresolverr)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse("https://1337x.to")
	if err != nil {
		return nil, err
	}
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	nodes := htmlSelectAll(document, "table.table-list a[href^='/torrent/'], a[href^='/torrent/']")
	if len(nodes) > 20 {
		nodes = nodes[:20]
	}
	var details []searchDetailLink
	for _, link := range nodes {
		title := strings.TrimSpace(htmlText(link))
		href, _ := htmlAttr(link, "href")
		if title == "" || href == "" {
			continue
		}
		details = append(details, searchDetailLink{title: title, url: href})
	}
	var output []webResult
	for _, detail := range details {
		joined, err := base.Parse(detail.url)
		if err != nil {
			return nil, fmt.Errorf("invalid 1337x detail URL: %w", err)
		}
		magnet, err := sanitizedMagnetFromDetail(ctx, joined.String(), flaresolverr)
		if err != nil {
			return nil, err
		}
		if magnet != "" {
			output = append(output, webResult{title: detail.title, magnet: magnet, source: "1337x"})
		}
		if len(output) >= 20 {
			break
		}
	}
	return output, nil
}

func rawMagnet(body string) string {
	regex, err := utils.CachedRegex(`magnet:\?xt=urn:bt(?:ih|mh):[0-9A-Za-z]{32,68}[^\s"'<>]*`)
	if err != nil {
		return ""
	}
	return regex.FindString(body)
}

func firstMagnet(body string) string {
	magnet := rawMagnet(body)
	if magnet == "" {
		return ""
	}
	sanitized, ok := utils.SanitizeMagnet(magnet, nil)
	if !ok {
		return ""
	}
	return sanitized
}

func buildMagnet(hash, title string) string {
	magnet := "magnet:?xt=urn:btih:" + strings.ToLower(hash) + "&dn=" + url.QueryEscape(title)
	sanitized, ok := utils.SanitizeMagnet(magnet, &title)
	if !ok {
		return ""
	}
	for _, tracker := range websearchTrackers {
		sanitized += "&tr=" + url.QueryEscape(tracker)
	}
	return sanitized
}
