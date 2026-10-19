package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// Discoverer finds peers for a file. *ed2k.Client implements it.
type Discoverer interface {
	Sources(ctx context.Context, hash string, size int64) ([]ed2k.PeerSource, error)
}

// ManagerConfig tunes the transfer queue.
type ManagerConfig struct {
	Fetch       Config
	MaxSources  int
	FetchBudget time.Duration
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
}

// NewManager creates a manager. The context bounds every transfer.
func NewManager(ctx context.Context, cfg ManagerConfig, discover Discoverer) *Manager {
	if cfg.MaxSources <= 0 {
		cfg.MaxSources = 50
	}
	if cfg.FetchBudget <= 0 {
		cfg.FetchBudget = 15 * time.Minute
	}
	managerCtx, cancel := context.WithCancel(ctx)
	manager := &Manager{
		cfg: cfg, discover: discover, ctx: managerCtx, cancel: cancel,
		transfers: make(map[string]*managedTransfer),
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
	queued := false
	for _, source := range sources {
		if ctx.Err() != nil {
			return
		}
		address := fmt.Sprintf("%s:%d", source.IP.String(), source.TCPPort)
		m.update(id, func(t *Transfer) { t.State = StateDownloading; t.Error = "" })

		fetchCtx, cancel := context.WithTimeout(ctx, m.cfg.FetchBudget)
		cfg := m.cfg.Fetch
		cfg.OnProgress = func(done, total int64) {
			m.update(id, func(t *Transfer) { t.Done = done; t.Size = total })
		}
		result, err := Fetch(fetchCtx, address, link, cfg)
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
		if IsQueued(err) {
			queued = true
			m.update(id, func(t *Transfer) { t.State = StateQueued; t.Error = err.Error() })
			continue
		}
		lastErr = err
	}
	if queued {
		m.update(id, func(t *Transfer) { t.State = StateQueued })
		return
	}
	if lastErr == nil {
		lastErr = errors.New("no source accepted the transfer")
	}
	m.fail(id, lastErr)
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
