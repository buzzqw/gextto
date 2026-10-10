package main

// standalone_auth.go is the standalone page's access control: a password (a
// bcrypt hash in settings.json) and a login session. Managed mode never uses
// it, so Gextto's use of the daemon is unchanged.

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"

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

// clientIP is the address of the request's peer, read from the socket. A
// reverse proxy header is deliberately ignored: only declared proxies could be
// trusted, and none are configured yet.
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
	if d.standaloneLocalBypass() && lanSource(clientIP(r)) {
		return true
	}
	if cookie, err := r.Cookie(uiSessionCookie); err == nil && d.sessions != nil && d.sessions.Valid(cookie.Value) {
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
		d.renderLogin(w, r, false)
		return
	}
	if !d.loginMatches(strings.TrimSpace(r.FormValue("user")), r.FormValue("password")) {
		d.renderLogin(w, r, true)
		return
	}
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

// renderLogin writes the sign-in page, localized like the daemon page.
func (d *Daemon) renderLogin(w http.ResponseWriter, r *http.Request, failed bool) {
	page := assets.LoginPage()
	errorHTML := ""
	if failed {
		errorHTML = `<p class="m err">Wrong username or password.</p>`
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
