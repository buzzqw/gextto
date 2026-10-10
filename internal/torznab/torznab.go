// Package torznab is a small client for Torznab indexers (Jackett, Prowlarr
// and anything speaking the same API): it runs a search and returns the results
// as plain items, with no dependency on Gextto types so it can be reused by the
// standalone daemon and unit tested against a fake server.
package torznab

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Indexer is one configured Torznab endpoint.
type Indexer struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	APIKey string `json:"apikey"`
}

// Item is one search result.
type Item struct {
	Title     string    `json:"title"`
	Size      int64     `json:"size"`
	Seeders   int       `json:"seeders"`
	Leechers  int       `json:"leechers"`
	Magnet    string    `json:"magnet,omitempty"`
	Link      string    `json:"link,omitempty"`
	Published time.Time `json:"published,omitzero"`
	Indexer   string    `json:"indexer,omitempty"`
}

// Download returns the best way to fetch the item: the magnet when present,
// otherwise the .torrent link.
func (i Item) Download() string {
	if i.Magnet != "" {
		return i.Magnet
	}
	return i.Link
}

// rssFeed is the Torznab RSS shape we read. The torznab attributes are matched
// by local name (Go's encoding/xml ignores the namespace when the tag has no
// space), so both the namespaced and the bare form parse.
type rssFeed struct {
	Items []rssItem `xml:"channel>item"`
}

type rssItem struct {
	Title     string `xml:"title"`
	Link      string `xml:"link"`
	PubDate   string `xml:"pubDate"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
	} `xml:"enclosure"`
	Attrs []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

// Search queries one indexer and returns its results.
func (idx Indexer) Search(ctx context.Context, query string) ([]Item, error) {
	base := strings.TrimRight(strings.TrimSpace(idx.URL), "/")
	if base == "" {
		return nil, fmt.Errorf("torznab: empty indexer URL")
	}
	endpoint, err := url.Parse(base + "/api")
	if err != nil {
		return nil, fmt.Errorf("torznab: invalid indexer URL: %w", err)
	}
	params := endpoint.Query()
	params.Set("t", "search")
	params.Set("q", query)
	if key := strings.TrimSpace(idx.APIKey); key != "" {
		params.Set("apikey", key)
	}
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torznab: %s returned HTTP %d", idx.Name, resp.StatusCode)
	}
	return Parse(resp.Body, idx.Name)
}

// Parse reads a Torznab RSS body into items.
func Parse(r io.Reader, indexerName string) ([]Item, error) {
	var feed rssFeed
	if err := xml.NewDecoder(r).Decode(&feed); err != nil {
		return nil, fmt.Errorf("torznab: cannot parse the feed: %w", err)
	}
	items := make([]Item, 0, len(feed.Items))
	for _, raw := range feed.Items {
		item := Item{
			Title:    strings.TrimSpace(raw.Title),
			Link:     strings.TrimSpace(raw.Enclosure.URL),
			Indexer:  indexerName,
			Leechers: -1,
		}
		if item.Link == "" {
			item.Link = strings.TrimSpace(raw.Link)
		}
		item.Size = raw.Enclosure.Length
		for _, attr := range raw.Attrs {
			switch strings.ToLower(strings.TrimSpace(attr.Name)) {
			case "seeders":
				item.Seeders = atoiOr(attr.Value, item.Seeders)
			case "leechers", "peers":
				item.Leechers = atoiOr(attr.Value, item.Leechers)
			case "size":
				item.Size = atollOr(attr.Value, item.Size)
			case "magneturl":
				item.Magnet = strings.TrimSpace(attr.Value)
			}
		}
		if t, err := parseTorznabTime(raw.PubDate); err == nil {
			item.Published = t
		}
		if item.Title == "" && item.Download() == "" {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// parseTorznabTime reads the RFC1123(Z) or RFC3339 dates indexers emit.
func parseTorznabTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 02 Jan 2006 15:04:05 +0000"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unknown date %q", value)
}

func atoiOr(value string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return n
	}
	return fallback
}

func atollOr(value string, fallback int64) int64 {
	if n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		return n
	}
	return fallback
}
