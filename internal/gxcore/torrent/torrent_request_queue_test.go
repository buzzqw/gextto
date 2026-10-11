package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

func TestAdaptiveRequestQueue(t *testing.T) {
	s := &Session{config: Config{DefaultRequestsOut: 50, MaxRequestsOut: 250}}
	tr := &torrent{session: s}

	// No advertised reqq: cap is MaxRequestsOut, base is DefaultRequestsOut.
	pe := &peer.Peer{}
	if got := tr.maxAllowedRequests(pe); got != 50 {
		t.Fatalf("initial = %d, want 50", got)
	}
	for i := 0; i < 10; i++ {
		tr.boostRequestQueue(pe)
	}
	if got := tr.maxAllowedRequests(pe); got != 60 {
		t.Fatalf("after 10 boosts = %d, want 60", got)
	}
	for i := 0; i < 1000; i++ {
		tr.boostRequestQueue(pe)
	}
	if got := tr.maxAllowedRequests(pe); got != 250 {
		t.Fatalf("at cap = %d, want 250", got)
	}
	tr.resetRequestQueue(pe)
	if got := tr.maxAllowedRequests(pe); got != 50 {
		t.Fatalf("after reset = %d, want 50", got)
	}

	// A peer that advertises a small reqq is capped there: no growth.
	pe2 := &peer.Peer{ExtensionHandshake: &peerprotocol.ExtensionHandshakeMessage{RequestQueue: 20}}
	if got := tr.maxAllowedRequests(pe2); got != 20 {
		t.Fatalf("reqq=20 initial = %d, want 20", got)
	}
	tr.boostRequestQueue(pe2)
	if got := tr.maxAllowedRequests(pe2); got != 20 {
		t.Fatalf("reqq=20 after boost = %d, want 20 (capped)", got)
	}
}
