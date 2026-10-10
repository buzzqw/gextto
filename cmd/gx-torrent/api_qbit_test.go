package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func qbitLogin(t *testing.T, base, user, password string) (int, string, []*http.Cookie) {
	t.Helper()
	resp, err := http.PostForm(base+"/api/v2/auth/login", url.Values{"username": {user}, "password": {password}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(body)), resp.Cookies()
}

func sidOf(cookies []*http.Cookie) string {
	for _, c := range cookies {
		if c.Name == qbitSIDCookie {
			return c.Value
		}
	}
	return ""
}

func TestQbitAPIRoutes(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	// Without a session the API is forbidden.
	resp, err := http.Get(srv.URL + "/api/v2/app/version")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthenticated version: %d, want 403", resp.StatusCode)
	}

	// Wrong credentials: qBittorrent answers 200 "Fails.".
	if code, body, _ := qbitLogin(t, srv.URL, "admin", "nope"); code != http.StatusOK || body != qbitInvalidCode {
		t.Fatalf("bad login: %d %q", code, body)
	}

	// Correct credentials: 200 "Ok." and a SID cookie.
	code, body, cookies := qbitLogin(t, srv.URL, "admin", "s3cret")
	if code != http.StatusOK || body != qbitOKCode {
		t.Fatalf("good login: %d %q", code, body)
	}
	sid := sidOf(cookies)
	if sid == "" {
		t.Fatal("login did not set a SID cookie")
	}

	// The session lists torrents (an empty array here).
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v2/torrents/info", nil)
	req.AddCookie(&http.Cookie{Name: qbitSIDCookie, Value: sid})
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("torrents/info: %d", resp.StatusCode)
	}
	var list []qbitTorrent
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("torrents/info is not a JSON array: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected an empty list, got %d", len(list))
	}

	// Logout drops the session.
	if _, body, _ := qbitLogin(t, srv.URL, "admin", "s3cret"); body != qbitOKCode {
		t.Fatalf("login for logout failed: %q", body)
	}
}

func TestQbitOpenWithoutPassword(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v2/app/webapiVersion")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != qbitWebAPIVer {
		t.Fatalf("webapiVersion: %d %q", resp.StatusCode, body)
	}
}

func TestQbitNotExposedInManaged(t *testing.T) {
	d := newTestDaemon(t) // managed by default
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v2/app/version")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("managed mode must not expose /api/v2: %d", resp.StatusCode)
	}
}

func TestQbitMutatingEndpoints(t *testing.T) {
	d := standaloneTestDaemon(t, "", true) // no password: open
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	for _, action := range []string{"pause", "resume", "recheck", "delete"} {
		resp, err := http.PostForm(srv.URL+"/api/v2/torrents/"+action, url.Values{"hashes": {"abc"}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", action, resp.StatusCode)
		}
	}

	resp, err := http.PostForm(srv.URL+"/api/v2/torrents/add", url.Values{"urls": {"not-a-torrent"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("add with an invalid url: status %d, want 415", resp.StatusCode)
	}
}

func TestQbitStateMapping(t *testing.T) {
	cases := map[string]string{
		"downloading":    "downloading",
		"seeding":        "uploading",
		"paused":         "pausedDL",
		"stalled":        "stalledDL",
		"checking_files": "checkingDL",
		"moving":         "moving",
		"error":          "error",
		"weird":          "unknown",
	}
	for in, want := range cases {
		if got := qbitState(in); got != want {
			t.Errorf("qbitState(%q) = %q, want %q", in, got, want)
		}
	}
}
