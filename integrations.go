package gextto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Integration API base URLs. They are package-level vars so tests can point
// them at httptest servers.
var (
	simklAPIBaseURL  = "https://api.simkl.com"
	simklDataBaseURL = "https://data.simkl.in"
)

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
