// Package rss fetches and parses RSS/Atom/Torznab feeds into download items and
// applies include/exclude rules, so the standalone daemon can follow feeds
// (Jackett, Prowlarr, MIRCrew and any RSS listing) and add matching torrents.
// It has no dependency on Gextto types so it can be reused and unit tested.
package rss

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Item is one feed entry reduced to what matters for downloading.
type Item struct {
	Title       string    `json:"title"`
	Magnet      string    `json:"magnet,omitempty"`
	Link        string    `json:"link,omitempty"` // a .torrent URL
	Description string    `json:"-"`
	Source      string    `json:"source,omitempty"`
	GUID        string    `json:"guid,omitempty"`
	Size        int64     `json:"size,omitempty"`
	Seeders     int       `json:"seeders"`
	Leechers    int       `json:"leechers"`
	Published   time.Time `json:"published,omitzero"`
}

// Download returns the best fetch target: the magnet when present, else the
// .torrent URL (empty when the item carries neither).
func (i Item) Download() string {
	if i.Magnet != "" {
		return i.Magnet
	}
	return i.Link
}

// TitleFilter filters items by title: Include (when non-empty) requires at
// least one match; any Exclude match rejects. Matching is a case-insensitive
// substring. It is the simple per-feed filter; the ordered Rule (rules.go) is
// the advanced one.
type TitleFilter struct {
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// Match reports whether the title passes the filter.
func (r TitleFilter) Match(title string) bool {
	lower := strings.ToLower(title)
	if len(r.Include) > 0 {
		ok := false
		for _, needle := range r.Include {
			if needle = strings.ToLower(strings.TrimSpace(needle)); needle != "" && strings.Contains(lower, needle) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, needle := range r.Exclude {
		if needle = strings.ToLower(strings.TrimSpace(needle)); needle != "" && strings.Contains(lower, needle) {
			return false
		}
	}
	return true
}

// Filter returns the items of list whose title passes the filter.
func (r TitleFilter) Filter(list []Item) []Item {
	out := make([]Item, 0, len(list))
	for _, item := range list {
		if item.Title != "" && r.Match(item.Title) && item.Download() != "" {
			out = append(out, item)
		}
	}
	return out
}

// Fetch downloads a feed and parses it. source is the feed's display name.
func Fetch(ctx context.Context, client *http.Client, rawURL, source string) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rss: %s returned HTTP %d", source, resp.StatusCode)
	}
	return Parse(resp.Body, source)
}

// Parse reads an RSS/Atom/Torznab body into items.
func Parse(r io.Reader, source string) ([]Item, error) {
	decoder := xml.NewDecoder(r)
	var items []Item
	var current string
	var item Item
	inItem := false
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			// Some feeds keep the connection open after a valid prefix; keep
			// what was parsed instead of discarding it for a missing tail.
			if len(items) > 0 {
				break
			}
			return nil, fmt.Errorf("rss: cannot parse %s: %w", source, err)
		}
		switch event := token.(type) {
		case xml.StartElement:
			name := localName(event.Name.Local)
			if name == "item" || name == "entry" {
				inItem = true
				item = Item{Source: source}
				current = ""
				continue
			}
			if !inItem {
				current = name
				continue
			}
			switch name {
			case "enclosure":
				if length, ok := attribute(event, "length"); ok {
					if parsed, err := strconv.ParseInt(strings.TrimSpace(length), 10, 64); err == nil && parsed > 0 {
						item.Size = parsed
					}
				}
				rememberLink(&item, attribute2(event, "url", "href"))
			case "attr":
				applyTorznabAttr(&item, event)
			case "link":
				rememberLink(&item, attribute2(event, "href", "url"))
			case "guid":
				// handled as text
			}
			current = name
		case xml.CharData:
			if inItem {
				value := strings.TrimSpace(string(event))
				switch current {
				case "title":
					item.Title = value
				case "description", "summary", "content":
					item.Description += value + " "
				case "link":
					rememberLink(&item, value)
				case "guid", "id":
					if item.GUID == "" {
						item.GUID = value
					}
				case "size":
					if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && parsed > 0 {
						item.Size = parsed
					}
				case "seeders":
					item.Seeders = atoiOr(value, item.Seeders)
				case "peers", "leechers":
					item.Leechers = atoiOr(value, item.Leechers)
				case "pubdate", "published", "updated", "date":
					if t, ok := parseDate(value); ok {
						item.Published = t
					}
				}
			}
		case xml.EndElement:
			if localName(event.Name.Local) == "item" || localName(event.Name.Local) == "entry" {
				inItem = false
				if magnet := magnetIn(item.Description); magnet != "" && item.Magnet == "" {
					item.Magnet = magnet
				}
				if item.GUID == "" {
					item.GUID = item.Download()
				}
				if item.Title != "" && item.Download() != "" {
					items = append(items, item)
				}
			}
			current = ""
		}
	}
	return items, nil
}

var magnetPattern = regexp.MustCompile(`magnet:\?[^"'\s<>()]+`)

// magnetIn extracts the first magnet link from arbitrary text.
func magnetIn(value string) string {
	if found := magnetPattern.FindString(value); found != "" {
		return found
	}
	return ""
}

// isTorrentURL reports whether a value looks like a .torrent download link.
func isTorrentURL(value string) bool {
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	path := lower
	if q := strings.IndexAny(path, "?#"); q >= 0 {
		path = path[:q]
	}
	return strings.HasSuffix(path, ".torrent")
}

// rememberLink stores a magnet or a .torrent URL found in an attribute or text.
func rememberLink(item *Item, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if magnet := magnetIn(value); magnet != "" {
		if item.Magnet == "" {
			item.Magnet = magnet
		}
		return
	}
	if item.Link == "" && isTorrentURL(value) {
		item.Link = value
	}
}

func applyTorznabAttr(item *Item, start xml.StartElement) {
	name, _ := attribute(start, "name")
	value, _ := attribute(start, "value")
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "seeders":
		item.Seeders = atoiOr(value, item.Seeders)
	case "leechers", "peers":
		item.Leechers = atoiOr(value, item.Leechers)
	case "size":
		if parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && parsed > 0 {
			item.Size = parsed
		}
	case "magneturl":
		if magnet := magnetIn(value); magnet != "" {
			item.Magnet = magnet
		}
	}
}

func localName(value string) string {
	if index := strings.LastIndexByte(value, ':'); index >= 0 {
		value = value[index+1:]
	}
	return strings.ToLower(value)
}

func attribute(start xml.StartElement, name string) (string, bool) {
	for _, attr := range start.Attr {
		if localName(attr.Name.Local) == name {
			return attr.Value, true
		}
	}
	return "", false
}

func attribute2(start xml.StartElement, names ...string) string {
	for _, name := range names {
		if value, ok := attribute(start, name); ok {
			return value
		}
	}
	return ""
}

func parseDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339, "Mon, 02 Jan 2006 15:04:05 +0000", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func atoiOr(value string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return n
	}
	return fallback
}
