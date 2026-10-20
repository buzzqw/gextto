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
	// OnComplete is called after a transfer is verified and renamed. The
	// daemon uses it to add the file to the upload index.
	OnComplete func(Result)
	// StatePath persists the transfer rows across daemon restarts. Empty
	// keeps the manager in memory only.
	StatePath string
}

// Transfer is the public state of one eD2k download.
type Transfer struct {
	ID      string    `json:"id"`
	Hash    string    `json:"hash"`
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	State   string    `json:"state"`
	Done    int64     `json:"done"`
	Sources int       `json:"sources"`
	Error   string    `json:"error,omitempty"`
	Path    string    `json:"path,omitempty"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

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
	cancel context.CancelFunc
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
	closed    bool

	callbackMu sync.Mutex
	pending    map[string]chan net.Conn
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
	managerCtx, cancel := context.WithCancel(ctx)
	manager := &Manager{
		cfg: cfg, discover: discover, ctx: managerCtx, cancel: cancel,
		transfers: make(map[string]*managedTransfer),
		pending:   make(map[string]chan net.Conn),
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
		ctx, cancel := context.WithCancel(m.ctx)
		existing.cancel = cancel
		existing.State = StateDiscovering
		existing.Error = ""
		existing.Updated = time.Now()
		transfer := existing.Transfer
		m.saveLocked()
		m.mu.Unlock()
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			m.run(ctx, id, link)
		}()
		return transfer, nil
	}
	if m.closed {
		m.mu.Unlock()
		return Transfer{}, errors.New("download: manager closed")
	}
	ctx, cancel := context.WithCancel(m.ctx)
	now := time.Now()
	managed := &managedTransfer{
		Transfer: Transfer{
			ID: id, Hash: id, Name: link.Name, Size: link.Size,
			State: StateDiscovering, Created: now, Updated: now,
		},
		cancel: cancel,
	}
	m.transfers[id] = managed
	transfer := managed.Transfer
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx, id, link)
	}()
	return transfer, nil
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

	var lastErr error
	for round := 0; ; round++ {
		if ctx.Err() != nil {
			return
		}
		if round > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.cfg.RetryDelay):
			}
			if refreshed, err := m.discover.Sources(ctx, link.Hash.Hex(), link.Size); err == nil && len(refreshed) > 0 {
				sources = refreshed
				if len(sources) > m.cfg.MaxSources {
					sources = sources[:m.cfg.MaxSources]
				}
				m.update(id, func(t *Transfer) { t.Sources = len(sources) })
			}
		}
		queued := false
		for _, source := range sources {
			if ctx.Err() != nil {
				return
			}
			m.update(id, func(t *Transfer) { t.State = StateDownloading; t.Error = "" })

			fetchCtx, cancel := context.WithTimeout(ctx, m.cfg.FetchBudget)
			cfg := m.cfg.Fetch
			if provider, ok := m.discover.(interface{ Stats() ed2k.Stats }); ok {
				cfg.ClientID = provider.Stats().ServerID
			}
			cfg.OnProgress = func(done, total int64) {
				m.update(id, func(t *Transfer) { t.Done = done; t.Size = total })
			}
			var result *Result
			var err error
			if source.TCPPort == 0 {
				result, err = m.fetchViaCallback(fetchCtx, source, link, cfg)
			} else {
				address := fmt.Sprintf("%s:%d", source.IP.String(), source.TCPPort)
				result, err = Fetch(fetchCtx, address, link, cfg)
			}
			cancel()
			if err == nil {
				m.update(id, func(t *Transfer) {
					t.State = StateCompleted
					t.Done = t.Size
					t.Path = result.Path
					t.Error = ""
				})
				if m.cfg.OnComplete != nil {
					m.cfg.OnComplete(*result)
				}
				return
			}
			if ctx.Err() != nil {
				return
			}
			if m.cfg.Fetch.Logf != nil {
				m.cfg.Fetch.Logf("download: source %s failed: %v", source.IP, err)
			}
			if IsQueued(err) {
				queued = true
				m.update(id, func(t *Transfer) { t.State = StateQueued; t.Error = err.Error() })
				continue
			}
			lastErr = err
		}
		if queued {
			m.update(id, func(t *Transfer) { t.State = StateQueued })
			continue
		}
		if lastErr == nil {
			lastErr = errors.New("no source accepted the transfer")
		}
		// Keep the row in the error state but retry: a source may come back,
		// a queue may free a slot, or a new source may appear in Kad.
		m.fail(id, lastErr)
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
	direct := source.UDPPort != 0
	buddy := source.BuddyID != (ed2k.ID{})
	if !direct && !buddy {
		return nil, errors.New("download: LowID source has no UDP port or buddy")
	}
	ip := source.IP.String()
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
	if result, _, err := wait(75 * time.Second); result != nil || err != nil {
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
	_, ok := m.pending[ip]
	return ok
}

// HandleIncoming hands an accepted callback connection to the waiting worker.
func (m *Manager) HandleIncoming(conn net.Conn) {
	ip := remoteIP(conn.RemoteAddr())
	m.callbackMu.Lock()
	channel := m.pending[ip]
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

// Pause stops the worker but keeps the row and the partial file.
func (m *Manager) Pause(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	transfer, ok := m.transfers[id]
	if !ok || transfer.State == StateCompleted || transfer.State == StatePaused {
		return false
	}
	transfer.cancel()
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
	ctx, cancel := context.WithCancel(m.ctx)
	transfer.cancel = cancel
	transfer.State = StateDiscovering
	transfer.Error = ""
	transfer.Updated = time.Now()
	link := Link{Hash: ed2k.ID{}, Size: transfer.Size, Name: transfer.Name}
	if hash, err := ed2k.ParseID(transfer.Hash); err == nil {
		link.Hash = hash
	}
	m.saveLocked()
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.run(ctx, id, link)
	}()
	return true
}

// Remove cancels a transfer and forgets it. Partial files stay on disk.
func (m *Manager) Remove(id string) bool {
	m.mu.Lock()
	transfer, ok := m.transfers[id]
	if ok {
		delete(m.transfers, id)
	}
	m.mu.Unlock()
	if ok && transfer.cancel != nil {
		transfer.cancel()
	}
	m.mu.Lock()
	m.saveLocked()
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
