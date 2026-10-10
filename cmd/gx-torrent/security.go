package main

// security.go holds the protections the standalone page needs when it is
// reachable from the LAN. They are active only in standalone mode: in managed
// mode Gextto drives the daemon and the page keeps behaving exactly as before.

import (
	"errors"
	"net"
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
		// First run: send the browser to the wizard until it has been done.
		if !d.setupComplete() && r.Method == http.MethodGet &&
			(r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/ui/")) && r.URL.Path != setupPath {
			http.Redirect(w, r, setupPath, http.StatusSeeOther)
			return
		}
		// DNS rebinding: a public name that resolves to this machine must not
		// reach the daemon. Only an IP literal or localhost is accepted.
		if !hostAllowed(r.Host) {
			writeError(w, http.StatusForbidden, errors.New("host not allowed: use an IP address or localhost"))
			return
		}
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

// hostAllowed reports whether the request Host is an IP literal (IPv4 or IPv6)
// or localhost. A plain hostname is rejected: it is the DNS-rebinding vector,
// because an attacker's name can resolve to this machine.
func hostAllowed(host string) bool {
	name := strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(name); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")
	if name == "" {
		return false
	}
	if strings.EqualFold(name, "localhost") {
		return true
	}
	return net.ParseIP(name) != nil
}

// lanSource reports whether an address is on the local network: loopback,
// RFC 1918 (10/8, 172.16/12, 192.168/16), RFC 4193 ULA (fc00::/7) or link-local
// (169.254/16, fe80::/10). It is the definition of "LAN" used to allow the
// standalone page without a password.
func lanSource(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
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
