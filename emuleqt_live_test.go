package gextto

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEmuleqtLive runs the real emule-qt image the way the managed core does
// — Gextto's preferences.yml, docker pull and run, the health check — then
// drives it through Gextto: status, preferences, folders and a search. It
// needs Docker and downloads the image (about 800 MB) the first time, so it
// runs only when asked:
//
//	GEXTTO_EMULEQT_LIVE_DIR=$(scripts/test-workdir.sh new emuleqt) go test -run TestEmuleqtLive -timeout 60m .
//
// It uses its own container name and ports (TCP 14662, UDP 14672, REST
// 14721), so a real aMule or emule-qt on the same machine keeps running, and
// UPnP is off so the router is not asked to forward test ports. The
// container is removed at the end; the image stays for the next run.
// GEXTTO_EMULEQT_LIVE_UPNP=1 keeps UPnP on (a High ID, more sources for a
// real download) and GEXTTO_EMULEQT_LIVE_KEEP=1 leaves the core running for
// a hand-driven try-out: its address and key are in the test log, and
// `docker rm -f gextto-emuleqt-livetest` removes it.
func TestEmuleqtLive(t *testing.T) {
	dir := strings.TrimSpace(os.Getenv("GEXTTO_EMULEQT_LIVE_DIR"))
	if dir == "" {
		t.Skip("set GEXTTO_EMULEQT_LIVE_DIR to run the real emule-qt image")
	}
	saved := []any{emuleqtContainer, emuleqtTCPPort, emuleqtUDPPort, emuleqtWebPort, emuleqtIPCPort}
	emuleqtContainer, emuleqtTCPPort, emuleqtUDPPort, emuleqtWebPort, emuleqtIPCPort = "gextto-emuleqt-livetest", 14662, 14672, 14721, 14722
	t.Cleanup(func() {
		emuleqtContainer, emuleqtTCPPort, emuleqtUDPPort, emuleqtWebPort, emuleqtIPCPort =
			saved[0].(string), saved[1].(int), saved[2].(int), saved[3].(int), saved[4].(int)
	})
	cfgValue := DefaultConfig()
	cfg := &cfgValue
	cfg.DataDir = dir
	cfg.Settings = map[string]string{"ed2k_enabled": "true", "ed2k_backend": "emuleqt_managed", "ed2k_search_timeout_secs": "40"}
	ed2kInvalidateHealth()

	managed, err := emuleqtManagedEndpoint(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prefsPath := filepath.Join(managed.Dir, "config", "preferences.yml")
	raw, err := os.ReadFile(prefsPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GEXTTO_EMULEQT_LIVE_UPNP") != "1" {
		if err := os.WriteFile(prefsPath, []byte(strings.Replace(string(raw), "enableUPnP: true", "enableUPnP: false", 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep := os.Getenv("GEXTTO_EMULEQT_LIVE_KEEP") == "1"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if out, err := emuleqtDocker(ctx, "logs", "--tail", "40", emuleqtContainer); err == nil {
			t.Logf("container log, last lines:\n%s", out)
		}
		if keep {
			t.Logf("kept running: %s, API key %s, data %s", managed.URL, managed.APIKey, managed.HostData)
		} else {
			emuleqtRemove(ctx)
		}
		ed2kInvalidateHealth()
	})

	started := time.Now()
	if err := emuleqtEnsure(cfg, time.Now()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Logf("container up after %s", time.Since(started).Round(time.Second))
	if err := emuleqtWaitHealthy(context.Background(), managed, 3*time.Minute); err != nil {
		t.Fatalf("the core does not answer: %v", err)
	}

	// The preferences as the running core reports them.
	request, _ := http.NewRequest(http.MethodGet, managed.URL+"/api/v1/preferences", nil)
	request.Header.Set("X-Api-Key", managed.APIKey)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var prefs map[string]any
	_ = json.NewDecoder(response.Body).Decode(&prefs)
	response.Body.Close()
	for key, want := range map[string]any{
		"networkED2K": true, "kadEnabled": true, "autoConnect": true, "autoUpdateServerList": true,
		"btEnabled": false, "usenetEnabled": false, "ed3kHosterEnabled": false, "ed3kEnabled": true,
		"port": float64(14662), "udpPort": float64(14672),
	} {
		if prefs[key] != want {
			t.Errorf("preference %s = %v, want %v", key, prefs[key], want)
		}
	}
	t.Logf("incomingDir %v, restart-free web port %v", prefs["incomingDir"], prefs["webServerPort"])

	core, err := ed2kClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := ed2kPreferences(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if folders.IncomingDir != filepath.Join(managed.HostData, "incoming") || folders.TempDir != filepath.Join(managed.HostData, "temp") {
		t.Errorf("folders seen by Gextto: incoming %q, temp %q", folders.IncomingDir, folders.TempDir)
	}

	// eD2k servers and Kad, joined by the core on its own.
	var connected bool
	for deadline := time.Now().Add(6 * time.Minute); time.Now().Before(deadline); {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		status, err := core.Status(ctx)
		cancel()
		if err == nil {
			t.Logf("%s: version %q, eD2k connected %v (connecting %v, server %q), Kad running %v connected %v firewalled %v",
				time.Since(started).Round(time.Second), status.Version, status.Ed2kConnected, status.Ed2kConnecting,
				status.ServerName, status.KadRunning, status.KadConnected, status.KadFirewalled)
			if status.Ed2kConnected && status.KadRunning {
				connected = true
				break
			}
		} else {
			t.Logf("status: %v", err)
		}
		time.Sleep(15 * time.Second)
	}
	if !connected {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		servers, _ := core.Servers(ctx)
		cancel()
		t.Errorf("the core did not join an eD2k server with Kad running (%d servers in its list)", len(servers))
	}

	// A search through Gextto, as the eD2k page does it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	results := SearchED2K(ctx, cfg, "debian iso")
	t.Logf("search: %d results", len(results))
	for i, result := range results {
		if i == 3 {
			break
		}
		t.Logf("  %s (%d sources)", result.Title, result.Sources)
	}
	if _, err := core.Downloads(ctx); err != nil {
		t.Errorf("downloads: %v", err)
	}
	if lines, err := core.LogLines(ctx); err != nil || len(lines) == 0 {
		t.Errorf("log: %d lines, %v", len(lines), err)
	}
}
