package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestCookiesSettingsForm covers the settings-page cookies editor: a JSON array
// is accepted, invalid JSON is refused, and empty clears them.
func TestCookiesSettingsForm(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	if err := d.saveCookiesFromForm("not json"); err == nil {
		t.Fatal("invalid JSON must be refused")
	}
	body := `[{"name":"sid","value":"abc","domain":"tracker.example","path":"/"}]`
	if err := d.saveCookiesFromForm(body); err != nil {
		t.Fatal(err)
	}
	got := d.storedCookies()
	if len(got) != 1 || got[0].Name != "sid" || got[0].Domain != "tracker.example" {
		t.Fatalf("cookies = %+v", got)
	}
	if !strings.Contains(d.cookiesPretty(), "tracker.example") {
		t.Fatalf("cookiesPretty = %q", d.cookiesPretty())
	}
	if err := d.saveCookiesFromForm(""); err != nil {
		t.Fatal(err)
	}
	if len(d.storedCookies()) != 0 {
		t.Fatal("cookies not cleared")
	}
}

// TestCookiesJar covers the live jar the engine and the daemon's fetches share:
// the stored cookies are applied to the matching hosts only.
func TestCookiesJar(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	if err := d.saveCookies([]storedCookie{
		{Name: "sid", Value: "abc", Domain: "tracker.example", Path: "/"},
		{Name: "flag", Value: "1", Domain: "", Path: "/"},
		{Name: "expired", Value: "x", Domain: "tracker.example", Path: "/", ExpirationDate: 1},
	}); err != nil {
		t.Fatal(err)
	}
	jar := d.cookieJar()
	names := func(u string) map[string]bool {
		parsed, err := url.Parse(u)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, c := range jar.Cookies(parsed) {
			out[c.Name] = true
		}
		return out
	}
	// tracker.example: the host cookie and the host-less one, not the expired.
	got := names("http://tracker.example/announce")
	if !got["sid"] || !got["flag"] || got["expired"] {
		t.Fatalf("jar cookies for tracker.example = %v", got)
	}
	// another host: only the host-less cookie.
	got = names("http://other.example/")
	if got["sid"] || !got["flag"] {
		t.Fatalf("jar cookies for other.example = %v", got)
	}
}

func TestCookieMatches(t *testing.T) {
	now := time.Now()
	u, _ := url.Parse("http://sub.tracker.example/announce?x=1")
	cases := []struct {
		cookie storedCookie
		want   bool
	}{
		{storedCookie{Name: "a", Domain: "tracker.example", Path: "/"}, true},
		{storedCookie{Name: "a", Domain: ".tracker.example", Path: "/announce"}, true},
		{storedCookie{Name: "a", Domain: "example", Path: "/"}, true},
		{storedCookie{Name: "a", Domain: "other.example", Path: "/"}, false},
		{storedCookie{Name: "a", Domain: "", Path: "/"}, true},
		{storedCookie{Name: "a", Domain: "tracker.example", Path: "/other"}, false},
		{storedCookie{Name: "a", Domain: "tracker.example", Path: "/", ExpirationDate: now.Add(-time.Hour).Unix()}, false},
		{storedCookie{Name: "a", Domain: "tracker.example", Path: "/", ExpirationDate: now.Add(time.Hour).Unix()}, true},
	}
	for _, c := range cases {
		if got := cookieMatches(c.cookie, u, now); got != c.want {
			t.Errorf("cookieMatches(%+v) = %v, want %v", c.cookie, got, c.want)
		}
	}
}
