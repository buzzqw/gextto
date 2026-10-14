package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestQbitCookiesAPI covers the qBittorrent cookie API and the live jar: the
// stored cookies are applied to the matching hosts only.
func TestQbitCookiesAPI(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	var cookies []qbitCookie
	getJSONInto(t, srv.URL+"/api/v2/app/cookies", &cookies)
	if len(cookies) != 0 {
		t.Fatalf("cookies = %+v, want empty", cookies)
	}

	body := `[` +
		`{"name":"sid","value":"abc","domain":"tracker.example","path":"/"},` +
		`{"name":"flag","value":"1","domain":"","path":"/"},` +
		`{"name":"expired","value":"x","domain":"tracker.example","path":"/","expirationDate":1}` +
		`]`
	resp, err := http.Post(srv.URL+"/api/v2/app/setCookies", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setCookies: %d", resp.StatusCode)
	}

	cookies = nil
	getJSONInto(t, srv.URL+"/api/v2/app/cookies", &cookies)
	if len(cookies) != 3 {
		t.Fatalf("cookies = %+v, want 3", cookies)
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

	// An invalid body is refused.
	resp2, err := http.Post(srv.URL+"/api/v2/app/setCookies", "application/json", strings.NewReader("nope"))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid cookies: %d, want 400", resp2.StatusCode)
	}
}

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

func TestQbitCookieMatches(t *testing.T) {
	now := time.Now()
	u, _ := url.Parse("http://sub.tracker.example/announce?x=1")
	cases := []struct {
		cookie qbitCookie
		want   bool
	}{
		{qbitCookie{Name: "a", Domain: "tracker.example", Path: "/"}, true},
		{qbitCookie{Name: "a", Domain: ".tracker.example", Path: "/announce"}, true},
		{qbitCookie{Name: "a", Domain: "example", Path: "/"}, true},
		{qbitCookie{Name: "a", Domain: "other.example", Path: "/"}, false},
		{qbitCookie{Name: "a", Domain: "", Path: "/"}, true},
		{qbitCookie{Name: "a", Domain: "tracker.example", Path: "/other"}, false},
		{qbitCookie{Name: "a", Domain: "tracker.example", Path: "/", ExpirationDate: now.Add(-time.Hour).Unix()}, false},
		{qbitCookie{Name: "a", Domain: "tracker.example", Path: "/", ExpirationDate: now.Add(time.Hour).Unix()}, true},
	}
	for _, c := range cases {
		if got := qbitCookieMatches(c.cookie, u, now); got != c.want {
			t.Errorf("qbitCookieMatches(%+v) = %v, want %v", c.cookie, got, c.want)
		}
	}
}
