package rss

// listing.go reads HTML torrent listings that are not RSS: an uploader or
// search page of ext.to (direct magnet buttons) or of TorrentGalaxy (rows that
// link to /post-detail/ pages carrying the magnet). The operator pastes the page
// URL as a feed, as in Gextto; the items then go through the same rules. A page
// that advertises a real feed (<link rel="alternate" type="application/rss+xml">)
// is followed instead.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

const (
	// listingMaxItems caps one page, like Gextto: ext.to lists exactly 50.
	listingMaxItems = 50
	// listingDetailWorkers bounds the detail pages fetched at once: TorrentGalaxy
	// answers 429 to a burst of parallel requests.
	listingDetailWorkers = 3
	// listingDetailCacheMax bounds the detail-page → magnet cache.
	listingDetailCacheMax = 4096
	// fetchBodyMax bounds a feed or listing body.
	fetchBodyMax = 16 << 20
	// browserUserAgent is sent to listing sites, which often reject Go's default.
	browserUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
)

// detailCache remembers the magnet of each detail page already resolved, so a
// listing polled every few minutes costs one request per new release only.
var detailCache = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func cachedDetail(key string) (string, bool) {
	detailCache.Lock()
	defer detailCache.Unlock()
	magnet, ok := detailCache.m[key]
	return magnet, ok
}

func storeDetail(key, magnet string) {
	detailCache.Lock()
	defer detailCache.Unlock()
	if len(detailCache.m) >= listingDetailCacheMax {
		// Dropping everything is simpler than an LRU and only costs one
		// refetch of the current pages.
		detailCache.m = map[string]string{}
	}
	detailCache.m[key] = magnet
}

// looksLikeHTML reports a body that is an HTML page rather than XML.
func looksLikeHTML(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "html") && !strings.Contains(strings.ToLower(contentType), "xhtml+xml") {
		return true
	}
	head := bytes.ToLower(bytes.TrimSpace(body[:min(len(body), 512)]))
	return bytes.HasPrefix(head, []byte("<!doctype html")) || bytes.HasPrefix(head, []byte("<html"))
}

// detailLink is a listing row that needs its detail page for the magnet.
type detailLink struct {
	title string
	url   string
}

// parseListing extracts the items of an HTML listing. follow is false when the
// page was itself reached through a feed link, so discovery cannot loop.
func parseListing(ctx context.Context, client *http.Client, pageURL string, body []byte, source string, follow bool) ([]Item, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("rss: cannot parse %s: %w", source, err)
	}
	base, _ := url.Parse(pageURL)
	var items []Item
	seen := map[string]bool{}
	var details []detailLink
	var feedLink string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "link":
				if feedLink == "" && strings.EqualFold(attr(n, "rel"), "alternate") {
					kind := strings.ToLower(attr(n, "type"))
					if strings.Contains(kind, "rss") || strings.Contains(kind, "atom") {
						feedLink = resolve(base, attr(n, "href"))
					}
				}
			case "a":
				href := strings.TrimSpace(attr(n, "href"))
				switch {
				case strings.HasPrefix(href, "magnet:"):
					key := magnetKey(href)
					if !seen[key] && len(items) < listingMaxItems {
						seen[key] = true
						title := cleanText(nodeText(n))
						if title == "" || strings.EqualFold(title, "magnet") {
							title = titleFromMagnet(href)
						}
						if title != "" {
							items = append(items, Item{Title: title, Magnet: href, GUID: key, Source: source})
						}
					}
				case strings.Contains(href, "/post-detail/"):
					title := cleanText(nodeText(n))
					target := resolve(base, href)
					if title != "" && target != "" && !seen[target] && len(details) < listingMaxItems {
						seen[target] = true
						details = append(details, detailLink{title: title, url: target})
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if len(items) == 0 && len(details) > 0 {
		items = resolveDetails(ctx, client, details, source)
	}
	if len(items) == 0 && follow && feedLink != "" && feedLink != pageURL {
		return fetch(ctx, client, feedLink, source, false)
	}
	if len(items) == 0 {
		if len(details) > 0 {
			return nil, fmt.Errorf("rss: %s: no magnet found in the detail pages", source)
		}
		return nil, fmt.Errorf("rss: %s is an HTML page without torrent links, not an RSS feed", source)
	}
	return items, nil
}

// resolveDetails fetches the detail pages (bounded, cached) and keeps the rows
// whose page carries a magnet. Order follows the listing.
func resolveDetails(ctx context.Context, client *http.Client, details []detailLink, source string) []Item {
	magnets := make([]string, len(details))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < listingDetailWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if magnet, ok := cachedDetail(details[i].url); ok {
					magnets[i] = magnet
					continue
				}
				body, _, err := get(ctx, client, details[i].url)
				if err != nil {
					continue
				}
				if magnet := magnetIn(html.UnescapeString(string(body))); magnet != "" {
					magnets[i] = magnet
					storeDetail(details[i].url, magnet)
				}
			}
		}()
	}
	for i := range details {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	items := make([]Item, 0, len(details))
	for i, detail := range details {
		if magnets[i] == "" {
			continue
		}
		// The detail URL is the stable identity of the release on the site.
		items = append(items, Item{Title: detail.title, Magnet: magnets[i], GUID: detail.url, Source: source})
	}
	return items
}

// get downloads a URL with a browser user agent and returns its body and
// content type.
func get(ctx context.Context, client *http.Client, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "application/rss+xml, application/xml;q=0.9, text/html;q=0.8, */*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchBodyMax))
	return body, resp.Header.Get("Content-Type"), err
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func cleanText(value string) string { return strings.Join(strings.Fields(value), " ") }

func resolve(base *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	ref, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if base == nil {
		return ref.String()
	}
	return base.ResolveReference(ref).String()
}

// magnetKey is the info-hash of a magnet (lower case), or the magnet itself.
func magnetKey(magnet string) string {
	parsed, err := url.Parse(magnet)
	if err == nil {
		for _, xt := range parsed.Query()["xt"] {
			if hash, ok := strings.CutPrefix(strings.ToLower(xt), "urn:btih:"); ok {
				return hash
			}
		}
	}
	return magnet
}

// titleFromMagnet returns the display name (dn) of a magnet.
func titleFromMagnet(magnet string) string {
	parsed, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	return cleanText(parsed.Query().Get("dn"))
}
