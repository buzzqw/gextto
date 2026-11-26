package dht

import (
	"testing"
	"time"
)

func TestPublishPeerResultStopsWhenConsumerIsGone(t *testing.T) {
	d := newTestDHT()
	d.stop = make(chan bool)
	d.PeersRequestResults = make(chan map[InfoHash][]string, 1)
	d.PeersRequestResults <- map[InfoHash][]string{}
	close(d.stop)

	done := make(chan struct{})
	go func() {
		d.publishPeerResult(map[InfoHash][]string{InfoHash("01234567890123456789"): {"192.0.2.1:6881"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publishing a peer result blocked after shutdown")
	}
}

func TestPublishPeerResultNeverBlocksOnFullLookupQueue(t *testing.T) {
	d := newTestDHT()
	d.stop = make(chan bool)
	d.PeersRequestResults = make(chan map[InfoHash][]string, 1)
	d.PeersRequestResults <- map[InfoHash][]string{}
	before := totalPeerNoticeDropped.Value()

	done := make(chan struct{})
	go func() {
		d.publishPeerResult(map[InfoHash][]string{InfoHash("01234567890123456789"): {"192.0.2.1:6881"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("a get_peers result blocked the packet loop on a full channel")
	}
	if got := totalPeerNoticeDropped.Value() - before; got != 1 {
		t.Fatalf("dropped peer notices = %d, want 1", got)
	}
}

func TestOfferAnnouncedPeerNeverBlocksTheConsumer(t *testing.T) {
	d := newTestDHT()
	d.stop = make(chan bool)
	d.PeersRequestResults = make(chan map[InfoHash][]string, 1)
	d.PeersRequestResults <- map[InfoHash][]string{}
	before := totalAnnounceNoticeDropped.Value()

	done := make(chan struct{})
	go func() {
		d.offerAnnouncedPeer(map[InfoHash][]string{InfoHash("01234567890123456789"): {"192.0.2.1:6881"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("an announce notice blocked the packet loop on a full channel")
	}
	if got := totalAnnounceNoticeDropped.Value() - before; got != 1 {
		t.Fatalf("dropped announce notices = %d, want 1", got)
	}
}
