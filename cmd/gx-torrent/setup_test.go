package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func setupPost(t *testing.T, d *Daemon, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8890/ui/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	d.handleUISetup(rec, req)
	return rec
}

func TestSetupCompleteDefaults(t *testing.T) {
	if !(&Daemon{}).setupComplete() {
		t.Fatal("a daemon with no settings store must be treated as configured")
	}
	d := standaloneTestDaemon(t, "", true)
	if d.setupComplete() {
		t.Fatal("a fresh standalone daemon must need the wizard")
	}
	d.opts.Settings.Set(setupCompleteKey, "true")
	if !d.setupComplete() {
		t.Fatal("setup-complete must be honoured")
	}
}

func TestSetupSavesSettings(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	download := filepath.Join(t.TempDir(), "downloads")
	rec := setupPost(t, d, url.Values{
		"lang":         {"de"},
		"download-dir": {download},
		"user":         {"alice"},
		"password":     {"s3cret"},
		"password2":    {"s3cret"},
		"local-bypass": {"1"},
		"peer-port":    {"51413"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup status = %d, body=%s", rec.Code, rec.Body.String())
	}
	store := d.opts.Settings
	if store.Get(setupCompleteKey, "") != "true" {
		t.Fatal("setup-complete not set")
	}
	if store.Get("lang", "") != "de" {
		t.Fatalf("lang = %q", store.Get("lang", ""))
	}
	if store.Get("download-dir", "") != download {
		t.Fatalf("download-dir = %q", store.Get("download-dir", ""))
	}
	if store.Get("auth-user", "") != "alice" {
		t.Fatalf("auth-user = %q", store.Get("auth-user", ""))
	}
	if store.Get("peer-ports", "") != "51413" {
		t.Fatalf("peer-ports = %q", store.Get("peer-ports", ""))
	}
	if d.standalonePassword() == "" || d.standalonePassword() == "s3cret" {
		t.Fatal("the password must be stored hashed, never in clear")
	}
	if !d.loginMatches("alice", "s3cret") {
		t.Fatal("the stored credentials do not match what was set")
	}
}

func TestSetupAppliesBandwidthLimits(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := setupPost(t, d, url.Values{
		"lang":           {"en"},
		"speed-download": {"4096"},
		"speed-upload":   {"512"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup status = %d, body=%s", rec.Code, rec.Body.String())
	}
	cfg, _, _ := d.config()
	if cfg.SpeedLimitDownload != 4096 || cfg.SpeedLimitUpload != 512 {
		t.Fatalf("limits = %d/%d KiB/s, want 4096/512", cfg.SpeedLimitDownload, cfg.SpeedLimitUpload)
	}
}

func TestSetupRejectsBadBandwidth(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := setupPost(t, d, url.Values{"lang": {"en"}, "speed-download": {"fast"}})
	if rec.Code != http.StatusOK || d.opts.Settings.Get(setupCompleteKey, "") == "true" {
		t.Fatalf("a non-numeric limit must be rejected: status=%d", rec.Code)
	}
	rec = setupPost(t, d, url.Values{"lang": {"en"}, "speed-upload": {"-1"}})
	if rec.Code != http.StatusOK || d.opts.Settings.Get(setupCompleteKey, "") == "true" {
		t.Fatalf("a negative limit must be rejected: status=%d", rec.Code)
	}
}

func TestSetupPageOffersPortTestAndBandwidth(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/setup", nil)
	d.handleUISetup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`name="speed-download"`, `name="speed-upload"`, `id="ptest"`, "/api/v1/portcheck", "Bandwidth", "Test port"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the wizard is missing %q", want)
		}
	}
}

func TestSetupRejectsRelativeFolder(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := setupPost(t, d, url.Values{"download-dir": {"relative/path"}, "lang": {"en"}})
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "settings.json saved") {
		t.Fatalf("a relative folder must be rejected: status=%d", rec.Code)
	}
	if d.opts.Settings.Get(setupCompleteKey, "") == "true" {
		t.Fatal("setup must not be marked complete after a validation error")
	}
}

func TestSetupRejectsPasswordMismatch(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	rec := setupPost(t, d, url.Values{"lang": {"en"}, "password": {"a"}, "password2": {"b"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if d.opts.Settings.Get(setupCompleteKey, "") == "true" {
		t.Fatal("setup must not be marked complete after a validation error")
	}
}

func TestSetupRedirectsUntilComplete(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	called := false
	guard := d.standaloneGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusOK) }))

	rec := httptest.NewRecorder()
	guard.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != setupPath || called {
		t.Fatalf("first run must redirect to the wizard: status=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	guard.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8890/ui/setup", nil))
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("the wizard page itself must be reachable: status=%d called=%v", rec.Code, called)
	}
}

func TestSetupSavesIndexerAndTemp(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	temp := filepath.Join(t.TempDir(), "tmp")
	rec := setupPost(t, d, url.Values{
		"lang":         {"en"},
		"temp-dir":     {temp},
		"indexer-name": {"Prowlarr"},
		"indexer-url":  {"http://127.0.0.1:9696"},
		"indexer-key":  {"secret"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup: %d", rec.Code)
	}
	if got := d.opts.Settings.Get("temp-dir", ""); got != temp {
		t.Fatalf("temp-dir = %q", got)
	}
	if got := d.opts.Settings.Get(indexersSettingKey, ""); !strings.Contains(got, "http://127.0.0.1:9696") || !strings.Contains(got, "secret") {
		t.Fatalf("indexers = %q", got)
	}

	// Invalid values are rejected without completing the setup.
	rec = setupPost(t, d, url.Values{"lang": {"en"}, "temp-dir": {"relative"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("relative temp folder must be rejected: %d", rec.Code)
	}
	rec = setupPost(t, d, url.Values{"lang": {"en"}, "indexer-url": {"ftp://host"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("non-http indexer URL must be rejected: %d", rec.Code)
	}
}
