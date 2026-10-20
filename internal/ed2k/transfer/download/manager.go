package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/ratelimit"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/partfile"
)

// Discoverer finds peers for a file. *ed2k.Client implements it.
type Discoverer interface {
	Sources(ctx context.Context, hash string, size int64) ([]ed2k.PeerSource, error)
}

// Callbacker sends an eMule direct callback to a LowID source. *ed2k.Client
// implements it; a fake can implement it in tests.
type Callbacker interface {
	SendDirectCallback(ctx context.Context, source ed2k.PeerSource, ourPort uint16) error
	SendBuddyCallback(ctx context.Context, source ed2k.PeerSource, fileHash ed2k.ID, ourPort uint16) error
	SendServerCallback(ctx context.Context, source ed2k.PeerSource) error
	CallbackIdentity() (ed2k.ID, uint16)
	CanCallback() bool
}

// ManagerConfig tunes the transfer queue.
type ManagerConfig struct {
	Fetch       Config
	MaxSources  int
	FetchBudget time.Duration
	// RetryDelay is the pause between queue rounds. Queued transfers keep
	// retrying until the manager is closed.
	RetryDelay time.Duration
	// MaxPeers is how many sources download disjoint parts in parallel.
	MaxPeers int
	// OnComplete is called after a transfer is verified and renamed. The
	// daemon uses it to add the file to the upload index.
	OnComplete func(Result)
	// StatePath persists the transfer rows across daemon restarts. Empty
	// keeps the manager in memory only.
	StatePath string
	// MaxActive caps how many transfers download at once; 0 means no cap.
	MaxActive int
	// GlobalLimiter caps all transfers together. Nil is unlimited.
	GlobalLimiter *ratelimit.Bucket
}

// Transfer is the public state of one eD2k download.
type Transfer struct {
	ID       string    `json:"id"`
	Hash     string    `json:"hash"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	State    string    `json:"state"`
	Done     int64     `json:"done"`
	Sources  int       `json:"sources"`
	Priority string    `json:"priority"`
	LimitKB  int       `json:"limit_kb"`
	Error    string    `json:"error,omitempty"`
	Path     string    `json:"path,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

// Priorities accepted by SetPriority.
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
)

const (
	StateDiscovering = "discovering"
	StateDownloading = "downloading"
	StateQueued      = "queued"
	StatePaused      = "paused"
	StateCompleted   = "completed"
	StateError       = "error"
)

type managedTransfer struct {
	Transfer
	cancel  context.CancelFunc
	link    Link
	running bool
	limiter *ratelimit.Bucket
}

// Manager runs eD2k transfers in the background and keeps their state for the
// daemon API. It is not wired into cmd/gx-ed2k yet.
type Manager struct {
	cfg      ManagerConfig
	discover Discoverer

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu        sync.Mutex
	transfers map[string]*managedTransfer
	active    int
	closed    bool

	callbackMu sync.Mutex
	pending    map[string]chan net.Conn

	addedMu sync.Mutex
	added   map[string][]ed2k.PeerSource

	aichMu sync.Mutex
	aich   map[string][20]byte
}

// NewManager creates a manager. The context bounds every transfer.
func NewManager(ctx context.Context, cfg ManagerConfig, discover Discoverer) *Manager {
	if cfg.MaxSources <= 0 {
		cfg.MaxSources = 50
	}
	if cfg.FetchBudget <= 0 {
		cfg.FetchBudget = 2 * time.Hour
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 30 * time.Second
	}
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = 3
	}
	managerCtx, cancel := context.WithCancel(ctx)
	manager := &Manager{
		cfg: cfg, discover: discover, ctx: managerCtx, cancel: cancel,
		transfers: make(map[string]*managedTransfer),
		pending:   make(map[string]chan net.Conn),
		added:     make(map[string][]ed2k.PeerSource),
		aich:      make(map[string][20]byte),
	}
	manager.load()
	return manager
}

// Add starts a transfer, or returns the existing one for the same hash.
func (m *Manager) Add(link Link) (Transfer, error) {
	if link.Hash == (ed2k.ID{}) || link.Size <= 0 {
		return Transfer{}, errors.New("download: invalid link")
	}
	id := link.Hash.Hex()
	m.mu.Lock()
	if existing, ok := m.transfers[id]; ok {
		if existing.State != StateError {
			transfer := existing.Transfer
			m.mu.Unlock()
			return transfer, nil
		}
		// An interrupted transfer is restarted explicitly by adding it again.
		existing.link = link
		existing.State = StateDiscovering
		existing.Error = ""
		existing.Updated = time.Now()
		m.saveLocked()
		m.dispatchLocked()
		transfer := existing.Transfer
		m.mu.Unlock()
		return transfer, nil
	}
	if m.closed {
		m.mu.Unlock()
		return Transfer{}, errors.New("download: manager closed")
	}
	now := time.Now()
	managed := &managedTransfer{
		Transfer: Transfer{
			ID: id, Hash: id, Name: link.Name, Size: link.Size,
			State: StateDiscovering, Created: now, Updated: now,
			Priority: PriorityNormal,
		},
		link: link,
	}
	m.transfers[id] = managed
	m.dispatchLocked()
	transfer := managed.Transfer
	m.mu.Unlock()
	return transfer, nil
}

// dispatchLocked starts pending transfers, highest priority and oldest first,
// while a slot is free. The caller holds m.mu.
func (m *Manager) dispatchLocked() {
	for !m.closed {
		if m.cfg.MaxActive > 0 && m.active >= m.cfg.MaxActive {
			return
		}
		next := m.nextPendingLocked()
		if next == nil {
			return
		}
		m.startLocked(next)
	}
}

func (m *Manager) nextPendingLocked() *managedTransfer {
	var best *managedTransfer
	for _, transfer := range m.transfers {
		if transfer.running || transfer.State == StateCompleted ||
			transfer.State == StatePaused || transfer.State == StateError {
			continue
		}
		if best == nil || priorityRank(transfer.Priority) < priorityRank(best.Priority) ||
			(priorityRank(transfer.Priority) == priorityRank(best.Priority) && transfer.Created.Before(best.Created)) {
			best = transfer
		}
	}
	return best
}

func priorityRank(priority string) int {
	switch priority {
	case PriorityHigh:
		return 0
	case PriorityLow:
		return 2
	default:
		return 1
	}
}

func (m *Manager) startLocked(transfer *managedTransfer) {
	ctx, cancel := context.WithCancel(m.ctx)
	transfer.cancel = cancel
	transfer.running = true
	m.active++
	if transfer.LimitKB > 0 && transfer.limiter == nil {
		transfer.limiter = ratelimit.New(int64(transfer.LimitKB) * 1024)
	}
	id, link := transfer.ID, transfer.link
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx, id, link)
		m.finished(id)
	}()
}

func (m *Manager) finished(id string) {
	m.mu.Lock()
	if transfer, ok := m.transfers[id]; ok && transfer.running {
		transfer.running = false
		if m.active > 0 {
			m.active--
		}
		m.dispatchLocked()
	}
	m.mu.Unlock()
}

func (m *Manager) run(ctx context.Context, id string, link Link) {
	sources, err := m.discover.Sources(ctx, link.Hash.Hex(), link.Size)
	if err != nil && len(sources) == 0 {
		m.fail(id, fmt.Errorf("sources: %w", err))
		return
	}
	if len(sources) == 0 {
		m.fail(id, errors.New("no sources found"))
		return
	}
	if len(sources) > m.cfg.MaxSources {
		sources = sources[:m.cfg.MaxSources]
	}
	m.update(id, func(t *Transfer) { t.Sources = len(sources) })

	partPath := filepath.Join(m.cfg.Fetch.TempDir, link.Hash.Hex()+".part")
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return
		}
		m.update(id, func(t *Transfer) { t.State = StateDownloading; t.Error = "" })
		if extra := m.takeSources(id); len(extra) > 0 {
			sources = append(sources, extra...)
			if len(sources) > m.cfg.MaxSources {
				sources = sources[:m.cfg.MaxSources]
			}
			m.update(id, func(t *Transfer) { t.Sources = len(sources) })
		}

		if _, statErr := os.Stat(partPath + ".met"); statErr != nil {
			if err := m.seedMetadata(ctx, id, link, sources); err != nil {
				m.fail(id, err)
				if !m.waitRetry(ctx) {
					return
				}
				if refreshed, err := m.discover.Sources(ctx, link.Hash.Hex(), link.Size); err == nil && len(refreshed) > 0 {
					sources = refreshed
					if len(sources) > m.cfg.MaxSources {
						sources = sources[:m.cfg.MaxSources]
					}
				}
				continue
			}
		}

		pf, err := partfile.Open(partPath)
		if err != nil {
			m.fail(id, err)
			return
		}
		missing := make([]int, 0, len(pf.Meta.Parts))
		for index := range pf.Meta.Parts {
			if !pf.Have(index) {
				missing = append(missing, index)
			}
		}
		_ = pf.Close()
		if len(missing) == 0 {
			m.complete(id, link, partPath)
			return
		}

		workers := len(sources)
		if workers > m.cfg.MaxPeers {
			workers = m.cfg.MaxPeers
		}
		if workers < 1 {
			workers = 1
		}
		type workerJob struct {
			source ed2k.PeerSource
			parts  map[int]bool
		}
		jobs := make([]workerJob, workers)
		for i := 0; i < workers; i++ {
			jobs[i] = workerJob{source: sources[i%len(sources)], parts: make(map[int]bool)}
		}
		for index, part := range missing {
			jobs[(index+1)%workers].parts[part] = true
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		var lastErr error
		for _, jobItem := range jobs {
			if len(jobItem.parts) == 0 {
				continue
			}
			wg.Add(1)
			go func(jobItem workerJob) {
				defer wg.Done()
				cfg := m.workerConfig(id)
				parts := jobItem.parts
				cfg.PartFilter = func(part int) bool { return parts[part] }
				cfg.Managed = true
				_, err := m.fetchWorker(ctx, jobItem.source, link, cfg)
				if err != nil && ctx.Err() == nil {
					mu.Lock()
					lastErr = err
					mu.Unlock()
				}
			}(jobItem)
		}
		wg.Wait()
		if ctx.Err() != nil {
			return
		}

		// Re-open and check whether this round closed the file.
		pf, err = partfile.Open(partPath)
		if err != nil {
			m.fail(id, err)
			return
		}
		complete := pf.Complete()
		_ = pf.Close()
		if complete {
			m.complete(id, link, partPath)
			return
		}
		if lastErr != nil {
			m.update(id, func(t *Transfer) { t.Error = lastErr.Error() })
		}
		if !m.waitRetry(ctx) {
			return
		}
		if refreshed, err := m.discover.Sources(ctx, link.Hash.Hex(), link.Size); err == nil && len(refreshed) > 0 {
			sources = refreshed
			if len(sources) > m.cfg.MaxSources {
				sources = sources[:m.cfg.MaxSources]
			}
			m.update(id, func(t *Transfer) { t.Sources = len(sources) })
		}
	}
}

func (m *Manager) takeSources(id string) []ed2k.PeerSource {
	m.addedMu.Lock()
	defer m.addedMu.Unlock()
	list := m.added[id]
	delete(m.added, id)
	return list
}

func (m *Manager) waitRetry(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(m.cfg.RetryDelay):
		return true
	}
}

func (m *Manager) workerConfig(id string) Config {
	cfg := m.cfg.Fetch
	m.mu.Lock()
	if transfer, ok := m.transfers[id]; ok {
		if transfer.LimitKB > 0 && transfer.limiter == nil {
			transfer.limiter = ratelimit.New(int64(transfer.LimitKB) * 1024)
		}
		cfg.Limiter = transfer.limiter
	}
	m.mu.Unlock()
	cfg.GlobalLimiter = m.cfg.GlobalLimiter
	if provider, ok := m.discover.(interface{ Stats() ed2k.Stats }); ok {
		cfg.ClientID = provider.Stats().ServerID
	}
	cfg.OnProgress = func(done, total int64) {
		m.update(id, func(t *Transfer) {
			if done > t.Done {
				t.Done = done
			}
			if total > 0 {
				t.Size = total
			}
		})
	}
	cfg.OnAICH = func(root [20]byte) {
		m.aichMu.Lock()
		m.aich[id] = root
		m.aichMu.Unlock()
	}
	cfg.OnSources = func(list []ed2k.PeerSource) {
		if filter := m.cfg.Fetch.IPFilter; filter != nil {
			kept := list[:0]
			for _, source := range list {
				if !filter.Blocked(source.IP) {
					kept = append(kept, source)
				}
			}
			list = kept
		}
		m.addedMu.Lock()
		m.added[id] = append(m.added[id], list...)
		m.addedMu.Unlock()
	}
	return cfg
}

func (m *Manager) fetchWorker(ctx context.Context, source ed2k.PeerSource, link Link, cfg Config) (*Result, error) {
	if m.cfg.Fetch.IPFilter.Blocked(source.IP) {
		return nil, errors.New("download: source blocked by the IP filter")
	}
	cfg.PeerHash = source.UserHash
	if source.Encryption&0x01 != 0 && source.UserHash != (ed2k.ID{}) {
		cfg.Obfuscation = true
	}
	if source.TCPPort == 0 {
		return m.fetchViaCallback(ctx, source, link, cfg)
	}
	return Fetch(ctx, fmt.Sprintf("%s:%d", source.IP.String(), source.TCPPort), link, cfg)
}

func (m *Manager) seedMetadata(ctx context.Context, id string, link Link, sources []ed2k.PeerSource) error {
	var lastErr error
	for _, source := range sources {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		cfg := m.workerConfig(id)
		cfg.Managed = true
		cfg.PartFilter = func(part int) bool { return part == 0 }
		if _, err := m.fetchWorker(ctx, source, link, cfg); err == nil {
			return nil
		} else {
			lastErr = err
			if m.cfg.Fetch.Logf != nil {
				m.cfg.Fetch.Logf("download: %s could not seed metadata: %v", source.IP, err)
			}
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no source provided the file metadata")
	}
	return lastErr
}

func (m *Manager) complete(id string, link Link, partPath string) {
	m.aichMu.Lock()
	root, hasRoot := m.aich[id]
	delete(m.aich, id)
	m.aichMu.Unlock()
	var aichRoot *[20]byte
	if hasRoot {
		aichRoot = &root
	}
	result, err := Finalize(partPath, link, link.Name, m.cfg.Fetch, aichRoot)
	if err != nil {
		m.fail(id, err)
		return
	}
	m.update(id, func(t *Transfer) {
		t.State = StateCompleted
		t.Done = t.Size
		t.Path = result.Path
		t.Error = ""
	})
	if m.cfg.OnComplete != nil {
		m.cfg.OnComplete(*result)
	}
}

func (m *Manager) fetchViaCallback(ctx context.Context, source ed2k.PeerSource, link Link, cfg Config) (*Result, error) {
	callback, ok := m.discover.(Callbacker)
	if !ok {
		return nil, errors.New("download: direct callback is not available")
	}
	if !callback.CanCallback() {
		return nil, errors.New("download: our TCP port is not reachable (LowID), callback skipped")
	}
	direct := source.UDPPort != 0 && source.IP != nil
	buddy := source.BuddyID != (ed2k.ID{})
	serverCallback := source.LowID && source.ClientID != 0 && source.ServerIP != nil && source.ServerPort != 0
	if !direct && !buddy && !serverCallback {
		return nil, errors.New("download: LowID source has no UDP port, buddy or server callback")
	}
	ip := ""
	if source.IP != nil {
		ip = source.IP.String()
	}
	channel, done := m.expectCallback(ip)
	defer done()
	_, ourPort := callback.CallbackIdentity()
	if direct {
		if err := callback.SendDirectCallback(ctx, source, ourPort); err != nil {
			if cfg.Logf != nil {
				cfg.Logf("download: direct callback to %s failed: %v", ip, err)
			}
			if !buddy {
				return nil, err
			}
		} else if cfg.Logf != nil {
			cfg.Logf("download: direct callback sent to %s:%d", ip, source.UDPPort)
		}
	}
	wait := func(duration time.Duration) (*Result, bool, error) {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, true, ctx.Err()
		case <-timer.C:
			return nil, false, nil
		case conn := <-channel:
			result, err := FetchCallback(ctx, conn, link, cfg)
			return result, true, err
		}
	}
	if direct {
		if result, done, err := wait(15 * time.Second); done {
			return result, err
		}
	}
	if buddy {
		if err := callback.SendBuddyCallback(ctx, source, link.Hash, ourPort); err != nil {
			return nil, err
		}
		if cfg.Logf != nil {
			cfg.Logf("download: buddy callback sent via %s:%d", source.BuddyIP, source.BuddyPort)
		}
	}
	if serverCallback {
		if err := callback.SendServerCallback(ctx, source); err != nil {
			if cfg.Logf != nil {
				cfg.Logf("download: server callback to LowID %d failed: %v", source.ClientID, err)
			}
			if !direct && !buddy {
				return nil, err
			}
		} else if cfg.Logf != nil {
			cfg.Logf("download: server callback for LowID %d via %s:%d", source.ClientID, source.ServerIP, source.ServerPort)
		}
	}
	waitFor := 75 * time.Second
	if serverCallback && !direct && !buddy {
		waitFor = 45 * time.Second
	}
	if result, _, err := wait(waitFor); result != nil || err != nil {
		return result, err
	}
	return nil, errors.New("download: callback timed out")
}

func (m *Manager) update(id string, change func(*Transfer)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if transfer, ok := m.transfers[id]; ok {
		change(&transfer.Transfer)
		transfer.Updated = time.Now()
		m.saveLocked()
	}
}

// load reads the persisted rows. A transfer that was running when the daemon
// stopped becomes an error row: its .part file is still on disk, and adding
// the same link again resumes it.
func (m *Manager) load() {
	path := m.cfg.StatePath
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var rows []Transfer
	if err := json.Unmarshal(raw, &rows); err != nil {
		return
	}
	for _, row := range rows {
		row := row
		if row.Priority == "" {
			row.Priority = PriorityNormal
		}
		if row.State != StateCompleted {
			row.State = StateError
			row.Error = "interrupted by a daemon restart; add the link again to resume"
		}
		m.transfers[row.ID] = &managedTransfer{Transfer: row}
	}
}

func (m *Manager) saveLocked() {
	path := m.cfg.StatePath
	if path == "" {
		return
	}
	rows := make([]Transfer, 0, len(m.transfers))
	for _, transfer := range m.transfers {
		rows = append(rows, transfer.Transfer)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (m *Manager) fail(id string, err error) {
	m.update(id, func(t *Transfer) {
		t.State = StateError
		t.Error = err.Error()
	})
}

// WantsIncoming reports whether an accepted connection comes from a source we
// asked to call us back.
func (m *Manager) WantsIncoming(conn net.Conn) bool {
	ip := remoteIP(conn.RemoteAddr())
	if ip == "" {
		return false
	}
	m.callbackMu.Lock()
	defer m.callbackMu.Unlock()
	if _, ok := m.pending[ip]; ok {
		return true
	}
	// A server callback for a LowID source does not reveal the source IP,
	// so a wildcard slot accepts the first incoming connection.
	_, ok := m.pending[""]
	return ok
}

// HandleIncoming hands an accepted callback connection to the waiting worker.
func (m *Manager) HandleIncoming(conn net.Conn) {
	ip := remoteIP(conn.RemoteAddr())
	m.callbackMu.Lock()
	channel := m.pending[ip]
	if channel == nil {
		channel = m.pending[""]
	}
	m.callbackMu.Unlock()
	if channel == nil {
		_ = conn.Close()
		return
	}
	select {
	case channel <- conn:
	default:
		_ = conn.Close()
	}
}

func (m *Manager) expectCallback(ip string) (chan net.Conn, func()) {
	channel := make(chan net.Conn, 1)
	m.callbackMu.Lock()
	m.pending[ip] = channel
	m.callbackMu.Unlock()
	return channel, func() {
		m.callbackMu.Lock()
		if m.pending[ip] == channel {
			delete(m.pending, ip)
		}
		m.callbackMu.Unlock()
	}
}

func remoteIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return ""
	}
	return strings.Trim(host, "[]")
}

// List returns a snapshot ordered by creation time.
func (m *Manager) List() []Transfer {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Transfer, 0, len(m.transfers))
	for _, transfer := range m.transfers {
		out = append(out, transfer.Transfer)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Created.Before(out[j-1].Created); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Get returns one transfer.
func (m *Manager) Get(id string) (Transfer, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[id]
	if !ok {
		return Transfer{}, false
	}
	return transfer.Transfer, true
}

// SetPriority changes the scheduling priority of a transfer.
func (m *Manager) SetPriority(id, priority string) bool {
	priority = strings.ToLower(strings.TrimSpace(priority))
	switch priority {
	case PriorityLow, PriorityNormal, PriorityHigh:
	default:
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[id]
	if !ok || transfer.State == StateCompleted {
		return false
	}
	transfer.Priority = priority
	transfer.Updated = time.Now()
	m.saveLocked()
	return true
}

// SetLimitKB caps a transfer at kb KiB/s; 0 removes the cap. Workers already
// running keep the previous bucket until they reconnect.
func (m *Manager) SetLimitKB(id string, kb int) bool {
	if kb < 0 || kb > 1_000_000 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[id]
	if !ok || transfer.State == StateCompleted {
		return false
	}
	transfer.LimitKB = kb
	if kb == 0 {
		transfer.limiter = nil
	} else {
		transfer.limiter = ratelimit.New(int64(kb) * 1024)
	}
	transfer.Updated = time.Now()
	m.saveLocked()
	return true
}

// Pause stops the worker but keeps the row and the partial file.
func (m *Manager) Pause(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[id]
	if !ok || transfer.State == StateCompleted || transfer.State == StatePaused {
		return false
	}
	if transfer.running && transfer.cancel != nil {
		transfer.cancel()
	}
	transfer.State = StatePaused
	transfer.Error = ""
	transfer.Updated = time.Now()
	m.saveLocked()
	return true
}

// Resume restarts a paused or failed transfer.
func (m *Manager) Resume(id string) bool {
	m.mu.Lock()
	transfer, ok := m.transfers[id]
	if !ok || (transfer.State != StatePaused && transfer.State != StateError) || m.closed {
		m.mu.Unlock()
		return false
	}
	if transfer.running {
		m.mu.Unlock()
		return false
	}
	link := Link{Hash: ed2k.ID{}, Size: transfer.Size, Name: transfer.Name}
	if hash, err := ed2k.ParseID(transfer.Hash); err == nil {
		link.Hash = hash
	}
	transfer.link = link
	transfer.State = StateDiscovering
	transfer.Error = ""
	transfer.Updated = time.Now()
	m.saveLocked()
	m.dispatchLocked()
	m.mu.Unlock()
	return true
}

// Remove cancels a transfer and forgets it. Partial files stay on disk.
func (m *Manager) Remove(id string) bool {
	m.mu.Lock()
	transfer, ok := m.transfers[id]
	if ok {
		delete(m.transfers, id)
		if transfer.running {
			transfer.running = false
			if m.active > 0 {
				m.active--
			}
			if transfer.cancel != nil {
				transfer.cancel()
			}
		}
		m.saveLocked()
		m.dispatchLocked()
	}
	m.mu.Unlock()
	return ok
}

// Close cancels every transfer and waits for the workers.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	for _, transfer := range m.transfers {
		if transfer.cancel != nil {
			transfer.cancel()
		}
	}
	m.mu.Unlock()
	m.cancel()
	m.wg.Wait()
}
