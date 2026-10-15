package gextto

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An option tied to an engine capability is active exactly with the engines
// whose level for it is not "none": the option and the capability matrix
// cannot drift apart.
func TestSettingCapabilitiesFollowTheMatrix(t *testing.T) {
	for key, capability := range uiSettingCapability {
		if !uiSettingIndexed[key] {
			t.Errorf("%s is tied to a capability but is not in the settings index", key)
		}
		allowed := uiSettingAllowedBackends(key)
		for backend, levels := range capabilityLevels {
			level, known := levels[capability]
			if !known {
				t.Fatalf("%s: capability %q is not in the matrix of %s", key, capability, backend)
			}
			if got, want := uiBackendAllows(allowed, backend), level != "none"; got != want {
				t.Errorf("%s with %s: active=%v, but capability %q is %q", key, backend, got, capability, level)
			}
		}
	}
	for key := range uiSettingGxTorrentOnly {
		if !uiSettingIndexed[key] {
			t.Errorf("%s is declared gx-torrent only but is not in the settings index", key)
		}
		if _, tied := uiSettingCapability[key]; tied {
			t.Errorf("%s is both capability-bound and gx-torrent only: keep one rule", key)
		}
	}
}

// With qBittorrent active, the options only gx-torrent reads render as not
// active (with the reason), while the options every engine honours stay
// editable.
func TestSettingsShowGxTorrentOnlyOptionsAsInactiveWithQbittorrent(t *testing.T) {
	state := newTestAppState(t)
	if err := saveConfigSetting(state.cfg.DataDir, "torrent_backend", BackendQbittorrent); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)
	_, network := v2Request(t, server, http.MethodGet, "/settings/body?tab=network", nil)
	_, performance := v2Request(t, server, http.MethodGet, "/settings/body?tab=performance", nil)
	row := func(body, key string) string {
		start := strings.Index(body, `id="v2-setting-`+key+`"`)
		if start < 0 {
			t.Fatalf("%s is not rendered", key)
		}
		rest := body[start:]
		if end := strings.Index(rest[1:], `id="v2-setting-`); end >= 0 {
			rest = rest[:end+1]
		}
		return rest
	}
	for _, check := range []struct{ body, key string }{
		{network, "torrent_holepunch"}, {network, "torrent_dht"}, {network, "torrent_encryption"},
		{performance, "torrent_ramdisk_enabled"}, {performance, "torrent_max_connections_per_torrent"},
	} {
		if !strings.Contains(row(check.body, check.key), "Non attivo con il motore") {
			t.Errorf("%s must be shown as not active with qBittorrent", check.key)
		}
	}
	if strings.Contains(row(performance, "torrent_sequential"), "Non attivo con il motore") {
		t.Errorf("torrent_sequential applies to qBittorrent too and must stay editable")
	}
}
