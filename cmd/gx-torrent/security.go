package main

// security.go holds the protections the standalone page needs when it is
// reachable from the LAN. They are active only in standalone mode: in managed
// mode Gextto drives the daemon and the page keeps behaving exactly as before.

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// standaloneGuard wraps the daemon's HTTP routes with the standalone security
// checks. In managed mode it returns the handler untouched.
func (d *Daemon) standaloneGuard(next http.Handler) http.Handler {
	if d.opts.Mode != ModeStandalone {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The page's forms POST to /ui/*. A browser always sends Origin (or
		// Referer) on a cross-site POST, so requiring it to match the request
		// Host blocks a page on another site from acting on the daemon.
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/ui/") && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, errors.New("cross-origin request rejected"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// sameOrigin reports whether a POST comes from the page itself. A request with
// no Origin and no Referer (a non-browser client, or a same-origin GET) is not
// a CSRF vector and is allowed; when a header is present it must match Host.
func sameOrigin(r *http.Request) bool {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		return originMatchesHost(origin, r.Host)
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		return originMatchesHost(referer, r.Host)
	}
	return true
}

// originMatchesHost compares the host of an Origin or Referer value with the
// Host the request was sent to. An unparseable value, or the literal "null"
// (a sandboxed document), never matches.
func originMatchesHost(raw, host string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, host)
}
