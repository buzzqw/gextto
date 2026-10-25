package gextto

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProviderLoginReasonIsEnglish(t *testing.T) {
	for raw, want := range map[string]string{
		"credenziali rifiutate o verifica richiesta": "credentials rejected or verification required",
		"sessione non autenticata":                   "session not authenticated",
		"":                                           "",
		"totally new failure":                        "",
	} {
		if got := providerLoginReason(raw); got != want {
			t.Errorf("providerLoginReason(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestProviderLoginStateNotifiesAfterGrace covers the decision the monitor
// takes: silence during the grace, one notice when it expires, at most one more
// every providerLoginNotifyInterval, and a single recovery notice.
func TestProviderLoginStateNotifiesAfterGrace(t *testing.T) {
	var state providerLoginState
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	if action := state.observe(true, "credenziali rifiutate", start); action != "" {
		t.Fatalf("first probe = %q, want no notice", action)
	}
	if action := state.observe(true, "credenziali rifiutate", start.Add(providerLoginGrace-time.Minute)); action != "" {
		t.Fatalf("inside the grace = %q, want no notice", action)
	}
	if action := state.observe(true, "credenziali rifiutate", start.Add(providerLoginGrace)); action != "failed" {
		t.Fatalf("grace expired = %q, want failed", action)
	}
	if state.reason != "credenziali rifiutate" {
		t.Fatalf("reason = %q", state.reason)
	}
	if action := state.observe(true, "credenziali rifiutate", start.Add(providerLoginGrace+time.Hour)); action != "" {
		t.Fatalf("within the notify interval = %q, want no repeat", action)
	}
	if action := state.observe(true, "credenziali rifiutate", start.Add(providerLoginGrace+providerLoginNotifyInterval)); action != "failed" {
		t.Fatalf("after the notify interval = %q, want failed again", action)
	}
	if action := state.observe(false, "", start.Add(providerLoginGrace+providerLoginNotifyInterval+time.Minute)); action != "recovered" {
		t.Fatalf("login back = %q, want recovered", action)
	}
	if action := state.observe(false, "", start.Add(providerLoginGrace+providerLoginNotifyInterval+2*time.Minute)); action != "" {
		t.Fatalf("healthy again = %q, want no repeat", action)
	}
}

// TestProviderLoginStateShortFailureIsSilent covers a login that fixes itself
// within the grace period, e.g. a service restart: no failure and no recovery
// notice, because there was nothing to report.
func TestProviderLoginStateShortFailureIsSilent(t *testing.T) {
	var state providerLoginState
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	state.observe(true, "temporaneo", start)
	if action := state.observe(true, "temporaneo", start.Add(10*time.Minute)); action != "" {
		t.Fatalf("short failure = %q, want no notice", action)
	}
	if action := state.observe(false, "", start.Add(11*time.Minute)); action != "" {
		t.Fatalf("recovery without a notice = %q, want silent", action)
	}
	// A new failure starts from scratch: the previous grace does not count.
	if action := state.observe(true, "nuovo", start.Add(12*time.Minute)); action != "" {
		t.Fatalf("new failure = %q, want no notice", action)
	}
	if action := state.observe(true, "nuovo", start.Add(12*time.Minute+providerLoginGrace)); action != "failed" {
		t.Fatalf("new failure after grace = %q, want failed", action)
	}
}

// TestMirCrewLoginProbeReportsReachability covers the three outcomes the
// monitor distinguishes: a service that cannot be read (neither failure nor
// recovery), a healthy login and a broken one.
func TestMirCrewLoginProbeReportsReachability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			_, _ = w.Write([]byte(`{"ok":true,"logged_in":true,"login_error":"","last_run":"2026-10-20T16:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	reason, failed, reachable := gh5_mirCrewLoginProbe(server.URL)
	if !reachable || failed || reason != "" {
		t.Fatalf("healthy = (%q,%v,%v), want (\"\",false,true)", reason, failed, reachable)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"logged_in":false,"login_error":"credenziali rifiutate o verifica richiesta","last_run":"2026-10-20T16:00:00Z"}`))
	}))
	defer broken.Close()
	reason, failed, reachable = gh5_mirCrewLoginProbe(broken.URL)
	if !reachable || !failed || reason == "" {
		t.Fatalf("broken = (%q,%v,%v), want a reason, failed, reachable", reason, failed, reachable)
	}

	// A service without /status must not look like a broken or recovered login.
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer missing.Close()
	if reason, failed, reachable = gh5_mirCrewLoginProbe(missing.URL); reachable || failed || reason != "" {
		t.Fatalf("missing /status = (%q,%v,%v), want (\"\",false,false)", reason, failed, reachable)
	}
}
