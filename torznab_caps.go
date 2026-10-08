package gextto

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// torznab_caps.go keeps the search parameters each indexer really supports, so
// a request never carries one the indexer rejects or answers with nothing:
//
//   - Jackett's "all" aggregate answers HTTP 400 (Torznab error 203, "tmdbid is
//     not supported") to a tv/movie search that carries tmdbid, so every
//     episode and movie search sent to it failed and put it in backoff.
//   - Prowlarr answers an id search with no result at all when its indexers do
//     not support that id (most public trackers), dropping every release.
//
// The capabilities are read once (Torznab t=caps, Prowlarr /api/v1/indexer)
// and cached; when they cannot be read the request is built as before.

const (
	searchCapsTTL      = 6 * time.Hour
	searchCapsRetryTTL = 10 * time.Minute
	searchCapsTimeout  = 10 * time.Second
)

// searchCaps lists, per search function (search, tvsearch, movie), whether it
// is available and the lower-cased parameters it accepts.
type searchCaps struct {
	available map[string]bool
	params    map[string]map[string]bool
}

type searchCapsEntry struct {
	caps    *searchCaps // nil: unknown, build requests as before
	fetched time.Time
}

var searchCapsCache sync.Map // capability URL -> searchCapsEntry

// searchCapsFunction maps a Gextto search type to the caps function name.
func searchCapsFunction(searchType string) string {
	switch searchType {
	case searchTypeTV:
		return "tvsearch"
	case searchTypeMovie:
		return "movie"
	}
	return "search"
}

// supports reports whether the function accepts the parameter. Unknown caps
// accept everything, so a missing caps reply never removes a parameter.
func (caps *searchCaps) supports(searchType, param string) bool {
	if caps == nil {
		return true
	}
	params, ok := caps.params[searchCapsFunction(searchType)]
	if !ok {
		return true
	}
	return params[strings.ToLower(param)]
}

// functionAvailable reports whether the indexer offers the search function.
func (caps *searchCaps) functionAvailable(searchType string) bool {
	if caps == nil {
		return true
	}
	available, ok := caps.available[searchCapsFunction(searchType)]
	return !ok || available
}

// parseTorznabCaps reads the <searching> block of a Torznab t=caps reply.
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
		var function string
		switch strings.ToLower(start.Name.Local) {
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
		availableValue, _ := attribute(start, "available")
		caps.available[function] = !strings.EqualFold(strings.TrimSpace(availableValue), "no")
		params := map[string]bool{}
		supported, _ := attribute(start, "supportedparams")
		for _, param := range strings.Split(supported, ",") {
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

// searchCapsURL is where the capabilities of an indexer are read.
func searchCapsURL(indexer IndexerConfig) (string, bool) {
	endpoint := torznab_endpoint(indexer)
	if strings.HasSuffix(endpoint, "/api/v1/search") {
		return strings.TrimSuffix(endpoint, "/api/v1/search") + "/api/v1/indexer?apikey=" + url.QueryEscape(indexer.APIKey), true
	}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "t=caps&apikey=" + url.QueryEscape(indexer.APIKey), false
}

// cachedSearchCaps returns the cached capabilities of an indexer without any
// network access (nil when unknown).
func cachedSearchCaps(indexer IndexerConfig) *searchCaps {
	capsURL, _ := searchCapsURL(indexer)
	if value, ok := searchCapsCache.Load(capsURL); ok {
		return value.(searchCapsEntry).caps
	}
	return nil
}

// ensureSearchCaps loads the capabilities of an indexer when they are missing
// or stale. A failed read is remembered briefly, so a broken caps endpoint is
// not asked again on every search.
func ensureSearchCaps(ctx context.Context, indexer IndexerConfig) *searchCaps {
	capsURL, prowlarr := searchCapsURL(indexer)
	if value, ok := searchCapsCache.Load(capsURL); ok {
		entry := value.(searchCapsEntry)
		ttl := searchCapsTTL
		if entry.caps == nil {
			ttl = searchCapsRetryTTL
		}
		if time.Since(entry.fetched) < ttl {
			return entry.caps
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, searchCapsTimeout)
	defer cancel()
	var caps *searchCaps
	response, err := httpDo(requestCtx, defaultHTTPClient, http.MethodGet, capsURL, nil, nil, "")
	if err == nil {
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		_ = response.Body.Close()
		if readErr == nil && response.StatusCode == http.StatusOK {
			if prowlarr {
				caps = parseProwlarrCaps(string(payload))
			} else {
				caps = parseTorznabCaps(string(payload))
			}
		}
	}
	if ctx.Err() != nil {
		// The search itself was cancelled: do not remember a failure.
		return caps
	}
	searchCapsCache.Store(capsURL, searchCapsEntry{caps: caps, fetched: time.Now()})
	return caps
}

// torznabUnsupportedParam reports whether a Torznab error says a parameter or
// function is not supported (201 incorrect parameter, 203 function not
// available): the request can be repeated without the optional parameters.
func torznabUnsupportedParam(torznabError string) bool {
	for _, code := range []int{201, 203} {
		if strings.HasPrefix(torznabError, "Torznab "+strconv.Itoa(code)+":") {
			return true
		}
	}
	return false
}
