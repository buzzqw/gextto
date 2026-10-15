package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestQbitCookiesAPI covers the qBittorrent cookie API: the store it reads and
// writes is the same one the settings page edits (cookies.go).
func TestQbitCookiesAPI(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()

	var cookies []storedCookie
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
