package torznab

// caps.go keeps the search parameters each indexer really supports, so a
// standalone daemon search never carries one the indexer rejects or answers
// with nothing. It mirrors the proven logic of Gextto's torznab_caps.go, but
// lives here so gx-torrent standalone gets the same compatibility with
// Jackett, Prowlarr, indexarr and plain Torznab services.
//
//   - Jackett's "all" aggregate answers HTTP 400 (Torznab error 203) to a
//     tv/movie search carrying tmdbid, so every such search failed.
//   - Prowlarr returns no results at all for an id its indexers do not
//     support, silently dropping releases.
//   - indexarr declares <limits max="100"/>; asking for more is pointless.

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	capsTTL      = 6 * time.Hour
	capsRetryTTL = 10 * time.Minute
	capsTimeout  = 10 * time.Second
)

// searchCaps lists, per search function (search, tvsearch, movie), whether it
// is available, the parameters it accepts and the page size it allows.
type searchCaps struct {
	available map[string]bool
	params    map[string]map[string]bool
	max       int
	def       int
}

type capsEntry struct {
	caps    *searchCaps // nil: unknown, build the request as before
	fetched time.Time
}

var capsCache sync.Map // capability URL -> capsEntry

var (
	episodePattern = regexp.MustCompile(`(?i)s(\d{1,2})[ ._-]?e(\d{1,3})`)
	seasonPattern  = regexp.MustCompile(`(?i)(?:^|[ ._-])s(\d{1,2})(?:$|[ ._-])`)
)

// capsFunction maps a search type to the Torznab caps function name.
func capsFunction(searchType string) string {
	switch strings.ToLower(strings.TrimSpace(searchType)) {
	case "tvsearch", "movie":
		return strings.ToLower(strings.TrimSpace(searchType))
	}
	return "search"
}

// supports reports whether the function accepts the parameter. Unknown caps
// accept everything, so a missing caps reply never removes a parameter.
func (caps *searchCaps) supports(searchType, param string) bool {
	if caps == nil {
		return true
	}
	params, ok := caps.params[capsFunction(searchType)]
	if !ok {
		return true
	}
	return params[strings.ToLower(strings.TrimSpace(param))]
}

// functionAvailable reports whether the indexer offers the search function.
func (caps *searchCaps) functionAvailable(searchType string) bool {
	if caps == nil {
		return true
	}
	available, ok := caps.available[capsFunction(searchType)]
	return !ok || available
}

// pageSize returns the largest page the indexer declares. It never exceeds the
// client's own ceiling: a very large caps max would only make one response
// heavier, not improve the results the daemon can use.
func (caps *searchCaps) pageSize() int {
	if caps == nil || caps.max <= 0 {
		return searchPageSize
	}
	if caps.max < searchPageSize {
		return caps.max
	}
	return searchPageSize
}

// parseTorznabCaps reads the <searching> and <limits> blocks of a t=caps reply.
func parseTorznabCaps(body string) *searchCaps {
	decoder := xml.NewDecoder(strings.NewReader(body))
	caps := &searchCaps{available: map[string]bool{}, params: map[string]map[string]bool{}}
	found := false
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		local := strings.ToLower(start.Name.Local)
		if local == "limits" {
			caps.max, _ = strconv.Atoi(xmlAttr(start, "max"))
			caps.def, _ = strconv.Atoi(xmlAttr(start, "default"))
			continue
		}
		var function string
		switch local {
		case "search":
			function = "search"
		case "tv-search":
			function = "tvsearch"
		case "movie-search":
			function = "movie"
		default:
			continue
		}
		found = true
		available, _ := xmlAttrOk(start, "available")
		caps.available[function] = !strings.EqualFold(strings.TrimSpace(available), "no")
		params := map[string]bool{}
		for _, param := range strings.Split(xmlAttr(start, "supportedparams"), ",") {
			if param = strings.ToLower(strings.TrimSpace(param)); param != "" {
				params[param] = true
			}
		}
		caps.params[function] = params
	}
	if !found {
		return nil
	}
	return caps
}

// parseProwlarrCaps keeps, per function, only the parameters every enabled
// Prowlarr indexer supports: an id token makes the indexers without that id
// return nothing, so it is sent only when no indexer would be lost.
func parseProwlarrCaps(body string) *searchCaps {
	var indexers []struct {
		Enable       bool `json:"enable"`
		Capabilities struct {
			SearchParams      []string `json:"searchParams"`
			TvSearchParams    []string `json:"tvSearchParams"`
			MovieSearchParams []string `json:"movieSearchParams"`
		} `json:"capabilities"`
	}
	if json.Unmarshal([]byte(body), &indexers) != nil {
		return nil
	}
	caps := &searchCaps{available: map[string]bool{}, params: map[string]map[string]bool{}}
	enabled := 0
	intersect := func(function string, values []string) {
		set := map[string]bool{}
		for _, value := range values {
			set[strings.ToLower(strings.TrimSpace(value))] = true
		}
		current, seen := caps.params[function]
		if !seen {
			caps.params[function] = set
			return
		}
		for param := range current {
			if !set[param] {
				delete(current, param)
			}
		}
	}
	for _, indexer := range indexers {
		if !indexer.Enable {
			continue
		}
		enabled++
		intersect("search", indexer.Capabilities.SearchParams)
		intersect("tvsearch", indexer.Capabilities.TvSearchParams)
		intersect("movie", indexer.Capabilities.MovieSearchParams)
	}
	if enabled == 0 {
		return nil
	}
	return caps
}

// capsURL is where the capabilities of an indexer are read. The second result
// reports the Prowlarr JSON shape, which has no Torznab caps document.
func (idx Indexer) capsURL(endpoint string) (string, bool) {
	if isProwlarrSearch(endpoint) {
		base := strings.TrimSuffix(strings.TrimRight(endpoint, "/"), prowlarrSearchPath)
		separator := "?"
		if strings.Contains(base, "?") {
			separator = "&"
		}
		return base + "/api/v1/indexer" + separator + "apikey=" + url.QueryEscape(idx.APIKey), true
	}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "t=caps&apikey=" + url.QueryEscape(idx.APIKey), false
}

// parseCapsBody decodes a caps reply in either of the two shapes.
func parseCapsBody(data []byte, prowlarr bool) *searchCaps {
	if prowlarr {
		return parseProwlarrCaps(string(data))
	}
	return parseTorznabCaps(string(data))
}

// storeCaps caches a caps body that was already fetched (for example by Ping),
// so a search never pays a second round trip just to discover capabilities.
func (idx Indexer) storeCaps(endpoint string, data []byte, prowlarr bool) *searchCaps {
	capsURL, _ := idx.capsURL(endpoint)
	caps := parseCapsBody(data, prowlarr)
	capsCache.Store(capsURL, capsEntry{caps: caps, fetched: time.Now()})
	return caps
}

// ensureCaps loads the capabilities of an indexer when missing or stale. A
// failed read is remembered briefly, so a broken caps endpoint is not asked
// again on every search. The cache holds only public capability data and the
// API key is never stored in a log line or in the repository.
func (idx Indexer) ensureCaps(ctx context.Context, endpoint string) *searchCaps {
	capsURL, prowlarr := idx.capsURL(endpoint)
	if value, ok := capsCache.Load(capsURL); ok {
		entry := value.(capsEntry)
		ttl := capsTTL
		if entry.caps == nil {
			ttl = capsRetryTTL
		}
		if time.Since(entry.fetched) < ttl {
			return entry.caps
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, capsTimeout)
	defer cancel()
	data, status, err := idx.getBody(requestCtx, capsURL)
	var caps *searchCaps
	if err == nil && status >= 200 && status < 300 {
		caps = parseCapsBody(data, prowlarr)
	}
	if ctx.Err() != nil {
		return caps // the caller was cancelled: do not remember a failure
	}
	capsCache.Store(capsURL, capsEntry{caps: caps, fetched: time.Now()})
	return caps
}

// episodeFromQuery extracts a season/episode pair from "Show S01E02".
func episodeFromQuery(query string) (int, int, bool) {
	match := episodePattern.FindStringSubmatch(query)
	if match == nil {
		return 0, 0, false
	}
	season, errSeason := strconv.Atoi(match[1])
	episode, errEpisode := strconv.Atoi(match[2])
	if errSeason != nil || errEpisode != nil || season <= 0 || episode <= 0 {
		return 0, 0, false
	}
	return season, episode, true
}

// seasonFromQuery extracts a season from an episode or a season-pack query.
func seasonFromQuery(query string) (int, bool) {
	if season, _, ok := episodeFromQuery(query); ok {
		return season, true
	}
	match := seasonPattern.FindStringSubmatch(query)
	if match == nil {
		return 0, false
	}
	season, err := strconv.Atoi(match[1])
	if err != nil || season <= 0 {
		return 0, false
	}
	return season, true
}

// normalizeSearchType keeps the automatic episode detection of generic callers.
func normalizeSearchType(searchType, query string) string {
	switch strings.ToLower(strings.TrimSpace(searchType)) {
	case "tvsearch", "movie", "search":
		return strings.ToLower(strings.TrimSpace(searchType))
	}
	if _, _, ok := episodeFromQuery(query); ok {
		return "tvsearch"
	}
	return "search"
}

// prowlarrQuery adds external-id tokens to a Prowlarr search, the same way
// Gextto does: Prowlarr takes one free-text query with {key:value} tokens.
func prowlarrQuery(query, searchType string, ids [][2]string) string {
	if searchType != "tvsearch" && searchType != "movie" {
		return query
	}
	var tokens []string
	for _, pair := range ids {
		key := strings.ToLower(strings.TrimSpace(pair[0]))
		value := strings.TrimSpace(pair[1])
		if value == "" {
			continue
		}
		switch {
		case searchType == "tvsearch" && (key == "imdbid" || key == "tvdbid" || key == "tmdbid" || key == "tvmazeid"):
			tokens = append(tokens, "{"+key+":"+value+"}")
		case searchType == "movie" && (key == "imdbid" || key == "tmdbid" || key == "traktid"):
			tokens = append(tokens, "{"+key+":"+value+"}")
		}
	}
	return query + strings.Join(tokens, "")
}

// torznabUnsupportedParam reports whether an error says a parameter or function
// is not supported (201 incorrect parameter, 203 function not available): the
// request can be repeated without the optional parameters.
func torznabUnsupportedParam(message string) bool {
	for _, code := range []int{201, 203} {
		number := strconv.Itoa(code)
		if strings.HasPrefix(strings.TrimSpace(message), "Torznab "+number+":") ||
			strings.Contains(message, "(code "+number+")") ||
			strings.Contains(message, "code=\""+number+"\"") {
			return true
		}
	}
	return false
}

func xmlAttr(start xml.StartElement, name string) string {
	value, _ := xmlAttrOk(start, name)
	return value
}

func xmlAttrOk(start xml.StartElement, name string) (string, bool) {
	for _, attr := range start.Attr {
		if strings.EqualFold(attr.Name.Local, name) {
			return attr.Value, true
		}
	}
	return "", false
}
