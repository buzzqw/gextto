package gextto

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// tvdbAPI is the base URL of TheTVDB v4 API. It is a package-level var so tests
// can point it at an httptest server.
var tvdbAPI = "https://api4.thetvdb.com/v4"

// TvdbClient is the Go implementation of gextto's minimal TVDB v4 client: login, series
// search and extended data. The API key lives in the Gextto settings
// (`tvdb_api_key`); the login token is cached in memory for ~23 hours.
type TvdbClient struct {
	client   *http.Client
	key      *string
	language string

	mu         sync.Mutex
	cachedAuth string
	expiry     time.Time
}

// New builds a TVDB client for the given API key (nil disables it), defaulting
// to the Italian language like the `TvdbClient::new`.
func New(key *string) *TvdbClient {
	return WithLanguage(key, "ita")
}

// WithLanguage builds a TVDB client with an explicit Accept-Language; a blank
// language falls back to "ita".
func WithLanguage(key *string, language string) *TvdbClient {
	// The builder used a 10s connect timeout and a 20s total timeout. The
	// shared helpers in httpx.go own the actual transport, so this client is
	// kept for structural parity but requests go through those helpers.
	client := &http.Client{
		Timeout: 20 * time.Second,
	}
	return &TvdbClient{
		client:   client,
		key:      key,
		language: fallbackLanguage(language),
	}
}

// Configured reports whether a non-blank TVDB API key is set.
func (c *TvdbClient) Configured() bool {
	return c.key != nil && strings.TrimSpace(*c.key) != ""
}

// token returns a cached login token, obtaining a fresh one when expired.
func (c *TvdbClient) token(ctx context.Context) (string, error) {
	if c.key == nil || strings.TrimSpace(*c.key) == "" {
		return "", fmt.Errorf("TVDB API key non configurata")
	}
	c.mu.Lock()
	if c.cachedAuth != "" && time.Now().Before(c.expiry) {
		cached := c.cachedAuth
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	payload, err := json.Marshal(map[string]any{"apikey": *c.key})
	if err != nil {
		return "", err
	}
	body, status, err := HTTPPostJSON(ctx, tvdbAPI+"/login", nil, payload)
	if err != nil {
		return "", err
	}
	if !tvdbSuccess(status) {
		return "", fmt.Errorf("TVDB login HTTP %d", status)
	}
	value, err := tvdbJSON(body)
	if err != nil {
		return "", err
	}
	token, ok := tvdbString(tvdbGet(tvdbGet(value, "data"), "token"))
	if !ok {
		return "", fmt.Errorf("TVDB login senza token")
	}
	c.mu.Lock()
	c.cachedAuth = token
	c.expiry = time.Now().Add(23 * time.Hour)
	c.mu.Unlock()
	return token, nil
}

// SearchSeries searches series by name. Returns compact results ready for the UI.
func (c *TvdbClient) SearchSeries(ctx context.Context, query string) ([]any, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("query", query)
	params.Set("type", "series")
	rawURL := tvdbAPI + "/search?" + params.Encode()
	headers := map[string]string{
		"Accept-Language": c.language,
		"Authorization":   "Bearer " + token,
	}
	body, status, err := HTTPGetBytes(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}
	if !tvdbSuccess(status) {
		return nil, fmt.Errorf("TVDB search HTTP %d", status)
	}
	value, err := tvdbJSON(body)
	if err != nil {
		return nil, err
	}
	items := tvdbArray(tvdbGet(value, "data"))
	out := make([]any, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		out = append(out, map[string]any{
			"tvdb_id":  tvdbFirst(item, "tvdb_id", "objectID"),
			"name":     tvdbFirst(item, "name"),
			"year":     tvdbFirst(item, "year"),
			"image":    tvdbFirst(item, "image_url", "thumbnail"),
			"overview": tvdbFirst(item, "overview"),
		})
	}
	return out, nil
}

// SearchMovies searches TVDB movies, normalised into the same compact format
// used for series. It is only used by explicit movie metadata selection.
func (c *TvdbClient) SearchMovies(ctx context.Context, query string) ([]any, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("query", query)
	params.Set("type", "movie")
	rawURL := tvdbAPI + "/search?" + params.Encode()
	headers := map[string]string{
		"Accept-Language": c.language,
		"Authorization":   "Bearer " + token,
	}
	body, status, err := HTTPGetBytes(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}
	if !tvdbSuccess(status) {
		return nil, fmt.Errorf("TVDB movie search HTTP %d", status)
	}
	value, err := tvdbJSON(body)
	if err != nil {
		return nil, err
	}
	items := tvdbArray(tvdbGet(value, "data"))
	out := make([]any, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		out = append(out, map[string]any{
			"id":             tvdbFirst(item, "tvdb_id", "movie_id", "objectID"),
			"title":          tvdbFirst(item, "name"),
			"original_title": tvdbFirst(item, "originalName"),
			"release_date":   tvdbFirst(item, "year"),
			"overview":       tvdbFirst(item, "overview"),
			"poster_path":    tvdbFirst(item, "image_url", "thumbnail"),
		})
	}
	return out, nil
}

// MovieDetails returns the editorial data of a selected TVDB movie. The payload
// stays raw JSON to tolerate the optional/variable fields of the v4 API.
func (c *TvdbClient) MovieDetails(ctx context.Context, id string) (any, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid TVDB movie id")
	}
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	rawURL := tvdbAPI + "/movies/" + strconv.FormatInt(parsed, 10) + "/extended"
	headers := map[string]string{
		"Accept-Language": c.language,
		"Authorization":   "Bearer " + token,
	}
	body, status, err := HTTPGetBytes(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}
	if !tvdbSuccess(status) {
		return nil, fmt.Errorf("TVDB movie details HTTP %d", status)
	}
	value, err := tvdbJSON(body)
	if err != nil {
		return nil, err
	}
	return tvdbGet(value, "data"), nil
}

// SeriesCharacters returns the main characters/actors as `{name, tvdb_id}`
// (TVDB people id).
func (c *TvdbClient) SeriesCharacters(ctx context.Context, id int64) ([]any, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	rawURL := tvdbAPI + "/series/" + strconv.FormatInt(id, 10) + "/extended"
	headers := map[string]string{
		"Accept-Language": c.language,
		"Authorization":   "Bearer " + token,
	}
	body, status, err := HTTPGetBytes(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}
	if !tvdbSuccess(status) {
		return nil, fmt.Errorf("TVDB series HTTP %d", status)
	}
	value, err := tvdbJSON(body)
	if err != nil {
		return nil, err
	}
	characters := tvdbArray(tvdbGet(tvdbGet(value, "data"), "characters"))
	out := []any{}
	for index, raw := range characters {
		if index >= 10 {
			break
		}
		character, _ := raw.(map[string]any)
		name, hasName := tvdbFirstString(character, "personName", "name")
		person, hasPerson := tvdbInt64(tvdbFirst(character, "peopleId"))
		if hasName && hasPerson {
			out = append(out, map[string]any{"name": name, "tvdb_id": person})
		}
	}
	return out, nil
}

// SeriesExtended returns extended series data (name, year, image, seasons
// summary, ...).
func (c *TvdbClient) SeriesExtended(ctx context.Context, id int64) (any, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	rawURL := tvdbAPI + "/series/" + strconv.FormatInt(id, 10) + "/extended"
	headers := map[string]string{
		"Accept-Language": c.language,
		"Authorization":   "Bearer " + token,
	}
	body, status, err := HTTPGetBytes(ctx, rawURL, headers)
	if err != nil {
		return nil, err
	}
	if !tvdbSuccess(status) {
		return nil, fmt.Errorf("TVDB series HTTP %d", status)
	}
	value, err := tvdbJSON(body)
	if err != nil {
		return nil, err
	}
	if data, ok := tvdbLookup(value, "data"); ok {
		return data, nil
	}
	return value, nil
}

// fallbackLanguage applies the blank-language default of "ita".
func fallbackLanguage(language string) string {
	if strings.TrimSpace(language) == "" {
		return "ita"
	}
	return language
}

// tvdbSuccess mirrors reqwest's StatusCode::is_success.
func tvdbSuccess(status int) bool {
	return status >= 200 && status < 300
}

// tvdbJSON decodes a JSON body preserving numeric literals (encoding/json uses
// json.Number so integer fields are not coerced to float64).
func tvdbJSON(body []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// tvdbLookup returns the value at key and whether the key was present. It
// mirrors encoding/json's Value::get on a non-object by returning ok=false.
func tvdbLookup(value any, key string) (any, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	item, ok := object[key]
	return item, ok
}

// tvdbGet returns the value at key or nil when absent.
func tvdbGet(value any, key string) any {
	item, _ := tvdbLookup(value, key)
	return item
}

// tvdbFirst returns the first present key, mirroring Value::get(...).or_else(...).
func tvdbFirst(object map[string]any, keys ...string) any {
	for _, key := range keys {
		if item, ok := object[key]; ok {
			return item
		}
	}
	return nil
}

// tvdbString mirrors encoding/json's Value::as_str.
func tvdbString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok
}

// tvdbFirstString returns the first key whose value is a string.
func tvdbFirstString(object map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if text, ok := tvdbString(object[key]); ok {
			return text, true
		}
	}
	return "", false
}

// tvdbArray mirrors encoding/json's Value::as_array, returning nil for non-arrays.
func tvdbArray(value any) []any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	return items
}

// tvdbInt64 mirrors encoding/json's Value::as_i64 (fractional numbers are rejected).
func tvdbInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case json.Number:
		parsed, err := number.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case float64:
		if number != math.Trunc(number) {
			return 0, false
		}
		return int64(number), true
	case int64:
		return number, true
	case int:
		return int64(number), true
	}
	return 0, false
}
