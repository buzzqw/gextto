package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// makeTorrentContent builds a single-file .torrent for the given name and
// content, returning the bytes and the v1 info hash.
func makeTorrentContent(t *testing.T, name string, content []byte) ([]byte, string) {
	t.Helper()
	const pieceLength = 16384
	var pieces []byte
	for off := 0; off < len(content); off += pieceLength {
		end := min(off+pieceLength, len(content))
		sum := sha1.Sum(content[off:end])
		pieces = append(pieces, sum[:]...)
	}
	info := map[string]any{
		"name":         name,
		"length":       len(content),
		"piece length": pieceLength,
		"pieces":       pieces,
	}
	hash := sha1.Sum(bencode(info))
	return bencode(map[string]any{"info": info}), hex.EncodeToString(hash[:])
}

// TestDaemonSwarmMergeLocalData proves Swarm Merging Fase 1 end to end: a torrent
// whose data is already on disk under another name (different info hash, same
// piece layout) is completed by reusing that data, without any peer.
func TestDaemonSwarmMergeLocalData(t *testing.T) {
	d := newTestDaemon(t)
	dir := t.TempDir()
	content := make([]byte, 16384*3+123)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}

	// B: its file already exists, so it verifies it from disk and seeds it.
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	dataB, hashB := makeTorrentContent(t, "b.bin", content)
	if _, _, err := d.add(addRequest{TorrentData: dataB, Destination: dir, SeedRatio: -1, SeedDays: -1}); err != nil {
		t.Fatalf("add B: %v", err)
	}
	waitFor(t, "B seeded from disk", func() bool {
		info, ok := findInfo(d, hashB)
		return ok && info.TotalSize > 0 && info.TotalDone == info.TotalSize
	})

	// A: same content, different name (so a different info hash but identical
	// pieces). Its file does not exist: completing it can only come from
	// reusing B's data.
	dataA, hashA := makeTorrentContent(t, "a.bin", content)
	if _, _, err := d.add(addRequest{TorrentData: dataA, Destination: dir, SeedRatio: -1, SeedDays: -1}); err != nil {
		t.Fatalf("add A: %v", err)
	}
	waitFor(t, "A completed via swarm merge", func() bool {
		info, ok := findInfo(d, hashA)
		return ok && info.TotalSize > 0 && info.TotalDone == info.TotalSize
	})

	got, err := os.ReadFile(filepath.Join(dir, "a.bin"))
	if err != nil {
		t.Fatalf("read a.bin: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("a.bin does not contain the reused content")
	}
}
