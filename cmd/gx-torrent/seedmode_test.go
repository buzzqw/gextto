package main

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedModeWasted reads the wasted bytes (blocks that failed their hash) of a
// torrent from the engine.
func seedModeWasted(t *testing.T, d *Daemon, hash string) int64 {
	t.Helper()
	d.mu.Lock()
	handle, _ := d.findLocked(hash)
	d.mu.Unlock()
	if handle == nil {
		t.Fatalf("torrent %s not found", hash)
	}
	return handle.Stats().Bytes.Wasted
}

// leechFrom adds the torrent to the leecher and points it at the seeder.
func leechFrom(t *testing.T, seeder, leecher *Daemon, data []byte, hash string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "dst")
	if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: dst}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "leecher running", func() bool { return stateOf(leecher, hash) == "downloading" })
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(hash)
	leecher.mu.Unlock()
	if err := handle.AddPeer(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)); err != nil {
		t.Fatal(err)
	}
	return dst
}

// TestSeedModeServesAfterLazyCheck adds a torrent over correct data without
// the initial check: it seeds at once, a leecher downloads it intact, and
// once every piece has been served (and so checked) seed mode ends by itself.
func TestSeedModeServesAfterLazyCheck(t *testing.T) {
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 47100, PortEnd: 47199, Encryption: 1})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 47200, PortEnd: 47299, Encryption: 1})
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "movie.bin", 300_000)
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src, SeedMode: true, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding in seed mode", func() bool {
		info, _ := findInfo(seeder, hash)
		return info.State == "seeding" && info.SeedMode
	})

	dst := leechFrom(t, seeder, leecher, data, hash)
	waitFor(t, "download from the seed-mode seeder", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.Progress == 100
	})
	want, _ := os.ReadFile(filepath.Join(src, "movie.bin"))
	got, err := os.ReadFile(filepath.Join(dst, "movie.bin"))
	if err != nil || !bytes.Equal(want, got) {
		t.Fatalf("payload differs after transfer: %v", err)
	}
	if w := seedModeWasted(t, leecher, hash); w != 0 {
		t.Fatalf("leecher wasted %d bytes: a piece arrived corrupt", w)
	}
	waitFor(t, "seed mode to end after every piece was checked", func() bool {
		info, _ := findInfo(seeder, hash)
		return !info.SeedMode && info.State == "seeding"
	})
}

// TestSeedModeCorruptPieceIsNeverSent adds a torrent in seed mode over data
// with one damaged piece. The seeder still claims everything, but the damaged
// piece never reaches the leecher (no wasted bytes there): its first hash
// fails, seed mode ends and the full check finds the piece missing. A restart
// afterwards does not bring the false claim back.
func TestSeedModeCorruptPieceIsNeverSent(t *testing.T) {
	data := t.TempDir()
	src := filepath.Join(t.TempDir(), "src")
	torrentData := makeTorrent(t, src, "movie.bin", 300_000)
	payload := filepath.Join(src, "movie.bin")
	raw, err := os.ReadFile(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw[5*16384+100] ^= 0xff // damage piece 5
	if err := os.WriteFile(payload, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{PortBegin: 47300, PortEnd: 47399, Encryption: 1},
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

	seeder, stopSeeder := boot()
	hash, _, err := seeder.add(addRequest{TorrentData: torrentData, Destination: src, SeedMode: true, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder claiming the damaged data in seed mode", func() bool {
		info, _ := findInfo(seeder, hash)
		return info.State == "seeding" && info.SeedMode && info.Progress == 100
	})

	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 47400, PortEnd: 47499, Encryption: 1})
	leechFrom(t, seeder, leecher, torrentData, hash)
	waitFor(t, "the damaged piece to end seed mode and the check to find it missing", func() bool {
		info, _ := findInfo(seeder, hash)
		return !info.SeedMode && info.Progress < 100 && info.State != "checking"
	})
	if w := seedModeWasted(t, leecher, hash); w != 0 {
		t.Fatalf("leecher wasted %d bytes: the damaged piece was sent", w)
	}
	stopSeeder()

	again, stopAgain := boot()
	defer stopAgain()
	waitFor(t, "restart to reload the torrent", func() bool {
		info, ok := findInfo(again, hash)
		return ok && info.State != "checking" && info.State != ""
	})
	time.Sleep(500 * time.Millisecond)
	if info, _ := findInfo(again, hash); info.SeedMode || info.Progress == 100 {
		t.Fatalf("after a restart the damaged torrent claims everything again: seed mode %v, progress %v", info.SeedMode, info.Progress)
	}
}

// TestSeedModeSurvivesRestart: a torrent still in seed mode is reloaded in
// seed mode (seeding at once, no full check), so the lazy check goes on.
func TestSeedModeSurvivesRestart(t *testing.T) {
	data := t.TempDir()
	src := filepath.Join(t.TempDir(), "src")
	torrentData := makeTorrent(t, src, "movie.bin", 200_000)
	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{PortBegin: 47500, PortEnd: 47599, Encryption: 1},
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
	hash, _, err := d1.add(addRequest{TorrentData: torrentData, Destination: src, SeedMode: true, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first run in seed mode", func() bool {
		info, _ := findInfo(d1, hash)
		return info.State == "seeding" && info.SeedMode
	})
	stop1()

	d2, stop2 := boot()
	defer stop2()
	waitFor(t, "reload still in seed mode", func() bool {
		info, _ := findInfo(d2, hash)
		return info.State == "seeding" && info.SeedMode && info.Progress == 100
	})
}

// TestQbitAddSkipCheckingIsSeedMode: qBittorrent's skip_checking on
// torrents/add adds the torrent in seed mode.
func TestQbitAddSkipCheckingIsSeedMode(t *testing.T) {
	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 47600, PortEnd: 47699, Encryption: 1})
	srv := httptest.NewServer(d.routesQbit())
	defer srv.Close()
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "movie.bin", 100_000)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("savepath", src)
	_ = mw.WriteField("skip_checking", "true")
	part, _ := mw.CreateFormFile("torrents", "movie.torrent")
	_, _ = part.Write(data)
	_ = mw.Close()
	resp, err := http.Post(srv.URL+"/api/v2/torrents/add", mw.FormDataContentType(), body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("torrents/add: status %d", resp.StatusCode)
	}
	waitFor(t, "torrent added with skip_checking seeding in seed mode", func() bool {
		for _, info := range d.list() {
			if info.SeedMode && info.State == "seeding" {
				return true
			}
		}
		return false
	})
}
