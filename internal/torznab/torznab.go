// Package torznab is a small client for Torznab indexers (Jackett, Prowlarr
// and anything speaking the same API): it runs a search and returns the results
// as plain items, with no dependency on Gextto types so it can be reused by the
// standalone daemon and unit tested against a fake server.
package torznab

import (
	"bytes"
	"context"
	"encoding/json"
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

// maxTorznabBody caps how much of a Torznab response is read: a search answer
// with a large result set is a few hundred kB, so a much bigger body means
// something is wrong.
const maxTorznabBody = 16 << 20

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

// Endpoint returns the Torznab API endpoint for the indexer.
func (idx Indexer) Endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(idx.URL), "/")
	if base == "" {
		return ""
	}
	if strings.HasSuffix(base, "/api") {
		return base
	}
	if strings.Contains(base, "/results/torznab") || strings.Contains(base, "/torznab") {
		return base + "/api"
	}
	lower := strings.ToLower(base)
	// Prowlarr and Jackett aggregate many trackers and have their own search
	// paths; anything else is a direct Torznab service, whose endpoint is
	// simply <base>/api. Gextto follows the same rule, so the address to type
	// is identical in both. Prowlarr's aggregate search is its JSON API, read
	// by ParseProwlarr.
	if strings.Contains(lower, "prowlarr") || strings.Contains(lower, ":9696") {
		return base + prowlarrSearchPath
	}
	// Jackett default port or jackett in URL (e.g. http://host:9117 or http://jackett/...)
	if strings.Contains(lower, "jackett") || strings.Contains(lower, ":9117") {
		return base + "/api/v2.0/indexers/all/results/torznab/api"
	}
	return base + "/api"
}

// prowlarrSearchPath is Prowlarr's aggregate search across all its indexers. It
// answers JSON, not Torznab XML, and takes `query`/`type`, not `t`/`q`.
const prowlarrSearchPath = "/api/v1/search"

func isProwlarrSearch(endpoint string) bool {
	return strings.HasSuffix(strings.TrimRight(endpoint, "/"), prowlarrSearchPath)
}

// Search queries one indexer and returns its results.
func (idx Indexer) Search(ctx context.Context, query string) ([]Item, error) {
	ep := idx.Endpoint()
	if ep == "" {
		return nil, fmt.Errorf("torznab: empty indexer URL")
	}
	items, err := idx.searchEndpoint(ctx, ep, query)
	if err != nil {
		base := strings.TrimRight(strings.TrimSpace(idx.URL), "/")
		jackettURL := base + "/api/v2.0/indexers/all/results/torznab/api"
		if strings.Contains(err.Error(), "HTTP 404") && ep != jackettURL {
			if fbItems, fbErr := idx.searchEndpoint(ctx, jackettURL, query); fbErr == nil {
				return fbItems, nil
			}
		}
		return nil, err
	}
	return items, nil
}

func (idx Indexer) searchEndpoint(ctx context.Context, endpointStr, query string) ([]Item, error) {
	endpoint, err := url.Parse(endpointStr)
	if err != nil {
		return nil, fmt.Errorf("torznab: invalid indexer URL: %w", err)
	}
	prowlarr := isProwlarrSearch(endpoint.Path)
	params := endpoint.Query()
	if prowlarr {
		params.Set("query", query)
		params.Set("type", "search")
		// Ask for a full page, as Gextto does; the default is much smaller.
		params.Set("limit", "100")
	} else {
		params.Set("t", "search")
		params.Set("q", query)
	}
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
	if prowlarr {
		return ParseProwlarr(resp.Body, idx.Name)
	}
	return Parse(resp.Body, idx.Name)
}

// prowlarrRelease is the part of a Prowlarr /api/v1/search result we use.
type prowlarrRelease struct {
	Title       string `json:"title"`
	Size        int64  `json:"size"`
	Seeders     *int   `json:"seeders"`
	Leechers    *int   `json:"leechers"`
	MagnetURL   string `json:"magnetUrl"`
	DownloadURL string `json:"downloadUrl"`
	InfoHash    string `json:"infoHash"`
	PublishDate string `json:"publishDate"`
	Protocol    string `json:"protocol"`
}

// ParseProwlarr reads the JSON answer of Prowlarr's aggregate search. Usenet
// results are skipped; a result without a magnet gets one from its infohash,
// the same fallback Gextto uses.
func ParseProwlarr(r io.Reader, indexerName string) ([]Item, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxTorznabBody))
	if err != nil {
		return nil, fmt.Errorf("torznab: cannot read the response: %w", err)
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '<' {
		// A Prowlarr behind a proxy may still answer Torznab XML (or an error
		// document): read it as such instead of failing on the JSON.
		return Parse(bytes.NewReader(data), indexerName)
	}
	var raw []prowlarrRelease
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("torznab: %s did not answer a Prowlarr search (check URL and API key): %w", indexerName, err)
	}
	items := make([]Item, 0, len(raw))
	for _, rel := range raw {
		if rel.Protocol != "" && !strings.EqualFold(rel.Protocol, "torrent") {
			continue
		}
		item := Item{
			Title:    strings.TrimSpace(rel.Title),
			Size:     rel.Size,
			Leechers: -1,
			Link:     strings.TrimSpace(rel.DownloadURL),
			Indexer:  indexerName,
		}
		if rel.Seeders != nil {
			item.Seeders = *rel.Seeders
		}
		if rel.Leechers != nil {
			item.Leechers = *rel.Leechers
		}
		switch {
		case strings.HasPrefix(strings.ToLower(rel.MagnetURL), "magnet:"):
			item.Magnet = strings.TrimSpace(rel.MagnetURL)
		case strings.HasPrefix(strings.ToLower(item.Link), "magnet:"):
			item.Magnet = item.Link
		default:
			item.Magnet = infoHashMagnet(rel.InfoHash, item.Title)
		}
		if t, err := parseTorznabTime(rel.PublishDate); err == nil {
			item.Published = t
		}
		if item.Title == "" || item.Download() == "" {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// infoHashMagnet builds a magnet from a v1 (40 hex) or v2 (64 hex) infohash.
func infoHashMagnet(hash, title string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	var magnet string
	switch len(hash) {
	case 40:
		magnet = "magnet:?xt=urn:btih:" + hash
	case 64:
		magnet = "magnet:?xt=urn:btmh:1220" + hash
	default:
		return ""
	}
	return magnet + "&dn=" + url.QueryEscape(title)
}

// Ping checks that the indexer answers a Torznab `t=caps` request. It validates
// reachability and the API key without depending on a query returning results.
func (idx Indexer) Ping(ctx context.Context) error {
	ep := idx.Endpoint()
	if ep == "" {
		return fmt.Errorf("torznab: empty indexer URL")
	}
	err := idx.pingEndpoint(ctx, ep)
	if err != nil {
		base := strings.TrimRight(strings.TrimSpace(idx.URL), "/")
		jackettURL := base + "/api/v2.0/indexers/all/results/torznab/api"
		if strings.Contains(err.Error(), "HTTP 404") && ep != jackettURL {
			if fbErr := idx.pingEndpoint(ctx, jackettURL); fbErr == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

func (idx Indexer) pingEndpoint(ctx context.Context, endpointStr string) error {
	endpoint, err := url.Parse(endpointStr)
	if err != nil {
		return fmt.Errorf("torznab: invalid indexer URL: %w", err)
	}
	if isProwlarrSearch(endpoint.Path) {
		return idx.pingProwlarr(ctx, endpoint)
	}
	params := endpoint.Query()
	params.Set("t", "caps")
	if key := strings.TrimSpace(idx.APIKey); key != "" {
		params.Set("apikey", key)
	}
	endpoint.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("torznab: %s returned HTTP %d", idx.Name, resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("<caps")) {
		return fmt.Errorf("torznab: %s answered without a caps document (check URL and API key)", idx.Name)
	}
	return nil
}

// pingProwlarr checks Prowlarr through its system status, which validates
// reachability and the API key (Prowlarr's aggregate search has no `caps`).
func (idx Indexer) pingProwlarr(ctx context.Context, search *url.URL) error {
	status := *search
	status.Path = strings.TrimSuffix(strings.TrimRight(status.Path, "/"), prowlarrSearchPath) + "/api/v1/system/status"
	params := url.Values{}
	if key := strings.TrimSpace(idx.APIKey); key != "" {
		params.Set("apikey", key)
	}
	status.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, status.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("torznab: %s rejected the API key", idx.Name)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("torznab: %s returned HTTP %d", idx.Name, resp.StatusCode)
	}
	var probe struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &probe) != nil || probe.Version == "" {
		return fmt.Errorf("torznab: %s answered without a Prowlarr status (check URL and API key)", idx.Name)
	}
	return nil
}

// torznabError is the error document a Torznab indexer (Jackett, Prowlarr)
// returns with HTTP 200 when the request itself is wrong — typically a bad API
// key. Decoding it as an empty feed would hide the failure, so Parse reports it.
type torznabError struct {
	XMLName xml.Name
	Code    string `xml:"code,attr"`
	Desc    string `xml:"description,attr"`
}

// Parse reads a Torznab RSS body into items.
func Parse(r io.Reader, indexerName string) ([]Item, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxTorznabBody))
	if err != nil {
		return nil, fmt.Errorf("torznab: cannot read the response: %w", err)
	}
	var probe torznabError
	_ = newLenientDecoder(data).Decode(&probe)
	if strings.EqualFold(probe.XMLName.Local, "error") {
		desc := strings.TrimSpace(probe.Desc)
		if desc == "" {
			desc = "indexer error"
		}
		if code := strings.TrimSpace(probe.Code); code != "" {
			return nil, fmt.Errorf("torznab: %s (code %s)", desc, code)
		}
		return nil, fmt.Errorf("torznab: %s", desc)
	}
	var feed rssFeed
	if err := newLenientDecoder(data).Decode(&feed); err != nil {
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
		// A magnet in the link/enclosure is a magnet, not a .torrent URL: some
		// indexers (MirCrew among them) put the magnet there and not in a
		// magneturl attribute, and fetching it over HTTP would fail.
		if item.Magnet == "" && strings.HasPrefix(strings.ToLower(item.Link), "magnet:") {
			item.Magnet = item.Link
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

// newLenientDecoder reads indexer XML the way browsers do: a stray "&" or an
// HTML entity in a title or URL (common in scraped trackers) must not make the
// whole answer unreadable.
func newLenientDecoder(data []byte) *xml.Decoder {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	decoder.Entity = xml.HTMLEntity
	return decoder
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
