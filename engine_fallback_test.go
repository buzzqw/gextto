package gextto

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubEngineFallback swaps the downloader and the restart action for the
// duration of a test and records what they saw.
func stubEngineFallback(t *testing.T) (downloads *int, restarts *[]string) {
	t.Helper()
	previousDownload := engineFallbackDownload
	previousRestart := engineFallbackRestart
	count := 0
	actions := []string{}
	engineFallbackDownload = func(context.Context, *Config) error {
		count++
		return nil
	}
	engineFallbackRestart = func(action string) { actions = append(actions, action) }
	t.Cleanup(func() {
		engineFallbackDownload = previousDownload
		engineFallbackRestart = previousRestart
	})
	return &count, &actions
}

func fallbackTestConfig(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.LibtorrentDir = t.TempDir()
	cfg.Settings = map[string]string{}
	return cfg
}

func readSettingValue(t *testing.T, dataDir, key string) string {
	t.Helper()
	conn, err := OpenConfigDB(filepath.Join(dataDir, "gextto_config.db"))
	if err != nil {
		t.Fatalf("OpenConfigDB: %v", err)
	}
	defer conn.Close()
	var value string
	if err := conn.QueryRow("SELECT value FROM settings WHERE key = ?1", key).Scan(&value); err != nil {
		t.Fatalf("setting %q not saved: %v", key, err)
	}
	return value
}

func TestPlanEngineFallback(t *testing.T) {
	cases := []struct {
		name       string
		current    string
		installed  bool
		external   bool
		want       string
		wantSave   bool // Download
		wantManage bool // Managed
	}{
		{"gx-torrent not installed downloads managed qBittorrent", BackendGxTorrent, false, false, BackendQbittorrent, true, true},
		{"gx-torrent already installed reuses qBittorrent", BackendGxTorrent, true, false, BackendQbittorrent, false, true},
		{"external qBittorrent is used as is", BackendGxTorrent, false, true, BackendQbittorrent, false, false},
		{"embedded goes to qBittorrent", BackendEmbedded, true, false, BackendQbittorrent, false, true},
		{"qBittorrent goes back to gx-torrent", BackendQbittorrent, true, false, BackendGxTorrent, false, false},
		{"qBittorrent with external config goes back to gx-torrent", BackendQbittorrent, false, true, BackendGxTorrent, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planEngineFallback(tc.current, tc.installed, tc.external)
			if plan.Backend != tc.want || plan.Download != tc.wantSave || plan.Managed != tc.wantManage {
				t.Fatalf("plan = %+v, want backend %q download %v managed %v", plan, tc.want, tc.wantSave, tc.wantManage)
			}
		})
	}
}

func TestEngineFallbackSecondary(t *testing.T) {
	// Without libtorrent the secondary of a failed gx-torrent is recovery.
	if plan := engineFallbackSecondary(BackendGxTorrent, false); plan.Backend != BackendRecovery {
		t.Fatalf("secondary without libtorrent = %q, want recovery", plan.Backend)
	}
	// With libtorrent the embedded engine is the last resort.
	if plan := engineFallbackSecondary(BackendGxTorrent, true); plan.Backend != BackendEmbedded {
		t.Fatalf("secondary with libtorrent = %q, want embedded", plan.Backend)
	}
	// A failed qBittorrent always falls back to gx-torrent.
	if plan := engineFallbackSecondary(BackendQbittorrent, true); plan.Backend != BackendGxTorrent {
		t.Fatalf("secondary of qBittorrent = %q, want gx-torrent", plan.Backend)
	}
	// The engine that just failed must never be the secondary.
	if plan := engineFallbackSecondary(BackendEmbedded, true); plan.Backend == BackendEmbedded {
		t.Fatal("the embedded engine must not fall back to itself")
	}
}

func TestExternalQbittorrentConfigured(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{}
	if externalQbittorrentConfigured(&cfg) {
		t.Fatal("an unconfigured qBittorrent must not count as external")
	}

	cfg.Settings["qbittorrent_url"] = defaultManagedQbittorrentURL
	if externalQbittorrentConfigured(&cfg) {
		t.Fatal("the default loopback URL must not count as external")
	}

	cfg.Settings["qbittorrent_url"] = "http://192.168.1.50:8080"
	if !externalQbittorrentConfigured(&cfg) {
		t.Fatal("a non-loopback URL must count as external")
	}

	// Managed mode always means the managed runtime, even on a LAN URL.
	cfg.Settings["qbittorrent_managed"] = "true"
	if externalQbittorrentConfigured(&cfg) {
		t.Fatal("managed mode must never count as external")
	}
}

func TestManagedQbittorrentInstalledRequiresMetadataAndBinary(t *testing.T) {
	cfg := fallbackTestConfig(t)
	if managedQbittorrentInstalled(&cfg) {
		t.Fatal("a fresh installation must not report a managed qBittorrent")
	}
}

func TestApplyEngineFallbackDownloadsAndSwitches(t *testing.T) {
	cfg := fallbackTestConfig(t)
	downloads, restarts := stubEngineFallback(t)

	switched := applyEngineFallback(context.Background(), &cfg, BackendGxTorrent, nil)
	if !switched {
		t.Fatal("the fallback must switch the engine when qBittorrent can be downloaded")
	}
	if *downloads != 1 {
		t.Fatalf("downloads = %d, want 1", *downloads)
	}
	if got := readSettingValue(t, cfg.DataDir, "torrent_backend"); got != BackendQbittorrent {
		t.Fatalf("torrent_backend = %q, want %q", got, BackendQbittorrent)
	}
	if got := readSettingValue(t, cfg.DataDir, "qbittorrent_managed"); got != "true" {
		t.Fatalf("qbittorrent_managed = %q, want true", got)
	}
	if got := readSettingValue(t, cfg.DataDir, "qbittorrent_url"); got != defaultManagedQbittorrentURL {
		t.Fatalf("qbittorrent_url = %q", got)
	}
	if len(*restarts) != 1 || (*restarts)[0] != "restart" {
		t.Fatalf("restarts = %v, want one restart", *restarts)
	}
	state := readEngineFallbackState(cfg.DataDir)
	if state.To != BackendQbittorrent || state.From != BackendGxTorrent {
		t.Fatalf("fallback state = %+v", state)
	}
}

func TestApplyEngineFallbackFromQbittorrentGoesToGxTorrent(t *testing.T) {
	cfg := fallbackTestConfig(t)
	downloads, restarts := stubEngineFallback(t)

	if !applyEngineFallback(context.Background(), &cfg, BackendQbittorrent, nil) {
		t.Fatal("qBittorrent failures must fall back to gx-torrent")
	}
	if *downloads != 0 {
		t.Fatalf("switching back to gx-torrent must not download anything (downloads=%d)", *downloads)
	}
	if got := readSettingValue(t, cfg.DataDir, "torrent_backend"); got != BackendGxTorrent {
		t.Fatalf("torrent_backend = %q, want %q", got, BackendGxTorrent)
	}
	if len(*restarts) != 1 {
		t.Fatalf("restarts = %v, want one restart", *restarts)
	}
}

func TestApplyEngineFallbackRecoveryWhenDownloadFails(t *testing.T) {
	cfg := fallbackTestConfig(t)
	previousDownload := engineFallbackDownload
	previousRestart := engineFallbackRestart
	restarts := 0
	engineFallbackDownload = func(context.Context, *Config) error { return errors.New("boom") }
	engineFallbackRestart = func(string) { restarts++ }
	t.Cleanup(func() {
		engineFallbackDownload = previousDownload
		engineFallbackRestart = previousRestart
	})

	switched := applyEngineFallback(context.Background(), &cfg, BackendGxTorrent, nil)
	if LibtorrentCompiled() {
		// A build with libtorrent falls back to the embedded engine.
		if !switched {
			t.Fatal("a libtorrent build must fall back to the embedded engine")
		}
		if got := readSettingValue(t, cfg.DataDir, "torrent_backend"); got != BackendEmbedded {
			t.Fatalf("torrent_backend = %q, want embedded", got)
		}
		return
	}
	if switched {
		t.Fatal("without libtorrent a failed download must not switch")
	}
	if restarts != 0 {
		t.Fatalf("restarts = %d, want 0 in recovery", restarts)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "gextto_config.db")); err == nil {
		// The database may exist from other reads; only the backend must be absent.
		conn, openErr := OpenConfigDB(filepath.Join(cfg.DataDir, "gextto_config.db"))
		if openErr == nil {
			defer conn.Close()
			var count int
			_ = conn.QueryRow("SELECT COUNT(*) FROM settings WHERE key = 'torrent_backend'").Scan(&count)
			if count != 0 {
				t.Fatal("recovery must not persist a backend")
			}
		}
	}
}

func TestApplyEngineFallbackCooldown(t *testing.T) {
	cfg := fallbackTestConfig(t)
	downloads, restarts := stubEngineFallback(t)
	if err := writeEngineFallbackState(cfg.DataDir, engineFallbackState{
		LastAt: time.Now().Add(-time.Minute),
		From:   BackendGxTorrent,
		To:     BackendQbittorrent,
	}); err != nil {
		t.Fatalf("writeEngineFallbackState: %v", err)
	}

	if applyEngineFallback(context.Background(), &cfg, BackendGxTorrent, nil) {
		t.Fatal("a switch inside the cooldown must be refused")
	}
	if *downloads != 0 || len(*restarts) != 0 {
		t.Fatalf("cooldown must not download or restart (downloads=%d restarts=%v)", *downloads, *restarts)
	}
}

func TestEngineFallbackStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	state := engineFallbackState{LastAt: time.Now().Truncate(time.Second), From: BackendGxTorrent, To: BackendQbittorrent}
	if err := writeEngineFallbackState(dir, state); err != nil {
		t.Fatalf("writeEngineFallbackState: %v", err)
	}
	got := readEngineFallbackState(dir)
	if !got.LastAt.Equal(state.LastAt) || got.From != state.From || got.To != state.To {
		t.Fatalf("state = %+v, want %+v", got, state)
	}
	if engineFallbackWithinCooldown(dir, time.Now()) != true {
		t.Fatal("a fresh switch must be inside the cooldown")
	}
	if engineFallbackWithinCooldown(dir, time.Now().Add(engineFallbackCooldown+time.Minute)) {
		t.Fatal("an old switch must be outside the cooldown")
	}
}

func TestApplyEngineOverride(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{}

	t.Setenv(EngineOverrideEnv, "QBITTORRENT")
	if got := ApplyEngineOverride(&cfg); got != BackendQbittorrent {
		t.Fatalf("override = %q, want %q", got, BackendQbittorrent)
	}
	if cfg.Settings["torrent_backend"] != BackendQbittorrent {
		t.Fatalf("setting not applied: %q", cfg.Settings["torrent_backend"])
	}

	t.Setenv(EngineOverrideEnv, "nope")
	cfg.Settings = map[string]string{}
	if got := ApplyEngineOverride(&cfg); got != "" {
		t.Fatalf("an unknown engine must be ignored, got %q", got)
	}
	if _, ok := cfg.Settings["torrent_backend"]; ok {
		t.Fatal("an unknown engine must not be applied")
	}

	t.Setenv(EngineOverrideEnv, "")
	if got := ApplyEngineOverride(&cfg); got != "" {
		t.Fatalf("an empty override must be a no-op, got %q", got)
	}
}
