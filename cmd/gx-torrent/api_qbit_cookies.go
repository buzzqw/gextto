package main

// api_qbit_cookies.go implements the qBittorrent cookie API and the cookie jar
// used when the daemon fetches .torrent files and metadata over HTTP, so a
// private tracker that needs a session cookie works. The cookies are the
// user-managed set (app/setCookies); response cookies are not persisted.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const cookiesSettingKey = "cookies"

// qbitCookie is one stored cookie, in the qBittorrent shape.
type qbitCookie struct {
	Name           string `json:"name"`
	Domain         string `json:"domain"`
	Path           string `json:"path"`
	Value          string `json:"value"`
	ExpirationDate int64  `json:"expirationDate,omitempty"`
}

func (d *Daemon) storedCookies() []qbitCookie {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(cookiesSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []qbitCookie
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}

func (d *Daemon) saveCookies(list []qbitCookie) error {
	if d.opts.Settings == nil {
		return nil
	}
	if len(list) == 0 {
		d.opts.Settings.Set(cookiesSettingKey, "")
		return d.opts.Settings.Save()
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	d.opts.Settings.Set(cookiesSettingKey, string(raw))
	return d.opts.Settings.Save()
}

// cookiesPretty renders the stored cookies as indented JSON for the settings
// page (empty when there are none).
func (d *Daemon) cookiesPretty() string {
	cookies := d.storedCookies()
	if len(cookies) == 0 {
		return ""
	}
	raw, err := json.MarshalIndent(cookies, "", "  ")
	if err != nil {
		return ""
	}
	return string(raw)
}

// saveCookiesFromForm validates and stores the cookies textarea: a JSON array of
// cookie objects, or empty to clear them.
func (d *Daemon) saveCookiesFromForm(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return d.saveCookies(nil)
	}
	var list []qbitCookie
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return errors.New("expecting a JSON array of cookies")
	}
	return d.saveCookies(list)
}

// cookieJar is the live http.CookieJar used by the engine and by the daemon's
// own fetches. It reads the stored cookies on every request, so setCookies
// takes effect at once without rebuilding the session.
type cookieJar struct{ d *Daemon }

// cookieJar returns the daemon's cookie jar (a fresh handle to the same store).
func (d *Daemon) cookieJar() http.CookieJar { return cookieJar{d: d} }

func (j cookieJar) Cookies(u *url.URL) []*http.Cookie {
	if u == nil {
		return nil
	}
	now := time.Now()
	var out []*http.Cookie
	for _, c := range j.d.storedCookies() {
		if qbitCookieMatches(c, u, now) {
			out = append(out, &http.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path})
		}
	}
	return out
}

// SetCookies is a no-op: the jar is user-managed through the API.
func (j cookieJar) SetCookies(*url.URL, []*http.Cookie) {}

// qbitCookieMatches reports whether a stored cookie applies to a URL: the
// domain suffix (or every host when empty) and the path prefix, and it has not
// expired.
func qbitCookieMatches(c qbitCookie, u *url.URL, now time.Time) bool {
	if c.ExpirationDate > 0 && now.Unix() > c.ExpirationDate {
		return false
	}
	domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(c.Domain)), ".")
	if domain != "" {
		host := strings.ToLower(u.Hostname())
		if host != domain && !strings.HasSuffix(host, "."+domain) {
			return false
		}
	}
	path := c.Path
	if path == "" {
		path = "/"
	}
	return strings.HasPrefix(u.Path, path)
}

func (d *Daemon) handleQbitGetCookies(w http.ResponseWriter, _ *http.Request) {
	out := d.storedCookies()
	if out == nil {
		out = []qbitCookie{}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleQbitSetCookies accepts a JSON array of cookies (the qBittorrent body)
// or a "cookies" form field.
func (d *Daemon) handleQbitSetCookies(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.FormValue("cookies"))
	if raw == "" {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		raw = strings.TrimSpace(string(body))
	}
	if raw == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var list []qbitCookie
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		http.Error(w, "invalid cookies", http.StatusBadRequest)
		return
	}
	if err := d.saveCookies(list); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
