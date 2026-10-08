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
	"syscall"
	"time"

	"github.com/cenkalti/rain/v2/torrent"
)

// diskFree returns the free (available to the user) and total bytes of the
// filesystem holding path. Zeroes when the path cannot be stat'ed.
func diskFree(path string) (free, total int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	block := int64(st.Bsize)
	return int64(st.Bavail) * block, int64(st.Blocks) * block
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
	// Fingerprint is reported by /api/v1/health so Gextto can adopt a daemon
	// it started earlier with the same binary and options.
	Fingerprint string
	// GexttoLog is the Gextto log shown in the web page (empty = no tab).
	GexttoLog string
	// IPFilterSource is the IP filter URL or path configured in Gextto.
	IPFilterSource string
	Tick           time.Duration
	ProbeWindow    time.Duration
}

// torrentMeta is what gx-torrent knows about a torrent beyond rain.
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
	Sequential bool      `json:"sequential,omitempty"`
	FirstLast  bool      `json:"first_last,omitempty"`
	RotatedAt  time.Time `json:"rotated_at,omitzero"`
	SeedRatio  float64   `json:"seed_ratio"`
	SeedDays   int64     `json:"seed_days"`
	SwarmSeeds int       `json:"swarm_seeds"`
	SwarmPeers int       `json:"swarm_peers"`
	// DoneBytes is the last verified amount, reported while rain cannot
	// compute it (a stopped torrent has no piece table).
	DoneBytes int64 `json:"done_bytes,omitempty"`
	// FilePriorities: one entry per file, 0 = skip (empty = all wanted).
	FilePriorities []int  `json:"file_priorities,omitempty"`
	Version        string `json:"version,omitempty"`
	Error          string `json:"error,omitempty"`
}

type persistedState struct {
	Version  int                     `json:"version"`
	Config   QueueConfig             `json:"config"`
	NextPos  int64                   `json:"next_pos"`
	Torrents map[string]*torrentMeta `json:"torrents"`
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

// Daemon owns the rain session and the queue.
type Daemon struct {
	opts Options

	mu             sync.Mutex
	session        *torrent.Session
	state          persistedState
	runtime        map[string]*runtimeInfo
	moving         map[string]bool
	dyn            dynamicQueue
	limits         effectiveLimits
	restartPending bool
	dirty          bool
	startedAt      time.Time

	listenHost    string
	peerPort      int
	mapper        *portMapper
	lsd           *lsdService
	lsdError      string
	ipFilterPath  string
	ipFilterRules int

	// Adaptive disk cache (see cache.go). rain reads the cache sizes when the
	// session is created, so a retune reopens the session on a coarse cadence.
	cacheRead      int64
	cacheWrite     int64
	cacheReason    string
	cacheClass     string
	cacheCheckedAt time.Time
	cacheAppliedAt time.Time

	// selection is read by rain from torrent goroutines: it has its own
	// lock and must never wait for d.mu.
	selMu     sync.RWMutex
	selection map[string][]bool

	wake chan struct{}
}

func newDaemon(opts Options) (*Daemon, error) {
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
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.openSessionLocked(); err != nil {
		return nil, err
	}
	d.reconcileLocked()
	d.saveLocked()
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
	d.state = persistedState{Version: 1, Config: defaultQueueConfig(), Torrents: map[string]*torrentMeta{}}
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
	loaded.Config = loaded.Config.normalized()
	d.state = loaded
	return nil
}

// saveLocked writes the state atomically. Failures are logged: the session
// keeps working and the next change retries.
func (d *Daemon) saveLocked() {
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
	// first tick. If rain resumed them itself (ResumeOnStartup), it would start a
	// torrent that must stay parked/paused before reconcileLocked can stop it,
	// and starting a torrent creates its destination files: a parked torrent
	// whose payload was moved or removed would get zero-filled placeholders
	// written at the old path. The queue is the single owner of start/stop.
	cfg.ResumeOnStartup = false
	cfg.RPCEnabled = false
	// rain rewrites every torrent's stats and bitfield in one fsync'd bolt
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
	cfg.CustomLogHandler = rainLogHandler(d.opts.Debug)
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

// restartSessionLocked applies settings rain reads only at session creation
// (peer limits, cache TTL, preallocation). Torrents resume exactly as they were: rain
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
	logf("session restarted to apply new settings")
}

// reconcileLocked aligns gx-torrent's state with the torrents rain loaded.
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
		// Torrents parked or paused must not run, whatever rain restored.
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

// findLocked looks a torrent up by info hash (case-insensitive) or rain ID.
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

// statsLocked returns rain's stats with the completed bytes fixed: rain
// reports 0 for a stopped torrent (its piece table exists only while it
// runs), which would make a finished, paused torrent look empty.
func (d *Daemon) statsLocked(t *torrent.Torrent) torrent.Stats {
	stats := t.Stats()
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

func (d *Daemon) tick(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session == nil {
		return
	}
	if d.restartPending && len(d.moving) == 0 {
		d.restartSessionLocked()
		if d.session == nil {
			return
		}
	}
	cfg := d.state.Config
	slowAfter := time.Duration(cfg.SlowAfterSecs) * time.Second

	torrents := d.session.ListTorrents()
	handles := make(map[string]*torrent.Torrent, len(torrents))
	items := make([]queueItem, 0, len(torrents))
	var aggregate int64
	queued := 0
	activeDownloads, activeSeeds := 0, 0
	for _, t := range torrents {
		id := t.ID()
		handles[id] = t
		meta := d.metaLocked(t)
		rt := d.runtimeLocked(id)
		stats := d.statsLocked(t)
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
				_ = t.Stop()
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
			d.refreshSwarmLocked(t, meta, rt, now)
		} else {
			rt.startedAt = time.Time{}
			rt.lastActive = time.Time{}
			rt.slow = false
			rt.numSeeds = 0
		}

		probing := !meta.ProbeUntil.IsZero()
		blocked := meta.UserPaused || meta.Parked || meta.Error != "" || d.moving[id]
		item := queueItem{
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

	d.limits = d.dyn.limits(cfg, aggregate, queued, now)
	d.adaptCacheLocked(now, activeDownloads, activeSeeds, aggregate)
	plan := planQueue(items, d.limits, cfg, now)
	for _, id := range plan.Rotate {
		if meta, ok := d.state.Torrents[id]; ok {
			meta.RotatedAt = now
			meta.Pos = d.nextPosLocked()
			d.dirty = true
			logf("%s moves no data: set aside so another torrent can run", nameOf(handles[id]))
		}
	}
	for _, id := range plan.Stop {
		if t := handles[id]; t != nil {
			if err := t.Stop(); err != nil {
				logf("queue stop %s: %v", id, err)
			}
		}
	}
	for _, id := range plan.Start {
		t := handles[id]
		if t == nil {
			continue
		}
		if err := t.Start(); err != nil {
			logf("queue start %s: %v", id, err)
			continue
		}
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
}

func nameOf(t *torrent.Torrent) string {
	if t == nil {
		return "torrent"
	}
	return fmt.Sprintf("«%s»", t.Stats().Name)
}

// refreshSwarmLocked samples peers every tick and tracker scrapes once a
// minute; the swarm size survives a stop so a parked torrent keeps its
// diagnosis.
func (d *Daemon) refreshSwarmLocked(t *torrent.Torrent, meta *torrentMeta, rt *runtimeInfo, now time.Time) {
	seeds := 0
	for _, peer := range t.Peers() {
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
	for _, tracker := range t.Trackers() {
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
	Hash            string  `json:"hash"`
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	State           string  `json:"state"`
	SavePath        string  `json:"save_path"`
	Progress        float64 `json:"progress"`
	TotalSize       int64   `json:"total_size"`
	TotalDone       int64   `json:"total_done"`
	DownloadRate    int64   `json:"download_rate"`
	UploadRate      int64   `json:"upload_rate"`
	Downloaded      int64   `json:"downloaded"`
	Uploaded        int64   `json:"uploaded"`
	SeedingSeconds  int64   `json:"seeding_seconds"`
	ActiveSeconds   int64   `json:"active_seconds"`
	ETASeconds      int64   `json:"eta_seconds"`
	CurrentTracker  string  `json:"current_tracker,omitempty"`
	QueuePosition   int     `json:"queue_position"`
	NumPeers        int     `json:"num_peers"`
	NumSeeds        int     `json:"num_seeds"`
	NumComplete     int     `json:"num_complete"`
	NumIncomplete   int     `json:"num_incomplete"`
	SeedRatio       float64 `json:"seed_ratio"`
	SeedDays        int64   `json:"seed_days"`
	HasMetadata     bool    `json:"has_metadata"`
	AutoManaged     bool    `json:"auto_managed"`
	Sequential      bool    `json:"sequential"`
	FirstLast       bool    `json:"first_last"`
	Pinned          bool    `json:"pinned"`
	Parked          bool    `json:"parked"`
	Probing         bool    `json:"probing"`
	Slow            bool    `json:"slow"`
	Private         bool    `json:"private"`
	TorrentVersion  string  `json:"torrent_version"`
	PiecesTotal     uint32  `json:"pieces_total"`
	PiecesHave      uint32  `json:"pieces_have"`
	PiecesAvailable uint32  `json:"pieces_available"`
	PiecesChecked   uint32  `json:"pieces_checked"`
	PieceLength     uint32  `json:"piece_length"`
	Wasted          int64   `json:"wasted"`
	Allocated       int64   `json:"allocated"`
	FileCount       int     `json:"file_count"`
	Error           string  `json:"error,omitempty"`
	AddedAt         int64   `json:"added_at"`
	CompletedAt     int64   `json:"completed_at,omitempty"`
}

// stateFor maps rain's status and gx-torrent's flags onto Gextto's states.
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

func (d *Daemon) infoLocked(t *torrent.Torrent, queuePos map[string]int) torrentInfo {
	stats := d.statsLocked(t)
	meta := d.metaLocked(t)
	rt := d.runtimeLocked(t.ID())
	moving := d.moving[t.ID()]
	if meta.Version == "" && stats.Status != torrent.DownloadingMetadata && stats.Bytes.Total > 0 {
		if data, err := t.Torrent(); err == nil && len(data) > 0 {
			// rain handles v1 and the v1 side of hybrid torrents; the raw
			// metainfo carries "meta version" only when a v2 layer is present.
			if bytes.Contains(data, []byte("12:meta versioni2e")) {
				meta.Version = "hybrid"
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
		Hash:            t.InfoHash().String(),
		ID:              t.ID(),
		Name:            stats.Name,
		State:           stateFor(meta, stats, moving),
		SavePath:        meta.SavePath,
		Progress:        progress,
		TotalSize:       stats.Bytes.Total,
		TotalDone:       stats.Bytes.Completed,
		DownloadRate:    int64(stats.Speed.Download),
		UploadRate:      int64(stats.Speed.Upload),
		Downloaded:      stats.Bytes.Downloaded,
		Uploaded:        stats.Bytes.Uploaded,
		SeedingSeconds:  int64(stats.SeededFor.Seconds()),
		ETASeconds:      eta,
		QueuePosition:   pos,
		NumPeers:        stats.Peers.Total,
		NumSeeds:        rt.numSeeds,
		CurrentTracker:  rt.tracker,
		NumComplete:     meta.SwarmSeeds,
		NumIncomplete:   meta.SwarmPeers,
		SeedRatio:       meta.SeedRatio,
		SeedDays:        meta.SeedDays,
		HasMetadata:     stats.Status != torrent.DownloadingMetadata && stats.Bytes.Total > 0,
		AutoManaged:     !meta.UserPaused && !meta.Parked && !meta.Pinned,
		Sequential:      meta.Sequential,
		FirstLast:       meta.FirstLast,
		Pinned:          meta.Pinned,
		Parked:          meta.Parked,
		Probing:         !meta.ProbeUntil.IsZero(),
		Slow:            rt.slow,
		Private:         stats.Private,
		TorrentVersion:  torrentVersion,
		PiecesTotal:     stats.Pieces.Total,
		PiecesHave:      stats.Pieces.Have,
		PiecesAvailable: stats.Pieces.Available,
		PiecesChecked:   stats.Pieces.Checked,
		PieceLength:     stats.PieceLength,
		Wasted:          stats.Bytes.Wasted,
		Allocated:       stats.Bytes.Allocated,
		FileCount:       stats.FileCount,
		Error:           meta.Error,
		AddedAt:         meta.AddedAt.Unix(),
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
func (d *Daemon) queuePositionsLocked(torrents []*torrent.Torrent) map[string]int {
	type entry struct {
		id  string
		pos int64
	}
	var list []entry
	for _, t := range torrents {
		meta := d.metaLocked(t)
		if meta.UserPaused || meta.Parked || meta.Pinned || meta.Error != "" {
			continue
		}
		if isComplete(d.statsLocked(t)) {
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

func (d *Daemon) list() []torrentInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.session == nil {
		return []torrentInfo{}
	}
	torrents := d.session.ListTorrents()
	positions := d.queuePositionsLocked(torrents)
	out := make([]torrentInfo, 0, len(torrents))
	for _, t := range torrents {
		out = append(out, d.infoLocked(t, positions))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hash < out[j].Hash })
	if d.dirty {
		d.saveLocked()
	}
	return out
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
	// FirstLast downloads the ends of every file first (gextto fork).
	FirstLast bool
	SeedRatio float64
	SeedDays  int64
}

// add registers a torrent stopped and lets the queue start it. A torrent
// already in the session is reported with existing=true and left untouched.
func (d *Daemon) add(req addRequest) (string, bool, error) {
	if req.Magnet != "" {
		// rain reads only the first xt: keep the v1 hash in front so a hybrid
		// magnet whose btmh comes first is still downloadable.
		req.Magnet = normalizeMagnetForRain(req.Magnet)
	}
	dest, err := d.validateDestination(req.Destination)
	if err != nil {
		return "", false, err
	}
	if (req.Magnet != "" && magnetIsV2Only(req.Magnet)) || (req.Magnet == "" && torrentIsV2Only(req.TorrentData)) {
		return "", false, errV2Only
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
	opt := &torrent.AddTorrentOptions{ID: id, Stopped: true, Sequential: req.Sequential || d.state.Config.Sequential, FirstLast: req.FirstLast}
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
	// rain accepts the same info hash twice: keep the first one. Removing the
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
		SeedRatio:  req.SeedRatio, SeedDays: req.SeedDays, SwarmSeeds: -1, SwarmPeers: -1,
	}
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

// resume hands the torrent back to the queue (it starts at once if a slot is
// free) and clears a previous error.
func (d *Daemon) resume(key string) error {
	return d.withTorrent(key, func(t *torrent.Torrent, meta *torrentMeta) error {
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
func (d *Daemon) setSeedLimits(key string, ratio *float64, days *int64) error {
	return d.withTorrent(key, func(_ *torrent.Torrent, meta *torrentMeta) error {
		if ratio != nil {
			meta.SeedRatio = *ratio
		}
		if days != nil {
			meta.SeedDays = *days
		}
		return nil
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
	// writes gigabytes of zeros and makes rain re-check them all at the
	// destination. They are dropped and rain recreates them there. Only a
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

	go d.finishMove(id, t, from, dest, name, wasRunning, associate, empty)
	return nil
}

func (d *Daemon) finishMove(id string, t *torrent.Torrent, from, dest, name string, wasRunning, associate, empty bool) {
	// rain stops asynchronously: wait until the files are closed.
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

// setConfig merges a partial configuration. Limits rain reads only at start
// schedule a session restart, applied by the queue loop between moves.
func (d *Daemon) setConfig(patch map[string]json.RawMessage) (QueueConfig, error) {
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
	var next QueueConfig
	if err := json.Unmarshal(encoded, &next); err != nil {
		return d.state.Config, err
	}
	next = next.normalized()
	previous := d.state.Config
	d.state.Config = next
	// Speed limits and the cache size change in place: reopening the session
	// would drop every peer and stall the transfers for seconds.
	if previous.SpeedLimitDownload != next.SpeedLimitDownload || previous.SpeedLimitUpload != next.SpeedLimitUpload {
		if d.session != nil {
			d.session.SetSpeedLimits(next.SpeedLimitDownload, next.SpeedLimitUpload)
		}
		logf("speed limits set to %d KiB/s download, %d KiB/s upload (0 = unlimited)", next.SpeedLimitDownload, next.SpeedLimitUpload)
	}
	if previous.CacheMB != next.CacheMB || !sameBoolPtr(previous.Auto, next.Auto) {
		d.cacheCheckedAt = time.Time{}
		d.cacheAppliedAt = time.Time{}
	}
	// Peer limits, the cache TTL and preallocation are read by rain only when
	// the session opens.
	if previous.MaxPeerDial != next.MaxPeerDial || previous.MaxPeerAccept != next.MaxPeerAccept ||
		previous.CacheTTLSecs != next.CacheTTLSecs || previous.Preallocate != next.Preallocate {
		d.restartPending = true
	}
	d.saveLocked()
	d.poke()
	return next, nil
}

func (d *Daemon) config() (QueueConfig, effectiveLimits, bool) {
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
	// Session holds rain's session counters (cache, disk, transfer).
	Session map[string]int64 `json:"session"`
}

func (d *Daemon) stats() daemonStats {
	views := d.list()
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
	read, write, auto := cacheSizes(d.state.Config, memoryTotal())
	if d.cacheRead > 0 {
		read, write = d.cacheRead, d.cacheWrite
	}
	out.CacheReadMB, out.CacheWriteMB, out.CacheAuto = read/mib, write/mib, auto
	out.CacheReason, out.CacheStorage, out.MemAvailableMB = d.cacheReason, d.cacheClass, memoryAvailable()/mib
	out.DiskFreeBytes, out.DiskTotalBytes = diskFree(d.opts.DownloadDir)
	if out.DiskTotalBytes == 0 {
		out.DiskFreeBytes, out.DiskTotalBytes = diskFree(d.opts.DataDir)
	}
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
