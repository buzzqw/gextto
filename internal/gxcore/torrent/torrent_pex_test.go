package torrent

import (
	"net"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

func compactAddr(t *testing.T, addr string) string {
	t.Helper()
	a, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	var b []byte
	if tracker.IsIPv6(a) {
		b, err = tracker.NewCompactPeer6(a).MarshalBinary()
	} else {
		b, err = tracker.NewCompactPeer(a).MarshalBinary()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestPEXCandidatesAddedOnly locks the BEP 11 fix: only "added"/"added6" become
// candidates; "dropped"/"dropped6" (peers that left) must not.
func TestPEXCandidatesAddedOnly(t *testing.T) {
	msg := peerprotocol.ExtensionPEXMessage{
		Added:   compactAddr(t, "203.0.113.5:6881"),
		Dropped: compactAddr(t, "198.51.100.9:51413"),
	}
	added4, added6 := pexCandidates(msg)
	if len(added4) != 1 || added4[0].Port != 6881 || added4[0].IP.String() != "203.0.113.5" {
		t.Fatalf("added4 = %v, want only the added peer", added4)
	}
	if len(added6) != 0 {
		t.Fatalf("added6 = %v, want none", added6)
	}
}

// TestPEXCandidatesMalformedAndV6 checks that a malformed "added" does not
// discard the valid "added6" list, and that dropped6 is ignored.
func TestPEXCandidatesMalformedAndV6(t *testing.T) {
	msg := peerprotocol.ExtensionPEXMessage{
		Added:    "bad", // length not a multiple of 6: decode error, skipped
		Added6:   compactAddr(t, "[2001:db8::1]:6881"),
		Dropped6: compactAddr(t, "[2001:db8::2]:6881"),
	}
	added4, added6 := pexCandidates(msg)
	if len(added4) != 0 {
		t.Fatalf("added4 = %v, want none", added4)
	}
	if len(added6) != 1 || added6[0].IP.String() != "2001:db8::1" || added6[0].Port != 6881 {
		t.Fatalf("added6 = %v, want only the added v6 peer", added6)
	}
}
