// Package tvmaze is the small client the standalone daemon uses to learn the
// real episode list of a series: TVmaze's public API needs no key and returns
// the seasons and episodes with their air dates. Only the two read-only calls
// the wanted page needs are implemented; Gextto's richer metadata clients stay
// in the main package.
package tvmaze

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the public TVmaze API.
const DefaultBaseURL = "https://api.tvmaze.com"

// maxBody bounds one response: a series with thousands of episodes is still a
// few hundred KiB, so a bigger body is not a legitimate answer.
const maxBody = 4 << 20

// Show is one search result.
type Show struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Premiered string `json:"premiered,omitempty"`
	Status    string `json:"status,omitempty"`
}

// Episode is one episode of a series.
type Episode struct {
	ID       int    `json:"id"`
	Name     string `json:"name,omitempty"`
	Season   int    `json:"season"`
	Number   int    `json:"number"`
	AirDate  string `json:"airdate,omitempty"`
	AirStamp string `json:"airstamp,omitempty"`
	Runtime  int    `json:"runtime,omitempty"`
}

// Aired reports whether the episode has already aired. An episode without a
// date is treated as not aired: the page must not ask for something that does
// not exist yet.
func (e Episode) Aired(now time.Time) bool {
	if e.Season <= 0 || e.Number <= 0 {
		return false
	}
	if stamp := strings.TrimSpace(e.AirStamp); stamp != "" {
		if t, err := time.Parse(time.RFC3339, stamp); err == nil {
			return !t.After(now)
		}
	}
	if date := strings.TrimSpace(e.AirDate); date != "" {
		if t, err := time.Parse("2006-01-02", date); err == nil {
			return !t.After(now)
		}
	}
	return false
}

// Client talks to a TVmaze-compatible endpoint. BaseURL is replaceable in tests.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a client with a bounded timeout.
func New() *Client {
	return &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 12 * time.Second}}
}

// Search returns the shows whose name matches the query, best first. The score
// is not exposed: TVmaze already orders the results.
func (c *Client) Search(ctx context.Context, query string) ([]Show, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("tvmaze: empty query")
	}
	var raw []struct {
		Score float64 `json:"score"`
		Show  Show    `json:"show"`
	}
	if err := c.get(ctx, "/search/shows?q="+url.QueryEscape(query), &raw); err != nil {
		return nil, err
	}
	out := make([]Show, 0, len(raw))
	for _, item := range raw {
		if item.Show.ID > 0 && strings.TrimSpace(item.Show.Name) != "" {
			out = append(out, item.Show)
		}
	}
	return out, nil
}

// Episodes returns every episode of a series, specials included (season 0).
func (c *Client) Episodes(ctx context.Context, showID int) ([]Episode, error) {
	if showID <= 0 {
		return nil, errors.New("tvmaze: invalid show id")
	}
	var raw []Episode
	if err := c.get(ctx, fmt.Sprintf("/shows/%d/episodes", showID), &raw); err != nil {
		return nil, err
	}
	out := make([]Episode, 0, len(raw))
	for _, episode := range raw {
		if episode.Season >= 0 && episode.Number > 0 {
			out = append(out, episode)
		}
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, path string, target any) error {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gx-torrent")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("tvmaze: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return err
	}
	if len(body) > maxBody {
		return errors.New("tvmaze: response too large")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("tvmaze: %w", err)
	}
	return nil
}
