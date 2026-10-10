package torrent

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
	"github.com/nictuku/dht"
)

// TestDHTIPv6AnnounceAndDiscover starts two DHT nodes on ::1 with the
// family-agnostic "udp" proto (BEP 32). One announces a torrent to the other,
// which must surface an 18-byte IPv6 peer contact: this exercises the whole
// dual-stack path (socket, query parsing, nodes6/announce over IPv6).
func TestDHTIPv6AnnounceAndDiscover(t *testing.T) {
	if !ipv6LoopbackAvailable() {
		t.Skip("no IPv6 loopback")
	}
	// Both nodes push results on a buffered channel of size 1; a goroutine
	// drains them so the DHT loop never blocks (and Stop can complete).
	results := make(chan string, 64)
	start := func() (*dht.DHT, string) {
		pc, err := net.ListenPacket("udp6", "[::1]:0")
		if err != nil {
			t.Fatalf("listen udp6: %v", err)
		}
		cfg := dht.NewConfig()
		cfg.PacketConn = pc
		cfg.UDPProto = "udp" // family-agnostic: resolve and accept IPv6
		cfg.Address = ""
		cfg.SaveRoutingTable = false
		cfg.DHTRouters = ""
		cfg.NumTargetPeers = 1
		n, err := dht.New(cfg)
		if err != nil {
			pc.Close()
			t.Fatalf("dht.New: %v", err)
		}
		if err := n.Start(); err != nil {
			pc.Close()
			t.Fatalf("dht.Start: %v", err)
		}
		go func() {
			for res := range n.PeersRequestResults {
				for _, peers := range res {
					for _, p := range peers {
						select {
						case results <- p:
						default:
						}
					}
				}
			}
		}()
		t.Cleanup(func() {
			// The engine closes the shared socket first, then stops the DHT:
			// with a provided PacketConn the DHT does not close it, and its
			// read loop would otherwise block Stop.
			pc.Close()
			n.Stop()
		})
		return n, pc.LocalAddr().String()
	}
	a, aAddr := start()
	b, bAddr := start()
	_, aPortStr, err := net.SplitHostPort(aAddr)
	if err != nil {
		t.Fatal(err)
	}
	aPort, _ := strconv.Atoi(aPortStr)

	a.AddNode(bAddr)
	b.AddNode(aAddr)

	ih := strings.Repeat("z", 20)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		a.PeersRequest(ih, true) // get_peers + announce_peer to the answering node
		select {
		case peer := <-results:
			if len(peer) != 18 {
				continue
			}
			var cp tracker.CompactPeer6
			if cp.UnmarshalBinary([]byte(peer)) != nil {
				continue
			}
			if addr := cp.Addr(); addr.IP.Equal(net.ParseIP("::1")) && addr.Port == aPort {
				return
			}
		case <-time.After(2 * time.Second):
		}
	}
	t.Fatal("no IPv6 DHT announce/discovery within the deadline")
}
