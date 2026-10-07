package gextto

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func authTestServer(t *testing.T, settings map[string]string) (*AppState, http.Handler) {
	t.Helper()
	state := newTestAppState(t)
	for key, value := range settings {
		if err := saveConfigSetting(state.cfg.DataDir, key, value); err != nil {
			t.Fatal(err)
		}
	}
	return state, AuthMiddleware(state, Router(state))
}

func authRequest(handler http.Handler, method, target, remote string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	request.RemoteAddr = remote
	if mutate != nil {
		mutate(request)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestAuthOffByDefault(t *testing.T) {
	_, handler := authTestServer(t, nil)
	if got := authRequest(handler, http.MethodGet, "/api/health", "203.0.113.9:1234", nil); got.Code != http.StatusOK {
		t.Fatalf("access control is off by default, got %d", got.Code)
	}
}

func TestAuthEnabledWithoutCredentialsStaysOpen(t *testing.T) {
	_, handler := authTestServer(t, map[string]string{"auth_enabled": "true"})
	if got := authRequest(handler, http.MethodGet, "/api/health", "203.0.113.9:1234", nil); got.Code != http.StatusOK {
		t.Fatalf("no password nor key: must not lock everybody out, got %d", got.Code)
	}
}

func TestAuthLocalBypassAndRemoteLogin(t *testing.T) {
	state, handler := authTestServer(t, map[string]string{
		"auth_enabled":  "true",
		"auth_password": "s3cret!",
		"auth_api_key":  "key-123",
	})
	stored := latestConfig(state).Settings["auth_password"]
	if !strings.HasPrefix(stored, authPasswordHashPrefix) || strings.Contains(stored, "s3cret!") {
		t.Fatalf("password stored in clear: %q", stored)
	}

	for _, remote := range []string{"127.0.0.1:5555", "192.168.1.20:5555", "10.0.0.3:1", "[::1]:80", "[fd00::5]:80"} {
		if got := authRequest(handler, http.MethodGet, "/api/health", remote, nil); got.Code != http.StatusOK {
			t.Fatalf("local client %s must pass without login, got %d", remote, got.Code)
		}
	}
	// A reverse proxy on the same host: the forwarded client is remote.
	proxied := authRequest(handler, http.MethodGet, "/api/health", "127.0.0.1:5555", func(r *http.Request) {
		r.Header.Set("X-Forwarded-For", "192.168.1.4, 198.51.100.7")
	})
	if proxied.Code != http.StatusUnauthorized {
		t.Fatalf("a remote client behind a local proxy must log in, got %d", proxied.Code)
	}

	remote := "198.51.100.7:4000"
	if got := authRequest(handler, http.MethodGet, "/api/health", remote, nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("remote API without credentials: want 401, got %d", got.Code)
	}
	page := authRequest(handler, http.MethodGet, "/?tab=log", remote, nil)
	if page.Code != http.StatusSeeOther || !strings.HasPrefix(page.Header().Get("Location"), "/login?next=") {
		t.Fatalf("remote page must redirect to the login, got %d %q", page.Code, page.Header().Get("Location"))
	}
	if got := authRequest(handler, http.MethodGet, "/login", remote, nil); got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `name="password"`) {
		t.Fatalf("login page not served: %d", got.Code)
	}
	if got := authRequest(handler, http.MethodGet, "/api/health", remote, func(r *http.Request) {
		r.Header.Set("X-Api-Key", "key-123")
	}); got.Code != http.StatusOK {
		t.Fatalf("API key must grant access, got %d", got.Code)
	}
	if got := authRequest(handler, http.MethodGet, "/api/health?apikey=wrong", remote, nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong API key must be refused, got %d", got.Code)
	}

	login := func(password string) *httptest.ResponseRecorder {
		form := url.Values{"username": {"admin"}, "password": {password}, "next": {"/?tab=log"}}
		request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.RemoteAddr = remote
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	if got := login("wrong"); got.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: want 401, got %d", got.Code)
	}
	ok := login("s3cret!")
	if ok.Code != http.StatusSeeOther || ok.Header().Get("Location") != "/?tab=log" {
		t.Fatalf("login failed: %d %q", ok.Code, ok.Header().Get("Location"))
	}
	cookies := ok.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != authCookieName || !cookies[0].HttpOnly {
		t.Fatalf("session cookie missing or not HttpOnly: %+v", cookies)
	}
	if got := authRequest(handler, http.MethodGet, "/api/health", remote, func(r *http.Request) {
		r.AddCookie(cookies[0])
	}); got.Code != http.StatusOK {
		t.Fatalf("session cookie must grant access, got %d", got.Code)
	}

	// Changing the password logs out the existing sessions.
	if err := saveConfigSetting(state.cfg.DataDir, "auth_password", "another"); err != nil {
		t.Fatal(err)
	}
	if got := authRequest(handler, http.MethodGet, "/api/health", remote, func(r *http.Request) {
		r.AddCookie(cookies[0])
	}); got.Code != http.StatusUnauthorized {
		t.Fatalf("old session must stop working after a password change, got %d", got.Code)
	}
}

func TestAuthLocalBypassCanBeTurnedOff(t *testing.T) {
	_, handler := authTestServer(t, map[string]string{
		"auth_enabled":      "true",
		"auth_password":     "pw",
		"auth_local_bypass": "false",
	})
	if got := authRequest(handler, http.MethodGet, "/api/health", "192.168.1.20:5555", nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("with the LAN exemption off a local client must log in, got %d", got.Code)
	}
}

func TestAuthDisableEnvironment(t *testing.T) {
	t.Setenv(authDisableEnv, "1")
	_, handler := authTestServer(t, map[string]string{"auth_enabled": "true", "auth_password": "pw"})
	if got := authRequest(handler, http.MethodGet, "/api/health", "198.51.100.7:1", nil); got.Code != http.StatusOK {
		t.Fatalf("GEXTTO_AUTH_DISABLE must switch access control off, got %d", got.Code)
	}
}

func TestAuthSafeNextStaysInside(t *testing.T) {
	for input, want := range map[string]string{
		"":                     "/",
		"/?tab=log":            "/?tab=log",
		"//evil.example/x":     "/",
		"https://evil.example": "/",
		"/\\evil.example":      "/",
	} {
		if got := authSafeNext(input); got != want {
			t.Errorf("authSafeNext(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAuthSessionExpires(t *testing.T) {
	hash, err := hashAuthPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	auth := authSettings{enabled: true, username: "admin", passwordHash: hash}
	now := time.Now()
	token := auth.sessionToken(now.Add(time.Hour))
	if !auth.sessionValid(token, now) {
		t.Fatal("fresh token refused")
	}
	if auth.sessionValid(token, now.Add(2*time.Hour)) {
		t.Fatal("expired token accepted")
	}
	if auth.sessionValid(token[:len(token)-2]+"xx", now) {
		t.Fatal("tampered token accepted")
	}
}

func TestAuthLogoutCookieSecureBehindHTTPS(t *testing.T) {
	_, handler := authTestServer(t, map[string]string{"auth_enabled": "true", "auth_password": "pw"})
	got := authRequest(handler, http.MethodGet, "/logout", "198.51.100.7:1", func(r *http.Request) {
		r.Header.Set("X-Forwarded-Proto", "https")
	})
	cookies := got.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || cookies[0].MaxAge >= 0 {
		t.Fatalf("logout over HTTPS must expire a Secure cookie, got %+v", cookies)
	}
}
