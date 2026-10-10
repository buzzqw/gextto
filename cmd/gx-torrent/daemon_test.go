package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// bencode encodes the few types a test torrent needs.
func bencode(value any) []byte {
	var buf bytes.Buffer
	var write func(any)
	write = func(v any) {
		switch x := v.(type) {
		case int:
			fmt.Fprintf(&buf, "i%de", x)
		case string:
			fmt.Fprintf(&buf, "%d:%s", len(x), x)
		case []byte:
			fmt.Fprintf(&buf, "%d:", len(x))
			buf.Write(x)
		case []any:
			buf.WriteByte('l')
			for _, item := range x {
				write(item)
			}
			buf.WriteByte('e')
		case map[string]any:
			keys := make([]string, 0, len(x))
			for key := range x {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			buf.WriteByte('d')
			for _, key := range keys {
				write(key)
				write(x[key])
			}
			buf.WriteByte('e')
		default:
			panic(fmt.Sprintf("unsupported %T", v))
		}
	}
	write(value)
	return buf.Bytes()
}

// makeTorrent writes a payload file in dir and returns its .torrent bytes.
func makeTorrent(t *testing.T, dir, name string, size int) []byte {
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
	return bencode(map[string]any{
		"info": map[string]any{
			"name":         name,
			"length":       size,
			"piece length": pieceLength,
			"pieces":       pieces,
		},
	})
}

func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	return newTestDaemonWith(t, NetworkOptions{PortBegin: 42000, PortEnd: 42100, Encryption: 1, PEX: true})
}

func newTestDaemonWith(t *testing.T, network NetworkOptions) *Daemon {
	t.Helper()
	data := t.TempDir()
	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     network,
		Debug:       os.Getenv("GX_TEST_DEBUG") == "1",
		Tick:        50 * time.Millisecond,
		ProbeWindow: time.Minute,
	}
	d, err := newDaemon(opts)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { d.run(stop); close(done) }()
	t.Cleanup(func() {
		close(stop)
		<-done
		d.close()
	})
	return d
}

func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func findInfo(d *Daemon, hash string) (torrentInfo, bool) {
	for _, info := range d.list() {
		if info.Hash == hash {
			return info, true
		}
	}
	return torrentInfo{}, false
}

func stateOf(d *Daemon, hash string) string {
	info, _ := findInfo(d, hash)
	return info.State
}

// TestDaemonRestartKeepsExistingPayload locks in the guarantee that a payload
// already on disk is never rewritten or truncated across a restart, even with
// the "preallocate" optimization enabled (which on filesystems without
// fallocate falls back to Truncate).
func TestDaemonRestartKeepsExistingPayload(t *testing.T) {
	data := t.TempDir()
	src := filepath.Join(t.TempDir(), "src")
	torrent := makeTorrent(t, src, "payload.bin", 200_000)
	payloadPath := filepath.Join(src, "payload.bin")
	before, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}

	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		Network:     NetworkOptions{PortBegin: 44000, PortEnd: 44100, Encryption: 1, PEX: true},
		Tick:        50 * time.Millisecond,
		ProbeWindow: time.Minute,
	}
	boot := func() (*Daemon, func()) {
		d, err := newDaemon(opts)
		if err != nil {
			t.Fatal(err)
		}
		d.mu.Lock()
		d.state.Config.Preallocate = true
		d.saveLocked()
		d.mu.Unlock()
		stop := make(chan struct{})
		done := make(chan struct{})
		go func() { d.run(stop); close(done) }()
		return d, func() { close(stop); <-done; d.close() }
	}

	d1, stop1 := boot()
	hash, _, err := d1.add(addRequest{TorrentData: torrent, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first run to seed the existing payload", func() bool {
		info, ok := findInfo(d1, hash)
		return ok && info.State == "seeding" && info.Progress == 100
	})
	stop1()

	d2, stop2 := boot()
	defer stop2()
	waitFor(t, "restart to reload the torrent", func() bool {
		_, ok := findInfo(d2, hash)
		return ok
	})
	after, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("the payload changed across a restart: %d bytes in, %d out", len(before), len(after))
	}
}

// TestSessionConfigDoesNotAutoResume locks in that the queue, not the engine, decides
// which torrents start. If the engine resumed torrents itself (ResumeOnStartup) it
// would start a parked/paused torrent before reconcileLocked can stop it, and
// starting a torrent creates its destination files: a parked torrent whose
// payload was moved or removed would get zero-filled placeholders written at
// the old path. The regression test below complements this by proving an
// existing payload is never rewritten across a restart.
func TestSessionConfigDoesNotAutoResume(t *testing.T) {
	d := newTestDaemon(t)
	if d.sessionConfig().ResumeOnStartup {
		t.Fatal("ResumeOnStartup must be off: the queue owns start/stop")
	}
}

func TestDaemonLifecycle(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)

	hash, existing, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil || existing {
		t.Fatalf("add: %v existing=%v", err, existing)
	}
	if _, again, err := d.add(addRequest{TorrentData: data, Destination: src}); err != nil || !again {
		t.Fatalf("a duplicate add must report the existing torrent: %v %v", err, again)
	}
	if len(d.list()) != 1 {
		t.Fatalf("the duplicate must not stay in the session: %+v", d.list())
	}

	// The queue starts it, the engine verifies the existing payload and seeds.
	waitFor(t, "seeding", func() bool {
		info, _ := findInfo(d, hash)
		return info.State == "seeding" && info.Progress == 100
	})
	info, _ := findInfo(d, hash)
	if info.SavePath != src || !info.HasMetadata || !info.AutoManaged || info.SeedRatio != -1 {
		t.Fatalf("unexpected view %+v", info)
	}

	// A user pause is never undone by the queue.
	if err := d.pause(hash); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "paused", func() bool { return stateOf(d, hash) == "paused" })
	time.Sleep(300 * time.Millisecond)
	if info, _ := findInfo(d, hash); info.State != "paused" || info.AutoManaged {
		t.Fatalf("the queue resumed a user-paused torrent: %+v", info)
	}
	if info, _ := findInfo(d, hash); info.Progress != 100 || info.TotalDone != info.TotalSize {
		t.Fatalf("a paused finished torrent must stay at 100%%: %+v", info)
	}
	// Same after the session reloads it stopped from its resume data.
	d.mu.Lock()
	d.restartSessionLocked()
	d.mu.Unlock()
	if info, _ := findInfo(d, hash); info.Progress != 100 || info.State != "paused" {
		t.Fatalf("a reloaded paused torrent must stay at 100%%: %+v", info)
	}
	if err := d.resume(hash); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding after resume", func() bool { return stateOf(d, hash) == "seeding" })

	// Move: the payload follows, the link is repointed, seeding resumes.
	dst := filepath.Join(t.TempDir(), "library")
	if err := d.move(hash, dst, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "move", func() bool {
		info, _ := findInfo(d, hash)
		return info.SavePath == dst && info.State == "seeding"
	})
	if _, err := os.Stat(filepath.Join(dst, "payload.bin")); err != nil {
		t.Fatalf("payload not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "payload.bin")); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}

	// Removal without deleteFiles keeps the payload.
	id := info.ID
	if err := d.remove(hash, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "payload.bin")); err != nil {
		t.Fatalf("a plain removal deleted the payload: %v", err)
	}
	if _, err := os.Lstat(d.linkPath(id)); !os.IsNotExist(err) {
		t.Fatalf("link left behind: %v", err)
	}

	// Removal with deleteFiles deletes only the torrent's own file.
	hash, _, err = d.add(addRequest{TorrentData: data, Destination: dst})
	if err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dst, "unrelated.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.remove(hash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "payload.bin")); !os.IsNotExist(err) {
		t.Fatalf("payload not deleted: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("an unrelated file was deleted: %v", err)
	}
}

func TestDaemonParkAndProbe(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 50_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	if err := d.park(hash); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if info, _ := findInfo(d, hash); info.State != "stalled" || !info.Parked {
		t.Fatalf("a parked torrent must stay stalled: %+v", info)
	}
	if err := d.restart(hash, 0); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "probe running", func() bool {
		info, _ := findInfo(d, hash)
		return info.Probing && !info.Parked && info.State == "seeding"
	})

	// State survives a daemon restart.
	if err := d.park(hash); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.restartSessionLocked()
	d.mu.Unlock()
	time.Sleep(300 * time.Millisecond)
	if info, _ := findInfo(d, hash); info.State != "stalled" {
		t.Fatalf("parked state lost across a session restart: %+v", info)
	}
	if err := d.unpark(hash); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "back in the queue", func() bool { return stateOf(d, hash) == "seeding" })
}

func TestDaemonRejectsRelativeDestinations(t *testing.T) {
	d := newTestDaemon(t)
	if _, _, err := d.add(addRequest{Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567", Destination: "relative/dir"}); err == nil {
		t.Fatal("relative destination accepted")
	}
	d.opts.AllowedRoots = []string{"/srv/media"}
	if _, err := d.validateDestination("/etc"); err == nil {
		t.Fatal("destination outside the allowed roots accepted")
	}
	if _, err := d.validateDestination("/srv/media/tv"); err != nil {
		t.Fatal(err)
	}
}

func TestAPIRequiresToken(t *testing.T) {
	d := newTestDaemon(t)
	d.opts.Token = "secret"
	server := httptest.NewServer(d.routes())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/torrents")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/torrents", nil)
	req.Header.Set("X-Gx-Token", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/config", strings.NewReader(`{"active_downloads":7,"speed_limit_download":500}`))
	req.Header.Set("X-Gx-Token", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cfg, _, _ := d.config()
	if resp.StatusCode != http.StatusOK || cfg.ActiveDownloads != 7 || cfg.SpeedLimitDownload != 500 {
		t.Fatalf("config not applied: %d %+v", resp.StatusCode, cfg)
	}
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/config", strings.NewReader(`{"bogus":1}`))
	req.Header.Set("X-Gx-Token", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown settings must be rejected, got %d", resp.StatusCode)
	}
}

// Two daemons on localhost: the leecher connects to the seeder's single
// shared port; the session routes the connection by info hash (plain and
// with forced MSE encryption) and the payload is transferred.
func TestSharedPortTransfer(t *testing.T) {
	for _, encryption := range []int{1, 2} {
		t.Run(fmt.Sprintf("encryption=%d", encryption), func(t *testing.T) {
			seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 42200, PortEnd: 42299, Encryption: encryption})
			leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 42300, PortEnd: 42399, Encryption: encryption})
			if seeder.peerPort == 0 || leecher.peerPort == 0 || seeder.peerPort == leecher.peerPort {
				t.Fatalf("unexpected ports %d %d", seeder.peerPort, leecher.peerPort)
			}
			src := filepath.Join(t.TempDir(), "src")
			data := makeTorrent(t, src, "movie.bin", 300_000)
			// A second torrent on the seeder proves the routing picks the
			// right one on the shared port.
			other := filepath.Join(t.TempDir(), "other")
			if _, _, err := seeder.add(addRequest{TorrentData: makeTorrent(t, other, "other.bin", 20_000), Destination: other}); err != nil {
				t.Fatal(err)
			}
			hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src})
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

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
			waitFor(t, "download through the shared port", func() bool {
				info, _ := findInfo(leecher, hash)
				return info.Progress == 100
			})
			want, _ := os.ReadFile(filepath.Join(src, "movie.bin"))
			got, err := os.ReadFile(filepath.Join(dst, "movie.bin"))
			if err != nil || !bytes.Equal(want, got) {
				t.Fatalf("payload differs after transfer: %v", err)
			}
		})
	}
}

// TestSharedPortTransferIPv6 is the IPv6 twin of TestSharedPortTransfer: both
// daemons listen on ::1 and the leecher dials the seeder's IPv6 address.
func TestSharedPortTransferIPv6(t *testing.T) {
	if l, err := net.Listen("tcp6", "[::1]:0"); err != nil {
		t.Skip("no IPv6 loopback")
	} else {
		l.Close()
	}
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 42600, PortEnd: 42699, Encryption: 1, ListenInterface: "::1"})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 42700, PortEnd: 42799, Encryption: 1, ListenInterface: "::1"})
	if seeder.peerPort == 0 || leecher.peerPort == 0 || seeder.peerPort == leecher.peerPort {
		t.Fatalf("unexpected ports %d %d", seeder.peerPort, leecher.peerPort)
	}
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "movie6.bin", 300_000)
	// A second torrent proves the routing picks the right one on the shared port.
	other := filepath.Join(t.TempDir(), "other")
	if _, _, err := seeder.add(addRequest{TorrentData: makeTorrent(t, other, "other6.bin", 20_000), Destination: other}); err != nil {
		t.Fatal(err)
	}
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

	dst := filepath.Join(t.TempDir(), "dst")
	if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: dst}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "leecher running", func() bool { return stateOf(leecher, hash) == "downloading" })
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(hash)
	leecher.mu.Unlock()
	if err := handle.AddPeer(fmt.Sprintf("[::1]:%d", seeder.peerPort)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "download through the IPv6 shared port", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.Progress == 100
	})
	if !sameFile(t, filepath.Join(src, "movie6.bin"), filepath.Join(dst, "movie6.bin")) {
		t.Fatal("payload differs after the IPv6 transfer")
	}
}

// makeMultiTorrent writes dir/name/<files> and returns the .torrent bytes.
func makeMultiTorrent(t *testing.T, dir, name string, sizes []int) []byte {
	t.Helper()
	const pieceLength = 16384
	var all []byte
	var files []any
	for i, size := range sizes {
		payload := make([]byte, size)
		if _, err := rand.Read(payload); err != nil {
			t.Fatal(err)
		}
		file := fmt.Sprintf("part%d.bin", i)
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name, file), payload, 0o644); err != nil {
			t.Fatal(err)
		}
		all = append(all, payload...)
		files = append(files, map[string]any{"length": size, "path": []any{file}})
	}
	var pieces []byte
	for offset := 0; offset < len(all); offset += pieceLength {
		sum := sha1.Sum(all[offset:min(offset+pieceLength, len(all))])
		pieces = append(pieces, sum[:]...)
	}
	return bencode(map[string]any{"info": map[string]any{
		"name": name, "files": files, "piece length": pieceLength, "pieces": pieces,
	}})
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// The leecher skips the middle file: it gets the other two, nothing of the
// skipped one lands in the save path, the torrent counts as complete. Then
// the file is selected again and downloaded too.
func TestFileSelection(t *testing.T) {
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 42400, PortEnd: 42499, Encryption: 1})
	// The leecher sends from 127.0.0.2 (outgoing interface): on one machine
	// the seeder would otherwise tell it "your IP is 127.0.0.1" and the engine would
	// then ignore 127.0.0.1 peers as itself.
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 42500, PortEnd: 42599, Encryption: 1, OutgoingInterface: "127.0.0.2"})
	src := filepath.Join(t.TempDir(), "src")
	data := makeMultiTorrent(t, src, "Season", []int{50_000, 70_000, 40_000})
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

	dst := filepath.Join(t.TempDir(), "dst")
	if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: dst, Paused: true}); err != nil {
		t.Fatal(err)
	}
	if err := leecher.setFilePriorities(hash, []int{4, 0, 4}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "selection applied", func() bool {
		leecher.mu.Lock()
		defer leecher.mu.Unlock()
		return len(leecher.moving) == 0
	})
	if err := leecher.resume(hash); err != nil {
		t.Fatal(err)
	}
	connect := func() {
		waitFor(t, "leecher running", func() bool {
			state := stateOf(leecher, hash)
			return state == "downloading" || state == "seeding"
		})
		leecher.mu.Lock()
		handle, _ := leecher.findLocked(hash)
		leecher.mu.Unlock()
		if err := handle.AddPeer(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)); err != nil {
			t.Fatal(err)
		}
	}
	connect()
	waitFor(t, "selected files downloaded", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.Progress == 100
	})
	info, _ := findInfo(leecher, hash)
	if info.TotalSize >= 160_000 || info.State != "seeding" {
		t.Fatalf("progress must refer to the selected files: %+v", info)
	}
	for _, i := range []int{0, 2} {
		name := fmt.Sprintf("part%d.bin", i)
		if !sameFile(t, filepath.Join(src, "Season", name), filepath.Join(dst, "Season", name)) {
			t.Fatalf("%s not downloaded correctly", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "Season", "part1.bin")); !os.IsNotExist(err) {
		t.Fatalf("the skipped file must not appear in the save path: %v", err)
	}

	// Select it again: it is downloaded and moved into the save path.
	if err := leecher.setFilePriorities(hash, []int{4, 4, 4}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "selection applied again", func() bool {
		leecher.mu.Lock()
		defer leecher.mu.Unlock()
		return len(leecher.moving) == 0
	})
	connect()
	defer func() {
		if t.Failed() {
			info, _ := findInfo(leecher, hash)
			t.Logf("leecher view: %+v", info)
			leecher.mu.Lock()
			handle, _ := leecher.findLocked(hash)
			leecher.mu.Unlock()
			st := handle.Stats()
			t.Logf("the engine: status=%v bytes=%+v pieces=%+v peers=%+v", st.Status, st.Bytes, st.Pieces, st.Peers)
		}
	}()
	waitFor(t, "whole torrent downloaded", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.Progress == 100 && info.TotalSize == 160_000
	})
	if !sameFile(t, filepath.Join(src, "Season", "part1.bin"), filepath.Join(dst, "Season", "part1.bin")) {
		t.Fatal("part1.bin not downloaded after re-selection")
	}
}

// RAM disk flow: the download starts on a tmpfs and is moved to the disk
// while it is still downloading (cross-filesystem copy); it must finish
// there with the right content and leave nothing on the RAM disk.
func TestRamdiskRelocationMidDownload(t *testing.T) {
	if info, err := os.Stat("/dev/shm"); err != nil || !info.IsDir() {
		t.Skip("no /dev/shm tmpfs")
	}
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 42600, PortEnd: 42699, Encryption: 1})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 42700, PortEnd: 42799, Encryption: 1, OutgoingInterface: "127.0.0.2"})
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "big.bin", 8<<20)
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })

	ramdisk, err := os.MkdirTemp("/dev/shm", "gxtest-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(ramdisk)
	if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: ramdisk}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "leecher running", func() bool { return stateOf(leecher, hash) == "downloading" })
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(hash)
	leecher.mu.Unlock()
	if err := handle.AddPeer(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "some data on the RAM disk", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.TotalDone > 0
	})
	disk := filepath.Join(t.TempDir(), "disk")
	before, _ := findInfo(leecher, hash)
	t.Logf("progress when the move starts: %.1f%%", before.Progress)
	if err := leecher.move(hash, disk, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "moved off the RAM disk", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.SavePath == disk && info.State != "moving"
	})
	// The move stopped the torrent: reconnect to the seeder.
	waitFor(t, "running again", func() bool {
		state := stateOf(leecher, hash)
		return state == "downloading" || state == "seeding"
	})
	leecher.mu.Lock()
	handle, _ = leecher.findLocked(hash)
	leecher.mu.Unlock()
	_ = handle.AddPeer(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort))
	waitFor(t, "download finished on disk", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.Progress == 100
	})
	if !sameFile(t, filepath.Join(src, "big.bin"), filepath.Join(disk, "big.bin")) {
		t.Fatal("content differs after the RAM disk relocation")
	}
	if _, err := os.Stat(filepath.Join(ramdisk, "big.bin")); !os.IsNotExist(err) {
		t.Fatalf("the RAM disk copy must be gone: %v", err)
	}
}

// testProxy is a minimal SOCKS5 (no auth) or HTTP CONNECT proxy that counts
// the tunnels it opens.
type testProxy struct {
	listener net.Listener
	tunnels  atomic.Int32
}

func startTestProxy(t *testing.T, kind string) *testProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &testProxy{listener: listener}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go p.serve(conn, kind)
		}
	}()
	return p
}

func (p *testProxy) serve(conn net.Conn, kind string) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	var target string
	if kind == "socks5" {
		header := make([]byte, 2)
		if _, err := io.ReadFull(reader, header); err != nil {
			return
		}
		methods := make([]byte, header[1])
		if _, err := io.ReadFull(reader, methods); err != nil {
			return
		}
		conn.Write([]byte{5, 0})
		request := make([]byte, 4)
		if _, err := io.ReadFull(reader, request); err != nil || request[3] != 1 {
			return
		}
		addr := make([]byte, 6)
		if _, err := io.ReadFull(reader, addr); err != nil {
			return
		}
		target = fmt.Sprintf("%d.%d.%d.%d:%d", addr[0], addr[1], addr[2], addr[3], int(addr[4])<<8|int(addr[5]))
		conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	} else {
		req, err := http.ReadRequest(reader)
		if err != nil || req.Method != http.MethodConnect {
			return
		}
		target = req.Host
		conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer upstream.Close()
	p.tunnels.Add(1)
	go io.Copy(upstream, reader)
	io.Copy(conn, upstream)
}

func TestProxyTransfer(t *testing.T) {
	for i, kind := range []string{"socks5", "http"} {
		t.Run(kind, func(t *testing.T) {
			proxy := startTestProxy(t, kind)
			base := 42800 + i*100
			seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: uint16(base), PortEnd: uint16(base + 49), Encryption: 1})
			leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: uint16(base + 50), PortEnd: uint16(base + 99), Encryption: 1,
				Proxy: kind + "://" + proxy.listener.Addr().String()})
			src := filepath.Join(t.TempDir(), "src")
			data := makeTorrent(t, src, "via-proxy.bin", 200_000)
			hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src})
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })
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
			waitFor(t, "download through the proxy", func() bool {
				info, _ := findInfo(leecher, hash)
				return info.Progress == 100
			})
			if proxy.tunnels.Load() == 0 {
				t.Fatal("the peer connection did not go through the proxy")
			}
			if !sameFile(t, filepath.Join(src, "via-proxy.bin"), filepath.Join(dst, "via-proxy.bin")) {
				t.Fatal("content differs")
			}
		})
	}
}

func TestIPFilterLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ipfilter.dat")
	rules := "Bad:1.2.3.0-1.2.3.255\n005.006.007.000 - 005.006.007.255 , 000 , emule\n10.0.0.0/8\n"
	if err := os.WriteFile(path, []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	d := newTestDaemonWith(t, NetworkOptions{PortBegin: 43000, PortEnd: 43099, Encryption: 1, IPFilter: path})
	if got := d.stats().IPFilterRules; got != 3 {
		t.Fatalf("expected 3 rules, got %d", got)
	}
	// Reloading keeps working after a session restart (new limits).
	d.mu.Lock()
	d.restartSessionLocked()
	d.mu.Unlock()
	if got := d.stats().IPFilterRules; got != 3 {
		t.Fatalf("rules lost after a session restart: %d", got)
	}
}

func TestIPFilterFromURL(t *testing.T) {
	rules := "Bad:1.2.3.0-1.2.3.255\n10.0.0.0/8\n"
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(rules)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	d := newTestDaemon(t)
	path, err := d.fetchIPFilterURL(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	n, err := d.loadIPFilter(path)
	if err != nil || n != 2 {
		t.Fatalf("load from url: n=%d err=%v", n, err)
	}
	stats := d.stats()
	if stats.IPFilterRules != 2 || stats.IPFilterPath == "" {
		t.Fatalf("stats = rules %d path %q", stats.IPFilterRules, stats.IPFilterPath)
	}
}

// uTP: the leecher dials over uTP only; the seeder accepts it on the shared
// UDP port, which also carries the DHT.
func TestUTPTransfer(t *testing.T) {
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 43100, PortEnd: 43199, Encryption: 1, UTP: true, DHT: true})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 43200, PortEnd: 43299, Encryption: 1, UTP: true, UTPOnly: true,
		OutgoingInterface: "127.0.0.2"})
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "over-utp.bin", 500_000)
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })
	dst := filepath.Join(t.TempDir(), "dst")
	if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: dst}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "leecher running", func() bool { return stateOf(leecher, hash) == "downloading" })
	before := leecher.session.Stats().OutgoingUTP
	leecher.mu.Lock()
	handle, _ := leecher.findLocked(hash)
	leecher.mu.Unlock()
	if err := handle.AddPeer(fmt.Sprintf("127.0.0.1:%d", seeder.peerPort)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "download over uTP", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.Progress == 100
	})
	if leecher.session.Stats().OutgoingUTP <= before {
		t.Fatal("the peer connection was not made over uTP")
	}
	if !sameFile(t, filepath.Join(src, "over-utp.bin"), filepath.Join(dst, "over-utp.bin")) {
		t.Fatal("content differs")
	}
	if !seeder.session.Stats().UTP || !seeder.session.Stats().DHT {
		t.Fatal("the seeder must run uTP and the DHT on the shared UDP port")
	}
	// A session restart (new speed limits) reopens the same UDP port.
	seeder.mu.Lock()
	seeder.restartSessionLocked()
	ok := seeder.session != nil && seeder.session.Stats().UTP
	seeder.mu.Unlock()
	if !ok {
		t.Fatal("uTP not available after a session restart")
	}
}

func TestParseLSD(t *testing.T) {
	msg := "BT-SEARCH * HTTP/1.1\r\nHost: 239.192.152.143:6771\r\nPort: 6881\r\nInfohash: 0123456789ABCDEF0123456789ABCDEF01234567\r\nInfohash: bad\r\ncookie: xyz\r\n\r\n\r\n"
	port, hashes, cookie, ok := parseLSD([]byte(msg))
	if !ok || port != 6881 || cookie != "xyz" || len(hashes) != 1 || hashes[0] != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("parse: %v %d %v %q", ok, port, hashes, cookie)
	}
	if _, _, _, ok := parseLSD([]byte("GET / HTTP/1.1\r\n\r\n")); ok {
		t.Fatal("not an LSD message")
	}
}

// The leecher finds the seeder only through LSD multicast announcements.
func TestLSDDiscovery(t *testing.T) {
	lsdTick = 200 * time.Millisecond
	// On one machine the sender is this host's own address, which the engine
	// ignores as itself: dial loopback instead.
	lsdPeerHost = func(net.IP) string { return "127.0.0.1" }
	defer func() {
		lsdTick = lsdTickDefault
		lsdPeerHost = func(ip net.IP) string { return ip.String() }
	}()
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 43300, PortEnd: 43399, Encryption: 1, LSD: true})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 43400, PortEnd: 43499, Encryption: 1, LSD: true})
	waitFor(t, "LSD started", func() bool {
		seeder.mu.Lock()
		defer seeder.mu.Unlock()
		return seeder.lsd != nil || seeder.lsdError != ""
	})
	if seeder.lsdError != "" {
		t.Skipf("multicast not available here: %s", seeder.lsdError)
	}
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "lan.bin", 200_000)
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })
	dst := filepath.Join(t.TempDir(), "dst")
	if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: dst}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "download from a peer found by LSD", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.Progress == 100
	})
	if leecher.stats().LSD.PeersFound == 0 {
		t.Fatal("no peer counted as found by LSD")
	}
}

// TestDaemonSequentialOption checks the per-add "sequential" option and the
// daemon-wide default that gextto sets through the config endpoint.
func TestDaemonSequentialOption(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 50_000)
	hash, existing, err := d.add(addRequest{TorrentData: data, Destination: src, Sequential: true, SeedRatio: -1, SeedDays: -1})
	if err != nil || existing {
		t.Fatalf("add: %v existing=%v", err, existing)
	}
	if info, _ := findInfo(d, hash); !info.Sequential {
		t.Fatalf("sequential not reported: %+v", info)
	}

	// The global default applies to torrents added afterwards.
	if _, err := d.setConfig(map[string]json.RawMessage{"sequential": json.RawMessage(`true`)}); err != nil {
		t.Fatal(err)
	}
	src2 := filepath.Join(t.TempDir(), "src2")
	data2 := makeTorrent(t, src2, "other.bin", 50_000)
	hash2, _, err := d.add(addRequest{TorrentData: data2, Destination: src2, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := findInfo(d, hash2); !info.Sequential {
		t.Fatalf("global sequential default not applied: %+v", info)
	}
}

// TestDaemonFirstLastOption checks the per-add "first_last" option, which
// prioritises the ends of every file without forcing sequential order.
func TestDaemonFirstLastOption(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 50_000)
	hash, existing, err := d.add(addRequest{TorrentData: data, Destination: src, FirstLast: true, SeedRatio: -1, SeedDays: -1})
	if err != nil || existing {
		t.Fatalf("add: %v existing=%v", err, existing)
	}
	if info, _ := findInfo(d, hash); !info.FirstLast {
		t.Fatalf("first_last not reported: %+v", info)
	}
}

func TestSpeedLimitsChangeWithoutSessionReopen(t *testing.T) {
	d := newTestDaemon(t)
	session := d.session
	if _, err := d.setConfig(map[string]json.RawMessage{
		"speed_limit_download": json.RawMessage(`500`),
		"speed_limit_upload":   json.RawMessage(`100`),
	}); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	pending := d.restartPending
	d.mu.Unlock()
	if pending {
		t.Fatal("a speed limit change must not reopen the session")
	}
	d.tick(time.Now())
	if d.session != session {
		t.Fatal("the session was replaced")
	}

	if _, err := d.setConfig(map[string]json.RawMessage{"max_peer_dial": json.RawMessage(`10`)}); err != nil {
		t.Fatal(err)
	}
	// The reopen is applied by the queue loop: wait for it instead of racing
	// the loop on restartPending.
	waitFor(t, "session reopened for the peer limits", func() bool {
		d.mu.Lock()
		changed := d.session != session
		d.mu.Unlock()
		return changed
	})
}

func TestMoveDropsTheEmptyFilesOfADownloadWithNothingYet(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "empty.bin", 4<<20)
	staging := filepath.Join(t.TempDir(), "staging")
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: staging})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "downloading without peers", func() bool { return stateOf(d, hash) == "downloading" })
	staged := filepath.Join(staging, "empty.bin")
	waitFor(t, "file allocated", func() bool { _, err := os.Stat(staged); return err == nil })
	// Unverified bytes in the staged file: they must not travel with the move.
	marker := []byte("not a verified piece")
	file, err := os.OpenFile(staged, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteAt(marker, 0)
	_ = file.Close()

	disk := filepath.Join(t.TempDir(), "disk")
	if err := d.move(hash, disk, false); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "moved", func() bool {
		info, _ := findInfo(d, hash)
		return info.SavePath == disk && info.State != "moving"
	})
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("the staged empty file must be gone: %v", err)
	}
	waitFor(t, "recreated at the destination", func() bool { _, err := os.Stat(filepath.Join(disk, "empty.bin")); return err == nil })
	head := make([]byte, len(marker))
	if file, err := os.Open(filepath.Join(disk, "empty.bin")); err == nil {
		_, _ = file.ReadAt(head, 0)
		_ = file.Close()
	}
	if string(head) == string(marker) {
		t.Fatal("the empty files were copied instead of recreated")
	}
	if state := stateOf(d, hash); state == "checking_files" {
		t.Fatalf("no re-check expected for a torrent with nothing downloaded, state %s", state)
	}
}

// TestDaemonSetTrackersReplacesTheList locks in that set-trackers replaces the
// list (including clearing it) instead of only adding, so the Gextto editor can
// remove trackers.
func TestDaemonSetTrackersReplacesTheList(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	trackerURLs := func() []string {
		d.mu.Lock()
		tt, _ := d.findLocked(hash)
		d.mu.Unlock()
		if tt == nil {
			return nil
		}
		var out []string
		for _, tr := range tt.Trackers() {
			out = append(out, tr.URL)
		}
		return out
	}

	if err := d.setTrackers(hash, []string{"http://127.0.0.1:1/announce", "udp://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two trackers", func() bool { return len(trackerURLs()) == 2 })

	// Replacement, not addition: the previous ones are gone.
	if err := d.setTrackers(hash, []string{"http://127.0.0.1:2/announce"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "replaced", func() bool {
		got := trackerURLs()
		return len(got) == 1 && got[0] == "http://127.0.0.1:2/announce"
	})

	// An empty list removes every tracker.
	if err := d.setTrackers(hash, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "cleared", func() bool { return len(trackerURLs()) == 0 })
}

// TestDaemonAddAndRemoveWebseeds checks the runtime web seed list: duplicates
// are ignored, removal matches by URL, and an empty removal changes nothing.
func TestDaemonAddAndRemoveWebseeds(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	webseeds := func() []string {
		d.mu.Lock()
		tt, _ := d.findLocked(hash)
		d.mu.Unlock()
		if tt == nil {
			return nil
		}
		var out []string
		for _, w := range tt.Webseeds() {
			out = append(out, w.URL)
		}
		return out
	}

	if err := d.addWebseeds(hash, []string{"http://127.0.0.1:9/a", "http://127.0.0.1:9/a"}); err != nil {
		t.Fatal(err)
	}
	if err := d.addWebseeds(hash, []string{"http://127.0.0.1:9/b"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two web seeds", func() bool { return len(webseeds()) == 2 })

	if err := d.removeWebseeds(hash, []string{"http://127.0.0.1:9/a"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "one web seed left", func() bool {
		got := webseeds()
		return len(got) == 1 && got[0] == "http://127.0.0.1:9/b"
	})

	// An empty removal must not touch the list (unlike an empty set-trackers).
	if err := d.removeWebseeds(hash, nil); err != nil {
		t.Fatal(err)
	}
	if got := webseeds(); len(got) != 1 {
		t.Fatalf("an empty removal changed the list: %v", got)
	}
}

// TestDaemonSequentialToggleReachesRunningTorrents checks that the session-wide
// sequential flag is pushed onto the torrents already running, like libtorrent,
// not only stored as a default for the next ones.
func TestDaemonSequentialToggleReachesRunningTorrents(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	sequential := func() bool {
		d.mu.Lock()
		tt, _ := d.findLocked(hash)
		d.mu.Unlock()
		return tt != nil && tt.Sequential()
	}
	if sequential() {
		t.Fatal("sequential must start off")
	}
	if _, err := d.setConfig(map[string]json.RawMessage{"sequential": json.RawMessage("true")}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sequential on", sequential)
	if _, err := d.setConfig(map[string]json.RawMessage{"sequential": json.RawMessage("false")}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "sequential off", func() bool { return !sequential() })
}

func TestCompressPieceRuns(t *testing.T) {
	if runs := compressPieceRuns(nil); len(runs) != 0 {
		t.Fatalf("empty states -> %v", runs)
	}
	got := compressPieceRuns([]string{"have", "have", "downloading", "", "", "skipped"})
	want := []pieceRun{{0, 1, "have"}, {2, 2, "downloading"}, {3, 4, ""}, {5, 5, "skipped"}}
	if len(got) != len(want) {
		t.Fatalf("runs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("run[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestDaemonPieceDiagnostics checks the pieces endpoint: a seeding torrent
// reports every piece as "have", and the runs cover the piece count exactly.
func TestDaemonPieceDiagnostics(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/torrents/" + hash + "/pieces")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pieces -> %d", resp.StatusCode)
	}
	var payload struct {
		PieceCount int        `json:"piece_count"`
		Runs       []pieceRun `json:"runs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.PieceCount != 7 {
		t.Fatalf("piece_count = %d, want 7", payload.PieceCount)
	}
	covered := 0
	for _, run := range payload.Runs {
		if run.State != "have" {
			t.Fatalf("a seeding torrent reported state %q", run.State)
		}
		covered += run.End - run.Begin + 1
	}
	if covered != payload.PieceCount {
		t.Fatalf("runs cover %d pieces, want %d", covered, payload.PieceCount)
	}

	// An unknown hash is a 404, not fabricated data.
	resp2, err := http.Get(srv.URL + "/api/v1/torrents/" + strings.Repeat("0", 40) + "/pieces")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown hash -> %d, want 404", resp2.StatusCode)
	}
}

// TestDaemonUIPiecesTab checks that the daemon's own web UI can render the
// piece map for a seeding torrent.
func TestDaemonUIPiecesTab(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	detail, err := d.uiDetailData(hash, "pieces")
	if err != nil {
		t.Fatal(err)
	}
	if detail.PiecesTotal != 7 || detail.PiecesHave != 7 {
		t.Fatalf("pieces = %d/%d, want 7/7", detail.PiecesHave, detail.PiecesTotal)
	}
	covered := 0
	for _, run := range detail.Pieces {
		covered += run.End - run.Begin + 1
	}
	if covered != 7 || len(detail.Pieces) == 0 {
		t.Fatalf("piece runs = %+v (covered %d)", detail.Pieces, covered)
	}
}

// TestDaemonPerTorrentSpeedLimits checks the per-torrent speed limits: the
// action sets them, the view reports them, and a session reload reapplies them.
func TestDaemonPerTorrentSpeedLimits(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	speedLimits := func() (int64, int64) {
		d.mu.Lock()
		tt, _ := d.findLocked(hash)
		d.mu.Unlock()
		if tt == nil {
			return -2, -2
		}
		return tt.SpeedLimits()
	}
	if dl, ul := speedLimits(); dl != -1 || ul != -1 {
		t.Fatalf("default limits = %d/%d, want -1/-1", dl, ul)
	}

	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/torrents/"+hash+"/seed-limits",
		"application/x-www-form-urlencoded",
		strings.NewReader("download_limit=512&upload_limit=0&seed_ratio=-1&seed_days=-1"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	waitFor(t, "limits applied", func() bool { dl, ul := speedLimits(); return dl == 512 && ul == 0 })

	info, _ := findInfo(d, hash)
	if info.DownloadLimitKib != 512 || info.UploadLimitKib != 0 {
		t.Fatalf("view limits = %d/%d, want 512/0", info.DownloadLimitKib, info.UploadLimitKib)
	}

	// The engine does not persist them: a session reload must reapply the stored ones.
	d.mu.Lock()
	d.restartSessionLocked()
	d.mu.Unlock()
	waitFor(t, "limits after reload", func() bool { dl, ul := speedLimits(); return dl == 512 && ul == 0 })
}

// TestDaemonPerTorrentConnLimits checks the per-torrent connection and upload
// caps: the action sets them and a session reload reapplies them.
func TestDaemonPerTorrentConnLimits(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	connLimits := func() (int, int) {
		d.mu.Lock()
		tt, _ := d.findLocked(hash)
		d.mu.Unlock()
		if tt == nil {
			return -1, -1
		}
		return tt.MaxConnections(), tt.MaxUploads()
	}
	if c, u := connLimits(); c != 0 || u != 0 {
		t.Fatalf("default caps = %d/%d, want 0/0", c, u)
	}

	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/v1/torrents/"+hash+"/conn-limits",
		"application/x-www-form-urlencoded", strings.NewReader("max_connections=25&max_uploads=4"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	waitFor(t, "caps applied", func() bool { c, u := connLimits(); return c == 25 && u == 4 })

	d.mu.Lock()
	d.restartSessionLocked()
	d.mu.Unlock()
	waitFor(t, "caps after reload", func() bool { c, u := connLimits(); return c == 25 && u == 4 })
}

// TestDaemonStreamServesARange checks the streaming endpoint: full download,
// HTTP Range (206 with Content-Range) and the unsatisfiable case.
func TestDaemonStreamServesARange(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ui/stream?hash=" + hash + "&file=0")
	if err != nil {
		t.Fatal(err)
	}
	full, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(full) != 100_000 {
		t.Fatalf("full stream -> %d, %d bytes", resp.StatusCode, len(full))
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Fatal("Accept-Ranges missing")
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/ui/stream?hash="+hash+"&file=0", nil)
	req.Header.Set("Range", "bytes=10-19")
	partial, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	chunk, _ := io.ReadAll(partial.Body)
	partial.Body.Close()
	if partial.StatusCode != http.StatusPartialContent || string(chunk) != string(full[10:20]) {
		t.Fatalf("range -> %d %q, want bytes 10-19", partial.StatusCode, chunk)
	}
	if cr := partial.Header.Get("Content-Range"); cr != "bytes 10-19/100000" {
		t.Fatalf("Content-Range = %q", cr)
	}

	req2, _ := http.NewRequest(http.MethodGet, srv.URL+"/ui/stream?hash="+hash+"&file=0", nil)
	req2.Header.Set("Range", "bytes=999999-")
	bad, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("bad range -> %d, want 416", bad.StatusCode)
	}
}

// TestDaemonUIWebSeedsAndTrackerRemoval checks the daemon page's tracker removal
// and web seed add/remove, so those features are reachable from its UI too.
func TestDaemonUIWebSeedsAndTrackerRemoval(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	post := func(path, body string) {
		t.Helper()
		resp, err := http.Post(srv.URL+path, "application/x-www-form-urlencoded", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	trackers := func() []string {
		d.mu.Lock()
		tt, _ := d.findLocked(hash)
		d.mu.Unlock()
		var out []string
		if tt != nil {
			for _, tr := range tt.Trackers() {
				out = append(out, tr.URL)
			}
		}
		return out
	}
	trackerURL := "http://127.0.0.1:9/announce"
	post("/ui/trackers", "hash="+hash+"&urls="+url.QueryEscape(trackerURL))
	waitFor(t, "tracker added", func() bool { return len(trackers()) == 1 })
	post("/ui/trackers", "hash="+hash+"&op=remove&url="+url.QueryEscape(trackerURL))
	waitFor(t, "tracker removed", func() bool { return len(trackers()) == 0 })

	webseeds := func() []string {
		d.mu.Lock()
		tt, _ := d.findLocked(hash)
		d.mu.Unlock()
		var out []string
		if tt != nil {
			for _, ws := range tt.Webseeds() {
				out = append(out, ws.URL)
			}
		}
		return out
	}
	seedURL := "http://127.0.0.1:9/seed"
	post("/ui/webseeds", "hash="+hash+"&urls="+url.QueryEscape(seedURL))
	waitFor(t, "webseed added", func() bool { return len(webseeds()) == 1 })
	post("/ui/webseeds", "hash="+hash+"&remove=1&urls="+url.QueryEscape(seedURL))
	waitFor(t, "webseed removed", func() bool { return len(webseeds()) == 0 })
}

// TestDaemonSuperSeedingOptionAndAction checks the per-add super-seeding option
// and the runtime toggle through the API and the daemon's own web page.
func TestDaemonSuperSeedingOptionAndAction(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 50_000)
	hash, existing, err := d.add(addRequest{TorrentData: data, Destination: src, SuperSeeding: true, SeedRatio: -1, SeedDays: -1})
	if err != nil || existing {
		t.Fatalf("add: %v existing=%v", err, existing)
	}
	if info, _ := findInfo(d, hash); !info.SuperSeeding {
		t.Fatalf("super-seeding not reported at add: %+v", info)
	}

	// Runtime toggle through the internal action.
	if err := d.setSuperSeeding(hash, false); err != nil {
		t.Fatal(err)
	}
	if info, _ := findInfo(d, hash); info.SuperSeeding {
		t.Fatal("super-seeding still reported after disabling")
	}

	// And through the daemon's own web page.
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/ui/super-seeding", "application/x-www-form-urlencoded", strings.NewReader("hash="+hash+"&enabled=1"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if info, _ := findInfo(d, hash); !info.SuperSeeding {
		t.Fatal("super-seeding not enabled from the web page")
	}
}

// TestSuperSeedingTransfer checks that a leecher fully downloads from a seed in
// super-seeding mode: the seed advertises a couple of pieces at a time and only
// serves those, so completing proves the advertised pieces cycle correctly.
func TestSuperSeedingTransfer(t *testing.T) {
	lsdTick = 200 * time.Millisecond
	// On one machine the sender is this host's own address, which the engine ignores
	// as itself: dial loopback instead.
	lsdPeerHost = func(net.IP) string { return "127.0.0.1" }
	defer func() {
		lsdTick = lsdTickDefault
		lsdPeerHost = func(ip net.IP) string { return ip.String() }
	}()
	seeder := newTestDaemonWith(t, NetworkOptions{PortBegin: 43500, PortEnd: 43599, Encryption: 1, LSD: true})
	leecher := newTestDaemonWith(t, NetworkOptions{PortBegin: 43600, PortEnd: 43699, Encryption: 1, LSD: true})
	waitFor(t, "LSD started", func() bool {
		seeder.mu.Lock()
		defer seeder.mu.Unlock()
		return seeder.lsd != nil || seeder.lsdError != ""
	})
	if seeder.lsdError != "" {
		t.Skipf("multicast not available here: %s", seeder.lsdError)
	}
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "seed.bin", 300_000)
	hash, _, err := seeder.add(addRequest{TorrentData: data, Destination: src, SuperSeeding: true, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeder seeding", func() bool { return stateOf(seeder, hash) == "seeding" })
	dst := filepath.Join(t.TempDir(), "dst")
	if _, _, err := leecher.add(addRequest{TorrentData: data, Destination: dst}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "download from a super-seeding peer", func() bool {
		info, _ := findInfo(leecher, hash)
		return info.Progress == 100
	})
}

// TestListEndpointAnswersWhileTheLockIsHeld locks in the guarantee that keeps
// Gextto from declaring the daemon unreachable: the REST list is served from
// the published snapshot, without d.mu nor any torrent run loop. A tick stuck
// on a torrent's storage I/O (a long move to a slow network mount) therefore
// cannot turn a slow response into "daemon down".
func TestListEndpointAnswersWhileTheLockIsHeld(t *testing.T) {
	d := newTestDaemon(t)
	// Seed the snapshot as the first tick would.
	d.list()

	server := httptest.NewServer(d.routes())
	defer server.Close()

	// Hold d.mu exactly as a tick blocked on a torrent run loop would.
	d.mu.Lock()
	defer d.mu.Unlock()

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(server.URL + "/api/v1/torrents")
	if err != nil {
		t.Fatalf("the list endpoint must answer without d.mu: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var views []torrentInfo
	if err := json.Unmarshal(body, &views); err != nil {
		t.Fatalf("invalid list body %q: %v", body, err)
	}
}

// TestVerifyRefusesAFinishedTorrentWhosePayloadIsGone covers a completed
// torrent left paused after Gextto archived and renamed its file: a recheck or
// a resume must not make the engine re-create an empty file in the library.
func TestVerifyRefusesAFinishedTorrentWhosePayloadIsGone(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "library")
	data := makeTorrent(t, src, "episode.mkv", 200_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the existing payload to seed", func() bool {
		info, ok := findInfo(d, hash)
		return ok && info.State == "seeding"
	})
	if err := d.pause(hash); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pause", func() bool { return stateOf(d, hash) == "paused" })
	payload := filepath.Join(src, "episode.mkv")
	if err := os.Rename(payload, filepath.Join(src, "Episode - S01E01.mkv")); err != nil {
		t.Fatal(err)
	}

	if err := d.verify(hash); err == nil {
		t.Fatal("verify must be refused when the downloaded data is gone")
	}
	if err := d.resume(hash); err == nil {
		t.Fatal("resume must be refused when the downloaded data is gone")
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Lstat(payload); !os.IsNotExist(err) {
		t.Fatalf("an empty placeholder was re-created at %s (err %v)", payload, err)
	}
}

// TestVerifyStopsOnceWhenFilesCannotBeAllocated locks in the engine fork fix: a
// verification whose file allocation fails stops with the error instead of
// restarting itself in a tight loop (thousands of log lines a second).
func TestVerifyStopsOnceWhenFilesCannotBeAllocated(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "library")
	data := makeTorrent(t, src, "episode.mkv", 200_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the existing payload to seed", func() bool { return stateOf(d, hash) == "seeding" })
	if err := d.pause(hash); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pause", func() bool { return stateOf(d, hash) == "paused" })
	if err := os.Remove(filepath.Join(src, "episode.mkv")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(src, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(src, 0o755) })

	d.mu.Lock()
	tor, _ := d.findLocked(hash)
	d.mu.Unlock()
	if err := tor.Verify(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the allocation error", func() bool {
		stats := tor.Stats()
		return stats.Status == torrent.Stopped && stats.Error != nil
	})
	for range 10 {
		time.Sleep(30 * time.Millisecond)
		if status := tor.Stats().Status; status != torrent.Stopped {
			t.Fatalf("the verification restarted after the error (status %v)", status)
		}
	}
}
