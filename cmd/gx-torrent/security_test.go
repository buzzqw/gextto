package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	mk := func(origin, referer, host string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "http://"+host+"/ui/action", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		return req
	}
	cases := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{"no headers", mk("", "", "127.0.0.1:8890"), true},
		{"matching origin", mk("http://127.0.0.1:8890", "", "127.0.0.1:8890"), true},
		{"foreign origin", mk("http://evil.example", "", "127.0.0.1:8890"), false},
		{"origin null", mk("null", "", "127.0.0.1:8890"), false},
		{"matching referer", mk("", "http://127.0.0.1:8890/", "127.0.0.1:8890"), true},
		{"foreign referer", mk("", "http://evil.example/x", "127.0.0.1:8890"), false},
	}
	for _, c := range cases {
		if got := sameOrigin(c.req); got != c.want {
			t.Errorf("%s: sameOrigin = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStandaloneGuard(t *testing.T) {
	post := func(origin string) (*httptest.ResponseRecorder, *bool) {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
		d := &Daemon{opts: Options{Mode: ModeStandalone}}
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/action", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		d.standaloneGuard(next).ServeHTTP(rec, req)
		return rec, &called
	}

	if rec, called := post("http://evil.example"); rec.Code != http.StatusForbidden || *called {
		t.Fatalf("standalone must reject a cross-origin POST: status=%d called=%v", rec.Code, *called)
	}
	if rec, called := post("http://127.0.0.1:8890"); rec.Code != http.StatusOK || !*called {
		t.Fatalf("standalone must allow a same-origin POST: status=%d called=%v", rec.Code, *called)
	}
}

func TestManagedGuardIsNoop(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/action", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	d.standaloneGuard(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("managed mode must not change behaviour: status=%d called=%v", rec.Code, called)
	}
}

func TestHostAllowed(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8890":    true,
		"127.0.0.1":         true,
		"192.168.1.10:8890": true,
		"[::1]:8890":        true,
		"[fd00::1]:8890":    true,
		"localhost:8890":    true,
		"localhost":         true,
		"evil.example":      false,
		"evil.example:8890": false,
		"":                  false,
	}
	for host, want := range cases {
		if got := hostAllowed(host); got != want {
			t.Errorf("hostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestLanSource(t *testing.T) {
	local := []string{"127.0.0.1", "10.1.2.3", "172.16.5.5", "172.31.255.254", "192.168.0.10", "169.254.1.1", "::1", "fc00::1", "fd12::1", "fe80::1"}
	for _, s := range local {
		if !lanSource(net.ParseIP(s)) {
			t.Errorf("lanSource(%s) = false, want true", s)
		}
	}
	remote := []string{"8.8.8.8", "1.1.1.1", "172.32.0.1", "2001:4860:4860::8888"}
	for _, s := range remote {
		if lanSource(net.ParseIP(s)) {
			t.Errorf("lanSource(%s) = true, want false", s)
		}
	}
	if lanSource(nil) {
		t.Error("lanSource(nil) = true, want false")
	}
}

func TestStandaloneAPIAuth(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", false)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) })

	// No session and no bypass: the whole v1 API is behind the password.
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/add", strings.NewReader("magnet=x"))
	req.RemoteAddr = "8.8.8.8:1234"
	rec := httptest.NewRecorder()
	d.authenticate(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("standalone API without a session must be 401: status=%d called=%v", rec.Code, called)
	}

	// A valid page session opens it.
	token := d.sessions.Create()
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/add", strings.NewReader("magnet=x"))
	req.RemoteAddr = "8.8.8.8:1234"
	req.AddCookie(&http.Cookie{Name: uiSessionCookie, Value: token})
	rec = httptest.NewRecorder()
	called = false
	d.authenticate(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("a valid session must open the API: status=%d called=%v", rec.Code, called)
	}
}

func TestStandaloneAPILanBypass(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/add", strings.NewReader("magnet=x"))
	req.RemoteAddr = "192.168.1.9:1234"
	rec := httptest.NewRecorder()
	d.authenticate(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("the LAN bypass must apply to the API too: status=%d called=%v", rec.Code, called)
	}
}

func TestStandaloneAPIRejectsCrossOrigin(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
	handler := d.standaloneGuard(d.authenticate(next))
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/add", strings.NewReader("magnet=x"))
	req.RemoteAddr = "8.8.8.8:1234"
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("a cross-origin API POST must be rejected: status=%d called=%v", rec.Code, called)
	}
}

func TestStandaloneSetConfigRequiresJSON(t *testing.T) {
	d := standaloneTestDaemon(t, "s3cret", true)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/config", strings.NewReader(`{"sequential":true}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	d.handleSetConfig(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a non-JSON config must be refused in standalone: status=%d", rec.Code)
	}
}

func TestManagedAPIAuthUnchanged(t *testing.T) {
	// Managed, no token: the API stays open exactly as before.
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/api/v1/add", strings.NewReader("magnet=x"))
	req.RemoteAddr = "8.8.8.8:1234"
	rec := httptest.NewRecorder()
	d.authenticate(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("managed mode must not require the standalone login: status=%d called=%v", rec.Code, called)
	}
}

func TestStandaloneGuardRejectsForeignHost(t *testing.T) {
	called := false
	d := &Daemon{opts: Options{Mode: ModeStandalone}}
	req := httptest.NewRequest(http.MethodGet, "http://evil.example/ui", nil)
	rec := httptest.NewRecorder()
	d.standaloneGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true })).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("standalone must reject a hostname Host (DNS rebinding): status=%d called=%v", rec.Code, called)
	}
}

func TestAllowedHosts(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(setupCompleteKey, "true")
	// A configured name is accepted; other names are still rejected.
	d.opts.Settings.Set(allowedHostsKey, "nas.local, media.box")
	if !d.hostAllowed("nas.local:8890") || !d.hostAllowed("media.box") {
		t.Fatal("a configured host must be allowed")
	}
	if d.hostAllowed("evil.example") || d.hostAllowed("nas.local.evil.example") {
		t.Fatal("only the exact configured hosts must be allowed")
	}
	if !d.hostAllowed("127.0.0.1:8890") {
		t.Fatal("the IP literal default must keep working")
	}
}

func TestTrustedProxyClientIP(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	d.opts.Settings.Set(trustedProxiesKey, "127.0.0.1")

	// A trusted proxy forwards an Internet client: the client is remote.
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	if ip := d.clientIP(req); ip == nil || ip.String() != "8.8.8.8" {
		t.Fatalf("clientIP = %v, want 8.8.8.8", ip)
	}

	// A trusted proxy forwarding a LAN client keeps the bypass.
	req.Header.Set("X-Forwarded-For", "192.168.1.20")
	if ip := d.clientIP(req); ip == nil || !lanSource(ip) {
		t.Fatalf("a LAN client behind a trusted proxy must be LAN: %v", ip)
	}

	// A chain "real, proxy" is reduced to the real client.
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 127.0.0.1")
	if ip := d.clientIP(req); ip == nil || ip.String() != "203.0.113.9" {
		t.Fatalf("clientIP = %v, want 203.0.113.9", ip)
	}
}

func TestUntrustedLoopbackProxyNotLAN(t *testing.T) {
	// No trusted-proxies configured: a loopback peer that carries a forwarding
	// header is not proof of a LAN client.
	d := standaloneTestDaemon(t, "s3cret", true)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	rec := httptest.NewRecorder()
	if d.standaloneAuthorized(rec, req) {
		t.Fatal("an undeclared proxy forwarding an external client must not be allowed by the LAN bypass")
	}
}
