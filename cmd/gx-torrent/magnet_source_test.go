package main

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTorrentWithHash is makeTorrent plus the v1 info hash (sha1 of the bencoded
// info dictionary), so a test can build a magnet for it.
func makeTorrentWithHash(t *testing.T, dir, name string, size int) ([]byte, string) {
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
	for off := 0; off < size; off += pieceLength {
		end := min(off+pieceLength, size)
		sum := sha1.Sum(payload[off:end])
		pieces = append(pieces, sum[:]...)
	}
	info := map[string]any{
		"name":         name,
		"length":       size,
		"piece length": pieceLength,
		"pieces":       pieces,
	}
	// bencode is deterministic (sorted keys), so the info dictionary is encoded
	// the same way inside the torrent and standalone: the sha1 of the latter is
	// the info hash the engine computes.
	hash := sha1.Sum(bencode(info))
	return bencode(map[string]any{"info": info}), hex.EncodeToString(hash[:])
}

// TestDaemonMagnetExactSource checks the magnet "xs" fast path: the daemon
// fetches the .torrent from the exact source instead of waiting for a peer or
// the DHT, so the metadata is available right after the add.
func TestDaemonMagnetExactSource(t *testing.T) {
	// The source is on 127.0.0.1: allow private addresses for this test (the
	// default refuses them, see TestMagnetSourceOnLANRefusedByDefault).
	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 42000, PortEnd: 42100, Encryption: 1, PEX: true, WebSourcesAllowPrivate: true})
	dir := t.TempDir()
	data, hash := makeTorrentWithHash(t, dir, "source.bin", 50_000)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	magnet := "magnet:?xt=urn:btih:" + hash + "&dn=source.bin&xs=" + url.QueryEscape(srv.URL+"/source.torrent")
	addedHash, existing, err := d.add(addRequest{Magnet: magnet, Destination: dir, SeedRatio: -1, SeedDays: -1})
	if err != nil || existing {
		t.Fatalf("add: %v existing=%v", err, existing)
	}
	if !strings.EqualFold(addedHash, hash) {
		t.Fatalf("added hash = %s, want %s", addedHash, hash)
	}

	// The metadata is present without any swarm: the file list is already known.
	info, ok := findInfo(d, hash)
	if !ok {
		t.Fatal("torrent not found after add")
	}
	if !info.HasMetadata || info.FileCount != 1 {
		t.Fatalf("metadata not available from the exact source: %+v", info)
	}
}

// TestDaemonMagnetExactSourceMismatch checks that a source serving a different
// torrent is rejected and the add falls back to the normal magnet: the torrent
// is still created (so the swarm can fetch the metadata) but carries none yet.
func TestDaemonMagnetExactSourceMismatch(t *testing.T) {
	d := newTestDaemon(t)
	dir := t.TempDir()
	magnetHash := strings.Repeat("cd", 20)
	// A real, valid .torrent that is NOT the one the magnet names.
	wrong := makeTorrent(t, dir, "wrong.bin", 50_000)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(wrong)
	}))
	defer srv.Close()

	magnet := "magnet:?xt=urn:btih:" + magnetHash + "&xs=" + url.QueryEscape(srv.URL+"/wrong.torrent")
	addedHash, existing, err := d.add(addRequest{Magnet: magnet, Destination: dir, SeedRatio: -1, SeedDays: -1})
	if err != nil || existing {
		t.Fatalf("add: %v existing=%v", err, existing)
	}
	if !strings.EqualFold(addedHash, magnetHash) {
		t.Fatalf("added hash = %s, want %s", addedHash, magnetHash)
	}
	if info, ok := findInfo(d, magnetHash); !ok || info.HasMetadata {
		t.Fatalf("a mismatching source must not provide metadata: %+v", info)
	}
}
