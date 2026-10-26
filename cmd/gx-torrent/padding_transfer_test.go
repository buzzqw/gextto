package main

// padding_transfer_test.go downloads a v1 torrent with BEP 47 padding files
// between two daemons. The padding bytes are never received, so the leecher
// can only verify those pieces if their buffer starts zeroed: the pieces with
// padding come after full pieces of the same torrent, whose buffers the pool
// hands out again uncleared.

import (
	"bytes"
	"crypto/sha1"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestPaddedTorrentTransfer(t *testing.T) {
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 43000, PortEnd: 43099, Encryption: 1})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 43100, PortEnd: 43199, Encryption: 1})

	const pieceLength = 16384
	const fileSize = 40000                   // 2 full pieces and 7,232 bytes
	const padSize = 3*pieceLength - fileSize // the rest of the third piece
	src := filepath.Join(t.TempDir(), "padded")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	var stream []byte
	var files []any
	payloads := map[string][]byte{}
	for i := 0; i < 6; i++ {
		name := "f" + strconv.Itoa(i) + ".bin"
		data := bytes.Repeat([]byte{byte(0x31 + i*17)}, fileSize)
		for j := range data {
			data[j] ^= byte(j * 7)
		}
		payloads[name] = data
		if err := os.WriteFile(filepath.Join(src, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		stream = append(stream, data...)
		files = append(files, map[string]any{"length": fileSize, "path": []any{name}})
		if i < 5 {
			stream = append(stream, make([]byte, padSize)...)
			files = append(files, map[string]any{"length": padSize, "path": []any{".pad", strconv.Itoa(padSize)}, "attr": "p"})
		}
	}
	var pieces []byte
	for offset := 0; offset < len(stream); offset += pieceLength {
		sum := sha1.Sum(stream[offset:min(offset+pieceLength, len(stream))])
		pieces = append(pieces, sum[:]...)
	}
	torrent := bencode(map[string]any{"info": map[string]any{
		"name": "padded", "piece length": pieceLength, "pieces": pieces, "files": files,
	}})

	hash, _, err := seeder.add(addRequest{TorrentData: torrent, Destination: filepath.Dir(src)})
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
	waitFor(t, "padded download complete", func() bool { return stateOf(leecher, hash) == "seeding" })
	for name, want := range payloads {
		got, err := os.ReadFile(filepath.Join(dst, "padded", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("downloaded %s differs: %v", name, err)
		}
	}
}
