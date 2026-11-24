package trackermanager

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist"
	"github.com/buzzqw/gextto/internal/gxcore/internal/netx"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

func TestUntrustedTrackersRefusePrivateLiterals(t *testing.T) {
	m := New(nil, time.Second, false)
	defer m.Close()

	for _, raw := range []string{
		"http://127.0.0.1:8080/announce",
		"https://[::1]:8443/announce",
		"udp://192.168.1.2:6881/announce",
		"ws://169.254.1.2/announce",
		"wss://10.0.0.3/announce",
		"http://[fec0::1]/announce",
	} {
		if _, err := m.GetUntrusted(raw, time.Second, "test", 1<<20); !errors.Is(err, netx.ErrPrivateAddress) {
			t.Errorf("GetUntrusted(%q) error = %v, want ErrPrivateAddress", raw, err)
		}
	}
}

func TestUntrustedTrackersAllowPublicLiterals(t *testing.T) {
	m := New(nil, time.Second, false)
	defer m.Close()

	for _, raw := range []string{
		"http://8.8.8.8:8080/announce",
		"https://[2606:4700:4700::1111]:8443/announce",
		"udp://1.1.1.1:6881/announce",
		"ws://9.9.9.9/announce",
		"wss://[2620:fe::fe]/announce",
	} {
		if _, err := m.GetUntrusted(raw, time.Second, "test", 1<<20); err != nil {
			t.Errorf("GetUntrusted(%q): %v", raw, err)
		}
	}
}

func TestPrivateTrackerPolicyIsExplicit(t *testing.T) {
	private := "http://127.0.0.1:8080/announce"

	trusted := New(nil, time.Second, false)
	if _, err := trusted.Get(private, time.Second, "test", 1<<20); err != nil {
		t.Fatalf("operator-supplied private tracker was refused: %v", err)
	}
	trusted.Close()

	optedIn := NewWithPolicy(nil, time.Second, false, true)
	defer optedIn.Close()
	if _, err := optedIn.GetUntrusted(private, time.Second, "test", 1<<20); err != nil {
		t.Fatalf("opted-in private tracker was refused: %v", err)
	}
}

func TestPublicTrackerDialRejectsPrivateDNS(t *testing.T) {
	m := New(nil, time.Second, false)
	defer m.Close()

	if _, err := m.publicDial(context.Background(), "tcp", "localhost:8080"); !errors.Is(err, netx.ErrPrivateAddress) {
		t.Fatalf("publicDial(localhost) error = %v, want ErrPrivateAddress", err)
	}
}

func TestUntrustedUDPTrackerRejectsPrivateDNS(t *testing.T) {
	m := New(nil, time.Second, false)
	defer m.Close()
	tr, err := m.GetUntrusted("udp://localhost:6881/announce", time.Second, "test", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Announce(context.Background(), tracker.AnnounceRequest{}); !errors.Is(err, netx.ErrPrivateAddress) {
		t.Fatalf("UDP announce to localhost error = %v, want ErrPrivateAddress", err)
	}
}

func TestUntrustedHTTPTrackerChecksRedirectDestination(t *testing.T) {
	var requests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "http://127.0.0.1:"+strconv.Itoa(server.Listener.Addr().(*net.TCPAddr).Port)+"/private", http.StatusFound)
	}))
	defer server.Close()

	m := New(nil, time.Second, false)
	defer m.Close()
	// Make the initial public-looking name reach the local test server without
	// weakening the destination check. The redirect's loopback literal must be
	// rejected before this dial function is called a second time.
	m.resolve = func(_ context.Context, hostport string, _ time.Duration, _ *blocklist.Blocklist) (net.IP, int, error) {
		host, port, err := net.SplitHostPort(hostport)
		if err != nil {
			return nil, 0, err
		}
		if host == "tracker.example" {
			portNumber, err := strconv.Atoi(port)
			return net.ParseIP("8.8.8.8"), portNumber, err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, 0, errors.New("unexpected test host")
		}
		portNumber, err := strconv.Atoi(port)
		return ip, portNumber, err
	}
	var dials atomic.Int32
	m.dialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}

	tr, err := m.GetUntrusted("http://tracker.example/announce", time.Second, "test", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Announce(context.Background(), tracker.AnnounceRequest{})
	if !errors.Is(err, netx.ErrPrivateAddress) {
		t.Fatalf("redirected announce error = %v, want ErrPrivateAddress", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("server received %d requests, want only the public tracker request", got)
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("dialled %d times, want the private redirect to be stopped before dialing", got)
	}
}
