package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

func testHash(b byte) torrent.InfoHash {
	var h torrent.InfoHash
	for i := range h {
		h[i] = b
	}
	return h
}

func TestMatchLocalData(t *testing.T) {
	a, b, c := testHash(1), testHash(2), testHash(3)
	target := bep38Hints{infoHash: a, collections: []string{"c1"}, similar: [][20]byte{b}}
	others := []bep38Hints{
		{infoHash: a, collections: []string{"c1"}}, // the target itself: skipped
		{infoHash: b}, // listed in target.similar
		{infoHash: c, collections: []string{"c1"}},          // shared collection
		{infoHash: testHash(4)},                             // no hint: no match
		{infoHash: testHash(5), collections: []string{"z"}}, // unrelated collection
	}
	if got := matchLocalData(target, others); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("matchLocalData = %v, want [1 2]", got)
	}
}

// TestMatchLocalDataSymmetry checks the BEP 38 symmetry rule: a torrent with no
// hint of its own still matches a torrent that lists its info hash in "similar".
func TestMatchLocalDataSymmetry(t *testing.T) {
	a, b := testHash(1), testHash(2)
	target := bep38Hints{infoHash: b}
	others := []bep38Hints{{infoHash: a, similar: [][20]byte{b}}}
	if got := matchLocalData(target, others); !slices.Equal(got, []int{0}) {
		t.Fatalf("matchLocalData = %v, want [0]", got)
	}
}

func TestSimilarHex(t *testing.T) {
	if got := similarHex(nil); got != nil {
		t.Fatalf("similarHex(nil) = %v, want nil", got)
	}
	got := similarHex([][20]byte{testHash(0xab)})
	if len(got) != 1 || got[0] != strings.Repeat("ab", 20) {
		t.Fatalf("similarHex = %v", got)
	}
}

// makeTorrentWithHints writes a payload file and returns its .torrent bytes with
// the given BEP 38 top-level hints.
func makeTorrentWithHints(t *testing.T, dir, name string, size int, collections []any, similar []any) []byte {
	t.Helper()
	payload := make([]byte, size)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	const pieceLength = 16384
	var pieces []byte
	for offset := 0; offset < size; offset += pieceLength {
		end := min(offset+pieceLength, size)
		sum := sha1.Sum(payload[offset:end])
		pieces = append(pieces, sum[:]...)
	}
	top := map[string]any{
		"info": map[string]any{
			"name":         name,
			"length":       size,
			"piece length": pieceLength,
			"pieces":       pieces,
		},
	}
	if len(collections) > 0 {
		top["collections"] = collections
	}
	if len(similar) > 0 {
		top["similar"] = similar
	}
	return bencode(top)
}

// TestDaemonCollectionsInspect checks the BEP 38 API: the collections/similar
// hints and the local torrents sharing a hint.
func TestDaemonCollectionsInspect(t *testing.T) {
	d := newTestDaemon(t)
	dir := t.TempDir()
	collection := []any{"com.example.series.s1"}

	hashA, _, err := d.add(addRequest{
		TorrentData: makeTorrentWithHints(t, dir, "a.bin", 20_000, collection, nil),
		Destination: dir, SeedRatio: -1, SeedDays: -1,
	})
	if err != nil {
		t.Fatalf("add A: %v", err)
	}
	hashB, _, err := d.add(addRequest{
		TorrentData: makeTorrentWithHints(t, dir, "b.bin", 20_000, collection, nil),
		Destination: dir, SeedRatio: -1, SeedDays: -1,
	})
	if err != nil {
		t.Fatalf("add B: %v", err)
	}

	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	var got collectionsInfo
	waitFor(t, "BEP 38 collections inspect", func() bool {
		resp, err := http.Get(srv.URL + "/api/v1/torrents/" + hashA + "/collections")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return false
		}
		got = collectionsInfo{}
		if json.NewDecoder(resp.Body).Decode(&got) != nil {
			return false
		}
		return len(got.Local) == 1
	})
	if len(got.Collections) != 1 || got.Collections[0] != "com.example.series.s1" {
		t.Fatalf("collections = %v", got.Collections)
	}
	if !strings.EqualFold(got.Local[0].Hash, hashB) {
		t.Fatalf("local candidate = %+v, want %s", got.Local[0], hashB)
	}
}

// TestDaemonDetailShowsBEP38 checks the daemon's detail page renders the BEP 38
// hint block when the torrent carries a hint.
func TestDaemonDetailShowsBEP38(t *testing.T) {
	d := newTestDaemon(t)
	dir := t.TempDir()
	hash, _, err := d.add(addRequest{
		TorrentData: makeTorrentWithHints(t, dir, "c.bin", 20_000, []any{"com.example.series.s2"}, nil),
		Destination: dir, SeedRatio: -1, SeedDays: -1,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	var body string
	waitFor(t, "BEP 38 detail block", func() bool {
		resp, err := http.Get(srv.URL + "/ui/detail?hash=" + hash + "&tab=general")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		body = string(b)
		return strings.Contains(body, "BEP 38 local data hints")
	})
	if !strings.Contains(body, "com.example.series.s2") {
		t.Fatalf("detail page missing the collection hint: %s", body)
	}
}
