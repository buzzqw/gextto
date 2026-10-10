package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/auth"
	"github.com/buzzqw/gextto/internal/gxcore/torrent"
	"github.com/buzzqw/gextto/internal/queue"
	"github.com/buzzqw/gextto/internal/settings"
)

// diskFree returns the free (available to the user) and total bytes of the
// filesystem holding path. Zeroes when the path cannot be queried. The platform
// detail lives in fsinfo_*.go.
func diskFree(path string) (free, total int64) {
	return diskFreeBytes(path)
}

// Options is the daemon's process configuration (flags and environment).
type Options struct {
	Listen       string
	DataDir      string
	LinkDir      string
	PartsDir     string
	DownloadDir  string
	DBPath       string
	StatePath    string
	Token        string
	AllowedRoots []string
	Network      NetworkOptions
	Debug        bool
	// Mode is how the daemon is run: managed by Gextto or standalone. Gextto's
	// flags win in managed; settings.json is authoritative in standalone.
	Mode Mode
	// Settings is the standalone settings store (nil in managed, where the
	// flags are the only source of truth).
	Settings *settings.Store
	// Fingerprint is reported by /api/v1/health so Gextto can adopt a daemon
	// it started earlier with the same binary and options.
	Fingerprint string
	// GexttoLog is the Gextto log shown in the web page (empty = no tab).
	GexttoLog string
	// IPFilterSource is the IP filter URL or path configured in Gextto.
	IPFilterSource string
	// Lang is the web page language (it, en, de, fr, es, pl; empty or unknown
	// means English). Gextto passes its interface language when it starts the
	// daemon; a ?lang= query overrides it per request.
	Lang string
	// NotifyURL is a webhook posted (JSON) on feed match/error. Empty disables
	// notifications.
	NotifyURL   string
	Tick        time.Duration
	ProbeWindow time.Duration
}

// torrentMeta is what gx-torrent knows about a torrent beyond the engine.
type torrentMeta struct {
	ID          string    `json:"id"`
	Hash        string    `json:"hash"`
	SavePath    string    `json:"save_path"`
	AddedAt     time.Time `json:"added_at"`
	CompletedAt time.Time `json:"completed_at,omitzero"`
	Pos         int64     `json:"pos"`
	UserPaused  bool      `json:"user_paused,omitempty"`
	Parked      bool      `json:"parked,omitempty"`
	Pinned      bool      `json:"pinned,omitempty"`
	ProbeUntil  time.Time `json:"probe_until,omitzero"`
	// StopAtMetadata pauses a magnet as soon as its metadata arrives.
	StopAtMetadata bool `json:"stop_at_metadata,omitempty"`
	// Sequential downloads pieces in order (streaming) instead of rarest-first.
	Sequential bool `json:"sequential,omitempty"`
	FirstLast  bool `json:"first_last,omitempty"`
	// SuperSeeding is BEP 16 super-seeding, a seeding strategy.
	SuperSeeding bool      `json:"super_seeding,omitempty"`
	RotatedAt    time.Time `json:"rotated_at,omitzero"`
	SeedRatio    float64   `json:"seed_ratio"`
	SeedDays     int64     `json:"seed_days"`
	// Per-torrent speed limits in KiB/s: nil = inherit the global
	// limit, 0 = unlimited, >0 = explicit. Pointers so a state file written
	// before these existed still means "inherit".
	DownloadLimitKib *int64 `json:"download_limit_kib,omitempty"`
	UploadLimitKib   *int64 `json:"upload_limit_kib,omitempty"`
	// Per-torrent connection and upload-slot caps: nil = session
	// default, 0 = unlimited, >0 = explicit.
	MaxConnections *int64 `json:"max_connections,omitempty"`
	MaxUploads     *int64 `json:"max_uploads,omitempty"`
	SwarmSeeds     int    `json:"swarm_seeds"`
	SwarmPeers     int    `json:"swarm_peers"`
	// DoneBytes is the last verified amount, reported while the engine cannot
	// compute it (a stopped torrent has no piece table).
	DoneBytes int64 `json:"done_bytes,omitempty"`
	// FilePriorities: one entry per file, 0 = skip (empty = all wanted).
	FilePriorities []int  `json:"file_priorities,omitempty"`
	Version        string `json:"version,omitempty"`
	Error          string `json:"error,omitempty"`
	// Category and Tags are the qBittorrent-style labels a client assigns
	// (standalone mode). They do not affect the transfer.
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

type persistedState struct {
	Version  int                     `json:"version"`
	Config   queue.Config            `json:"config"`
	NextPos  int64                   `json:"next_pos"`
	Torrents map[string]*torrentMeta `json:"torrents"`
	// BadTrackers maps a tracker URL to the Unix time until which it stays
	// disabled after failing continuously (see tracker_health.go).
	BadTrackers map[string]int64 `json:"bad_trackers,omitempty"`
	// Categories maps a category name to its save path (qBittorrent-style).
	Categories map[string]string `json:"categories,omitempty"`
	// Tags is the set of known tag names (a tag can exist before it is used).
	Tags []string `json:"tags,omitempty"`
	// FeedSeen records the feed items already added, so a restart does not add
	// them again (key: feed name + item guid).
	FeedSeen map[string]int64 `json:"feed_seen,omitempty"`
	// FeedSmart is the highest episode added per series+season (smart episode).
	FeedSmart map[string]int `json:"feed_smart,omitempty"`
}

// runtimeInfo is volatile per-torrent bookkeeping.
type runtimeInfo struct {
	startedAt   time.Time
	lastActive  time.Time
	lastTracker time.Time
	slow        bool
	numSeeds    int
	tracker     string
}

// Daemon owns the engine session and the queue.
type Daemon struct {
	opts Options

	mu             sync.Mutex
	session        *torrent.Session
	state          persistedState
	runtime        map[string]*runtimeInfo
	moving         map[string]bool
	dyn            queue.Dynamic
	limits         queue.Limits
	restartPending bool
	dirty          bool
	startedAt      time.Time
	// lastPeerNoiseReport rate-limits the periodic peer/tracker error summary.
	lastPeerNoiseReport time.Time
	// trackers drops trackers that never work (see tracker_health.go).
	trackers *trackerHealth
	// sessions holds the standalone login sessions (nil in managed).
	sessions *auth.Sessions
	// feedMu guards feedStatuses (the RSS feeds' last outcome).
	feedMu       sync.Mutex
	feedStatuses map[string]feedStatus
	// appliedDL/appliedUL are the global limits currently on the session.
	appliedDL int64
	appliedUL int64

	// snapshot is the last published torrent view list. The REST list and the
	// web page read it without d.mu, so a slow per-torrent call — a torrent run
	// loop blocked on a network mount, say — can never make the daemon look
	// unreachable.
	snapMu   sync.RWMutex
	snapshot []torrentInfo

	listenHost    string
	peerPort      int
	mapper        *portMapper
	lsd           *lsdService
	lsdError      string
	ipFilterPath  string
	ipFilterRules int
	// ipFilterData caches the raw filter file so a session reopen (peer limits,
	// cache TTL, preallocation) does not re-read a multi-megabyte file from
	// disk; ipFilterStamp is path|size|mtime and is empty when not cached.
	ipFilterData  []byte
	ipFilterStamp string

	// Adaptive disk cache (see cache.go). The engine reads the cache sizes when the
	// session is created, so a retune reopens the session on a coarse cadence.
	cacheRead      int64
	cacheWrite     int64
	cacheReason    string
	cacheClass     string
	cacheCheckedAt time.Time
	cacheAppliedAt time.Time

	// classCached is the storage class of the download dir, refreshed without
	// holding d.mu: statfs on an unresponsive network mount can block for a
	// long time and must never stall the daemon lock.
	classMu        sync.Mutex
	classCached    string
	classCheckedAt time.Time

	// selection is read by the engine from torrent goroutines: it has its own
	// lock and must never wait for d.mu.
	selMu     sync.RWMutex
	selection map[string][]bool

	wake chan struct{}
}

func newDaemon(opts Options) (*Daemon, error) {
	if opts.Mode == "" {
		// Conservative default: never turn on standalone behaviour implicitly.
		opts.Mode = ModeManaged
	}
	for _, dir := range []string{opts.DataDir, opts.LinkDir, opts.DownloadDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if opts.PartsDir == "" {
		opts.PartsDir = filepath.Join(opts.DataDir, "parts")
	}
	d := &Daemon{
		opts:      opts,
		selection: map[string][]bool{},
		runtime:   map[string]*runtimeInfo{},
		moving:    map[string]bool{},
		startedAt: time.Now(),
		wake:      make(chan struct{}, 1),
		trackers:  newTrackerHealth(),
		appliedDL: -1,
		appliedUL: -1,
	}
	if opts.Mode == ModeStandalone {
		d.sessions = auth.NewSessions(30 * 24 * time.Hour)
	}
	if err := d.loadState(); err != nil {
		return nil, err
	}
	for id, meta := range d.state.Torrents {
		d.setSelection(id, meta.FilePriorities)
	}
	if err := d.prepareNetwork(); err != nil {
		return nil, err
	}
	// Classify the download storage before taking the lock: statfs on a network
	// mount must never run under d.mu.
	d.refreshStorageClass(time.Now())
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.openSessionLocked(); err != nil {
		return nil, err
	}
	d.reconcileLocked()
	d.saveLocked()
	d.publishViewsLocked(d.buildViewsLocked())
	if d.peerPort > 0 {
		d.mapper = newPortMapper(d.peerPort, opts.Network.UPnP, opts.Network.NATPMP)
		d.mapper.start()
	}
	n := opts.Network
	if n.LSD && d.peerPort > 0 && n.Proxy == "" && n.OutgoingInterface == "" {
		// LSD takes d.mu itself: start it after this function returns.
		go func() {
			lsd, err := newLSD(d, d.peerPort)
			d.mu.Lock()
			defer d.mu.Unlock()
			if err != nil {
				d.lsdError = err.Error()
				logf("local peer discovery (LSD) unavailable: %v", err)
				return
			}
			d.lsd = lsd
		}()
	}
	return d, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// persistence
// ---------------------------------------------------------------------------

func (d *Daemon) loadState() error {
	d.state = persistedState{Version: 1, Config: queue.DefaultConfig(), Torrents: map[string]*torrentMeta{}}
	data, err := os.ReadFile(d.opts.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var loaded persistedState
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("corrupted state file %s: %w", d.opts.StatePath, err)
	}
	if loaded.Torrents == nil {
		loaded.Torrents = map[string]*torrentMeta{}
	}
	loaded.Config = loaded.Config.Normalized()
	d.state = loaded
	d.trackers.loadDisabled(loaded.BadTrackers)
	return nil
}

// saveLocked writes the state atomically. Failures are logged: the session
// keeps working and the next change retries.
func (d *Daemon) saveLocked() {
	d.state.BadTrackers = d.trackers.disabledSnapshot()
	data, err := json.MarshalIndent(d.state, "", "  ")
	if err != nil {
		logf("cannot encode state: %v", err)
		return
	}
	tmp := d.opts.StatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logf("cannot write state: %v", err)
		return
	}
	if err := os.Rename(tmp, d.opts.StatePath); err != nil {
		logf("cannot replace state: %v", err)
		return
	}
	d.dirty = false
}

func (d *Daemon) nextPosLocked() int64 {
	d.state.NextPos++
	return d.state.NextPos
}

// ---------------------------------------------------------------------------
// session
// ---------------------------------------------------------------------------

func (d *Daemon) sessionConfig() torrent.Config {
	cfg := torrent.DefaultConfig
	cfg.Database = d.opts.DBPath
	cfg.DataDir = d.opts.LinkDir
	cfg.DataDirIncludesTorrentID = true
	// Torrents are loaded stopped and started by gx-torrent's own queue on the
	// first tick. If the engine resumed them itself (ResumeOnStartup), it would start a
	// torrent that must stay parked/paused before reconcileLocked can stop it,
	// and starting a torrent creates its destination files: a parked torrent
	// whose payload was moved or removed would get zero-filled placeholders
	// written at the old path. The queue is the single owner of start/stop.
	cfg.ResumeOnStartup = false
	cfg.RPCEnabled = false
	// The engine rewrites every torrent's stats and bitfield in one fsync'd bolt
	// transaction per interval: 30 s by default, about 300 MB a day on the
	// state disk. Two minutes cuts that by four; the bitfield is also saved on
	// completion and stop, so a crash only re-downloads the last pieces.
	cfg.ResumeWriteInterval = 2 * time.Minute
	d.applyNetwork(&cfg)
	d.applyCache(&cfg)
	cfg.FileSelection = d.selectionFor
	cfg.PartsDir = d.opts.PartsDir
	cfg.SpeedLimitDownload = d.state.Config.SpeedLimitDownload
	cfg.SpeedLimitUpload = d.state.Config.SpeedLimitUpload
	if d.state.Config.MaxPeerDial > 0 {
		cfg.MaxPeerDial = d.state.Config.MaxPeerDial
	}
	if d.state.Config.MaxPeerAccept > 0 {
		cfg.MaxPeerAccept = d.state.Config.MaxPeerAccept
	}
	cfg.CustomLogHandler = engineLogHandler(d.opts.Debug)
	return cfg
}

func (d *Daemon) openSessionLocked() error {
	session, err := torrent.NewSession(d.sessionConfig())
	if err != nil {
		return err
	}
	d.session = session
	if path := d.ipFilterPath; path != "" || d.opts.Network.IPFilter != "" {
		if path == "" {
			path = d.opts.Network.IPFilter
		}
		if rules, err := d.loadIPFilterLocked(path); err != nil {
			logf("IP filter %s not loaded: %v", path, err)
		} else {
			logf("IP filter loaded: %d rules from %s", rules, path)
		}
	}
	now := time.Now()
	for id := range d.runtime {
		delete(d.runtime, id)
	}
	for _, t := range session.ListTorrents() {
		d.runtime[t.ID()] = &runtimeInfo{startedAt: now}
	}
	return nil
}

// restartSessionLocked applies settings the engine reads only at session creation
// (peer limits, cache TTL, preallocation). Torrents resume exactly as they were: the engine
// restores the started flag and gx-torrent keeps its own state.
func (d *Daemon) restartSessionLocked() {
	d.restartPending = false
	if d.session != nil {
		if err := d.session.Close(); err != nil {
			logf("session close: %v", err)
		}
		d.session = nil
	}
	if err := d.openSessionLocked(); err != nil {
		logf("cannot reopen the session: %v", err)
		d.restartPending = true
		return
	}
	// The engine does not persist the per-torrent speed limits: reapply them (and the
	// paused/parked state) to the reloaded torrents.
	d.reconcileLocked()
	logf("session restarted to apply new settings")
}

// reconcileLocked aligns gx-torrent's state with the torrents the engine loaded.
func (d *Daemon) reconcileLocked() {
	live := map[string]bool{}
	for _, t := range d.session.ListTorrents() {
		id := t.ID()
		live[id] = true
		meta, ok := d.state.Torrents[id]
		if !ok {
			savePath, found := d.readLink(id)
			if !found {
				savePath = d.opts.DownloadDir
			}
			meta = &torrentMeta{
				ID: id, SavePath: savePath, AddedAt: t.AddedAt(), Pos: d.nextPosLocked(),
				SeedRatio: -1, SeedDays: -1, SwarmSeeds: -1, SwarmPeers: -1,
			}
			d.state.Torrents[id] = meta
			d.dirty = true
		}
		hash := t.InfoHash().String()
		if meta.Hash != hash {
			meta.Hash = hash
			d.dirty = true
		}
		// Re-apply the per-torrent speed limits stored across restarts.
		if meta.DownloadLimitKib != nil || meta.UploadLimitKib != nil {
			t.SetSpeedLimits(kibOrInherit(meta.DownloadLimitKib), kibOrInherit(meta.UploadLimitKib))
		}
		if meta.MaxConnections != nil {
			t.SetMaxConnections(int(*meta.MaxConnections))
		}
		if meta.MaxUploads != nil {
			t.SetMaxUploads(int(*meta.MaxUploads))
		}
		// Super-seeding is a per-torrent seeding strategy; keep it in sync with
		// the daemon state (the engine persists it too).
		_ = t.SetSuperSeeding(meta.SuperSeeding)
		// Torrents parked or paused must not run, whatever the engine restored.
		if meta.UserPaused || meta.Parked {
			_ = t.Stop()
		}
	}
	for id := range d.state.Torrents {
		if !live[id] {
			delete(d.state.Torrents, id)
			d.dirty = true
		}
	}
}

func (d *Daemon) close() {
	d.mapper.close()
	d.mu.Lock()
	lsd := d.lsd
	d.lsd = nil
	d.mu.Unlock()
	lsd.close()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.saveLocked()
	if d.session != nil {
		if err := d.session.Close(); err != nil {
			logf("session close: %v", err)
		}
		d.session = nil
	}
}

// findLocked looks a torrent up by info hash (case-insensitive) or the engine ID.
func (d *Daemon) findLocked(key string) (*torrent.Torrent, *torrentMeta) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || d.session == nil {
		return nil, nil
	}
	for _, t := range d.session.ListTorrents() {
		if t.InfoHash().String() == key || t.ID() == key {
			return t, d.metaLocked(t)
		}
	}
	return nil, nil
}

func (d *Daemon) metaLocked(t *torrent.Torrent) *torrentMeta {
	meta, ok := d.state.Torrents[t.ID()]
	if !ok {
		meta = &torrentMeta{
			ID: t.ID(), Hash: t.InfoHash().String(), SavePath: d.opts.DownloadDir, AddedAt: t.AddedAt(),
			Pos: d.nextPosLocked(), SeedRatio: -1, SeedDays: -1, SwarmSeeds: -1, SwarmPeers: -1,
		}
		if savePath, found := d.readLink(t.ID()); found {
			meta.SavePath = savePath
		}
		d.state.Torrents[t.ID()] = meta
		d.dirty = true
	}
	return meta
}

func (d *Daemon) runtimeLocked(id string) *runtimeInfo {
	rt, ok := d.runtime[id]
	if !ok {
		rt = &runtimeInfo{}
		d.runtime[id] = rt
	}
	return rt
}

// poke asks the queue loop for an immediate round.
func (d *Daemon) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// ---------------------------------------------------------------------------
// queue loop
// ---------------------------------------------------------------------------

func (d *Daemon) run(stop <-chan struct{}) {
	// The RSS engine runs beside the queue loop, only in standalone mode.
	go d.runFeeds(stop)
	ticker := time.NewTicker(d.opts.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		case <-d.wake:
		}
		d.tick(time.Now())
	}
}

func isRunning(status torrent.Status) bool {
	return status != torrent.Stopped && status != torrent.Stopping
}

// statsLocked returns the engine's stats with the completed bytes fixed: the engine
// reports 0 for a stopped torrent (its piece table exists only while it
// runs), which would make a finished, paused torrent look empty.
func (d *Daemon) statsLocked(t *torrent.Torrent) torrent.Stats {
	return d.adjustStatsLocked(t, t.Stats())
}

// adjustStatsLocked applies the meta-driven fixes to a sample already taken
// from the torrent run loop. The tick passes a sample collected outside d.mu,
// so the per-torrent call itself never runs under the lock.
func (d *Daemon) adjustStatsLocked(t *torrent.Torrent, stats torrent.Stats) torrent.Stats {
	total := stats.Bytes.Total
	if total <= 0 {
		return stats
	}
	meta := d.metaLocked(t)
	running := isRunning(stats.Status)
	done := stats.Bytes.Completed
	if !running || done == 0 {
		estimate := int64(stats.Pieces.Have) * int64(stats.PieceLength)
		if stats.Pieces.Total > 0 && stats.Pieces.Have >= stats.Pieces.Total {
			estimate = total
		}
		done = max(done, min(estimate, total))
		if done == 0 && !running && meta.DoneBytes > 0 {
			done = min(meta.DoneBytes, total)
		}
	}
	if delta := done - meta.DoneBytes; delta != 0 && (done == total || delta*20 >= total || -delta*20 >= total) {
		meta.DoneBytes = done
		d.dirty = true
	}
	stats.Bytes.Completed = done
	stats.Bytes.Incomplete = total - done
	// With a file selection, sizes and progress refer to the wanted files.
	if stats.Bytes.Selected > 0 && stats.Bytes.Selected < total {
		stats.Bytes.Total = stats.Bytes.Selected
		stats.Bytes.Completed = stats.Bytes.SelectedCompleted
		stats.Bytes.Incomplete = stats.Bytes.Selected - stats.Bytes.SelectedCompleted
	}
	return stats
}

func isComplete(stats torrent.Stats) bool {
	return stats.Status != torrent.DownloadingMetadata && stats.Bytes.Total > 0 && stats.Bytes.Incomplete == 0
}

// collectedTorrent is one torrent sampled from its run loop. The sample is
// taken before d.mu is held: the engine's per-torrent calls can block on storage I/O
// (a slow or unresponsive network mount), and holding the daemon lock through
// them would freeze every API request.
type collectedTorrent struct {
	stats    torrent.Stats
	peers    []torrent.Peer
	trackers []torrent.Tracker
}

// peerNoiseReportEvery is how often the demoted peer/tracker errors are
// summarised. They are not printed one by one (see swarmNoiseFilter), so an
// idle log does not hide a growing connectivity problem.
const peerNoiseReportEvery = 10 * time.Minute

// reportPeerNoise logs the peer/tracker errors demoted since the last report.
// It normally runs on the single queue-loop goroutine; the timestamp is taken
// under d.mu so a test (or a future caller) ticking concurrently cannot race it.
func (d *Daemon) reportPeerNoise(now time.Time) {
	d.mu.Lock()
	if !d.lastPeerNoiseReport.IsZero() && now.Sub(d.lastPeerNoiseReport) < peerNoiseReportEvery {
		d.mu.Unlock()
		return
	}
	d.lastPeerNoiseReport = now
	d.mu.Unlock()
	if line := formatPeerNoise(peerNoiseCounters.drain()); line != "" {
		logf("peer noise: %s", line)
	}
}

func (d *Daemon) tick(now time.Time) {
	d.reportPeerNoise(now)
	// Phase 1: sample the run loops without d.mu.
	d.mu.Lock()
	session := d.session
	d.mu.Unlock()
	if session == nil {
		return
	}
	torrents := session.ListTorrents()
	collected := make(map[string]collectedTorrent, len(torrents))
	for _, t := range torrents {
		collected[t.ID()] = collectedTorrent{
			stats:    t.Stats(),
			peers:    t.Peers(),
			trackers: t.Trackers(),
		}
	}
	// statfs on the download dir can block on a network mount: classify it
	// outside d.mu.
	d.refreshStorageClass(now)
	// Drop trackers that have been failing for too long. It takes d.mu through
	// setTrackers, so it must run outside the section below.
	d.updateTrackerHealth(collected, now)

	// Phase 2: apply the samples under d.mu.
	d.mu.Lock()
	if d.session != session {
		d.mu.Unlock()
		return
	}
	if d.restartPending && len(d.moving) == 0 {
		// The session is rebuilt: the samples belong to the old handles. The
		// next tick (the config change pokes the queue) samples the new ones.
		d.restartSessionLocked()
		d.mu.Unlock()
		return
	}
	cfg := d.state.Config
	slowAfter := time.Duration(cfg.SlowAfterSecs) * time.Second

	torrents = d.session.ListTorrents()
	statsOf := make(map[string]torrent.Stats, len(torrents))
	for _, t := range torrents {
		if sample, ok := collected[t.ID()]; ok {
			statsOf[t.ID()] = d.adjustStatsLocked(t, sample.stats)
		}
	}
	positions := d.queuePositionsLocked(torrents, statsOf)

	handles := make(map[string]*torrent.Torrent, len(torrents))
	items := make([]queue.Item, 0, len(torrents))
	var aggregate int64
	queued := 0
	activeDownloads, activeSeeds := 0, 0
	var pendingStops []*torrent.Torrent
	for _, t := range torrents {
		id := t.ID()
		stats, ok := statsOf[id]
		if !ok {
			// Added between the two phases: handled on the next tick.
			continue
		}
		handles[id] = t
		meta := d.metaLocked(t)
		rt := d.runtimeLocked(id)
		running := isRunning(stats.Status)
		complete := isComplete(stats)
		if running {
			if complete {
				activeSeeds++
			} else {
				activeDownloads++
			}
		}

		if complete && meta.CompletedAt.IsZero() {
			meta.CompletedAt = now
			d.dirty = true
		}
		if !running && stats.Error != nil && meta.Error == "" && !d.moving[id] {
			meta.Error = stats.Error.Error()
			d.dirty = true
			logf("%s stopped with an error: %s", stats.Name, meta.Error)
		}
		if meta.StopAtMetadata && stats.Status != torrent.DownloadingMetadata && stats.Bytes.Total > 0 {
			meta.StopAtMetadata = false
			meta.UserPaused = true
			d.dirty = true
			if running {
				pendingStops = append(pendingStops, t)
				running = false
			}
		}
		if !meta.ProbeUntil.IsZero() && now.After(meta.ProbeUntil) {
			meta.ProbeUntil = time.Time{}
			d.dirty = true
		}

		if running {
			if rt.startedAt.IsZero() {
				rt.startedAt = now
			}
			rate := int64(stats.Speed.Download)
			if complete {
				rate = int64(stats.Speed.Upload)
			}
			if rate >= cfg.SlowRate || stats.Status == torrent.Verifying || stats.Status == torrent.Allocating {
				rt.lastActive = now
			}
			base := rt.startedAt
			if rt.lastActive.After(base) {
				base = rt.lastActive
			}
			rt.slow = now.Sub(base) >= slowAfter
			if !complete {
				aggregate += int64(stats.Speed.Download)
			}
			sample := collected[id]
			d.applySwarmLocked(meta, rt, now, sample.peers, sample.trackers)
		} else {
			rt.startedAt = time.Time{}
			rt.lastActive = time.Time{}
			rt.slow = false
			rt.numSeeds = 0
		}

		probing := !meta.ProbeUntil.IsZero()
		blocked := meta.UserPaused || meta.Parked || meta.Error != "" || d.moving[id]
		item := queue.Item{
			ID:        id,
			Complete:  complete,
			Running:   running,
			Managed:   !blocked && !meta.Pinned && !probing,
			Forced:    !blocked && (meta.Pinned || probing),
			Slow:      rt.slow,
			RotatedAt: meta.RotatedAt,
			Pos:       meta.Pos,
		}
		if d.moving[id] {
			// The mover owns this torrent: the queue neither starts nor stops it.
			continue
		}
		if item.Managed && !complete && !running {
			queued++
		}
		items = append(items, item)
	}

	d.limits = d.dyn.Limits(cfg, aggregate, queued, now)
	d.adaptCacheLocked(now, activeDownloads, activeSeeds, aggregate)
	d.applyEffectiveSpeedLimitsLocked(now)
	plan := queue.PlanQueue(items, d.limits, cfg, now)
	for _, id := range plan.Rotate {
		if meta, ok := d.state.Torrents[id]; ok {
			meta.RotatedAt = now
			meta.Pos = d.nextPosLocked()
			d.dirty = true
			logf("«%s» moves no data: set aside so another torrent can run", statsOf[id].Name)
		}
	}
	for _, id := range plan.Stop {
		if t := handles[id]; t != nil {
			pendingStops = append(pendingStops, t)
		}
	}
	// Start and stop are issued outside d.mu: the engine's sendCommand waits on the
	// torrent run loop, which can be busy on storage I/O. The queue retries on
	// the next tick, so a delayed action is not lost.
	var pendingStarts []*torrent.Torrent
	for _, id := range plan.Start {
		t := handles[id]
		if t == nil {
			continue
		}
		pendingStarts = append(pendingStarts, t)
		rt := d.runtimeLocked(id)
		rt.startedAt = now
		rt.lastActive = time.Time{}
		if meta, ok := d.state.Torrents[id]; ok && !meta.RotatedAt.IsZero() {
			meta.RotatedAt = time.Time{}
			d.dirty = true
		}
	}
	if d.dirty {
		d.saveLocked()
	}

	// Publish the snapshot for the lock-free read path.
	views := make([]torrentInfo, 0, len(handles))
	for _, t := range torrents {
		id := t.ID()
		stats, ok := statsOf[id]
		if !ok {
			continue
		}
		views = append(views, d.infoLocked(t, stats, d.runtimeLocked(id), d.moving[id], positions))
	}
	sort.Slice(views, func(i, j int) bool { return views[i].Hash < views[j].Hash })
	d.publishViewsLocked(views)
	d.mu.Unlock()

	// Phase 3: act on the run loops without holding d.mu.
	for _, t := range pendingStops {
		if err := t.Stop(); err != nil {
			logf("queue stop %s: %v", t.ID(), err)
		}
	}
	for _, t := range pendingStarts {
		if err := t.Start(); err != nil {
			logf("queue start %s: %v", t.ID(), err)
		}
	}
}

// applySwarmLocked records the peer count sampled this tick and, once a minute,
// the tracker scrape. The peers and trackers are collected outside d.mu (see
// tick); the swarm size survives a stop so a parked torrent keeps its
// diagnosis.
func (d *Daemon) applySwarmLocked(meta *torrentMeta, rt *runtimeInfo, now time.Time, peers []torrent.Peer, trackers []torrent.Tracker) {
	seeds := 0
	for _, peer := range peers {
		if peer.Seed || peer.DownloadSpeed > 0 {
			seeds++
		}
	}
	rt.numSeeds = seeds
	if now.Sub(rt.lastTracker) < time.Minute {
		return
	}
	rt.lastTracker = now
	swarmSeeds, swarmPeers, working := -1, -1, false
	rt.tracker = ""
	for _, tracker := range trackers {
		if tracker.Status != torrent.Working {
			continue
		}
		if !working {
			rt.tracker = tracker.URL
		}
		working = true
		if tracker.Seeders > swarmSeeds {
			swarmSeeds = tracker.Seeders
		}
		if tracker.Leechers > swarmPeers {
			swarmPeers = tracker.Leechers
		}
	}
	if working && (meta.SwarmSeeds != swarmSeeds || meta.SwarmPeers != swarmPeers) {
		meta.SwarmSeeds = swarmSeeds
		meta.SwarmPeers = swarmPeers
		d.dirty = true
	}
}

// ---------------------------------------------------------------------------
// views
// ---------------------------------------------------------------------------

type torrentInfo struct {
	Hash           string  `json:"hash"`
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	State          string  `json:"state"`
	SavePath       string  `json:"save_path"`
	Progress       float64 `json:"progress"`
	TotalSize      int64   `json:"total_size"`
	TotalDone      int64   `json:"total_done"`
	DownloadRate   int64   `json:"download_rate"`
	UploadRate     int64   `json:"upload_rate"`
	Downloaded     int64   `json:"downloaded"`
	Uploaded       int64   `json:"uploaded"`
	SeedingSeconds int64   `json:"seeding_seconds"`
	ActiveSeconds  int64   `json:"active_seconds"`
	ETASeconds     int64   `json:"eta_seconds"`
	CurrentTracker string  `json:"current_tracker,omitempty"`
	QueuePosition  int     `json:"queue_position"`
	NumPeers       int     `json:"num_peers"`
	NumSeeds       int     `json:"num_seeds"`
	NumComplete    int     `json:"num_complete"`
	NumIncomplete  int     `json:"num_incomplete"`
	SeedRatio      float64 `json:"seed_ratio"`
	SeedDays       int64   `json:"seed_days"`
	// Per-torrent speed limits in KiB/s: -1 global, 0 unlimited.
	DownloadLimitKib int64  `json:"download_limit_kib"`
	UploadLimitKib   int64  `json:"upload_limit_kib"`
	HasMetadata      bool   `json:"has_metadata"`
	AutoManaged      bool   `json:"auto_managed"`
	Sequential       bool   `json:"sequential"`
	FirstLast        bool   `json:"first_last"`
	SuperSeeding     bool   `json:"super_seeding"`
	Pinned           bool   `json:"pinned"`
	Parked           bool   `json:"parked"`
	Probing          bool   `json:"probing"`
	Slow             bool   `json:"slow"`
	Private          bool   `json:"private"`
	TorrentVersion   string `json:"torrent_version"`
	PiecesTotal      uint32 `json:"pieces_total"`
	PiecesHave       uint32 `json:"pieces_have"`
	PiecesAvailable  uint32 `json:"pieces_available"`
	PiecesChecked    uint32 `json:"pieces_checked"`
	PieceLength      uint32 `json:"piece_length"`
	Wasted           int64  `json:"wasted"`
	Allocated        int64  `json:"allocated"`
	FileCount        int    `json:"file_count"`
	Error            string `json:"error,omitempty"`
	AddedAt          int64  `json:"added_at"`
	CompletedAt      int64  `json:"completed_at,omitempty"`
	// Category and Tags are the qBittorrent-style labels (standalone mode).
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// stateFor maps the engine's status and gx-torrent's flags onto Gextto's states.
func stateFor(meta *torrentMeta, stats torrent.Stats, moving bool) string {
	switch {
	case moving:
		return "moving"
	case meta.Error != "":
		return "error"
	case meta.Parked:
		return "stalled"
	}
	switch stats.Status {
	case torrent.Verifying:
		return "checking_files"
	case torrent.DownloadingMetadata:
		return "downloading_metadata"
	case torrent.Downloading, torrent.Allocating:
		return "downloading"
	case torrent.Seeding:
		return "seeding"
	}
	// Stopped or stopping. A download the queue set aside for moving no data
	// stays "stalled" until it runs again, so Gextto's stall clock keeps going.
	if !meta.RotatedAt.IsZero() && !isComplete(stats) {
		return "stalled"
	}
	return "paused"
}

func (d *Daemon) infoLocked(t *torrent.Torrent, stats torrent.Stats, rt *runtimeInfo, moving bool, queuePos map[string]int) torrentInfo {
	meta := d.metaLocked(t)
	if meta.Version == "" && stats.Status != torrent.DownloadingMetadata && stats.Bytes.Total > 0 {
		if data, err := t.Torrent(); err == nil && len(data) > 0 {
			// The engine handles v1 and the v1 side of hybrid torrents; the raw
			// metainfo carries "meta version" only when a v2 layer is present.
			if bytes.Contains(data, []byte("12:meta versioni2e")) {
				if bytes.Contains(data, []byte("6:pieces")) {
					meta.Version = "hybrid"
				} else {
					meta.Version = "v2"
				}
			} else {
				meta.Version = "v1"
			}
			d.dirty = true
		}
	}
	torrentVersion := meta.Version
	if torrentVersion == "" {
		torrentVersion = "v1"
	}
	progress := 0.0
	if stats.Bytes.Total > 0 {
		progress = float64(stats.Bytes.Completed) * 100 / float64(stats.Bytes.Total)
	}
	eta := int64(-1)
	if stats.ETA != nil {
		eta = int64(stats.ETA.Seconds())
	}
	pos, ok := queuePos[t.ID()]
	if !ok {
		pos = -1
	}
	info := torrentInfo{
		Hash:             t.InfoHash().String(),
		ID:               t.ID(),
		Name:             stats.Name,
		State:            stateFor(meta, stats, moving),
		SavePath:         meta.SavePath,
		Progress:         progress,
		TotalSize:        stats.Bytes.Total,
		TotalDone:        stats.Bytes.Completed,
		DownloadRate:     int64(stats.Speed.Download),
		UploadRate:       int64(stats.Speed.Upload),
		Downloaded:       stats.Bytes.Downloaded,
		Uploaded:         stats.Bytes.Uploaded,
		SeedingSeconds:   int64(stats.SeededFor.Seconds()),
		ETASeconds:       eta,
		QueuePosition:    pos,
		NumPeers:         stats.Peers.Total,
		NumSeeds:         rt.numSeeds,
		CurrentTracker:   rt.tracker,
		NumComplete:      meta.SwarmSeeds,
		NumIncomplete:    meta.SwarmPeers,
		SeedRatio:        meta.SeedRatio,
		SeedDays:         meta.SeedDays,
		DownloadLimitKib: kibOrInherit(meta.DownloadLimitKib),
		UploadLimitKib:   kibOrInherit(meta.UploadLimitKib),
		HasMetadata:      stats.Status != torrent.DownloadingMetadata && stats.Bytes.Total > 0,
		AutoManaged:      !meta.UserPaused && !meta.Parked && !meta.Pinned,
		Sequential:       meta.Sequential,
		FirstLast:        meta.FirstLast,
		SuperSeeding:     meta.SuperSeeding,
		Pinned:           meta.Pinned,
		Parked:           meta.Parked,
		Probing:          !meta.ProbeUntil.IsZero(),
		Slow:             rt.slow,
		Private:          stats.Private,
		TorrentVersion:   torrentVersion,
		PiecesTotal:      stats.Pieces.Total,
		PiecesHave:       stats.Pieces.Have,
		PiecesAvailable:  stats.Pieces.Available,
		PiecesChecked:    stats.Pieces.Checked,
		PieceLength:      stats.PieceLength,
		Wasted:           stats.Bytes.Wasted,
		Allocated:        stats.Bytes.Allocated,
		FileCount:        stats.FileCount,
		Error:            meta.Error,
		AddedAt:          meta.AddedAt.Unix(),
		Category:         meta.Category,
		Tags:             meta.Tags,
	}
	if info.Error == "" && stats.Error != nil {
		info.Error = stats.Error.Error()
	}
	if !rt.startedAt.IsZero() {
		info.ActiveSeconds = int64(time.Since(rt.startedAt).Seconds())
	}
	if !meta.CompletedAt.IsZero() {
		info.CompletedAt = meta.CompletedAt.Unix()
	}
	return info
}

// queuePositionsLocked numbers the queue-managed downloads (0 = next to run).
// statsOf holds the already-adjusted per-torrent stats, so this never queries
// a torrent run loop.
func (d *Daemon) queuePositionsLocked(torrents []*torrent.Torrent, statsOf map[string]torrent.Stats) map[string]int {
	type entry struct {
		id  string
		pos int64
	}
	var list []entry
	for _, t := range torrents {
		stats, ok := statsOf[t.ID()]
		if !ok {
			continue
		}
		meta := d.metaLocked(t)
		if meta.UserPaused || meta.Parked || meta.Pinned || meta.Error != "" {
			continue
		}
		if isComplete(stats) {
			continue
		}
		list = append(list, entry{t.ID(), meta.Pos})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].pos != list[j].pos {
			return list[i].pos < list[j].pos
		}
		return list[i].id < list[j].id
	})
	out := make(map[string]int, len(list))
	for index, item := range list {
		out[item.id] = index
	}
	return out
}

// buildViewsLocked rebuilds the torrent view list. Callers hold d.mu. It
// queries the torrent run loops (statsLocked); the tick path passes samples
// collected outside the lock through the same helpers, so a slow run loop
// cannot stall the read path.
func (d *Daemon) buildViewsLocked() []torrentInfo {
	if d.session == nil {
		return []torrentInfo{}
	}
	torrents := d.session.ListTorrents()
	statsOf := make(map[string]torrent.Stats, len(torrents))
	for _, t := range torrents {
		statsOf[t.ID()] = d.statsLocked(t)
	}
	positions := d.queuePositionsLocked(torrents, statsOf)
	out := make([]torrentInfo, 0, len(torrents))
	for _, t := range torrents {
		out = append(out, d.infoLocked(t, statsOf[t.ID()], d.runtimeLocked(t.ID()), d.moving[t.ID()], positions))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hash < out[j].Hash })
	return out
}

// publishViewsLocked stores the view list for the lock-free readers.
func (d *Daemon) publishViewsLocked(views []torrentInfo) {
	d.snapMu.Lock()
	d.snapshot = views
	d.snapMu.Unlock()
}

// snapshotViews returns a copy of the last published view list. It never takes
// d.mu nor touches a torrent run loop, so the REST list and the web page keep
// answering even while the queue is blocked on storage I/O.
func (d *Daemon) snapshotViews() []torrentInfo {
	d.snapMu.RLock()
	defer d.snapMu.RUnlock()
	out := make([]torrentInfo, len(d.snapshot))
	copy(out, d.snapshot)
	return out
}

// list builds a fresh view list, publishes it and returns it. Internal callers
// and tests use it; the hot read path uses snapshotViews.
func (d *Daemon) list() []torrentInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	views := d.buildViewsLocked()
	if d.dirty {
		d.saveLocked()
	}
	d.publishViewsLocked(views)
	return views
}

// ---------------------------------------------------------------------------
// operations
// ---------------------------------------------------------------------------

var errNotFound = errors.New("torrent not found")

type addRequest struct {
	Magnet      string
	TorrentData []byte
	Destination string
	Paused      bool
	QueueTop    bool
	// StopAtMetadata pauses the torrent as soon as its metadata arrives.
	StopAtMetadata bool
	// Sequential downloads pieces in order (streaming) instead of rarest-first.
	Sequential bool
	// FirstLast downloads the ends of every file first.
	FirstLast bool
	// SuperSeeding enables BEP 16 super-seeding, a seeding strategy.
	SuperSeeding bool
	SeedRatio    float64
	SeedDays     int64
	// Category and Tags are the qBittorrent-style labels (standalone mode).
	Category string
	Tags     []string
}

// add registers a torrent stopped and lets the queue start it. A torrent
// already in the session is reported with existing=true and left untouched.
func (d *Daemon) add(req addRequest) (string, bool, error) {
	if req.Magnet != "" {
		// The engine reads only the first xt: keep the v1 hash in front so a hybrid
		// magnet whose btmh comes first is still downloadable.
		req.Magnet = normalizeMagnet(req.Magnet)
	}
	dest, err := d.validateDestination(req.Destination)
	if err != nil {
		return "", false, err
	}
	if req.Magnet != "" {
		if hash, ok := magnetInfoHash(req.Magnet); ok {
			d.mu.Lock()
			t, _ := d.findLocked(hash)
			d.mu.Unlock()
			if t != nil {
				return hash, true, nil
			}
		}
	}
	id, err := randomID()
	if err != nil {
		return "", false, err
	}
	if err := d.pointLink(id, dest); err != nil {
		return "", false, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session == nil {
		_ = os.Remove(d.linkPath(id))
		return "", false, errors.New("session not available")
	}
	opt := &torrent.AddTorrentOptions{ID: id, Stopped: true, Sequential: req.Sequential || d.state.Config.Sequential, FirstLast: req.FirstLast, SuperSeeding: req.SuperSeeding}
	var t *torrent.Torrent
	if req.Magnet != "" {
		t, err = d.session.AddURI(req.Magnet, opt)
	} else {
		t, err = d.session.AddTorrent(strings.NewReader(string(req.TorrentData)), opt)
	}
	if err != nil {
		_ = os.Remove(d.linkPath(id))
		return "", false, err
	}
	hash := t.InfoHash().String()
	// The engine accepts the same info hash twice: keep the first one. Removing the
	// duplicate only unlinks its symlink, never the payload.
	for _, other := range d.session.ListTorrents() {
		if other.ID() != id && other.InfoHash().String() == hash {
			if err := d.session.RemoveTorrent(id, false); err != nil {
				logf("cannot drop duplicate %s: %v", id, err)
			}
			return hash, true, nil
		}
	}
	pos := d.nextPosLocked()
	if req.QueueTop {
		for _, other := range d.state.Torrents {
			if other.Pos <= pos {
				pos = other.Pos - 1
			}
		}
	}
	d.state.Torrents[id] = &torrentMeta{
		ID: id, Hash: hash, SavePath: dest, AddedAt: time.Now(), Pos: pos,
		UserPaused: req.Paused, StopAtMetadata: req.StopAtMetadata && !isComplete(d.statsLocked(t)),
		Sequential: req.Sequential || d.state.Config.Sequential,
		FirstLast:  req.FirstLast,
		// SuperSeeding is a per-torrent seeding strategy, not a queue default.
		SuperSeeding: req.SuperSeeding,
		SeedRatio:    req.SeedRatio, SeedDays: req.SeedDays, SwarmSeeds: -1, SwarmPeers: -1,
		Category: req.Category,
		Tags:     req.Tags,
	}
	d.state.Tags = mergeTagNames(d.state.Tags, req.Tags)
	d.saveLocked()
	d.poke()
	return hash, false, nil
}

// remove drops a torrent. The payload is deleted only when asked.
func (d *Daemon) remove(key string, deleteFiles bool) error {
	d.mu.Lock()
	t, meta := d.findLocked(key)
	if t == nil {
		d.mu.Unlock()
		return errNotFound
	}
	if d.moving[t.ID()] {
		d.mu.Unlock()
		return errors.New("the torrent is being moved")
	}
	id := t.ID()
	name := t.Stats().Name
	savePath := meta.SavePath
	d.protectLegacyDir(id)
	err := d.session.RemoveTorrent(id, false)
	delete(d.state.Torrents, id)
	delete(d.runtime, id)
	d.setSelection(id, nil)
	_ = os.RemoveAll(filepath.Join(d.opts.PartsDir, id))
	d.saveLocked()
	d.mu.Unlock()
	d.poke()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if deleteFiles {
		return deletePayload(savePath, name)
	}
	return nil
}

// withTorrent runs fn on a torrent under the lock, then saves and pokes.
func (d *Daemon) withTorrent(key string, fn func(t *torrent.Torrent, meta *torrentMeta) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	t, meta := d.findLocked(key)
	if t == nil {
		return errNotFound
	}
	if d.moving[t.ID()] {
		return errors.New("the torrent is being moved")
	}
	if err := fn(t, meta); err != nil {
		return err
	}
	d.saveLocked()
	d.poke()
	return nil
}

// pause is a user pause: the queue never resumes it.
func (d *Daemon) pause(key string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		meta.UserPaused = true
		meta.ProbeUntil = time.Time{}
		return t.Stop()
	})
}

// errPayloadGone refuses to start or verify a finished torrent whose data is no
// longer at its save path (archived and renamed, or deleted): the engine would
// re-create every file empty there, in the middle of the library.
func errPayloadGone(t *torrent.Torrent, meta *torrentMeta) error {
	if meta.CompletedAt.IsZero() || t.Name() == "" {
		return nil
	}
	path := filepath.Join(meta.SavePath, t.Name())
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("the downloaded data is no longer in %s", path)
	}
	return nil
}

// resume hands the torrent back to the queue (it starts at once if a slot is
// free) and clears a previous error.
func (d *Daemon) resume(key string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		if err := errPayloadGone(t, meta); err != nil {
			return err
		}
		meta.UserPaused = false
		meta.Parked = false
		meta.RotatedAt = time.Time{}
		meta.Error = ""
		return nil
	})
}

// park sets a stalled torrent aside until Gextto probes it again.
func (d *Daemon) park(key string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		meta.Parked = true
		meta.ProbeUntil = time.Time{}
		return t.Stop()
	})
}

// unpark returns a parked torrent to the queue.
func (d *Daemon) unpark(key string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		meta.Parked = false
		meta.RotatedAt = time.Time{}
		return nil
	})
}

// restart is Gextto's stall probe: the torrent runs outside the queue for
// the probe window and is announced again to trackers and DHT.
func (d *Daemon) restart(key string, window time.Duration) error {
	if window <= 0 {
		window = d.opts.ProbeWindow
	}
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		meta.Parked = false
		meta.UserPaused = false
		meta.Error = ""
		meta.RotatedAt = time.Time{}
		meta.ProbeUntil = time.Now().Add(window)
		if err := t.Start(); err != nil {
			return err
		}
		rt := d.runtimeLocked(t.ID())
		rt.startedAt = time.Now()
		rt.lastActive = time.Time{}
		t.Announce()
		return nil
	})
}

func (d *Daemon) reannounce(key string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, _ *torrentMeta) error {
		t.Announce()
		return nil
	})
}

func (d *Daemon) verify(key string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		if err := errPayloadGone(t, meta); err != nil {
			return err
		}
		meta.Error = ""
		return t.Verify()
	})
}

// setPin pins or unpins one torrent; an empty key unpins all.
func (d *Daemon) setPin(key string, pinned bool) error {
	if strings.TrimSpace(key) == "" {
		d.mu.Lock()
		for _, meta := range d.state.Torrents {
			meta.Pinned = false
		}
		d.saveLocked()
		d.mu.Unlock()
		d.poke()
		return nil
	}
	return d.withTorrent(key, func(_ *torrent.Torrent, meta *torrentMeta) error {
		meta.Pinned = pinned
		if pinned {
			meta.UserPaused = false
		}
		return nil
	})
}

// setSeedLimits stores the per-torrent seed policy (-1 = global, 0 =
// unlimited). Gextto enforces it.
// kibOrInherit maps a stored per-torrent limit to the engine convention: nil means
// inherit the global limit (-1).
func kibOrInherit(value *int64) int64 {
	if value == nil {
		return -1
	}
	return *value
}

// setLimits stores the per-torrent seed policy and, when given, the speed
// limits (KiB/s), applying the latter to the running torrent at once.
func (d *Daemon) setLimits(key string, download, upload *int64, ratio *float64, days *int64) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		if download != nil {
			value := *download
			meta.DownloadLimitKib = &value
		}
		if upload != nil {
			value := *upload
			meta.UploadLimitKib = &value
		}
		if ratio != nil {
			meta.SeedRatio = *ratio
		}
		if days != nil {
			meta.SeedDays = *days
		}
		if download != nil || upload != nil {
			t.SetSpeedLimits(kibOrInherit(meta.DownloadLimitKib), kibOrInherit(meta.UploadLimitKib))
		}
		return nil
	})
}

// setConnLimits stores and applies the per-torrent connection and upload-slot
// caps. A nil limit is left unchanged; a negative one clears it.
func (d *Daemon) setConnLimits(key string, maxConnections, maxUploads *int64) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		if maxConnections != nil {
			if *maxConnections < 0 {
				meta.MaxConnections = nil
				t.SetMaxConnections(-1)
			} else {
				value := *maxConnections
				meta.MaxConnections = &value
				t.SetMaxConnections(int(value))
			}
		}
		if maxUploads != nil {
			if *maxUploads < 0 {
				meta.MaxUploads = nil
				t.SetMaxUploads(-1)
			} else {
				value := *maxUploads
				meta.MaxUploads = &value
				t.SetMaxUploads(int(value))
			}
		}
		return nil
	})
}

// setSuperSeeding toggles BEP 16 super-seeding on a torrent. It
// is a seeding strategy: it does not touch the queue, the seed policy or the
// bandwidth limits.
func (d *Daemon) setSuperSeeding(key string, enabled bool) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		meta.SuperSeeding = enabled
		return t.SetSuperSeeding(enabled)
	})
}

// toggleSequential flips sequential download on a running torrent.
func (d *Daemon) toggleSequential(key string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
		meta.Sequential = !meta.Sequential
		return t.SetSequential(meta.Sequential)
	})
}

// removeTrackers drops the given tracker URLs from a torrent, keeping the rest.
func (d *Daemon) removeTrackers(key string, urls []string) error {
	drop := make(map[string]bool, len(urls))
	for _, u := range urls {
		drop[strings.TrimSpace(u)] = true
	}
	return d.withTorrent(key, func(t *torrent.Torrent, _ *torrentMeta) error {
		var keep []string
		for _, tr := range t.Trackers() {
			if !drop[strings.TrimSpace(tr.URL)] {
				keep = append(keep, tr.URL)
			}
		}
		return t.SetTrackers(keep)
	})
}

// editTracker replaces one tracker URL with another.
func (d *Daemon) editTracker(key, origURL, newURL string) error {
	origURL = strings.TrimSpace(origURL)
	return d.withTorrent(key, func(t *torrent.Torrent, _ *torrentMeta) error {
		var list []string
		for _, tr := range t.Trackers() {
			if strings.TrimSpace(tr.URL) == origURL {
				list = append(list, newURL)
			} else {
				list = append(list, tr.URL)
			}
		}
		return t.SetTrackers(list)
	})
}

// moveToTop puts a queued download at the head of the queue.
func (d *Daemon) moveToTop(key string) error {
	return d.withTorrent(key, func(_ *torrent.Torrent, meta *torrentMeta) error {
		lowest := meta.Pos
		for _, other := range d.state.Torrents {
			if other.Pos < lowest {
				lowest = other.Pos
			}
		}
		meta.Pos = lowest - 1
		meta.RotatedAt = time.Time{}
		return nil
	})
}

func (d *Daemon) addTrackers(key string, urls []string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, _ *torrentMeta) error {
		for _, url := range urls {
			if err := t.AddTracker(url); err != nil {
				return err
			}
		}
		return nil
	})
}

// setTrackers replaces the torrent's tracker list; an empty list removes every
// tracker. Unlike addTrackers it is not additive.
func (d *Daemon) setTrackers(key string, urls []string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, _ *torrentMeta) error {
		return t.SetTrackers(urls)
	})
}

// addWebseeds and removeWebseeds change the per-torrent web seed list at runtime.
func (d *Daemon) addWebseeds(key string, urls []string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, _ *torrentMeta) error {
		return t.AddWebseeds(urls)
	})
}

func (d *Daemon) removeWebseeds(key string, urls []string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, _ *torrentMeta) error {
		return t.RemoveWebseeds(urls)
	})
}

// move relocates the payload and repoints the link. It runs in the
// background; the new save_path appears in the list when it is done, and a
// failure is reported in the torrent's error.
func (d *Daemon) move(key, destination string, associate bool) error {
	dest, err := d.validateDestination(destination)
	if err != nil {
		return err
	}
	d.mu.Lock()
	t, meta := d.findLocked(key)
	if t == nil {
		d.mu.Unlock()
		return errNotFound
	}
	if d.moving[t.ID()] {
		d.mu.Unlock()
		return errors.New("the torrent is already being moved")
	}
	if filepath.Clean(meta.SavePath) == dest {
		d.mu.Unlock()
		return nil
	}
	id := t.ID()
	stats := t.Stats()
	wasRunning := isRunning(stats.Status)
	// A torrent that is downloading but has no verified piece yet holds only
	// empty (sparse or preallocated) files: copying them to another disk
	// writes gigabytes of zeros and makes the engine re-check them all at the
	// destination. They are dropped and the engine recreates them there. Only a
	// running download qualifies: its piece table is authoritative, while a
	// stopped or verifying torrent may sit on data not checked yet.
	empty := !associate && stats.Status == torrent.Downloading && stats.Pieces.Have == 0 && stats.Bytes.Completed == 0
	d.moving[id] = true
	if err := t.Stop(); err != nil {
		delete(d.moving, id)
		d.mu.Unlock()
		return err
	}
	from := meta.SavePath
	name := stats.Name
	d.mu.Unlock()
	// Publish the "moving" state promptly: the read snapshot is refreshed by
	// the tick, which may otherwise be up to Tick seconds away.
	d.poke()

	go d.finishMove(id, t, from, dest, name, wasRunning, associate, empty)
	return nil
}

func (d *Daemon) finishMove(id string, t *torrent.Torrent, from, dest, name string, wasRunning, associate, empty bool) {
	// The engine stops asynchronously: wait until the files are closed.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && t.Stats().Status != torrent.Stopped {
		time.Sleep(200 * time.Millisecond)
	}
	var err error
	switch {
	case associate:
	case empty:
		err = discardEmptyPayload(from, dest, name)
	default:
		err = movePayload(from, dest, name)
	}
	if err == nil {
		err = d.pointLink(id, dest)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.moving, id)
	meta, ok := d.state.Torrents[id]
	if !ok {
		return
	}
	if err != nil {
		meta.Error = "move failed: " + err.Error()
		logf("moving «%s» to %s failed: %v", name, dest, err)
	} else {
		meta.SavePath = dest
		if empty {
			logf("«%s» moved to %s (nothing downloaded yet: its empty files are recreated there)", name, dest)
		} else {
			logf("«%s» moved to %s", name, dest)
		}
		if associate {
			if verifyErr := t.Verify(); verifyErr != nil {
				logf("verify after relocation of «%s»: %v", name, verifyErr)
			}
		} else if wasRunning {
			// The queue restarts it if it still deserves a slot.
			if startErr := t.Start(); startErr != nil {
				logf("restart after move of «%s»: %v", name, startErr)
			}
		}
	}
	d.saveLocked()
	d.poke()
}

// setConfig merges a partial configuration. Limits the engine reads only at start
// schedule a session restart, applied by the queue loop between moves.
func (d *Daemon) setConfig(patch map[string]json.RawMessage) (queue.Config, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	current, err := json.Marshal(d.state.Config)
	if err != nil {
		return d.state.Config, err
	}
	merged := map[string]json.RawMessage{}
	if err := json.Unmarshal(current, &merged); err != nil {
		return d.state.Config, err
	}
	for key, value := range patch {
		if _, known := merged[key]; !known {
			return d.state.Config, fmt.Errorf("unknown setting %q", key)
		}
		merged[key] = value
	}
	encoded, err := json.Marshal(merged)
	if err != nil {
		return d.state.Config, err
	}
	var next queue.Config
	if err := json.Unmarshal(encoded, &next); err != nil {
		return d.state.Config, err
	}
	next = next.Normalized()
	previous := d.state.Config
	d.state.Config = next
	// Speed limits and the cache size change in place: reopening the session
	// would drop every peer and stall the transfers for seconds.
	if previous.SpeedLimitDownload != next.SpeedLimitDownload || previous.SpeedLimitUpload != next.SpeedLimitUpload {
		d.applyEffectiveSpeedLimitsLocked(time.Now())
	}
	if previous.CacheMB != next.CacheMB || !queue.SameBoolPtr(previous.Auto, next.Auto) {
		d.cacheCheckedAt = time.Time{}
		d.cacheAppliedAt = time.Time{}
	}
	// Sequential is a session-wide order, like libtorrent's: applying it must
	// reach the torrents already running, not only the next ones.
	if previous.Sequential != next.Sequential {
		d.applySequentialLocked(next.Sequential)
	}
	// Peer limits, the cache TTL and preallocation are read by the engine only when
	// the session opens.
	if previous.MaxPeerDial != next.MaxPeerDial || previous.MaxPeerAccept != next.MaxPeerAccept ||
		previous.CacheTTLSecs != next.CacheTTLSecs || previous.Preallocate != next.Preallocate {
		d.restartPending = true
	}
	d.saveLocked()
	d.poke()
	return next, nil
}

// applySequentialLocked pushes the session-wide sequential flag onto every
// running torrent and records it in their metadata (Gextto's view).
func (d *Daemon) applySequentialLocked(sequential bool) {
	if d.session == nil {
		return
	}
	for _, t := range d.session.ListTorrents() {
		if err := t.SetSequential(sequential); err != nil {
			logf("cannot set sequential on %s: %v", t.ID(), err)
			continue
		}
		if meta, ok := d.state.Torrents[t.ID()]; ok {
			meta.Sequential = sequential
		}
	}
	d.dirty = true
}

func (d *Daemon) config() (queue.Config, queue.Limits, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state.Config, d.limits, d.restartPending
}

type daemonStats struct {
	Version         string        `json:"version"`
	UptimeSeconds   int64         `json:"uptime_seconds"`
	Torrents        int           `json:"torrents"`
	Downloading     int           `json:"downloading"`
	Seeding         int           `json:"seeding"`
	Queued          int           `json:"queued"`
	Stalled         int           `json:"stalled"`
	Paused          int           `json:"paused"`
	Slow            int           `json:"slow"`
	Moving          int           `json:"moving"`
	DownloadRate    int64         `json:"download_rate"`
	UploadRate      int64         `json:"upload_rate"`
	Peers           int           `json:"peers"`
	PortsAvailable  int           `json:"ports_available"`
	ActiveDownloads int           `json:"active_downloads"`
	ActiveSeeds     int           `json:"active_seeds"`
	ActiveLimit     int           `json:"active_limit"`
	RestartPending  bool          `json:"restart_pending"`
	Ratio           float64       `json:"ratio"`
	PeerPort        int           `json:"peer_port"`
	ListenAddress   string        `json:"listen_address"`
	PortMapping     portMapStatus `json:"port_mapping"`
	IPFilterRules   int           `json:"ip_filter_rules"`
	IPFilterPath    string        `json:"ip_filter_path,omitempty"`
	Encryption      int           `json:"encryption"`
	Proxy           bool          `json:"proxy"`
	DHT             bool          `json:"dht"`
	UTP             bool          `json:"utp"`
	CacheReadMB     int64         `json:"cache_read_mb"`
	CacheWriteMB    int64         `json:"cache_write_mb"`
	CacheAuto       bool          `json:"cache_auto"`
	CacheReason     string        `json:"cache_reason,omitempty"`
	CacheStorage    string        `json:"cache_storage,omitempty"`
	MemAvailableMB  int64         `json:"mem_available_mb,omitempty"`
	Preallocate     bool          `json:"preallocate"`
	LSD             lsdStatus     `json:"lsd"`
	DiskFreeBytes   int64         `json:"disk_free_bytes"`
	DiskTotalBytes  int64         `json:"disk_total_bytes"`
	// Session holds the engine's session counters (cache, disk, transfer).
	Session map[string]int64 `json:"session"`
	// PeerErrors counts the peer/tracker errors demoted by the log filter since
	// start-up, by category. Useful to tell a chatty swarm from a real problem
	// without flooding the log.
	PeerErrors map[string]int64 `json:"peer_errors,omitempty"`
}

func (d *Daemon) stats() daemonStats {
	views := d.snapshotViews()
	// statfs on the download dir can block on a network mount: compute it
	// outside d.mu.
	diskFreeBytes, diskTotalBytes := diskFree(d.opts.DownloadDir)
	if diskTotalBytes == 0 {
		diskFreeBytes, diskTotalBytes = diskFree(d.opts.DataDir)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := daemonStats{
		Version:         runtimeVersion(),
		UptimeSeconds:   int64(time.Since(d.startedAt).Seconds()),
		Torrents:        len(views),
		ActiveDownloads: d.limits.Downloads,
		ActiveSeeds:     d.limits.Seeds,
		ActiveLimit:     d.limits.Limit,
		RestartPending:  d.restartPending,
		PeerPort:        d.peerPort,
		ListenAddress:   d.listenHost,
		PortMapping:     d.mapper.status(),
		IPFilterRules:   d.ipFilterRules,
		IPFilterPath:    d.ipFilterPath,
		Encryption:      d.opts.Network.Encryption,
		Proxy:           d.opts.Network.Proxy != "",
		DHT:             d.opts.Network.DHT && d.opts.Network.Proxy == "",
		LSD:             d.lsd.status(),
		Preallocate:     d.state.Config.Preallocate,
	}
	if peerErrors := peerNoiseCounters.totals(); len(peerErrors) > 0 {
		out.PeerErrors = peerErrors
	}
	read, write, auto := cacheSizes(d.state.Config, memoryTotal())
	if d.cacheRead > 0 {
		read, write = d.cacheRead, d.cacheWrite
	}
	out.CacheReadMB, out.CacheWriteMB, out.CacheAuto = read/mib, write/mib, auto
	out.CacheReason, out.CacheStorage, out.MemAvailableMB = d.cacheReason, d.cacheClass, memoryAvailable()/mib
	out.DiskFreeBytes, out.DiskTotalBytes = diskFreeBytes, diskTotalBytes
	if out.LSD.Error == "" && d.lsdError != "" {
		out.LSD.Error = d.lsdError
	}
	var downloaded, uploaded int64
	for _, view := range views {
		switch view.State {
		case "downloading", "downloading_metadata":
			out.Downloading++
		case "seeding":
			out.Seeding++
		case "stalled":
			out.Stalled++
		case "moving":
			out.Moving++
		case "paused":
			if view.AutoManaged {
				out.Queued++
			} else {
				out.Paused++
			}
		}
		if view.Slow {
			out.Slow++
		}
		out.DownloadRate += view.DownloadRate
		out.UploadRate += view.UploadRate
		downloaded += view.Downloaded
		uploaded += view.Uploaded
	}
	if downloaded > 0 {
		out.Ratio = float64(uploaded) / float64(downloaded)
	}
	if d.session != nil {
		s := d.session.Stats()
		out.Peers = s.Peers
		out.PortsAvailable = s.PortsAvailable
		out.UTP = s.UTP
		out.DHT = s.DHT
		out.Session = map[string]int64{
			"uptime_seconds":           int64(s.Uptime.Seconds()),
			"torrents":                 int64(s.Torrents),
			"peers":                    int64(s.Peers),
			"blocklist_rules":          int64(s.BlockListRules),
			"read_cache_objects":       int64(s.ReadCacheObjects),
			"read_cache_bytes":         s.ReadCacheSize,
			"read_cache_hit_percent":   int64(s.ReadCacheUtilization),
			"reads_per_second":         int64(s.ReadsPerSecond),
			"reads_active":             int64(s.ReadsActive),
			"reads_pending":            int64(s.ReadsPending),
			"write_cache_objects":      int64(s.WriteCacheObjects),
			"write_cache_bytes":        s.WriteCacheSize,
			"write_cache_pending_keys": int64(s.WriteCachePendingKeys),
			"writes_per_second":        int64(s.WritesPerSecond),
			"writes_active":            int64(s.WritesActive),
			"writes_pending":           int64(s.WritesPending),
			"download_rate":            int64(s.SpeedDownload),
			"upload_rate":              int64(s.SpeedUpload),
			"disk_read_rate":           int64(s.SpeedRead),
			"disk_write_rate":          int64(s.SpeedWrite),
			"bytes_downloaded":         s.BytesDownloaded,
			"bytes_uploaded":           s.BytesUploaded,
			"bytes_read":               s.BytesRead,
			"bytes_written":            s.BytesWritten,
			"peers_outgoing_utp":       s.OutgoingUTP,
			"peers_outgoing_tcp":       s.OutgoingTCP,
			"peers_incoming_utp":       s.IncomingUTP,
			"peers_incoming_tcp":       s.IncomingTCP,
			"peers_tcp":                int64(s.TCPPeers),
			"peers_utp":                int64(s.UTPPeers),
			"read_ops_total":           s.ReadOpsTotal,
			"write_ops_total":          s.WriteOpsTotal,
			"disk_queue_depth":         int64(s.ReadsActive + s.ReadsPending + s.WritesActive + s.WritesPending),
			"peer_wire_downloaded":     s.PeerWireDownloaded,
			"peer_wire_uploaded":       s.PeerWireUploaded,
		}
		if overhead := (s.PeerWireDownloaded - s.BytesDownloaded) + (s.PeerWireUploaded - s.BytesUploaded); overhead > 0 {
			out.Session["protocol_overhead_bytes"] = overhead
		}
		for key, value := range s.DHTStats {
			out.Session["dht_"+key] = value
		}
		out.Session["lsd_peers_found"] = out.LSD.PeersFound
	}
	return out
}

// ---------------------------------------------------------------------------
// Categories and tags (standalone, qBittorrent-style)
// ---------------------------------------------------------------------------

// mergeTagNames appends new tag names, dropping empties and duplicates.
func mergeTagNames(existing, add []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(add))
	for _, list := range [][]string{existing, add} {
		for _, tag := range list {
			tag = strings.TrimSpace(tag)
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out
}

// dropTagNames removes the given tag names.
func dropTagNames(existing, remove []string) []string {
	drop := map[string]bool{}
	for _, tag := range remove {
		drop[strings.TrimSpace(tag)] = true
	}
	out := make([]string, 0, len(existing))
	for _, tag := range existing {
		if !drop[tag] {
			out = append(out, tag)
		}
	}
	return out
}

// setCategory assigns (or, with "", clears) the category of a torrent.
func (d *Daemon) setCategory(key, category string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, meta := d.findLocked(key)
	if meta == nil {
		return errNotFound
	}
	meta.Category = strings.TrimSpace(category)
	d.dirty = true
	d.saveLocked()
	d.poke()
	return nil
}

func (d *Daemon) addTags(key string, tags []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, meta := d.findLocked(key)
	if meta == nil {
		return errNotFound
	}
	meta.Tags = mergeTagNames(meta.Tags, tags)
	d.state.Tags = mergeTagNames(d.state.Tags, tags)
	d.dirty = true
	d.saveLocked()
	d.poke()
	return nil
}

func (d *Daemon) removeTags(key string, tags []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, meta := d.findLocked(key)
	if meta == nil {
		return errNotFound
	}
	meta.Tags = dropTagNames(meta.Tags, tags)
	d.dirty = true
	d.saveLocked()
	d.poke()
	return nil
}

func (d *Daemon) createCategory(name, savePath string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.Categories == nil {
		d.state.Categories = map[string]string{}
	}
	d.state.Categories[name] = strings.TrimSpace(savePath)
	d.dirty = true
	d.saveLocked()
	d.poke()
}

// removeCategories deletes categories and clears them on the torrents that
// used them.
func (d *Daemon) removeCategories(names []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, name := range names {
		name = strings.TrimSpace(name)
		delete(d.state.Categories, name)
		for _, meta := range d.state.Torrents {
			if meta.Category == name {
				meta.Category = ""
			}
		}
	}
	d.dirty = true
	d.saveLocked()
	d.poke()
}

func (d *Daemon) createTags(tags []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state.Tags = mergeTagNames(d.state.Tags, tags)
	d.dirty = true
	d.saveLocked()
	d.poke()
}

// deleteTags forgets tags everywhere: the known set and every torrent.
func (d *Daemon) deleteTags(tags []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state.Tags = dropTagNames(d.state.Tags, tags)
	for _, meta := range d.state.Torrents {
		meta.Tags = dropTagNames(meta.Tags, tags)
	}
	d.dirty = true
	d.saveLocked()
	d.poke()
}

func (d *Daemon) categoriesSnapshot() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]string, len(d.state.Categories))
	for name, path := range d.state.Categories {
		out[name] = path
	}
	return out
}

func (d *Daemon) tagsSnapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := append([]string(nil), d.state.Tags...)
	sort.Strings(out)
	return out
}

// categorySavePath is the configured save path of a category, if any.
func (d *Daemon) categorySavePath(name string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state.Categories[strings.TrimSpace(name)]
}

// setTags replaces a torrent's tags and keeps the known-tag set in sync.
func (d *Daemon) setTags(key string, tags []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, meta := d.findLocked(key)
	if meta == nil {
		return errNotFound
	}
	meta.Tags = mergeTagNames(nil, tags)
	d.state.Tags = mergeTagNames(d.state.Tags, tags)
	d.dirty = true
	d.saveLocked()
	d.poke()
	return nil
}
