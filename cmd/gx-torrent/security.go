package main

// security.go holds the protections the standalone page needs when it is
// reachable from the LAN. They are active only in standalone mode: in managed
// mode Gextto drives the daemon and the page keeps behaving exactly as before.

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Standalone-only settings keys (settings.json). They let an operator put the
// daemon behind a reverse proxy or reach it by a local name, without turning
// off the protections above.
const (
	// allowedHostsKey lists extra Host values (a reverse-proxy name, a NAS
	// hostname) accepted in addition to an IP literal or localhost.
	allowedHostsKey = "allowed-hosts"
	// trustedProxiesKey lists the proxy addresses (IP or CIDR) whose
	// X-Forwarded-For / X-Real-IP may be believed.
	trustedProxiesKey = "trusted-proxies"
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
			(r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/ui/")) &&
			r.URL.Path != setupPath && r.URL.Path != uiBrowsePath {
			http.Redirect(w, r, setupPath, http.StatusSeeOther)
			return
		}
		// DNS rebinding: a public name that resolves to this machine must not
		// reach the daemon. Only an IP literal, localhost, or a hostname the
		// operator explicitly allowed (allowed-hosts) is accepted.
		if !d.hostAllowed(r.Host) {
			writeError(w, http.StatusForbidden, errors.New("host not allowed: use an IP address, localhost, or list the name in allowed-hosts"))
			return
		}
		// The page's forms POST to /ui/*; the REST API and the qBittorrent API
		// POST to /api/*. A browser always sends Origin (or Referer) on a
		// cross-site mutating request, so requiring it to match the request Host
		// blocks a page on another site from acting on the daemon — including
		// the state-changing /api routes, which have no other CSRF defence.
		if isStateChanging(r.Method) &&
			(strings.HasPrefix(r.URL.Path, "/ui/") || strings.HasPrefix(r.URL.Path, "/api/")) &&
			!sameOrigin(r) {
			writeError(w, http.StatusForbidden, errors.New("cross-origin request rejected"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isStateChanging reports whether an HTTP method mutates state and must be
// protected against cross-site requests.
func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
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

// hostAllowed is hostAllowed plus the operator's allowed-hosts: a reverse proxy
// that preserves the original Host, or access by a NAS name, is accepted when
// the name is listed in settings.json.
func (d *Daemon) hostAllowed(host string) bool {
	if hostAllowed(host) {
		return true
	}
	name := strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(name); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")
	if name == "" {
		return false
	}
	for _, allowed := range d.allowedHosts() {
		if strings.EqualFold(allowed, name) {
			return true
		}
	}
	return false
}

// allowedHosts returns the extra Host values accepted (the "allowed-hosts"
// setting: a comma/newline separated list). The parsed list is cached: the
// setting only changes when settings.json is rewritten.
func (d *Daemon) allowedHosts() []string {
	if d.opts.Settings == nil {
		return nil
	}
	raw := d.opts.Settings.Get(allowedHostsKey, "")
	d.secMu.Lock()
	defer d.secMu.Unlock()
	if raw == d.hostsRaw {
		return d.hosts
	}
	d.hostsRaw, d.hosts = raw, splitList(raw)
	return d.hosts
}

// trustedProxies returns the networks whose forwarding headers may be believed
// (the "trusted-proxies" setting: IPs or CIDRs, comma/newline separated). The
// parsed networks are cached (a request calls this on every clientIP).
func (d *Daemon) trustedProxies() []*net.IPNet {
	if d.opts.Settings == nil {
		return nil
	}
	raw := d.opts.Settings.Get(trustedProxiesKey, "")
	d.secMu.Lock()
	defer d.secMu.Unlock()
	if raw == d.proxiesRaw {
		return d.proxies
	}
	var out []*net.IPNet
	for _, item := range splitList(raw) {
		if !strings.Contains(item, "/") {
			ip := net.ParseIP(item)
			if ip == nil {
				continue
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			item = fmt.Sprintf("%s/%d", item, bits)
		}
		if _, network, err := net.ParseCIDR(item); err == nil {
			out = append(out, network)
		}
	}
	d.proxiesRaw, d.proxies = raw, out
	return out
}

// ipInNetworks reports whether ip falls in any of the given networks.
func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// hasForwardedHeader reports whether the request carries a proxy forwarding
// header.
func hasForwardedHeader(r *http.Request) bool {
	return len(r.Header.Values("X-Forwarded-For")) > 0 || strings.TrimSpace(r.Header.Get("X-Real-Ip")) != ""
}

// forwardedClientIP extracts the real client address a proxy claims: the
// rightmost X-Forwarded-For entry that is not itself a trusted proxy, else
// X-Real-IP. It returns nil when no usable value is present.
func forwardedClientIP(r *http.Request, trusted []*net.IPNet) net.IP {
	var chain []string
	for _, value := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				chain = append(chain, part)
			}
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		ip := parseForwardedIP(chain[i])
		if ip == nil || ipInNetworks(ip, trusted) {
			continue
		}
		return ip
	}
	return parseForwardedIP(r.Header.Get("X-Real-Ip"))
}

// parseForwardedIP parses an address that may carry a port or brackets.
func parseForwardedIP(value string) net.IP {
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if value == "" {
		return nil
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return net.ParseIP(strings.Trim(value, "[]"))
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
