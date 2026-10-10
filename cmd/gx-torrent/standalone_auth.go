package main

// standalone_auth.go is the standalone page's access control: a password (a
// bcrypt hash in settings.json) and a login session. Managed mode never uses
// it, so Gextto's use of the daemon is unchanged.

import (
	"crypto/subtle"
	"errors"
	"html"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/auth"
	"github.com/buzzqw/gextto/internal/webui/assets"
)

const (
	uiSessionCookie       = "gx_session"
	standaloneUserKey     = "auth-user"
	standalonePasswordKey = "auth-password"
	standaloneBypassKey   = "local-bypass"
	defaultStandaloneUser = "admin"
)

func (d *Daemon) standaloneSetting(key, fallback string) string {
	if d.opts.Settings == nil {
		return fallback
	}
	return d.opts.Settings.Get(key, fallback)
}

func (d *Daemon) standaloneUser() string {
	return strings.TrimSpace(d.standaloneSetting(standaloneUserKey, defaultStandaloneUser))
}

func (d *Daemon) standalonePassword() string {
	return strings.TrimSpace(d.standaloneSetting(standalonePasswordKey, ""))
}

// standaloneLocalBypass is on by default: requests from the LAN do not need a
// password.
func (d *Daemon) standaloneLocalBypass() bool {
	return strings.ToLower(d.standaloneSetting(standaloneBypassKey, "true")) != "false"
}

// standaloneAuthActive reports whether a password is configured. With no
// password the page stays open, so a fresh standalone run can be reached to
// complete the setup.
func (d *Daemon) standaloneAuthActive() bool {
	return d.standalonePassword() != ""
}

// clientIP is the raw peer of the connection, read from the socket. The
// daemon method clientIP adds the trusted-proxy handling on top.
func clientIP(r *http.Request) net.IP {
	host := strings.TrimSpace(r.RemoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return net.ParseIP(host)
}

// standaloneAuthorized decides whether a page request may proceed: no password
// configured, a LAN source with the bypass on, or a valid session cookie.
// Otherwise it sends the visitor to the login page.
func (d *Daemon) standaloneAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if !d.standaloneAuthActive() {
		return true
	}
	if d.standaloneLocalBypass() && lanSource(d.clientIP(r)) {
		return true
	}
	if d.sessionCookieValid(r) {
		return true
	}
	if r.Method == http.MethodGet {
		next := r.URL.RequestURI()
		if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
			next = "/"
		}
		http.Redirect(w, r, "/ui/login?next="+next, http.StatusSeeOther)
		return false
	}
	writeError(w, http.StatusUnauthorized, errors.New("authentication required"))
	return false
}

// standaloneAPIAllowed is the /api/v1 counterpart of standaloneAuthorized: the
// same criteria (no password, LAN bypass, or a valid session) but it answers a
// boolean so the caller can return 401 instead of redirecting a programmatic
// client to the login page. Accepting both the page cookie (gx_session) and the
// qBittorrent one (SID) lets the same session drive the page and the API.
func (d *Daemon) standaloneAPIAllowed(r *http.Request) bool {
	if !d.standaloneAuthActive() {
		return true
	}
	if d.standaloneLocalBypass() && lanSource(d.clientIP(r)) {
		return true
	}
	return d.sessionCookieValid(r)
}

// sessionCookieValid reports whether the request carries a live login session
// in any of the cookies the standalone daemon sets.
func (d *Daemon) sessionCookieValid(r *http.Request) bool {
	if d.sessions == nil {
		return false
	}
	for _, name := range []string{uiSessionCookie, qbitSIDCookie} {
		if cookie, err := r.Cookie(name); err == nil && d.sessions.Valid(cookie.Value) {
			return true
		}
	}
	return false
}

// clientIP is the address used for the LAN check. Normally it is the peer of
// the socket; when a declared reverse proxy (trusted-proxies) forwards the
// request, the client address it names is used instead. With a proxy but no
// declared trusted-proxies, a loopback peer is not treated as LAN when a
// forwarding header is present, so a forwarded Internet client still logs in.
func (d *Daemon) clientIP(r *http.Request) net.IP {
	peer := clientIP(r)
	trusted := d.trustedProxies()
	if len(trusted) > 0 && peer != nil && ipInNetworks(peer, trusted) {
		if forwarded := forwardedClientIP(r, trusted); forwarded != nil {
			return forwarded
		}
	}
	if peer != nil && peer.IsLoopback() && len(trusted) == 0 && hasForwardedHeader(r) {
		return nil
	}
	return peer
}

// Login rate limiting: qBittorrent bans an address after five failed attempts.
// Without it, a standalone daemon with a password can be brute-forced at full
// speed over the LAN or the Internet.
const (
	loginMaxFailures = 5
	loginBlockFor    = 15 * time.Minute
)

type loginAttempt struct {
	count        int
	last         time.Time
	blockedUntil time.Time
}

// clientIPString is the rate-limit key: the peer address, or "" when it cannot
// be determined (the counter is then skipped rather than shared by everyone).
func (d *Daemon) clientIPString(r *http.Request) string {
	if ip := d.clientIP(r); ip != nil {
		return ip.String()
	}
	return ""
}

// loginAllowed reports whether an address may try to log in; a blocked address
// is refused before the password is even checked.
func (d *Daemon) loginAllowed(ip string) bool {
	if ip == "" {
		return true
	}
	d.loginMu.Lock()
	defer d.loginMu.Unlock()
	attempt, ok := d.loginFails[ip]
	if !ok {
		return true
	}
	return !time.Now().Before(attempt.blockedUntil)
}

// recordLoginFailure counts a failed attempt and blocks the address once it
// reaches the threshold; a long idle gap resets the counter.
func (d *Daemon) recordLoginFailure(ip string) {
	if ip == "" {
		return
	}
	d.loginMu.Lock()
	defer d.loginMu.Unlock()
	if d.loginFails == nil {
		d.loginFails = map[string]loginAttempt{}
	}
	now := time.Now()
	attempt := d.loginFails[ip]
	if now.Sub(attempt.last) > loginBlockFor {
		attempt.count = 0
	}
	attempt.count++
	attempt.last = now
	if attempt.count >= loginMaxFailures {
		attempt.blockedUntil = now.Add(loginBlockFor)
	}
	d.loginFails[ip] = attempt
}

// clearLoginFailures forgets the failures of an address after a good login.
func (d *Daemon) clearLoginFailures(ip string) {
	if ip == "" {
		return
	}
	d.loginMu.Lock()
	defer d.loginMu.Unlock()
	delete(d.loginFails, ip)
}

// loginMatches checks the submitted credentials against the configured ones.
func (d *Daemon) loginMatches(user, password string) bool {
	if d.sessions == nil {
		return false
	}
	stored := d.standalonePassword()
	if stored == "" || password == "" {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(user), []byte(d.standaloneUser())) != 1 {
		return false
	}
	return auth.VerifyPassword(stored, password)
}

// handleUILogin renders the sign-in form (GET) and processes it (POST).
func (d *Daemon) handleUILogin(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		d.renderLogin(w, r, "")
		return
	}
	ip := d.clientIPString(r)
	if !d.loginAllowed(ip) {
		d.renderLogin(w, r, "Too many failed attempts. Try again later.")
		return
	}
	if !d.loginMatches(strings.TrimSpace(r.FormValue("user")), r.FormValue("password")) {
		d.recordLoginFailure(ip)
		d.renderLogin(w, r, "Wrong username or password.")
		return
	}
	d.clearLoginFailures(ip)
	token := d.sessions.Create()
	http.SetCookie(w, &http.Cookie{
		Name:     uiSessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(d.sessions.TTL().Seconds()),
	})
	target := strings.TrimSpace(r.URL.Query().Get("next"))
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// renderLogin writes the sign-in page, localized like the daemon page. errKey
// is shown as an error when non-empty.
func (d *Daemon) renderLogin(w http.ResponseWriter, r *http.Request, errKey string) {
	page := assets.LoginPage()
	errorHTML := ""
	if errKey != "" {
		errorHTML = `<p class="m err">` + html.EscapeString(errKey) + `</p>`
	}
	page = strings.Replace(page, "<!--ERROR-->", errorHTML, 1)
	lang := d.uiLang(r)
	if lang != "" && lang != "en" {
		page = strings.Replace(page, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	page = uiTranslateHTML(page, uiDictionary(lang))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}
