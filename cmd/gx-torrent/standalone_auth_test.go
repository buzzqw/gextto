package main

import (
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
