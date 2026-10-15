package main

// cookies.go is the daemon's cookie store: the user-managed cookies sent when
// the daemon fetches .torrent files and metadata over HTTP, so a private
// tracker that needs a session cookie works. The settings page edits them and
// the engine and the daemon's own fetches share the same live jar. The JSON
// shape is qBittorrent's, because the compatible API (api_qbit_cookies.go) is
// one of the ways to read and write the list.

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const cookiesSettingKey = "cookies"

// storedCookie is one stored cookie.
type storedCookie struct {
	Name           string `json:"name"`
	Domain         string `json:"domain"`
	Path           string `json:"path"`
	Value          string `json:"value"`
	ExpirationDate int64  `json:"expirationDate,omitempty"`
}

func (d *Daemon) storedCookies() []storedCookie {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(cookiesSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []storedCookie
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil
	}
	return list
}

func (d *Daemon) saveCookies(list []storedCookie) error {
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
	var list []storedCookie
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
		if cookieMatches(c, u, now) {
			out = append(out, &http.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path})
		}
	}
	return out
}

// SetCookies is a no-op: the jar is user-managed through the page and the API.
func (j cookieJar) SetCookies(*url.URL, []*http.Cookie) {}

// cookieMatches reports whether a stored cookie applies to a URL: the domain
// suffix (or every host when empty) and the path prefix, and it has not expired.
func cookieMatches(c storedCookie, u *url.URL, now time.Time) bool {
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
