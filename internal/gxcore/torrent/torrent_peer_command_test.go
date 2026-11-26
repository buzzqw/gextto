package torrent

import (
	"testing"
	"time"
)

func TestResolvedManualPeerIsDeliveredToRunLoop(t *testing.T) {
	tr := &torrent{
		commandC: make(chan func(), 1),
		closeC:   make(chan struct{}),
		session:  &Session{config: Config{DNSResolveTimeout: time.Second}},
	}
	tr.resolveAndAddPeer("localhost", 6881)
	select {
	case <-tr.commandC:
	default:
		t.Fatal("resolved peer was not queued for the torrent run loop")
	}
}
