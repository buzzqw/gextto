package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Integration API base URLs. They are package-level vars so tests can point
// them at httptest servers.
var (
	traktAPIBaseURL  = "https://api.trakt.tv"
	simklAPIBaseURL  = "https://api.simkl.com"
	simklDataBaseURL = "https://data.simkl.in"
)

// TraktClient is the Go implementation of gextto's Trakt integration client.
type TraktClient struct {
	clientID     string
	clientSecret string
	accessToken  string
	refreshToken string
}

// FromSettings builds a TraktClient from the persisted settings map.
func (c *TraktClient) FromSettings(settings map[string]string) *TraktClient {
	return &TraktClient{
		clientID:     settings["trakt_client_id"],
		clientSecret: settings["trakt_client_secret"],
		accessToken:  settings["trakt_access_token"],
		refreshToken: settings["trakt_refresh_token"],
	}
}

func (c *TraktClient) headers(auth bool) map[string]string {
	headers := map[string]string{
		"trakt-api-key":     c.clientID,
		"trakt-api-version": "2",
	}
	if auth && c.accessToken != "" {
		headers["Authorization"] = "Bearer " + c.accessToken
	}
	return headers
}

// DeviceStart requests a Trakt device code.
func (c *TraktClient) DeviceStart(ctx context.Context) (any, error) {
	payload, err := json.Marshal(map[string]any{"client_id": c.clientID})
	if err != nil {
		return nil, err
	}
	body, status, err := HTTPPostJSON(ctx, traktAPIBaseURL+"/oauth/device/code", c.headers(false), payload)
	if err != nil {
		return nil, err
	}
	return integrationJSON(body, status)
}

// DevicePoll polls the Trakt device token endpoint.
func (c *TraktClient) DevicePoll(ctx context.Context, code string) (any, error) {
	payload, err := json.Marshal(map[string]any{
		"code":          code,
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
	})
	if err != nil {
		return nil, err
	}
	body, status, err := HTTPPostJSON(ctx, traktAPIBaseURL+"/oauth/device/token", c.headers(false), payload)
	if err != nil {
		return nil, err
	}
	return integrationJSON(body, status)
}

// Refresh exchanges the stored refresh token for a new access token.
func (c *TraktClient) Refresh(ctx context.Context) (any, error) {
	payload, err := json.Marshal(map[string]any{
		"refresh_token": c.refreshToken,
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
		"grant_type":    "refresh_token",
	})
	if err != nil {
		return nil, err
	}
	body, status, err := HTTPPostJSON(ctx, traktAPIBaseURL+"/oauth/token", c.headers(false), payload)
	if err != nil {
		return nil, err
	}
	return integrationJSON(body, status)
}

// Watchlist fetches the authenticated user's show watchlist.
func (c *TraktClient) Watchlist(ctx context.Context) (any, error) {
	return c.get(ctx, "/sync/watchlist/shows")
}

// Calendar fetches the authenticated user's show calendar for the given number
// of days (clamped to 1..=31).
func (c *TraktClient) Calendar(ctx context.Context, days int64) (any, error) {
	path := fmt.Sprintf(
		"/calendars/my/shows/%s/%d",
		time.Now().UTC().Format("2006-01-02"),
		clampInt64(days, 1, 31),
	)
	return c.get(ctx, path)
}

// Scrobble posts a start/pause/stop scrobble for the authenticated user.
func (c *TraktClient) Scrobble(ctx context.Context, action string, payload any) (any, error) {
	switch action {
	case "start", "pause", "stop":
	default:
		return nil, fmt.Errorf("invalid Trakt scrobble action")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	raw, status, err := HTTPPostJSON(ctx, traktAPIBaseURL+"/scrobble/"+action, c.headers(true), body)
	if err != nil {
		return nil, err
	}
	return integrationJSON(raw, status)
}

func (c *TraktClient) get(ctx context.Context, path string) (any, error) {
	raw, status, err := HTTPGetString(ctx, traktAPIBaseURL+path, c.headers(true))
	if err != nil {
		return nil, err
	}
	return integrationJSON([]byte(raw), status)
}

// Configured reports whether a Trakt client id is set.
func (c *TraktClient) Configured() bool {
	return c.clientID != ""
}

// Authenticated reports whether a Trakt access token is set.
func (c *TraktClient) Authenticated() bool {
	return c.accessToken != ""
}

// SimklClient is the Go implementation of gextto's Simkl integration client.
type SimklClient struct {
	clientID    string
	accessToken string
}

// FromSettings builds a SimklClient from the persisted settings map.
func (c *SimklClient) FromSettings(settings map[string]string) *SimklClient {
	return &SimklClient{
		clientID:    settings["simkl_client_id"],
		accessToken: settings["simkl_access_token"],
	}
}

func (c *SimklClient) params() [][2]string {
	return [][2]string{
		{"client_id", c.clientID},
		{"app-name", "gextto"},
		{"app-version", "1"},
	}
}

func (c *SimklClient) headers(auth bool) map[string]string {
	headers := map[string]string{}
	if auth && c.accessToken != "" {
		headers["Authorization"] = "Bearer " + c.accessToken
	}
	return headers
}

// PinStart requests a Simkl PIN code for device authentication.
func (c *SimklClient) PinStart(ctx context.Context) (any, error) {
	rawURL := simklAPIBaseURL + "/oauth/pin?" + encodeParams(c.params())
	body, status, err := HTTPGetString(ctx, rawURL, c.headers(false))
	if err != nil {
		return nil, err
	}
	return integrationJSON([]byte(body), status)
}

// PinPoll polls the Simkl PIN endpoint for the given code.
func (c *SimklClient) PinPoll(ctx context.Context, code string) (any, error) {
	rawURL := simklAPIBaseURL + "/oauth/pin/" + code + "?" + encodeParams(c.params())
	body, status, err := HTTPGetString(ctx, rawURL, c.headers(false))
	if err != nil {
		return nil, err
	}
	return integrationJSON([]byte(body), status)
}

// Watchlist fetches all Simkl list items.
func (c *SimklClient) Watchlist(ctx context.Context) (any, error) {
	return c.get(ctx, "/sync/all-items")
}

// Calendar fetches the Simkl calendar data.
func (c *SimklClient) Calendar(ctx context.Context) (any, error) {
	rawURL := simklDataBaseURL + "/calendars?" + encodeParams(c.params())
	body, status, err := HTTPGetString(ctx, rawURL, c.headers(true))
	if err != nil {
		return nil, err
	}
	return integrationJSON([]byte(body), status)
}

// MarkWatched posts history entries to Simkl.
func (c *SimklClient) MarkWatched(ctx context.Context, payload any) (any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	rawURL := simklAPIBaseURL + "/sync/history?" + encodeParams(c.params())
	raw, status, err := HTTPPostJSON(ctx, rawURL, c.headers(true), body)
	if err != nil {
		return nil, err
	}
	return integrationJSON(raw, status)
}

func (c *SimklClient) get(ctx context.Context, path string) (any, error) {
	rawURL := simklAPIBaseURL + path + "?" + encodeParams(c.params())
	body, status, err := HTTPGetString(ctx, rawURL, c.headers(true))
	if err != nil {
		return nil, err
	}
	return integrationJSON([]byte(body), status)
}

// Configured reports whether a Simkl client id is set.
func (c *SimklClient) Configured() bool {
	return c.clientID != ""
}

// Authenticated reports whether a Simkl access token is set.
func (c *SimklClient) Authenticated() bool {
	return c.accessToken != ""
}

// TokenString extracts a non-empty string field from an integration JSON
// response, mirroring gextto's token_string helper.
func TokenString(value any, key string) (string, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("integration response missing %s", key)
	}
	raw, ok := object[key]
	if !ok {
		return "", fmt.Errorf("integration response missing %s", key)
	}
	str, ok := raw.(string)
	if !ok || str == "" {
		return "", fmt.Errorf("integration response missing %s", key)
	}
	return str, nil
}

// integrationJSON validates the HTTP status and decodes a JSON response body,
// mirroring reqwest's error_for_status().json() chain.
func integrationJSON(body []byte, status int) (any, error) {
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("HTTP %d", status)
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// encodeParams serializes query parameters in order.
func encodeParams(params [][2]string) string {
	parts := make([]string, 0, len(params))
	for _, param := range params {
		parts = append(parts, url.QueryEscape(param[0])+"="+url.QueryEscape(param[1]))
	}
	return strings.Join(parts, "&")
}

// clampInt64 clamps value to the inclusive range [lo, hi].
func clampInt64(value, lo, hi int64) int64 {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}
