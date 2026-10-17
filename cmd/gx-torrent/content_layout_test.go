package main

import (
	"crypto/rand"
	"fmt"
	"crypto/sha1"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// layoutFile is one file of a multi-file test torrent.
type layoutFile struct {
	path string
	size int
}

// makeLayoutTorrent builds a multi-file .torrent named name and writes its data
// under dataDir by each file's torrent-relative path (without the root
// folder), so the caller decides where the content already is.
func makeLayoutTorrent(t *testing.T, dataDir, name string, files []layoutFile) []byte {
	t.Helper()
	const pieceLength = 16384
	var all []byte
	list := make([]any, 0, len(files))
	for _, f := range files {
		payload := make([]byte, f.size)
		if _, err := rand.Read(payload); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dataDir, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, payload, 0o644); err != nil {
			t.Fatal(err)
		}
		all = append(all, payload...)
		parts := make([]any, 0, 2)
		for _, p := range strings.Split(f.path, "/") {
			parts = append(parts, p)
		}
		list = append(list, map[string]any{"length": f.size, "path": parts})
	}
	var pieces []byte
	for off := 0; off < len(all); off += pieceLength {
		sum := sha1.Sum(all[off:min(off+pieceLength, len(all))])
		pieces = append(pieces, sum[:]...)
	}
	return bencode(map[string]any{"info": map[string]any{
		"name": name, "files": list, "piece length": pieceLength, "pieces": pieces,
	}})
}

// torrentInfoOf returns the listed view of one torrent.
func torrentInfoOf(d *Daemon, hash string) (torrentInfo, bool) {
	for _, info := range d.list() {
		if strings.EqualFold(info.Hash, hash) {
			return info, true
		}
	}
	return torrentInfo{}, false
}

// TestContentLayoutNoSubfolder adds a multi-file torrent whose files sit
// straight in the save path, next to a file that is not the torrent's: the
// engine finds the data there, content_path is the save path, a move takes only
// the torrent's files and a removal with data deletes only them.
func TestContentLayoutNoSubfolder(t *testing.T) {
	d := newTestDaemon(t)
	dest := t.TempDir()
	foreign := filepath.Join(dest, "keep.txt")
	if err := os.WriteFile(foreign, []byte("not the torrent's"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dest, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	foreignInSub := filepath.Join(dest, "sub", "mine.txt")
	if err := os.WriteFile(foreignInSub, []byte("also not the torrent's"), 0o644); err != nil {
		t.Fatal(err)
	}
	data := makeLayoutTorrent(t, dest, "Pack", []layoutFile{{"e1.bin", 20_000}, {"sub/e2.bin", 15_000}})

	hash, _, err := d.add(addRequest{TorrentData: data, Destination: dest, ContentLayout: "NoSubfolder"})
	if err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	waitFor(t, "the check to find the data in place", func() bool {
		info, ok := torrentInfoOf(d, hash)
		return ok && info.Progress >= 100
	})
	info, _ := torrentInfoOf(d, hash)
	if info.ContentPath != dest {
		t.Fatalf("content_path = %q, want the save path %q", info.ContentPath, dest)
	}
	if _, err := os.Stat(filepath.Join(dest, "Pack")); !os.IsNotExist(err) {
		t.Fatalf("a root folder was created: %v", err)
	}

	// The layout is persisted: after the session reopens the data is still
	// found in place.
	d.mu.Lock()
	d.restartSessionLocked()
	d.mu.Unlock()
	waitFor(t, "the reopened session to find the data", func() bool {
		info, ok := torrentInfoOf(d, hash)
		return ok && info.Progress >= 100 && info.ContentPath == dest
	})

	// Move: only the torrent's files go, the foreign ones stay.
	moved := t.TempDir()
	if err := d.move(hash, moved, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the move", func() bool {
		info, ok := torrentInfoOf(d, hash)
		return ok && info.SavePath == moved && info.State != "moving"
	})
	for _, rel := range []string{"e1.bin", "sub/e2.bin"} {
		if _, err := os.Stat(filepath.Join(moved, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s not moved: %v", rel, err)
		}
		if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("%s left behind: %v", rel, err)
		}
	}
	for _, keep := range []string{foreign, foreignInSub} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("a file that is not the torrent's was touched: %v", err)
		}
	}

	// Removal with data: only the torrent's files, and the folder they leave
	// empty; the save path itself stays.
	if err := os.WriteFile(filepath.Join(moved, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.remove(hash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(moved, "e1.bin")); !os.IsNotExist(err) {
		t.Fatalf("e1.bin not deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moved, "sub")); !os.IsNotExist(err) {
		t.Fatalf("the emptied folder was kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(moved, "other.txt")); err != nil {
		t.Fatalf("a file that is not the torrent's was deleted: %v", err)
	}
}

// TestContentLayoutSubfolder puts a single-file torrent in a folder named after
// it, and the compatible API's content_path points into that folder.
func TestContentLayoutSubfolder(t *testing.T) {
	d := newTestDaemon(t)
	dest := t.TempDir()
	data, hash := makeTorrentWithHash(t, filepath.Join(dest, "movie"), "movie.bin", 30_000)
	if _, _, err := d.add(addRequest{TorrentData: data, Destination: dest, ContentLayout: torrent.ContentLayoutSubfolder}); err != nil {
		t.Fatal(err)
	}
	waitListed(t, d, hash)
	waitFor(t, "the check to find the data in the subfolder", func() bool {
		info, ok := torrentInfoOf(d, hash)
		return ok && info.Progress >= 100
	})
	info, _ := torrentInfoOf(d, hash)
	if want := filepath.Join(dest, "movie"); info.ContentPath != want {
		t.Fatalf("content_path = %q, want %q", info.ContentPath, want)
	}
	if got := qbitContentPath(info); got != filepath.Join(dest, "movie") {
		t.Fatalf("qBittorrent content_path = %q", got)
	}
}

// TestContentLayoutRefusesUnknown keeps a typo from silently meaning Original.
func TestContentLayoutRefusesUnknown(t *testing.T) {
	d := newTestDaemon(t)
	dest := t.TempDir()
	data, _ := makeTorrentWithHash(t, dest, "x.bin", 1000)
	if _, _, err := d.add(addRequest{TorrentData: data, Destination: dest, ContentLayout: "flat"}); err == nil {
		t.Fatal("an unknown content layout must be refused")
	}
}

// BenchmarkPayloadContentPath measures the content_path that every list
// refresh computes per torrent, on a 2,000-file torrent: with a folder of its
// own (Original) and with loose files (NoSubfolder, which counts the files).
func BenchmarkPayloadContentPath(b *testing.B) {
	t := &testing.T{}
	d := newTestDaemonBench(b)
	files := make([]layoutFile, 2000)
	for i := range files {
		files[i] = layoutFile{path: fmt.Sprintf("d%d/f%d.bin", i%7, i), size: 64}
	}
	for _, layout := range []string{torrent.ContentLayoutOriginal, torrent.ContentLayoutNoSubfolder} {
		dest := b.TempDir()
		data := makeLayoutTorrent(t, dest, "Pack", files)
		hash, _, err := d.add(addRequest{TorrentData: data, Destination: dest, ContentLayout: layout, Paused: true})
		if err != nil {
			b.Fatal(err)
		}
		d.mu.Lock()
		tt, meta := d.findLocked(hash)
		d.mu.Unlock()
		b.Run(layout, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = payloadContentPath(tt, meta, "Pack")
			}
		})
	}
}
