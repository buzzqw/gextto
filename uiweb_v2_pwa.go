package gextto

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// The web interface can be installed on a phone as an app (PWA). Installed,
// it opens full screen and appears in the system "Share" menu: sharing a
// magnet or a .torrent link to Gextto opens Scarico with the link ready to add.

const v2Manifest = `{
  "name": "Gextto",
  "short_name": "Gextto",
  "description": "EXpert Torrent Transfer Orchestrator",
  "start_url": "/?view=dashboard",
  "scope": "/",
  "display": "standalone",
  "background_color": "#0b1220",
  "theme_color": "#0b1220",
  "icons": [
    {"src": "/static/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any maskable"},
    {"src": "/static/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any maskable"}
  ],
  "share_target": {
    "action": "/share",
    "method": "GET",
    "params": {"title": "title", "text": "text", "url": "url"}
  }
}
`

// v2ServiceWorker only makes the app installable: it caches nothing, so the
// interface always shows live data.
const v2ServiceWorker = `self.addEventListener("install", function () { self.skipWaiting(); });
self.addEventListener("activate", function (event) { event.waitUntil(self.clients.claim()); });
`

// V2Manifest serves the web app manifest.
func V2Manifest(w http.ResponseWriter, r *http.Request, s *AppState) {
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write([]byte(v2Manifest))
}

// V2ServiceWorker serves the service worker from the site root, so its scope
// covers the whole interface.
func V2ServiceWorker(w http.ResponseWriter, r *http.Request, s *AppState) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(v2ServiceWorker))
}

var v2SharedLinkPattern = regexp.MustCompile(`(?i)(magnet:\?[^\s"'<>]+|https?://[^\s"'<>]+)`)

// v2SharedLink picks the link to add from what another app shared: a magnet
// first, otherwise the first http(s) link (a .torrent file URL).
func v2SharedLink(values ...string) string {
	var fallback string
	for _, value := range values {
		for _, match := range v2SharedLinkPattern.FindAllString(value, -1) {
			if strings.HasPrefix(strings.ToLower(match), "magnet:") {
				return match
			}
			if fallback == "" {
				fallback = match
			}
		}
	}
	return fallback
}

// V2Share receives the system share sheet: it never adds anything by itself,
// it opens Scarico with the shared link in the "add" field.
func V2Share(w http.ResponseWriter, r *http.Request, s *AppState) {
	link := v2SharedLink(r.FormValue("url"), r.FormValue("text"), r.FormValue("title"))
	// The shared link only ever lands in the query string of a local path, so
	// the redirect cannot leave the app.
	query := url.Values{"view": {"downloads"}}
	if link == "" {
		query.Set("msg_err", "1")
		query.Set("msg", "Nessun magnet o link .torrent nel contenuto condiviso.")
	} else {
		query.Set("add", link)
		query.Set("msg", "Link ricevuto: controlla le opzioni e premi Aggiungi.")
	}
	http.Redirect(w, r, (&url.URL{Path: "/", RawQuery: query.Encode()}).String(), http.StatusSeeOther)
}
