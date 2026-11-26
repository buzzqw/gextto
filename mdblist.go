package gextto

// mdblist.go is a small client for MDbList, a list provider that needs only an
// API key (no OAuth device flow). The items it returns are normalized to the
// shape the transactional list importer already understands.

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var mdblistAPIBaseURL = "https://api.mdblist.com"

const (
	mdblistAPIKeyKey = "mdblist_api_key"
	mdblistListIDKey = "mdblist_list_id"
)

type MDbListClient struct {
	apiKey string
	listID string
}

func (c *MDbListClient) FromSettings(settings map[string]string) *MDbListClient {
	if c == nil {
		c = &MDbListClient{}
	}
	c.apiKey = strings.TrimSpace(settings[mdblistAPIKeyKey])
	c.listID = strings.TrimSpace(settings[mdblistListIDKey])
	return c
}

func (c *MDbListClient) Configured() bool { return c != nil && c.apiKey != "" }

func (c *MDbListClient) get(ctx context.Context, path string) (any, error) {
	if !c.Configured() {
		return nil, errors.New("MDbList is not configured")
	}
	params := url.Values{}
	params.Set("apikey", c.apiKey)
	address := strings.TrimRight(mdblistAPIBaseURL, "/") + path
	if strings.Contains(address, "?") {
		address += "&" + params.Encode()
	} else {
		address += "?" + params.Encode()
	}
	body, status, err := HTTPGetString(ctx, address, nil)
	if err != nil {
		return nil, err
	}
	return integrationJSON([]byte(body), status)
}

// Lists returns the lists visible to the configured key.
func (c *MDbListClient) Lists(ctx context.Context) (any, error) {
	return c.get(ctx, "/lists/user")
}

// Items returns the entries of the selected list. A key alone is not enough:
// the operator chooses which list to import, so a wrong list never silently
// replaces a library.
func (c *MDbListClient) Items(ctx context.Context) (any, error) {
	if strings.TrimSpace(c.listID) == "" {
		return nil, errors.New("no MDbList list is selected")
	}
	return c.get(ctx, "/lists/"+url.PathEscape(c.listID)+"/items")
}

// normalizeMDbListItems maps MDbList's flat fields to the ids/title/year shape
// shared with Simkl, so the importer needs no provider-specific branch.
func normalizeMDbListItems(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	movies := []any{}
	shows := []any{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title := firstString(item, "title", "name")
		year := firstString(item, "release_year", "year")
		ids := map[string]any{}
		for target, source := range map[string]string{"imdb": "imdb_id", "tmdb": "tmdb_id", "tvdb": "tvdb_id"} {
			if id := firstString(item, source); id != "" {
				ids[target] = id
			}
		}
		kind := firstString(item, "type")
		entry := map[string]any{"title": title, "year": year, "ids": ids}
		if kind != "" {
			entry["type"] = kind
		}
		if kind == "movie" || kind == "film" {
			movies = append(movies, entry)
		} else {
			shows = append(shows, entry)
		}
	}
	return map[string]any{"movies": movies, "shows": shows}
}

func firstString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := item[key].(type) {
		case string:
			if text := strings.TrimSpace(value); text != "" {
				return text
			}
		case float64:
			if value > 0 {
				return fmt.Sprintf("%d", int64(value))
			}
		}
	}
	return ""
}
