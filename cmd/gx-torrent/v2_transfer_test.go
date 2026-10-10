package main

// v2_transfer_test.go proves the whole BitTorrent v2 (BEP 52) path end to end: a
// v2-only multi-file .torrent (with top-level "piece layers") is seeded and
// leeched by two daemons over the shared port, and the downloaded bytes must
// match. The merkle trees are computed here with crypto/sha256 so the test does
// not depend on the internal engine packages. The piece length is 32 KiB, so
// each piece is a merkle node (not a flat hash) and the tail piece of a file is
// shorter than the piece length.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	zbencode "github.com/zeebo/bencode"
)

// v2MagnetFromTorrent builds the v2-only magnet of a .torrent: btmh carries the
// SHA-256 of the raw info dict, which is the torrent identity (BEP 52).
func v2MagnetFromTorrent(t *testing.T, torrent []byte) string {
	t.Helper()
	var top struct {
		Info zbencode.RawMessage `bencode:"info"`
	}
	if err := zbencode.DecodeBytes(torrent, &top); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(top.Info)
	return "magnet:?xt=urn:btmh:1220" + hex.EncodeToString(sum[:])
}

const v2BlockSize = 16 * 1024

func v2HashPair(a, b [32]byte) [32]byte {
	var buf [64]byte
	copy(buf[:32], a[:])
	copy(buf[32:], b[:])
	return sha256.Sum256(buf[:])
}

func v2LeafHashes(data []byte) [][32]byte {
	var out [][32]byte
	for i := 0; i < len(data); i += v2BlockSize {
		end := i + v2BlockSize
		if end > len(data) {
			end = len(data)
		}
		out = append(out, sha256.Sum256(data[i:end]))
	}
	return out
}

func v2Root(leaves [][32]byte) [32]byte {
	n := 1
	for n < len(leaves) {
		n <<= 1
	}
	layer := make([][32]byte, n)
	copy(layer, leaves)
	for len(layer) > 1 {
		next := make([][32]byte, len(layer)/2)
		for i := range next {
			next[i] = v2HashPair(layer[2*i], layer[2*i+1])
		}
		layer = next
	}
	return layer[0]
}

// v2PieceLayer returns the hashes of the layer that covers one piece (ratio
// 16 KiB blocks per piece), truncated to the pieces that cover real data.
func v2PieceLayer(leaves [][32]byte, ratio int) [][32]byte {
	level := 0
	for r := ratio; r > 1; r >>= 1 {
		level++
	}
	n := 1
	for n < len(leaves) {
		n <<= 1
	}
	layer := make([][32]byte, n)
	copy(layer, leaves)
	groupSize := 1 << level
	for len(layer) > n/groupSize {
		next := make([][32]byte, len(layer)/2)
		for i := range next {
			next[i] = v2HashPair(layer[2*i], layer[2*i+1])
		}
		layer = next
	}
	count := (len(leaves) + groupSize - 1) / groupSize
	if count < len(layer) {
		layer = layer[:count]
	}
	return layer
}

type v2FileSpec struct {
	name string
	data []byte
}

// makeV2TorrentMulti builds a multi-file v2-only torrent with its piece layers.
func makeV2TorrentMulti(t *testing.T, specs []v2FileSpec, pieceLength int) []byte {
	t.Helper()
	tree := map[string]any{}
	layers := map[string][]byte{}
	for _, s := range specs {
		leaves := v2LeafHashes(s.data)
		root := v2Root(leaves)
		tree[s.name] = map[string]any{"": map[string]any{"length": len(s.data), "pieces root": root[:]}}
		if len(s.data) > pieceLength {
			layer := v2PieceLayer(leaves, pieceLength/v2BlockSize)
			lb := make([]byte, 0, len(layer)*32)
			for _, h := range layer {
				lb = append(lb, h[:]...)
			}
			layers[string(root[:])] = lb
		}
	}
	info := struct {
		Name        string `bencode:"name"`
		PieceLength int    `bencode:"piece length"`
		MetaVersion int    `bencode:"meta version"`
		FileTree    any    `bencode:"file tree"`
	}{Name: "v2set", PieceLength: pieceLength, MetaVersion: 2, FileTree: tree}
	infoBytes, err := zbencode.EncodeBytes(info)
	if err != nil {
		t.Fatal(err)
	}
	top := struct {
		Info        zbencode.RawMessage `bencode:"info"`
		PieceLayers map[string][]byte   `bencode:"piece layers"`
	}{Info: infoBytes, PieceLayers: layers}
	out, err := zbencode.EncodeBytes(top)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func v2Payload(n int, seed byte) []byte {
	p := make([]byte, n)
	for i := range p {
		p[i] = byte(i*31+7) ^ seed
	}
	return p
}

func TestV2OnlyTransfer(t *testing.T) {
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 42800, PortEnd: 42899, Encryption: 1})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 42900, PortEnd: 42999, Encryption: 1})

	payloadA := v2Payload(5*v2BlockSize, 0x21) // 3 pieces, tail of 1 block
	payloadB := v2Payload(3*v2BlockSize, 0x53) // 2 pieces, tail of 1 block
	payloadC := v2Payload(10*1024, 0x7a)       // one block: a single-piece file
	torrent := makeV2TorrentMulti(t, []v2FileSpec{
		{"a.bin", payloadA}, {"b.bin", payloadB}, {"c.bin", payloadC},
	}, 2*v2BlockSize)

	src := t.TempDir()
	for name, data := range map[string][]byte{"a.bin": payloadA, "b.bin": payloadB, "c.bin": payloadC} {
		if err := os.WriteFile(filepath.Join(src, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hash, _, err := seeder.add(addRequest{TorrentData: torrent, Destination: src})
	if err != nil {
		t.Fatalf("seeder add: %v", err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

	dst := t.TempDir()
	if _, _, err := leecher.add(addRequest{TorrentData: torrent, Destination: dst}); err != nil {
		t.Fatalf("leecher add: %v", err)
	}
	waitFor(t, "leecher running", func() bool { return stateOf(leecher, hash) == "downloading" })
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(hash)
	leecher.mu.Unlock()
	if handle == nil {
		t.Fatal("leecher has no handle")
	}
	if err := handle.AddPeer("127.0.0.1:" + strconv.Itoa(seeder.peerPort)); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "v2 download complete", func() bool { return stateOf(leecher, hash) == "seeding" })
	for name, want := range map[string][]byte{"a.bin": payloadA, "b.bin": payloadB, "c.bin": payloadC} {
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("downloaded %s differs: %v", name, err)
		}
	}
}

// TestV2MagnetTransfer downloads a v2-only torrent from a magnet link: the
// leecher resolves the info dict over BEP 9, then fetches the per-file "piece
// layers" from the seeder with BEP 52 hash request/hashes before downloading
// and verifying the data.
func TestV2MagnetTransfer(t *testing.T) {
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 43000, PortEnd: 43099, Encryption: 1})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 43100, PortEnd: 43199, Encryption: 1})

	payloadA := v2Payload(5*v2BlockSize, 0x21) // 3 pieces
	payloadB := v2Payload(3*v2BlockSize, 0x53) // 2 pieces
	payloadC := v2Payload(10*1024, 0x7a)       // one block, single-piece file
	torrent := makeV2TorrentMulti(t, []v2FileSpec{
		{"a.bin", payloadA}, {"b.bin", payloadB}, {"c.bin", payloadC},
	}, 2*v2BlockSize)

	src := t.TempDir()
	for name, data := range map[string][]byte{"a.bin": payloadA, "b.bin": payloadB, "c.bin": payloadC} {
		if err := os.WriteFile(filepath.Join(src, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hash, _, err := seeder.add(addRequest{TorrentData: torrent, Destination: src})
	if err != nil {
		t.Fatalf("seeder add: %v", err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

	magnet := v2MagnetFromTorrent(t, torrent)
	dst := t.TempDir()
	leechHash, _, err := leecher.add(addRequest{Magnet: magnet, Destination: dst})
	if err != nil {
		t.Fatalf("leecher add magnet: %v", err)
	}
	if leechHash != hash {
		t.Fatalf("magnet hash %s != torrent hash %s", leechHash, hash)
	}
	// The queue starts the torrent asynchronously; a manual peer added while it
	// is still paused would be dropped.
	waitFor(t, "leecher fetching metadata", func() bool { return stateOf(leecher, hash) == "downloading_metadata" })
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(hash)
	leecher.mu.Unlock()
	if handle == nil {
		t.Fatal("leecher has no handle")
	}
	if err := handle.AddPeer("127.0.0.1:" + strconv.Itoa(seeder.peerPort)); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "v2 magnet download complete", func() bool { return stateOf(leecher, hash) == "seeding" })
	for name, want := range map[string][]byte{"a.bin": payloadA, "b.bin": payloadB, "c.bin": payloadC} {
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("downloaded %s differs: %v", name, err)
		}
	}
}

// TestV2MagnetLayersSurviveRestart checks that the piece layers fetched for a
// v2 magnet are persisted with the resume data: after a restart the torrent is
// rebuilt and seeded without asking any peer for the layers again.
func TestV2MagnetLayersSurviveRestart(t *testing.T) {
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 44400, PortEnd: 44499, Encryption: 1})

	payload := v2Payload(5*v2BlockSize, 0x21) // 3 pieces
	torrent := makeV2TorrentMulti(t, []v2FileSpec{{"movie.bin", payload}}, 2*v2BlockSize)
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "movie.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, _, err := seeder.add(addRequest{TorrentData: torrent, Destination: src})
	if err != nil {
		t.Fatalf("seeder add: %v", err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

	data := t.TempDir()
	dst := t.TempDir()
	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{PortBegin: 44500, PortEnd: 44599, Encryption: 1},
		Tick:        50 * time.Millisecond,
		ProbeWindow: time.Minute,
	}
	boot := func() (*Daemon, func()) {
		d, err := newDaemon(opts)
		if err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() { d.run(stop); close(done) }()
		return d, func() { close(stop); <-done; d.close() }
	}

	d1, stop1 := boot()
	magnet := v2MagnetFromTorrent(t, torrent)
	if _, _, err := d1.add(addRequest{Magnet: magnet, Destination: dst}); err != nil {
		t.Fatalf("leecher add magnet: %v", err)
	}
	waitFor(t, "leecher fetching metadata", func() bool { return stateOf(d1, hash) == "downloading_metadata" })
	d1.mu.Lock()
	handle, _ := d1.findLocked(hash)
	d1.mu.Unlock()
	if handle == nil {
		t.Fatal("leecher has no handle")
	}
	if err := handle.AddPeer("127.0.0.1:" + strconv.Itoa(seeder.peerPort)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "v2 magnet download complete", func() bool { return stateOf(d1, hash) == "seeding" })
	stop1()

	d2, stop2 := boot()
	defer stop2()
	waitFor(t, "v2 magnet reloaded", func() bool { return stateOf(d2, hash) == "seeding" })
	d2.mu.Lock()
	tor, _ := d2.findLocked(hash)
	d2.mu.Unlock()
	if tor == nil {
		t.Fatal("torrent not found after reload")
	}
	exported, err := tor.Torrent()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !bytes.Contains(exported, []byte("12:piece layers")) {
		t.Fatal("the fetched piece layers were not persisted in the resume data")
	}
}

// TestV2TorrentSurvivesRestart locks in that a v2 torrent is not lost when the
// daemon restarts: its piece layers are persisted with the resume data, so the
// torrent is rebuilt and verified again.
func TestV2TorrentSurvivesRestart(t *testing.T) {
	data := t.TempDir()
	src := t.TempDir()
	payload := v2Payload(3*v2BlockSize, 0x11) // 2 pieces
	torrent := makeV2TorrentMulti(t, []v2FileSpec{{"movie.bin", payload}}, 2*v2BlockSize)
	if err := os.WriteFile(filepath.Join(src, "movie.bin"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{PortBegin: 44200, PortEnd: 44299, Encryption: 1},
		Tick:        50 * time.Millisecond,
		ProbeWindow: time.Minute,
	}
	boot := func() (*Daemon, func()) {
		d, err := newDaemon(opts)
		if err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() { d.run(stop); close(done) }()
		return d, func() { close(stop); <-done; d.close() }
	}

	d1, stop1 := boot()
	hash, _, err := d1.add(addRequest{TorrentData: torrent, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(d1, hash) == "seeding" })
	stop1()

	d2, stop2 := boot()
	defer stop2()
	waitFor(t, "v2 torrent reloaded", func() bool { _, ok := findInfo(d2, hash); return ok })
	waitFor(t, "reloaded v2 seeding", func() bool { return stateOf(d2, hash) == "seeding" })

	// The exported .torrent must still carry the v2 piece layers.
	d2.mu.Lock()
	tor, _ := d2.findLocked(hash)
	d2.mu.Unlock()
	if tor == nil {
		t.Fatal("torrent not found after reload")
	}
	exported, err := tor.Torrent()
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !bytes.Contains(exported, []byte("12:piece layers")) {
		t.Fatal("the exported v2 torrent lost its piece layers")
	}
}
