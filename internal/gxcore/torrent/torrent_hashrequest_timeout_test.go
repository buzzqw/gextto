package torrent

import (
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
)

func TestExpireV2HashRequestsReleasesStaleRequest(t *testing.T) {
	now := time.Now()
	silent, fresh := &peer.Peer{}, &peer.Peer{}
	stale := &v2LayerFile{inflightIndex: 0, inflightAt: now.Add(-v2HashTimeout - time.Second)}
	recent := &v2LayerFile{inflightIndex: 0, inflightAt: now.Add(-time.Second)}
	tr := &torrent{
		v2Pending:     map[*peer.Peer]*v2LayerFile{silent: stale, fresh: recent},
		v2NoHashPeers: map[*peer.Peer]struct{}{},
	}
	tr.expireV2HashRequests(now)
	if stale.inflightIndex != -1 {
		t.Fatal("stale request still in flight: its file would stay stalled")
	}
	if _, ok := tr.v2Pending[silent]; ok {
		t.Fatal("silent peer still pending")
	}
	if _, ok := tr.v2NoHashPeers[silent]; !ok {
		t.Fatal("silent peer would be asked again")
	}
	if recent.inflightIndex != 0 {
		t.Fatal("a recent request was expired")
	}
	if _, ok := tr.v2Pending[fresh]; !ok {
		t.Fatal("a recent request lost its peer")
	}
}
