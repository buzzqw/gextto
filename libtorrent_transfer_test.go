//go:build cgo

package gextto

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/utils"
)

// ---------------------------------------------------------------------------
// Minimal bencode encoder + torrent builder (no external dependency).
// ---------------------------------------------------------------------------

func bcString(value string) []byte {
	return append([]byte(strconv.Itoa(len(value))+":"), value...)
}

func bcBytes(value []byte) []byte {
	out := []byte(strconv.Itoa(len(value)) + ":")
	return append(out, value...)
}

func bcInt(value int64) []byte {
	return []byte("i" + strconv.FormatInt(value, 10) + "e")
}

func bcDict(pairs [][2]any) []byte {
	// bencode requires sorted keys.
	sorted := append([][2]any(nil), pairs...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i][0].(string) < sorted[j][0].(string)
	})
	out := []byte("d")
	for _, pair := range sorted {
		out = append(out, bcString(pair[0].(string))...)
		switch value := pair[1].(type) {
		case string:
			out = append(out, bcString(value)...)
		case []byte:
			out = append(out, bcBytes(value)...)
		case int:
			out = append(out, bcInt(int64(value))...)
		case int64:
			out = append(out, bcInt(value)...)
		case [][2]any:
			out = append(out, bcDict(value)...)
		default:
			panic(fmt.Sprintf("unsupported bencode type %T", value))
		}
	}
	return append(out, 'e')
}

// buildTorrentBytes produces a single-file .torrent for data with the given
// tracker announce URL, returning the bytes and the v1 infohash.
func buildTorrentBytes(t *testing.T, name string, data []byte, pieceLength int, announce string) ([]byte, string) {
	t.Helper()
	hasher := sha1.New()
	var pieces []byte
	for offset := 0; offset < len(data); offset += pieceLength {
		end := offset + pieceLength
		if end > len(data) {
			end = len(data)
		}
		hasher.Reset()
		hasher.Write(data[offset:end])
		pieces = append(pieces, hasher.Sum(nil)...)
	}
	info := [][2]any{
		{"length", len(data)},
		{"name", name},
		{"piece length", pieceLength},
		{"pieces", pieces},
	}
	torrent := bcDict([][2]any{
		{"announce", announce},
		{"info", info},
	})
	hash, ok := utils.TorrentInfoHash(torrent)
	if !ok {
		t.Fatal("could not compute infohash")
	}
	return torrent, hash
}

// ---------------------------------------------------------------------------
// Minimal HTTP tracker: records peers per info_hash and returns compact peers.
// ---------------------------------------------------------------------------

type trackerPeer struct {
	ip   string
	port uint16
}

type localTracker struct {
	mu    sync.Mutex
	peers map[string]map[string]trackerPeer
}

func newLocalTracker() *localTracker {
	return &localTracker{peers: map[string]map[string]trackerPeer{}}
}

func (t *localTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	infoHash := query.Get("info_hash")
	portValue, _ := strconv.Atoi(query.Get("port"))
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := ip + ":" + strconv.Itoa(portValue)

	t.mu.Lock()
	bucket := t.peers[infoHash]
	if bucket == nil {
		bucket = map[string]trackerPeer{}
		t.peers[infoHash] = bucket
	}
	if portValue > 0 && portValue <= 65535 {
		bucket[key] = trackerPeer{ip: ip, port: uint16(portValue)}
	}
	var compact []byte
	keys := make([]string, 0, len(bucket))
	for k := range bucket {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		peer := bucket[k]
		if k == key {
			continue
		}
		parsed := net.ParseIP(peer.ip)
		if parsed == nil {
			continue
		}
		ipv4 := parsed.To4()
		if ipv4 == nil {
			continue
		}
		compact = append(compact, ipv4...)
		compact = binary.BigEndian.AppendUint16(compact, peer.port)
	}
	t.mu.Unlock()

	response := bcDict([][2]any{
		{"interval", int64(2)},
		{"peers", compact},
	})
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write(response)
}

// ---------------------------------------------------------------------------
// Real transfer test
// ---------------------------------------------------------------------------

func freeTCPPort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer listener.Close()
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

func transferTestConfig(t *testing.T, dataDir string, port uint16) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.DataDir = dataDir
	cfg.StateDir = filepath.Join(dataDir, "state")
	cfg.LibtorrentDir = filepath.Join(dataDir, "downloads")
	temp := filepath.Join(dataDir, "temp")
	cfg.LibtorrentTempDir = &temp
	trash := filepath.Join(dataDir, "trash")
	cfg.TrashPath = &trash
	cfg.DryRun = false
	cfg.Active = false
	cfg.LibtorrentEnabled = true
	// This helper exercises the embedded libtorrent engine on purpose: keep the
	// backend explicit so the new gx-torrent install default does not suppress
	// the native session.
	cfg.Settings["torrent_backend"] = BackendEmbedded
	cfg.Libtorrent.PortMin = port
	cfg.Libtorrent.PortMax = port
	cfg.Libtorrent.Dht = false
	cfg.Libtorrent.Pex = false
	cfg.Libtorrent.Lsd = false
	cfg.Libtorrent.Upnp = false
	cfg.Libtorrent.Natpmp = false
	cfg.Libtorrent.DynamicQueue = false
	cfg.Libtorrent.SeedRatio = 0
	cfg.Libtorrent.ActiveDownloads = 5
	cfg.Libtorrent.ActiveSeeds = 5
	cfg.Libtorrent.ActiveLimit = 10
	return cfg
}

func findTorrent(client *LibtorrentClient, hash string) *TorrentViewAlias {
	for _, torrent := range client.List() {
		if strings.EqualFold(torrent.Hash, hash) {
			value := torrent
			return &TorrentViewAlias{Hash: value.Hash, Progress: value.Progress, State: value.State, TotalDone: value.TotalDone, TotalSize: value.TotalSize, NumPeers: value.NumPeers}
		}
	}
	return nil
}

// TorrentViewAlias is a tiny snapshot used by the transfer test diagnostics.
type TorrentViewAlias struct {
	Hash      string
	Progress  float64
	State     string
	TotalDone int64
	TotalSize int64
	NumPeers  int
}

// TestLibtorrentLocalTransfer performs a real BitTorrent transfer: a seeder
// session holds the complete file, a leecher session downloads it over the
// loopback using a local HTTP tracker, and the received bytes are compared with
// the original. It requires no external network.
func TestLibtorrentLocalTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real transfer test in short mode")
	}

	payload := make([]byte, 1<<20) // 1 MiB
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("random payload: %v", err)
	}

	tracker := newLocalTracker()
	server := httptest.NewServer(tracker)
	defer server.Close()

	torrentBytes, infoHash := buildTorrentBytes(t, "sample.bin", payload, 16384, server.URL+"/announce")
	torrentPath := filepath.Join(t.TempDir(), "sample.torrent")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}

	seedDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(seedDir, "sample.bin"), payload, 0o644); err != nil {
		t.Fatalf("write seed data: %v", err)
	}
	leechDir := t.TempDir()

	seedCfg := transferTestConfig(t, t.TempDir(), freeTCPPort(t))
	leechCfg := transferTestConfig(t, t.TempDir(), freeTCPPort(t))

	seeder, err := NewLibtorrentClient(&seedCfg)
	if err != nil {
		t.Fatalf("seeder session: %v", err)
	}
	defer seeder.Shutdown(&seedCfg)

	leecher, err := NewLibtorrentClient(&leechCfg)
	if err != nil {
		t.Fatalf("leecher session: %v", err)
	}
	defer leecher.Shutdown(&leechCfg)

	seedHash, err := seeder.AddTorrentFile(torrentPath, seedDir)
	if err != nil || seedHash == nil {
		t.Fatalf("add seed torrent: %v (hash=%v)", err, seedHash)
	}
	leechHash, err := leecher.AddTorrentFile(torrentPath, leechDir)
	if err != nil || leechHash == nil {
		t.Fatalf("add leech torrent: %v (hash=%v)", err, leechHash)
	}
	if !strings.EqualFold(*seedHash, infoHash) {
		t.Fatalf("seed infohash %s != %s", *seedHash, infoHash)
	}

	deadline := time.Now().Add(120 * time.Second)
	var last *TorrentViewAlias
	for time.Now().Before(deadline) {
		last = findTorrent(leecher, infoHash)
		if last != nil && last.TotalDone >= int64(len(payload)) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if last == nil {
		t.Fatal("leecher does not know the torrent")
	}
	if last.TotalDone < int64(len(payload)) {
		t.Fatalf("transfer did not complete: state=%s progress=%.1f%% peers=%d done=%d/%d",
			last.State, last.Progress, last.NumPeers, last.TotalDone, len(payload))
	}

	downloaded, err := os.ReadFile(filepath.Join(leechDir, "sample.bin"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(downloaded, payload) {
		t.Fatalf("downloaded %d bytes differ from the original %d", len(downloaded), len(payload))
	}
	if err := validateCompletedFile(filepath.Join(leechDir, "sample.bin")); err != nil {
		// .bin has no video extension, so validation is a no-op; the assertion
		// documents that non-video payloads are accepted untouched.
		t.Fatalf("validate downloaded file: %v", err)
	}
}

// TestLibtorrentMagnetTransfer exercises the acquisition path actually used by
// indexers: the leecher joins a magnet link (no .torrent file), fetches the
// metadata from the local seeder over the wire and downloads the payload.
func TestLibtorrentMagnetTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real transfer test in short mode")
	}

	payload := make([]byte, 768*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("random payload: %v", err)
	}

	tracker := newLocalTracker()
	server := httptest.NewServer(tracker)
	defer server.Close()

	torrentBytes, infoHash := buildTorrentBytes(t, "magnet.bin", payload, 16384, server.URL+"/announce")
	torrentPath := filepath.Join(t.TempDir(), "magnet.torrent")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}

	seedDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(seedDir, "magnet.bin"), payload, 0o644); err != nil {
		t.Fatalf("write seed data: %v", err)
	}
	leechDir := t.TempDir()

	seedCfg := transferTestConfig(t, t.TempDir(), freeTCPPort(t))
	leechCfg := transferTestConfig(t, t.TempDir(), freeTCPPort(t))

	seeder, err := NewLibtorrentClient(&seedCfg)
	if err != nil {
		t.Fatalf("seeder session: %v", err)
	}
	defer seeder.Shutdown(&seedCfg)
	leecher, err := NewLibtorrentClient(&leechCfg)
	if err != nil {
		t.Fatalf("leecher session: %v", err)
	}
	defer leecher.Shutdown(&leechCfg)

	if hash, err := seeder.AddTorrentFile(torrentPath, seedDir); err != nil || hash == nil {
		t.Fatalf("add seed torrent: %v", err)
	}
	// Wait for the seeder to finish checking so metadata is available.
	seedDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(seedDeadline) {
		if current := findTorrent(seeder, infoHash); current != nil && current.State == "seeding" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	magnet := "magnet:?xt=urn:btih:" + infoHash + "&dn=magnet.bin&tr=" + url.QueryEscape(server.URL+"/announce")
	if _, err := leecher.AddWithPath(magnet, &leechCfg, &leechDir); err != nil {
		t.Fatalf("add magnet: %v", err)
	}

	deadline := time.Now().Add(120 * time.Second)
	var last *TorrentViewAlias
	for time.Now().Before(deadline) {
		last = findTorrent(leecher, infoHash)
		if last != nil && last.TotalDone >= int64(len(payload)) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if last == nil {
		t.Fatal("magnet torrent was not added")
	}
	if last.TotalDone < int64(len(payload)) {
		t.Fatalf("magnet transfer did not complete: state=%s progress=%.1f%% peers=%d",
			last.State, last.Progress, last.NumPeers)
	}
	downloaded, err := os.ReadFile(filepath.Join(leechDir, "magnet.bin"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(downloaded, payload) {
		t.Fatal("magnet-downloaded bytes differ from the original")
	}
}

// TestLibtorrentFastresumeSurvivesRestart is a real reliability test: a leecher
// is given a valid partial file (the first quarter of the payload, the rest
// zero-filled), so libtorrent checks it and records a partial bitfield. The
// session is then shut down (saving fastresume) and a fresh session on the same
// data/state directories must restore that progress instead of starting over.
// The partial file makes the test deterministic, independent of transfer speed.
func TestLibtorrentFastresumeSurvivesRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real restart test in short mode")
	}

	const total = 4 << 20 // 4 MiB
	payload := make([]byte, total)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("random payload: %v", err)
	}

	tracker := newLocalTracker()
	server := httptest.NewServer(tracker)
	defer server.Close()

	torrentBytes, infoHash := buildTorrentBytes(t, "resume.bin", payload, 16384, server.URL+"/announce")
	torrentPath := filepath.Join(t.TempDir(), "resume.torrent")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}

	// The leecher and the restored session share the data and state directories.
	leechData := t.TempDir()
	leechDir := filepath.Join(leechData, "downloads")
	if err := os.MkdirAll(leechDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Valid first quarter, zeros for the rest: a genuine partial download.
	partialFile := make([]byte, total)
	copy(partialFile, payload[:total/4])
	if err := os.WriteFile(filepath.Join(leechDir, "resume.bin"), partialFile, 0o644); err != nil {
		t.Fatalf("write partial file: %v", err)
	}

	firstCfg := transferTestConfig(t, leechData, freeTCPPort(t))
	leecher, err := NewLibtorrentClient(&firstCfg)
	if err != nil {
		t.Fatalf("leecher session: %v", err)
	}
	if hash, err := leecher.AddTorrentFile(torrentPath, leechDir); err != nil || hash == nil {
		t.Fatalf("add leech torrent: %v", err)
	}

	observed := 0.0
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		current := findTorrent(leecher, infoHash)
		if current != nil && current.Progress >= 20 {
			observed = current.Progress
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if observed == 0 {
		leecher.Shutdown(&firstCfg)
		t.Fatalf("libtorrent did not recognise the partial file (progress stayed below 20%%)")
	}
	if err := leecher.Shutdown(&firstCfg); err != nil {
		t.Fatalf("shutdown leecher: %v", err)
	}
	time.Sleep(time.Second)

	secondCfg := transferTestConfig(t, leechData, freeTCPPort(t))
	restored, err := NewLibtorrentClient(&secondCfg)
	if err != nil {
		t.Fatalf("restored session: %v", err)
	}
	defer restored.Shutdown(&secondCfg)

	// Poll until libtorrent has finished the initial check of the existing data
	// and reports the recovered progress. Reading the first non-nil status is
	// racy: right after start the torrent can still be in `checking`, reporting
	// 0%, so a fast machine and a slow CI runner disagree.
	restoredProgress := 0.0
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		current := findTorrent(restored, infoHash)
		if current != nil {
			restoredProgress = current.Progress
			if restoredProgress >= observed*0.5 {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("partial=%.1f%% restored=%.1f%%", observed, restoredProgress)
	if restoredProgress < observed*0.5 {
		t.Fatalf("progress was not persisted: %.1f%% before restart, %.1f%% after", observed, restoredProgress)
	}
}

// TestLibtorrentPeriodicResumeSurvivesCrash reproduces the loss seen after a
// forced kill: a torrent added from a .torrent file (no metadata copy in the
// state directory) used to be persisted only by a clean shutdown. With the
// periodic save its resume file, info dictionary included, is written while the
// session runs, so a new session started without a clean shutdown restores it.
func TestLibtorrentPeriodicResumeSurvivesCrash(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real crash-restore test in short mode")
	}
	const total = 1 << 20
	payload := make([]byte, total)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("random payload: %v", err)
	}
	torrentBytes, infoHash := buildTorrentBytes(t, "crash.bin", payload, 16384, "http://127.0.0.1:1/announce")
	torrentPath := filepath.Join(t.TempDir(), "crash.torrent")
	if err := os.WriteFile(torrentPath, torrentBytes, 0o644); err != nil {
		t.Fatalf("write torrent: %v", err)
	}
	data := t.TempDir()
	downloads := filepath.Join(data, "downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	firstCfg := transferTestConfig(t, data, freeTCPPort(t))
	first, err := NewLibtorrentClient(&firstCfg)
	if err != nil {
		t.Fatalf("first session: %v", err)
	}
	// Shut it down only at the end, after the "crashed" state was used: the
	// restore below must not depend on it.
	throwaway := firstCfg
	throwaway.StateDir = t.TempDir()
	defer first.Shutdown(&throwaway)
	if hash, err := first.AddTorrentFile(torrentPath, downloads); err != nil || hash == nil {
		t.Fatalf("add torrent: %v", err)
	}
	resumeFile := filepath.Join(firstCfg.StateDir, infoHash+".fastresume")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		first.RequestResumeSave()
		first.PollEvents()
		if info, err := os.Stat(resumeFile); err == nil && info.Size() > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := os.Stat(resumeFile); err != nil {
		t.Fatalf("periodic save wrote no resume file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(firstCfg.StateDir, infoHash+".torrent")); err == nil {
		t.Log("a .torrent copy exists too; the test still checks the resume file alone")
	}

	secondCfg := transferTestConfig(t, data, freeTCPPort(t))
	second, err := NewLibtorrentClient(&secondCfg)
	if err != nil {
		t.Fatalf("second session: %v", err)
	}
	defer second.Shutdown(&throwaway)
	restored := findTorrent(second, infoHash)
	if restored == nil {
		t.Fatal("torrent lost after an unclean stop despite the periodic save")
	}
	if restored.TotalSize != total {
		t.Fatalf("restored torrent has no metadata: size %d, want %d", restored.TotalSize, total)
	}
}
