package magnet

import (
	"encoding/hex"
	"slices"
	"strings"
	"testing"
)

func TestHybridMagnetCarriesV2Hash(t *testing.T) {
	v1 := strings.Repeat("ab", 20) // 20 bytes hex
	v2 := strings.Repeat("cd", 32) // 32 bytes hex
	m, err := New("magnet:?xt=urn:btmh:1220" + v2 + "&xt=urn:btih:" + v1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !m.HasV2 {
		t.Fatal("HasV2 = false, want true")
	}
	wantV2, _ := hex.DecodeString(v2)
	if hex.EncodeToString(m.V2InfoHash[:]) != hex.EncodeToString(wantV2) {
		t.Errorf("v2 hash = %x, want %s", m.V2InfoHash, v2)
	}
	wantV1, _ := hex.DecodeString(v1)
	if hex.EncodeToString(m.InfoHash[:]) != hex.EncodeToString(wantV1) {
		t.Errorf("v1 hash = %x, want %s", m.InfoHash, v1)
	}
}

func TestV2OnlyMagnetUsesTruncatedHash(t *testing.T) {
	v2 := strings.Repeat("cd", 32)
	m, err := New("magnet:?xt=urn:btmh:1220" + v2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.HasV1 {
		t.Error("v2-only magnet reported a v1 hash")
	}
	if !m.HasV2 {
		t.Fatal("HasV2 = false, want true")
	}
	want, _ := hex.DecodeString(v2)
	if hex.EncodeToString(m.V2InfoHash[:]) != v2 {
		t.Errorf("v2 hash = %x, want %s", m.V2InfoHash, v2)
	}
	if hex.EncodeToString(m.InfoHash[:]) != hex.EncodeToString(want[:20]) {
		t.Errorf("info hash = %x, want truncated %x", m.InfoHash, want[:20])
	}
	// The link round-trips as a btmh topic, not a bogus btih one.
	if got := m.String(); !strings.Contains(got, "urn:btmh:1220"+v2) || strings.Contains(got, "btih") {
		t.Errorf("String() = %q", got)
	}
}

func TestPlainMagnetHasNoV2(t *testing.T) {
	m, err := New("magnet:?xt=urn:btih:" + strings.Repeat("ab", 20))
	if err != nil {
		t.Fatal(err)
	}
	if m.HasV2 {
		t.Error("plain v1 magnet reported a v2 hash")
	}
}

func TestMagnetSelectOnly(t *testing.T) {
	v1 := strings.Repeat("ab", 20)
	cases := []struct {
		name string
		so   string
		want []int
	}{
		{"absent", "", nil},
		{"single", "&so=1", []int{1}},
		{"list", "&so=0,2,5", []int{0, 2, 5}},
		{"range", "&so=4-7", []int{4, 5, 6, 7}},
		{"mixed", "&so=0,2,4-6", []int{0, 2, 4, 5, 6}},
		{"spaces and dupes", "&so=3,+3,+1", []int{1, 3}},
		{"repeated params", "&so=2&so=0", []int{0, 2}},
		{"garbage ignored", "&so=a,b-", nil},
		{"descending range", "&so=5-3", []int{3, 4, 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := New("magnet:?xt=urn:btih:" + v1 + tc.so)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(m.SelectOnly, tc.want) {
				t.Errorf("SelectOnly = %v, want %v", m.SelectOnly, tc.want)
			}
		})
	}
}

func TestMagnetSelectOnlyRoundTrip(t *testing.T) {
	v1 := strings.Repeat("ab", 20)
	m, err := New("magnet:?xt=urn:btih:" + v1 + "&so=1,3,5")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := New(m.String())
	if err != nil {
		t.Fatalf("re-parse %q: %v", m.String(), err)
	}
	if !slices.Equal(m2.SelectOnly, []int{1, 3, 5}) {
		t.Errorf("round trip SelectOnly = %v, want [1 3 5]", m2.SelectOnly)
	}
}

func TestMagnetWebSeeds(t *testing.T) {
	v1 := strings.Repeat("ab", 20)
	// Repeated ws, plus an empty one that must be dropped.
	m, err := New("magnet:?xt=urn:btih:" + v1 +
		"&ws=http%3A%2F%2Fa.test%2Ff.bin&ws=http%3A%2F%2Fb.test%2Fx&ws=")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"http://a.test/f.bin", "http://b.test/x"}
	if !slices.Equal(m.WebSeeds, want) {
		t.Fatalf("WebSeeds = %v, want %v", m.WebSeeds, want)
	}
	m2, err := New(m.String())
	if err != nil {
		t.Fatalf("re-parse %q: %v", m.String(), err)
	}
	if !slices.Equal(m2.WebSeeds, want) {
		t.Errorf("round trip WebSeeds = %v, want %v", m2.WebSeeds, want)
	}
}

func TestMagnetDHTNodes(t *testing.T) {
	v1 := strings.Repeat("ab", 20)
	// Repeated dht nodes, one with surrounding spaces, plus an empty one.
	m, err := New("magnet:?xt=urn:btih:" + v1 +
		"&dht=router.bittorrent.com%3A6881&dht=%20dht.example.org%3A6881%20&dht=")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"router.bittorrent.com:6881", "dht.example.org:6881"}
	if !slices.Equal(m.DHTNodes, want) {
		t.Fatalf("DHTNodes = %v, want %v", m.DHTNodes, want)
	}
	m2, err := New(m.String())
	if err != nil {
		t.Fatalf("re-parse %q: %v", m.String(), err)
	}
	if !slices.Equal(m2.DHTNodes, want) {
		t.Errorf("round trip DHTNodes = %v, want %v", m2.DHTNodes, want)
	}
}

func TestMagnetSources(t *testing.T) {
	v1 := strings.Repeat("ab", 20)
	m, err := New("magnet:?xt=urn:btih:" + v1 +
		"&xs=https%3A%2F%2Fexact.test%2Fa.torrent&as=http%3A%2F%2Fmirror.test%2Fa.torrent&as=")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"https://exact.test/a.torrent"}; !slices.Equal(m.ExactSources, want) {
		t.Fatalf("ExactSources = %v, want %v", m.ExactSources, want)
	}
	if want := []string{"http://mirror.test/a.torrent"}; !slices.Equal(m.AcceptableSources, want) {
		t.Fatalf("AcceptableSources = %v, want %v", m.AcceptableSources, want)
	}

	m2, err := New(m.String())
	if err != nil {
		t.Fatalf("re-parse %q: %v", m.String(), err)
	}
	if want := []string{"https://exact.test/a.torrent", "http://mirror.test/a.torrent"}; !slices.Equal(m2.MetadataSources(), want) {
		t.Fatalf("round trip MetadataSources = %v, want %v", m2.MetadataSources(), want)
	}
}

// TestMagnetMetadataSourcesSkipsNonHTTP checks that a nested magnet or a
// malformed URL in "xs"/"as" is ignored by MetadataSources.
func TestMagnetMetadataSourcesSkipsNonHTTP(t *testing.T) {
	m := &Magnet{
		ExactSources:      []string{"magnet:?xt=urn:btih:" + strings.Repeat("cd", 20), "https://exact.test/a.torrent"},
		AcceptableSources: []string{"not a url", "http://mirror.test/a.torrent"},
	}
	got := m.MetadataSources()
	want := []string{"https://exact.test/a.torrent", "http://mirror.test/a.torrent"}
	if !slices.Equal(got, want) {
		t.Fatalf("MetadataSources = %v, want %v", got, want)
	}
}
