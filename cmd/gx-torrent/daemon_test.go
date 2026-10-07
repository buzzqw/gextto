package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	data := t.TempDir()
	opts := Options{
		Listen:      "127.0.0.1:0",
		DataDir:     data,
		LinkDir:     filepath.Join(data, "links"),
		DownloadDir: filepath.Join(data, "downloads"),
		DBPath:      filepath.Join(data, "session.db"),
		StatePath:   filepath.Join(data, "state.json"),
		PortBegin:   42000,
		PortEnd:     42100,
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
