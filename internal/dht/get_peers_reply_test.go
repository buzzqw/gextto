package dht

// get_peers_reply_test.go covers what a get_peers reply carries: the BEP 33
// scrape filters ride along with the normal answer instead of replacing it,
// so a scrape ask learns the swarm's size and its contacts in one datagram.

import (
	"net"
	"testing"
)

func scrapeReplyFor(t *testing.T, ih InfoHash, scrape int) replyMessage {
	t.Helper()
	d := newTestDHT()
	d.tokenSecrets = []string{"secret"}
	d.peerStore.addContact(ih, encodeCompactPeerAddr("1.2.3.4:6881"), false)
	d.peerStore.addContact(ih, encodeCompactPeerAddr("5.6.7.8:6882"), true)
	addr := net.UDPAddr{IP: net.ParseIP("203.0.113.5"), Port: 6881}
	return d.buildGetPeersReply(addr, responseType{
		T: "aa",
		A: answerType{InfoHash: ih, Scrape: scrape},
	})
}

// TestScrapeReplyKeepsThePeers: BEP 33 says the filters are added to the
// normal reply, so the contacts must still be there.
func TestScrapeReplyKeepsThePeers(t *testing.T) {
	ih := InfoHash("01234567890123456789")
	reply := scrapeReplyFor(t, ih, 1)
	if _, ok := reply.R["BFpe"]; !ok {
		t.Fatal("the scrape reply has no peer filter")
	}
	if _, ok := reply.R["BFsd"]; !ok {
		t.Fatal("the scrape reply has no seed filter")
	}
	values, ok := reply.R["values"].([]string)
	if !ok || len(values) != 2 {
		t.Fatalf("a scrape reply must still carry the swarm's contacts, got %v", reply.R["values"])
	}
}

// A plain get_peers has no filters, and the contacts stay as before.
func TestPlainGetPeersReplyHasNoFilters(t *testing.T) {
	ih := InfoHash("01234567890123456789")
	reply := scrapeReplyFor(t, ih, 0)
	if _, ok := reply.R["BFpe"]; ok {
		t.Fatal("a plain get_peers reply must not carry a peer filter")
	}
	if values, ok := reply.R["values"].([]string); !ok || len(values) != 2 {
		t.Fatalf("a plain reply must carry the swarm's contacts, got %v", reply.R["values"])
	}
}

// With no peer for the swarm, the scrape reply falls back to the closest
// nodes like any other reply.
func TestScrapeReplyWithoutPeersFallsBackToNodes(t *testing.T) {
	d := newTestDHT()
	d.tokenSecrets = []string{"secret"}
	d.insertTestNode(t, "1.2.3.4:6881", "aaaaaaaaaaaaaaaaaaaa", "udp4")
	ih := InfoHash("01234567890123456789")
	addr := net.UDPAddr{IP: net.ParseIP("203.0.113.5"), Port: 6881}
	reply := d.buildGetPeersReply(addr, responseType{T: "aa", A: answerType{InfoHash: ih, Scrape: 1}})
	if _, ok := reply.R["values"]; ok {
		t.Fatal("a swarm with no known peer must not reply with values")
	}
	if _, ok := reply.R["nodes"]; !ok {
		t.Fatal("the reply must fall back to the closest nodes")
	}
}
