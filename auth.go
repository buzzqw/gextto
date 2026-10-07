package gextto

// auth.go is the optional access control of the web interface and the API.
// Gextto is meant for a trusted LAN, so it is off by default; when it is on,
// requests from local addresses (loopback, private LAN ranges) still pass
// without a login unless the operator turns that exemption off too. Remote
// clients log in with a username and password (signed session cookie) or send
// the API key in the X-Api-Key header (scripts, the TUI, calendar apps).

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/buzzqw/gextto/internal/logging"
)

const (
	authEnabledSetting     = "auth_enabled"
	authUsernameSetting    = "auth_username"
	authPasswordSetting    = "auth_password"
	authAPIKeySetting      = "auth_api_key"
	authLocalBypassSetting = "auth_local_bypass"
	authCookieName         = "gextto_session"
	authSessionLifetime    = 30 * 24 * time.Hour
	authPasswordHashPrefix = "bcrypt:"
	// authDisableEnv switches the access control off without touching the
	// configuration, for an operator locked out of the interface.
	authDisableEnv = "GEXTTO_AUTH_DISABLE"
)

// authSettings is the effective access-control configuration.
type authSettings struct {
	enabled      bool
	username     string
	passwordHash string
	apiKey       string
	localBypass  bool
}

func authSettingsFrom(cfg *Config) authSettings {
	if cfg == nil {
		return authSettings{}
	}
	settings := authSettings{
		enabled:      settingsBool(cfg, authEnabledSetting, false),
		username:     strings.TrimSpace(cfg.Settings[authUsernameSetting]),
		passwordHash: strings.TrimSpace(cfg.Settings[authPasswordSetting]),
		apiKey:       strings.TrimSpace(cfg.Settings[authAPIKeySetting]),
		localBypass:  settingsBool(cfg, authLocalBypassSetting, true),
	}
	if settings.username == "" {
		settings.username = "admin"
	}
	if v := strings.TrimSpace(os.Getenv(authDisableEnv)); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		settings.enabled = false
	}
	return settings
}

// active reports whether requests must be checked. Access control without
// any credential would lock everybody out, so it stays off (with a warning)
// until a password or an API key is set.
func (a authSettings) active() bool {
	return a.enabled && (a.hasPassword() || a.apiKey != "")
}

func (a authSettings) hasPassword() bool {
	return strings.HasPrefix(a.passwordHash, authPasswordHashPrefix)
}

// hashAuthPassword stores a password as a bcrypt hash; an already hashed
// value (or an empty one) is returned unchanged.
func hashAuthPassword(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, authPasswordHashPrefix) {
		return value, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return authPasswordHashPrefix + string(hash), nil
}

func (a authSettings) passwordMatches(password string) bool {
	if !a.hasPassword() {
		return false
	}
	hash := strings.TrimPrefix(a.passwordHash, authPasswordHashPrefix)
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (a authSettings) apiKeyMatches(key string) bool {
	key = strings.TrimSpace(key)
	return a.apiKey != "" && key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(a.apiKey)) == 1
}

// sessionKey signs the session cookies. It is derived from the password hash,
// so changing the password logs out every open session.
func (a authSettings) sessionKey() []byte {
	sum := sha256.Sum256([]byte("gextto-session\x00" + a.username + "\x00" + a.passwordHash))
	return sum[:]
}

func (a authSettings) sessionToken(expires time.Time) string {
	payload := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, a.sessionKey())
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a authSettings) sessionValid(token string, now time.Time) bool {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || !a.hasPassword() {
		return false
	}
	expires, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || now.Unix() >= expires {
		return false
	}
	expected := a.sessionToken(time.Unix(expires, 0))
	return hmac.Equal([]byte(token), []byte(expected)) && signature != ""
}

// isLocalAddress reports loopback, private (RFC 1918 / IPv6 ULA) and
// link-local addresses.
func isLocalAddress(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// requestIsLocal reports whether the client is on the local network. A reverse
// proxy on the same host makes every request look local, so when the request
// carries forwarding headers every address they name must be local too.
func requestIsLocal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !isLocalAddress(net.ParseIP(strings.TrimSpace(host))) {
		return false
	}
	forwarded := []string{}
	for _, value := range r.Header.Values("X-Forwarded-For") {
		forwarded = append(forwarded, strings.Split(value, ",")...)
	}
	forwarded = append(forwarded, r.Header.Values("X-Real-Ip")...)
	for _, value := range r.Header.Values("Forwarded") {
		for _, part := range strings.Split(value, ",") {
			for _, pair := range strings.Split(part, ";") {
				key, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
				if ok && strings.EqualFold(key, "for") {
					forwarded = append(forwarded, val)
				}
			}
		}
	}
	for _, value := range forwarded {
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if value == "" {
			continue
		}
		if parsedHost, _, err := net.SplitHostPort(value); err == nil {
			value = parsedHost
		}
		value = strings.Trim(value, "[]")
		if !isLocalAddress(net.ParseIP(value)) {
			return false
		}
	}
	return true
}

// authClientAddress names the client for logs and the login rate limit: the
// address a local reverse proxy appended last to X-Forwarded-For, otherwise
// the connection address.
func authClientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if !isLocalAddress(net.ParseIP(host)) {
		return host
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		if real := strings.TrimSpace(r.Header.Get("X-Real-Ip")); real != "" {
			return real
		}
		return host
	}
	parts := strings.Split(values[len(values)-1], ",")
	if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
		return last
	}
	return host
}

// authPublicPath lists what the login page itself and an installed app need
// before logging in.
func authPublicPath(path string) bool {
	switch path {
	case "/login", "/logout", "/favicon.ico", "/manifest.webmanifest", "/sw.js":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

func requestWantsJSON(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/feed") ||
		strings.Contains(r.Header.Get("Accept"), "application/json") || r.Header.Get("HX-Request") != ""
}

var authInactiveWarned sync.Once

// AuthMiddleware enforces the optional access control in front of every
// route. With access control off (the default) it only forwards the request.
func AuthMiddleware(s *AppState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := authSettingsFrom(latestConfig(s))
		if !auth.active() {
			if auth.enabled {
				authInactiveWarned.Do(func() {
					logging.Warn("Access control is enabled but no password or API key is set: the interface stays open until one is configured")
				})
			}
			next.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case "/login":
			authLoginHandler(s, auth, w, r)
			return
		case "/logout":
			http.SetCookie(w, &http.Cookie{Name: authCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if authPublicPath(r.URL.Path) || (auth.localBypass && requestIsLocal(r)) {
			next.ServeHTTP(w, r)
			return
		}
		if auth.apiKeyMatches(r.Header.Get("X-Api-Key")) || auth.apiKeyMatches(r.URL.Query().Get("apikey")) {
			next.ServeHTTP(w, r)
			return
		}
		if cookie, err := r.Cookie(authCookieName); err == nil && auth.sessionValid(cookie.Value, time.Now()) {
			next.ServeHTTP(w, r)
			return
		}
		if requestWantsJSON(r) || r.Method != http.MethodGet {
			jsonError(w, http.StatusUnauthorized, "login required")
			return
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
	})
}

// authSafeNext keeps the post-login redirect inside the application.
func authSafeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	parsed, err := url.Parse(next)
	if err != nil || parsed.Host != "" || parsed.Scheme != "" {
		return "/"
	}
	return parsed.RequestURI()
}

// authLoginAttempts slows down password guessing: after five failures from
// the same address further attempts wait for a minute.
var authLoginAttempts = struct {
	sync.Mutex
	failures map[string][]time.Time
}{failures: map[string][]time.Time{}}

func authLoginBlocked(client string, now time.Time) bool {
	authLoginAttempts.Lock()
	defer authLoginAttempts.Unlock()
	recent := authLoginAttempts.failures[client][:0]
	for _, at := range authLoginAttempts.failures[client] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	authLoginAttempts.failures[client] = recent
	return len(recent) >= 5
}

func authLoginFailed(client string, now time.Time) {
	authLoginAttempts.Lock()
	defer authLoginAttempts.Unlock()
	authLoginAttempts.failures[client] = append(authLoginAttempts.failures[client], now)
}

func authLoginHandler(s *AppState, auth authSettings, w http.ResponseWriter, r *http.Request) {
	italian := v2Language(s) == "it"
	next := authSafeNext(r.FormValue("next"))
	if r.Method != http.MethodPost {
		authRenderLogin(w, italian, next, "", http.StatusOK)
		return
	}
	client := authClientAddress(r)
	now := time.Now()
	if authLoginBlocked(client, now) {
		authRenderLogin(w, italian, next, pick(italian, "Troppi tentativi: riprova tra un minuto.", "Too many attempts: try again in a minute."), http.StatusTooManyRequests)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	if subtle.ConstantTimeCompare([]byte(username), []byte(auth.username)) != 1 || !auth.passwordMatches(r.FormValue("password")) {
		authLoginFailed(client, now)
		logging.Warn(fmt.Sprintf("Failed login to the web interface from %s", client))
		authRenderLogin(w, italian, next, pick(italian, "Utente o password errati.", "Wrong username or password."), http.StatusUnauthorized)
		return
	}
	expires := now.Add(authSessionLifetime)
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     authCookieName,
		Value:    auth.sessionToken(expires),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	logging.Info(fmt.Sprintf("🔑 Login to the web interface from %s", client))
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func pick(italian bool, it, en string) string {
	if italian {
		return it
	}
	return en
}

func authRenderLogin(w http.ResponseWriter, italian bool, next, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	lang := pick(italian, "it", "en")
	errorBlock := ""
	if message != "" {
		errorBlock = `<p class="error" role="alert">` + html.EscapeString(message) + `</p>`
	}
	fmt.Fprintf(w, `<!doctype html>
<html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Gextto — %s</title><link rel="icon" href="/favicon.ico"><link rel="manifest" href="/manifest.webmanifest">
<style>
:root{color-scheme:light dark;--bg:#f4f5f7;--card:#fff;--fg:#1d2433;--muted:#5b6577;--accent:#2f6fde;--err:#b42318}
@media (prefers-color-scheme:dark){:root{--bg:#12151b;--card:#1b2029;--fg:#e6e9ef;--muted:#9aa3b2;--accent:#6c9cff;--err:#ff8a7a}}
body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--fg);font:16px/1.5 system-ui,sans-serif;padding:16px;box-sizing:border-box}
form{background:var(--card);padding:28px;border-radius:12px;width:100%%;max-width:340px;box-shadow:0 2px 12px rgba(0,0,0,.12)}
h1{margin:0 0 4px;font-size:1.4rem}p{margin:0 0 18px;color:var(--muted)}
label{display:block;margin:12px 0 4px;font-weight:600}
input{width:100%%;box-sizing:border-box;padding:10px;border:1px solid var(--muted);border-radius:8px;background:transparent;color:inherit;font:inherit}
button{margin-top:20px;width:100%%;padding:11px;border:0;border-radius:8px;background:var(--accent);color:#fff;font:inherit;font-weight:600;cursor:pointer}
.error{color:var(--err);margin:12px 0 0}
</style></head><body>
<form method="post" action="/login">
<h1>Gextto</h1><p>%s</p>
<input type="hidden" name="next" value="%s">
<label for="u">%s</label><input id="u" name="username" autocomplete="username" required autofocus>
<label for="p">Password</label><input id="p" name="password" type="password" autocomplete="current-password" required>
%s<button type="submit">%s</button>
</form></body></html>`,
		lang,
		pick(italian, "Accesso", "Login"),
		pick(italian, "Accedi per continuare.", "Log in to continue."),
		html.EscapeString(next),
		pick(italian, "Utente", "Username"),
		errorBlock,
		pick(italian, "Entra", "Log in"),
	)
}
