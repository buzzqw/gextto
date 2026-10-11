package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/auth"
	"github.com/buzzqw/gextto/internal/settings"
)

func standaloneTestDaemon(t *testing.T, password string, bypass bool) *Daemon {
	t.Helper()
	store, err := settings.Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if password != "" {
		hash, hashErr := auth.HashPassword(password)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		store.Set(standalonePasswordKey, hash)
	}
	store.Set(standaloneBypassKey, fmt.Sprintf("%t", bypass))
	return &Daemon{
		opts:     Options{Mode: ModeStandalone, Settings: store, StatePath: filepath.Join(t.TempDir(), "state.json")},
		sessions: auth.NewSessions(time.Hour),
	}
}

func TestStandaloneAuthNoPasswordIsOpen(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	if !d.standaloneAuthorized(rec, req) {
		t.Fatal("with no password the page must be open")
	}
}

func TestStandaloneAuthLocalBypass(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	req.RemoteAddr = "192.168.1.50:1234"
	if !d.standaloneAuthorized(rec, req) {
		t.Fatal("a LAN source must be allowed while the bypass is on")
	}
}

func TestStandaloneAuthRemoteRedirectsToLogin(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	if d.standaloneAuthorized(rec, req) {
		t.Fatal("a remote source without a session must not be allowed")
	}
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/ui/login") {
		t.Fatalf("expected a redirect to the login page, got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestStandaloneAuthSessionCookie(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", false)
	token := d.sessions.Create()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	req.AddCookie(&http.Cookie{Name: uiSessionCookie, Value: token})
	if !d.standaloneAuthorized(rec, req) {
		t.Fatal("a valid session cookie must be accepted")
	}
}

func TestLoginMatches(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	if !d.loginMatches("admin", "s3cret") {
		t.Fatal("correct credentials rejected")
	}
	if d.loginMatches("admin", "wrong") {
		t.Fatal("wrong password accepted")
	}
	if d.loginMatches("root", "s3cret") {
		t.Fatal("wrong username accepted")
	}
}

func TestHandleUILogin(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)

	// Wrong password: the form is re-rendered with an error, no cookie.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/login", strings.NewReader("user=admin&password=nope"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	d.handleUILogin(rec, req)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("wrong login: status=%d cookies=%d", rec.Code, len(rec.Result().Cookies()))
	}

	// Correct password: a session cookie and a redirect to next.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/login?next=/?open=abc", strings.NewReader("user=admin&password=s3cret"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	d.handleUILogin(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("correct login: status=%d", rec.Code)
	}
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == uiSessionCookie {
			session = c
		}
	}
	if session == nil || session.Value == "" {
		t.Fatal("no session cookie set on login")
	}
	if !d.sessions.Valid(session.Value) {
		t.Fatal("the session cookie is not a live session")
	}
	if got := rec.Header().Get("Location"); got != "/?open=abc" {
		t.Fatalf("redirect target = %q, want ?open=abc", got)
	}
}

// TestSafeLoginRedirect locks C-xx: the post-login ?next= must stay on-site.
// "//host" is protocol-relative and "/\host" is normalized by browsers to the
// same, so both must fall back to the root.
func TestSafeLoginRedirect(t *testing.T) {
	cases := map[string]string{
		"":                "/",
		"//evil.example":  "/",
		"/\\evil.example": "/",
		"http://evil":     "/",
		"/ui/settings":    "/ui/settings",
		"/?open=abc":      "/?open=abc",
	}
	for in, want := range cases {
		if got := safeLoginRedirect(in); got != want {
			t.Errorf("safeLoginRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLoginCookieSecureOnlyOverTLS locks that the session cookie is marked
// Secure only when the request actually arrived over TLS, so plain-HTTP LAN use
// keeps working.
func TestLoginCookieSecureOnlyOverTLS(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	sessionCookie := func(overTLS bool) *http.Cookie {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/login",
			strings.NewReader("user=admin&password=s3cret"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if overTLS {
			req.TLS = &tls.ConnectionState{}
		}
		rec := httptest.NewRecorder()
		d.handleUILogin(rec, req)
		for _, c := range rec.Result().Cookies() {
			if c.Name == uiSessionCookie {
				return c
			}
		}
		return nil
	}
	if c := sessionCookie(false); c == nil || c.Secure {
		t.Fatalf("plain-HTTP session cookie must not be Secure: %+v", c)
	}
	if c := sessionCookie(true); c == nil || !c.Secure {
		t.Fatalf("TLS session cookie must be Secure: %+v", c)
	}
}

func TestLoginRateLimit(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", false)
	ip := "8.8.8.8"
	post := func(password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/login",
			strings.NewReader("user=admin&password="+password))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		d.handleUILogin(rec, req)
		return rec
	}
	for i := 0; i < loginMaxFailures-1; i++ {
		post("wrong")
		if !d.loginAllowed(ip) {
			t.Fatalf("blocked after only %d failures", i+1)
		}
	}
	post("wrong") // the threshold-th failure
	if d.loginAllowed(ip) {
		t.Fatal("the address must be blocked after the threshold")
	}
	// Even the right password is refused while blocked.
	if rec := post("s3cret"); len(rec.Result().Cookies()) != 0 {
		t.Fatal("a blocked address must not get a session")
	}
}

// TestLoginFailuresBounded locks C-01: the failure map is pruned and capped so a
// brute-force run from many addresses cannot grow it without bound.
func TestLoginFailuresBounded(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", false)
	for i := 0; i < loginFailMaxEntries+500; i++ {
		d.recordLoginFailure(fmt.Sprintf("10.%d.%d.%d", i>>16&0xff, i>>8&0xff, i&0xff))
	}
	d.loginMu.Lock()
	n := len(d.loginFails)
	d.loginMu.Unlock()
	if n > loginFailMaxEntries {
		t.Fatalf("loginFails holds %d entries, want <= %d", n, loginFailMaxEntries)
	}
}

func TestManagedDoesNotUseStandaloneAuth(t *testing.T) {
	// Managed with no token: the page stays open exactly as before.
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	req.RemoteAddr = "8.8.8.8:1234"
	if !d.uiAuthorized(rec, req) {
		t.Fatal("managed mode must not require the standalone login")
	}
}
