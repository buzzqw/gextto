package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

func TestIPFilterRulesCache(t *testing.T) {
	d := newTestDaemon(t)
	path := filepath.Join(t.TempDir(), "filter.dat")
	if err := os.WriteFile(path, []byte("1.2.3.0/24\n5.6.7.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if n, err := d.loadIPFilterLocked(path); err != nil || n != 2 {
		t.Fatalf("first load = %d/%v, want 2 rules", n, err)
	}
	if d.ipFilterSet.Len() != 2 {
		t.Fatalf("cached set holds %d rules, want 2", d.ipFilterSet.Len())
	}

	// Same path, size and mtime: the parsed rules are reused and the file is
	// not read. Garbage of the same length proves it.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a filter at all!!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if n, err := d.loadIPFilterLocked(path); err != nil || n != 2 {
		t.Fatalf("cached load = %d/%v, want the 2 cached rules", n, err)
	}

	// A changed file is parsed again.
	if err := os.WriteFile(path, []byte("9.9.9.0/24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := d.loadIPFilterLocked(path); err != nil || n != 1 {
		t.Fatalf("reload = %d/%v, want 1 rule", n, err)
	}
}

// TestIPFilterDownloadUsesTheProxy: with a proxy configured, fetching the IP
// filter list must go through it. A direct fetch would reveal the operator's
// address and bypass the tunnel the proxy exists for.
func TestIPFilterDownloadUsesTheProxy(t *testing.T) {
	proxy := startTestProxy(t, "http")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/filter.dat" {
			t.Errorf("filter path = %q, want /filter.dat", r.URL.Path)
		}
		_, _ = w.Write([]byte("1.2.3.0/24\n5.6.7.0/24\n"))
	}))
	defer server.Close()

	d := newTestDaemonWith(t, NetworkOptions{
		PortBegin: 49000, PortEnd: 49099, Encryption: 1,
		Proxy: "http://" + proxy.listener.Addr().String(),
	})
	path, err := d.fetchIPFilterURL(server.URL + "/filter.dat")
	if err != nil {
		t.Fatal(err)
	}
	if proxy.tunnels.Load() == 0 {
		t.Fatal("the IP filter download did not go through the proxy")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("the downloaded filter is empty")
	}
}

// Holepunching punches UDP mappings, so it follows uTP: enabling it without uTP
// must still leave it off in the engine session.
func TestApplyNetworkHolepunchFollowsUTP(t *testing.T) {
	d := &Daemon{}
	d.peerPort = 51413

	cfg := &torrent.Config{}
	d.opts.Network = NetworkOptions{UTP: true, Holepunch: true}
	d.applyNetwork(cfg)
	if !cfg.UTP || !cfg.Holepunch {
		t.Fatalf("utp=%v holepunch=%v, want both on", cfg.UTP, cfg.Holepunch)
	}

	cfg = &torrent.Config{}
	d.opts.Network = NetworkOptions{UTP: false, Holepunch: true}
	d.applyNetwork(cfg)
	if cfg.UTP || cfg.Holepunch {
		t.Fatalf("utp=%v holepunch=%v, want both off", cfg.UTP, cfg.Holepunch)
	}
}
