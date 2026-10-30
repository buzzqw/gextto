package netx

import (
	"net/http"
	"net/url"
	"testing"
)

// TestIsLocalHost locks the proxy bypass: services on the machine, on the LAN
// or with a single-label name must be reached directly, while public names and
// addresses still go through the proxy.
func TestIsLocalHost(t *testing.T) {
	local := []string{
		"localhost", "localhost.", "127.0.0.1", "::1", "0.0.0.0",
		"192.168.1.10", "10.1.2.3", "172.16.0.5", "169.254.1.1",
		"nas.local", "jackett", "prowlarr.lan", "router.home", "svc.internal",
	}
	for _, host := range local {
		if !IsLocalHost(host) {
			t.Errorf("IsLocalHost(%q) = false, want true", host)
		}
	}
	remote := []string{"github.com", "1.1.1.1", "8.8.8.8", "tracker.example.org", "foo.onion"}
	for _, host := range remote {
		if IsLocalHost(host) {
			t.Errorf("IsLocalHost(%q) = true, want false", host)
		}
	}
}

// TestHTTPTransportProxiesOnlyRemoteHosts: the per-request proxy function must
// return nil for local destinations and the configured proxy for the rest.
func TestHTTPTransportProxiesOnlyRemoteHosts(t *testing.T) {
	proxy, err := url.Parse("http://127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	old := Current()
	Configure(Settings{Proxy: proxy})
	t.Cleanup(func() { Configure(old) })

	tr := HTTPTransport()
	if tr.Proxy == nil {
		t.Fatal("HTTPTransport has no proxy function")
	}
	for _, raw := range []string{
		"http://localhost:9117/api",
		"http://127.0.0.1:9696/api",
		"http://192.168.1.20:9117/api",
		"http://jackett:9117/api",
	} {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := tr.Proxy(req)
		if err != nil || got != nil {
			t.Errorf("proxy for %s = %v, %v; want direct", raw, got, err)
		}
	}
	for _, raw := range []string{"https://github.com/x", "http://indexer.example.org/api"} {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := tr.Proxy(req)
		if err != nil || got == nil || got.String() != proxy.String() {
			t.Errorf("proxy for %s = %v, %v; want %v", raw, got, err, proxy)
		}
	}
}
