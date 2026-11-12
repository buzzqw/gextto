package torrent

import "testing"

func peerID(prefix string) [20]byte {
	var id [20]byte
	copy(id[:], prefix)
	for i := len(prefix); i < 20; i++ {
		id[i] = 0xa0 // the random tail is not printable
	}
	return id
}

func TestClientFilterMatchesPrefixesAndNames(t *testing.T) {
	f := newClientFilter([]string{" -XL ", "thunder", "", "-xl"})
	if f == nil || len(f.rules) != 2 {
		t.Fatalf("rules = %+v", f)
	}
	if _, ok := f.matchPeerID(peerID("-XL0012-")); !ok {
		t.Fatal("-XL prefix not refused")
	}
	if _, ok := f.matchPeerID(peerID("-qB4650-")); ok {
		t.Fatal("qBittorrent refused")
	}
	if _, ok := f.matchClientName("Thunder 11.2"); !ok {
		t.Fatal("client name not refused")
	}
	if _, ok := f.matchClientName("qBittorrent/4.6.7"); ok {
		t.Fatal("qBittorrent name refused")
	}
	// A "-" rule is a peer ID prefix only, never matched inside a name.
	if _, ok := f.matchClientName("my-XL-client"); ok {
		t.Fatal("prefix rule matched a client name")
	}
	if newClientFilter(nil) != nil || newClientFilter([]string{"  "}) != nil {
		t.Fatal("an empty list must turn the filter off")
	}
	var off *clientFilter
	if _, ok := off.matchPeerID(peerID("-XL0012-")); ok {
		t.Fatal("a nil filter refused a peer")
	}
}

func TestSessionClientFilterCountsRefusals(t *testing.T) {
	var s Session
	s.SetClientFilter(DefaultClientFilter)
	if !s.refuseClientByID(peerID("-SD0100-")) {
		t.Fatal("suggested list does not refuse -SD")
	}
	if s.refuseClientByID(peerID("-GX1220-")) {
		t.Fatal("gx-core refused")
	}
	if !s.refuseClientByName("Xunlei 0.0.1") {
		t.Fatal("suggested list does not refuse Xunlei")
	}
	if got := s.ClientFilterRefused(); got != 2 {
		t.Fatalf("refused = %d, want 2", got)
	}
	s.SetClientFilter(nil)
	if s.refuseClientByID(peerID("-SD0100-")) {
		t.Fatal("filter off still refuses")
	}
}

func TestClampWeight(t *testing.T) {
	for in, want := range map[int]int{-5: 0, 0: 0, 5: 10, 100: 100, 5000: 1000} {
		if got := clampWeight(in); got != want {
			t.Errorf("clampWeight(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestReciprocityWeight(t *testing.T) {
	cases := []struct {
		rate, avg int
		want      int32
	}{{0, 100, 100}, {100, 0, 100}, {50, 100, 150}, {100, 100, 200}, {200, 100, 300}, {1000, 100, 400}}
	for _, c := range cases {
		if got := reciprocityWeight(c.rate, c.avg); got != c.want {
			t.Errorf("reciprocityWeight(%d, %d) = %d, want %d", c.rate, c.avg, got, c.want)
		}
	}
}
