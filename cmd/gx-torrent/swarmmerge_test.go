package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// TestDaemonSwarmMergeResumePath proves the resume-path merge end to end: a
// torrent reloaded from its resume bitfield (kept, still incomplete) completes
// from a sibling's local data after a full daemon restart, without any peer.
// Before the merge ran on the resume path the torrent stayed empty forever
// (the reloaded bitfield makes it skip the fresh-verification branch).
func TestDaemonSwarmMergeResumePath(t *testing.T) {
	data := t.TempDir()
	dest := t.TempDir()
	content := make([]byte, 200_000)
	if _, err := rand.Read(content); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{PortBegin: 45000, PortEnd: 45100, Encryption: 1, PEX: true},
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

	dataA, hashA := makeTorrentContent(t, "a.bin", content)

	// First run: A is added with no sibling, so it only allocates empty
	// storage. Pausing it persists that (empty) bitfield to the resume data.
	d1, stop1 := boot()
	if _, _, err := d1.add(addRequest{TorrentData: dataA, Destination: dest, SeedRatio: -1, SeedDays: -1}); err != nil {
		t.Fatalf("add A: %v", err)
	}
	waitFor(t, "A to allocate", func() bool { return stateOf(d1, hashA) == "downloading" })
	if err := d1.pause(hashA); err != nil {
		t.Fatalf("pause A: %v", err)
	}

	// B: the same content already on disk under another name, so it seeds
	// from disk after a verification.
	if err := os.WriteFile(filepath.Join(dest, "b.bin"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	dataB, hashB := makeTorrentContent(t, "b.bin", content)
	if _, _, err := d1.add(addRequest{TorrentData: dataB, Destination: dest, SeedRatio: -1, SeedDays: -1}); err != nil {
		t.Fatalf("add B: %v", err)
	}
	waitFor(t, "B seeded", func() bool {
		info, ok := findInfo(d1, hashB)
		return ok && info.TotalSize > 0 && info.TotalDone == info.TotalSize
	})
	stop1()

	// Second run: A reloads from resume (its bitfield kept, still incomplete)
	// while B seeds; starting A runs the merge on the resume path.
	d2, stop2 := boot()
	defer stop2()
	waitFor(t, "B seeded after restart", func() bool {
		info, ok := findInfo(d2, hashB)
		return ok && info.State == "seeding" && info.TotalDone == info.TotalSize
	})
	if err := d2.resume(hashA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	waitFor(t, "A completed via the resume-path merge", func() bool {
		info, ok := findInfo(d2, hashA)
		return ok && info.TotalSize > 0 && info.TotalDone == info.TotalSize
	})

	got, err := os.ReadFile(filepath.Join(dest, "a.bin"))
	if err != nil {
		t.Fatalf("read a.bin: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("a.bin does not contain the reused content")
	}
}
