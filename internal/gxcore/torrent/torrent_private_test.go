package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// TestPrivateSwarmDisablesDiscovery is the BEP 27 guard: the DHT, PEX and LSD
// paths all key off torrent.privateSwarm(), so a private torrent must be
// detected (and unknown metadata treated as non-private, since a private magnet
// is stopped as soon as the info dict arrives).
func TestPrivateSwarmDisablesDiscovery(t *testing.T) {
	if (&torrent{}).privateSwarm() {
		t.Fatal("a torrent without metadata must not be treated as private")
	}
	if !(&torrent{info: &metainfo.Info{Private: true}}).privateSwarm() {
		t.Fatal("a private torrent must be detected")
	}
	if (&torrent{info: &metainfo.Info{Private: false}}).privateSwarm() {
		t.Fatal("a public torrent must not be private")
	}
}
