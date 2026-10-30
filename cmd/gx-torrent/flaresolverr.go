package main

// flaresolverr.go wires the standalone daemon to an optional FlareSolverr
// service: feeds and Torznab indexers behind a Cloudflare challenge are fetched
// through it. It mirrors Gextto's behaviour — try the direct request first, fall
// back to FlareSolverr only for remote hosts (a LAN service that fails is not a
// Cloudflare problem) — with a stateless client (internal/flaresolverr).

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/flaresolverr"
	"github.com/buzzqw/gextto/internal/gxcore/torrent"
	"github.com/buzzqw/gextto/internal/rss"
)

// flareURLKey holds the FlareSolverr service URL in settings.json (standalone).
const flareURLKey = "flaresolverr-url"

// maxRemoteBody caps a page read through the direct fetch.
const maxRemoteBody = 32 << 20

// flareEndpoint returns the configured FlareSolverr /v1 endpoint, or "".
func (d *Daemon) flareEndpoint() string {
	if d.opts.Settings == nil {
		return ""
	}
	return flaresolverr.Endpoint(d.opts.Settings.Get(flareURLKey, ""))
}

// remoteGet fetches rawURL and returns the body. It tries the direct request
// first; on a network error or a non-2xx status, when a FlareSolverr URL is
// configured and the host is not local, it retries through FlareSolverr. It is
// the Torznab transport (see torznab.Indexer.Get) and the feed fallback.
func (d *Daemon) remoteGet(ctx context.Context, rawURL string) ([]byte, error) {
	data, status, err := directGet(ctx, rawURL)
	if err == nil && status >= 200 && status < 300 {
		return data, nil
	}
	original := err
	if err == nil {
		original = fmt.Errorf("HTTP %d", status)
	}
	ep := d.flareEndpoint()
	// With a proxy configured FlareSolverr would fetch the page on its own
	// network and bypass the tunnel, so the direct error stands. Run
	// FlareSolverr behind the same proxy if you need it.
	if ep == "" || isLocalURL(rawURL) || torrent.ProxyEnabled() {
		return nil, original
	}
	if solved, ferr := flaresolverr.Get(ctx, ep, rawURL); ferr == nil {
		return solved, nil
	}
	return nil, original
}

// directGet performs one plain GET and returns the body and the status. A non-2xx
// status is returned, not turned into an error, so the caller can decide to fall
// back to FlareSolverr.
func directGet(ctx context.Context, rawURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := outboundClient(0).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxRemoteBody))
	return data, resp.StatusCode, nil
}

// isLocalURL reports whether a URL points at localhost or a private/LAN address,
// where FlareSolverr cannot help.
func isLocalURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return lanSource(ip)
	}
	return false
}

// fetchFeedItems reads a feed: directly, and through FlareSolverr when the
// direct read fails on a remote host (a Cloudflare-protected RSS/HTML page).
func (d *Daemon) fetchFeedItems(ctx context.Context, client *http.Client, rawURL, name string) ([]rss.Item, error) {
	items, err := rss.Fetch(ctx, client, rawURL, name)
	if err == nil {
		return items, nil
	}
	ep := d.flareEndpoint()
	// As in remoteGet: FlareSolverr would fetch on its own network.
	if ep == "" || isLocalURL(rawURL) || torrent.ProxyEnabled() {
		return nil, err
	}
	data, ferr := flaresolverr.Get(ctx, ep, rawURL)
	if ferr != nil {
		return nil, err
	}
	parsed, perr := rss.Parse(bytes.NewReader(data), name)
	if perr != nil {
		return nil, err
	}
	return parsed, nil
}

// handleUIRssFlareTest checks the FlareSolverr URL typed in the field (before it
// is saved) and answers with the round-trip time or the error. It is the RSS
// page's Test FlareSolverr button.
func (d *Daemon) handleUIRssFlareTest(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	raw := strings.TrimSpace(r.FormValue("url"))
	if raw == "" {
		raw = d.opts.Settings.Get(flareURLKey, "")
	}
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), flaresolverr.DefaultTimeout+15*time.Second)
	defer cancel()
	err := flaresolverr.Test(ctx, raw)
	ms := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "ms": ms})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ms": ms})
}
