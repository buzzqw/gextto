package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

	// The queue starts it, rain verifies the existing payload and seeds.
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
	// the seeder would otherwise tell it "your IP is 127.0.0.1" and rain would
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
			t.Logf("rain: status=%v bytes=%+v pieces=%+v peers=%+v", st.Status, st.Bytes, st.Pieces, st.Peers)
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
	// On one machine the sender is this host's own address, which rain
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
