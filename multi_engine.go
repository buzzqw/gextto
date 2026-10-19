package gextto

// multi_engine.go is the transfer-plane router described in the eD2k design:
// one TorrentEngine surface, one row per file, each row carrying its network.
// It is deliberately not installed while gx-ed2k is search-only: routing a
// download to an engine that cannot finish it would be worse than showing the
// result as unavailable. When gx-ed2k advertises "download": true this type is
// the only wiring change the automation needs, because every caller keeps
// using activeEngine().

import (
	"sort"
	"strings"
	"sync"

	"github.com/buzzqw/gextto/internal/models"
)

const (
	networkTorrent = "torrent"
	networkEd2k    = "ed2k"
)

// multiEngine routes every hash to one of the per-network engines.
type multiEngine struct {
	torrent TorrentEngine
	ed2k    TorrentEngine

	mu    sync.RWMutex
	route map[string]string
	// persist is called after a route is learned from an Add. It may be nil.
	persist func(hash, network string)
}

var (
	_ TorrentEngine         = (*multiEngine)(nil)
	_ TorrentSessionHealth  = (*multiEngine)(nil)
	_ TorrentEngineIdentity = (*multiEngine)(nil)
	_ TorrentPieceInspector = (*multiEngine)(nil)
)

func newMultiEngine(torrent, ed2k TorrentEngine, persisted map[string]string) *multiEngine {
	engine := &multiEngine{torrent: torrent, ed2k: ed2k, route: map[string]string{}}
	for hash, network := range persisted {
		if network == networkEd2k {
			engine.route[strings.ToLower(hash)] = networkEd2k
		}
	}
	return engine
}

// SetRoute records the network of a file. Hashing is lowercase everywhere.
func (m *multiEngine) SetRoute(hash, network string) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if hash == "" {
		return
	}
	if network != networkEd2k {
		network = networkTorrent
	}
	m.mu.Lock()
	m.route[hash] = network
	persist := m.persist
	m.mu.Unlock()
	if persist != nil {
		persist(hash, network)
	}
}

// NetworkOf returns the network of a hash; unknown hashes belong to the
// torrent engine, which keeps existing sessions working unchanged.
func (m *multiEngine) NetworkOf(hash string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if network, ok := m.route[strings.ToLower(strings.TrimSpace(hash))]; ok {
		return network
	}
	return networkTorrent
}

func (m *multiEngine) engineFor(hash string) TorrentEngine {
	if m.NetworkOf(hash) == networkEd2k && m.ed2k != nil {
		return m.ed2k
	}
	if m.torrent != nil {
		return m.torrent
	}
	return nullEngine{}
}

func (m *multiEngine) Name() string { return "multi" }

// Close releases the torrent plane; the eD2k adapter is stateless HTTP.
func (m *multiEngine) Close() error {
	if closer, ok := m.torrent.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// Capabilities reports what both planes can do. A capability is true only when
// the torrent engine has it: the eD2k plane is advisory and must not make the
// UI offer an operation the active downloads cannot perform.
func (m *multiEngine) Capabilities() map[string]bool {
	if m.torrent == nil {
		return map[string]bool{}
	}
	out := map[string]bool{}
	for key, value := range m.torrent.Capabilities() {
		out[key] = value
	}
	return out
}

func (m *multiEngine) List() []models.TorrentView {
	var rows []models.TorrentView
	if m.torrent != nil {
		for _, row := range m.torrent.List() {
			row.Network = networkTorrent
			rows = append(rows, row)
		}
	}
	if m.ed2k != nil {
		for _, row := range m.ed2k.List() {
			row.Network = networkEd2k
			// Rebuild the route on every snapshot: after a restart the rows
			// are still eD2k files, even if the route table was not persisted.
			m.SetRoute(row.Hash, networkEd2k)
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	return rows
}

func (m *multiEngine) PollEvents() []models.TorrentEvent {
	var events []models.TorrentEvent
	if m.torrent != nil {
		events = append(events, m.torrent.PollEvents()...)
	}
	if m.ed2k != nil {
		events = append(events, m.ed2k.PollEvents()...)
	}
	return events
}

func (m *multiEngine) Stats() map[string]any {
	out := map[string]any{"multi": true}
	if m.torrent != nil {
		for key, value := range m.torrent.Stats() {
			out[key] = value
		}
		out["backend"] = "multi"
	}
	if m.ed2k != nil {
		out["ed2k"] = m.ed2k.Stats()
	}
	return out
}

func (m *multiEngine) AdjustQueue(cfg *Config, effectiveDownloadKib int64) {
	if m.torrent != nil {
		m.torrent.AdjustQueue(cfg, effectiveDownloadKib)
	}
}

func (m *multiEngine) Add(magnet string, cfg *Config) (bool, error) {
	return m.AddWithOptions(magnet, cfg, nil, AddOptions{})
}

func (m *multiEngine) AddWithPath(magnet string, cfg *Config, preferredPath *string) (bool, error) {
	return m.AddWithOptions(magnet, cfg, preferredPath, AddOptions{})
}

func (m *multiEngine) AddWithOptions(link string, cfg *Config, preferredPath *string, options AddOptions) (bool, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(link)), "ed2k://") {
		if m.ed2k == nil {
			return false, backendCapabilityError(networkEd2k, "add")
		}
		added, err := m.ed2k.AddWithOptions(link, cfg, preferredPath, options)
		if added {
			if hash, ok := ed2kHashFromLink(link); ok {
				m.SetRoute(hash, networkEd2k)
			}
		}
		return added, err
	}
	if m.torrent == nil {
		return false, backendCapabilityError(networkTorrent, "add")
	}
	return m.torrent.AddWithOptions(link, cfg, preferredPath, options)
}

func (m *multiEngine) AddTorrentFile(torrentPath, savePath string) (*string, error) {
	return m.AddTorrentFileEx(torrentPath, savePath, AddOptions{})
}

func (m *multiEngine) AddTorrentFileEx(torrentPath, savePath string, options AddOptions) (*string, error) {
	if m.torrent == nil {
		return nil, backendCapabilityError(networkTorrent, "add")
	}
	return m.torrent.AddTorrentFileEx(torrentPath, savePath, options)
}

func (m *multiEngine) AddTorrentFileWithOptions(torrentPath string, cfg *Config, preferredPath *string, options AddOptions) (*string, error) {
	if m.torrent == nil {
		return nil, backendCapabilityError(networkTorrent, "add")
	}
	return m.torrent.AddTorrentFileWithOptions(torrentPath, cfg, preferredPath, options)
}

func (m *multiEngine) AddFileWithPath(torrentPath string, cfg *Config, preferredPath *string) (bool, error) {
	if m.torrent == nil {
		return false, backendCapabilityError(networkTorrent, "add")
	}
	return m.torrent.AddFileWithPath(torrentPath, cfg, preferredPath)
}

func (m *multiEngine) Pause(hash string) (bool, error)   { return m.engineFor(hash).Pause(hash) }
func (m *multiEngine) Resume(hash string) (bool, error)  { return m.engineFor(hash).Resume(hash) }
func (m *multiEngine) Restart(hash string) (bool, error) { return m.engineFor(hash).Restart(hash) }
func (m *multiEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	return m.engineFor(hash).Remove(hash, deleteFiles)
}
func (m *multiEngine) ForceRecheck(hash string) (bool, error) {
	return m.engineFor(hash).ForceRecheck(hash)
}
func (m *multiEngine) Reannounce(hash string) (bool, error) {
	return m.engineFor(hash).Reannounce(hash)
}
func (m *multiEngine) MoveStorage(hash, destination string) (bool, error) {
	return m.engineFor(hash).MoveStorage(hash, destination)
}
func (m *multiEngine) MarkStalled(hash string) (bool, error) {
	return m.engineFor(hash).MarkStalled(hash)
}
func (m *multiEngine) ClearStalled(hash string) { m.engineFor(hash).ClearStalled(hash) }
func (m *multiEngine) RamdiskUncommittedBytes(ramdisk, excludeHash string) uint64 {
	if m.torrent == nil {
		return 0
	}
	return m.torrent.RamdiskUncommittedBytes(ramdisk, excludeHash)
}

func (m *multiEngine) Files(hash string) ([]models.FileView, bool, error) {
	return m.engineFor(hash).Files(hash)
}
func (m *multiEngine) Peers(hash string) ([]models.PeerView, bool, error) {
	return m.engineFor(hash).Peers(hash)
}
func (m *multiEngine) Trackers(hash string) ([]models.TrackerView, bool, error) {
	return m.engineFor(hash).Trackers(hash)
}
func (m *multiEngine) SetFilePriorities(hash string, priorities []int32) (bool, error) {
	return m.engineFor(hash).SetFilePriorities(hash, priorities)
}
func (m *multiEngine) SetTrackers(hash string, trackers []TrackerEntry) (bool, error) {
	return m.engineFor(hash).SetTrackers(hash, trackers)
}
func (m *multiEngine) WebSeeds(hash, urls string, remove bool) (bool, error) {
	return m.engineFor(hash).WebSeeds(hash, urls, remove)
}
func (m *multiEngine) SetLimits(hash string, downloadLimit, uploadLimit int64, seedRatio float64, seedDays int64) (bool, error) {
	return m.engineFor(hash).SetLimits(hash, downloadLimit, uploadLimit, seedRatio, seedDays)
}
func (m *multiEngine) SetGlobalSpeedLimits(downloadKib, uploadKib int64) (bool, error) {
	if m.torrent == nil {
		return false, backendCapabilityError(networkTorrent, "limits")
	}
	return m.torrent.SetGlobalSpeedLimits(downloadKib, uploadKib)
}
func (m *multiEngine) SetMaxConnections(hash string, value int) (bool, error) {
	return m.engineFor(hash).SetMaxConnections(hash, value)
}
func (m *multiEngine) SetMaxUploads(hash string, value int) (bool, error) {
	return m.engineFor(hash).SetMaxUploads(hash, value)
}
func (m *multiEngine) SetPin(hash string, pinned bool) (bool, error) {
	return m.engineFor(hash).SetPin(hash, pinned)
}
func (m *multiEngine) SetSequential(enabled bool) (bool, error) {
	if m.torrent == nil {
		return false, backendCapabilityError(networkTorrent, "sequential")
	}
	return m.torrent.SetSequential(enabled)
}
func (m *multiEngine) SetFirstLastDefault(enabled bool) {
	if m.torrent != nil {
		m.torrent.SetFirstLastDefault(enabled)
	}
}
func (m *multiEngine) AssociateStorage(hash, destination string) (bool, error) {
	return m.engineFor(hash).AssociateStorage(hash, destination)
}
func (m *multiEngine) TorrentFilePath(hash string) (string, bool) {
	return m.engineFor(hash).TorrentFilePath(hash)
}

// SessionHealthy reports the torrent plane only: an unreachable eD2k daemon
// must never make the torrent reconciliation destructive or paused. The eD2k
// rows simply disappear from the merged list while it is down.
func (m *multiEngine) SessionHealthy() bool {
	if healthy, ok := m.torrent.(TorrentSessionHealth); ok {
		return healthy.SessionHealthy()
	}
	return m.torrent != nil
}

func (m *multiEngine) EngineVersion() (string, string) {
	if identity, ok := m.torrent.(TorrentEngineIdentity); ok {
		return identity.EngineVersion()
	}
	return "", ""
}

func (m *multiEngine) PieceRuns(hash string) ([]TorrentPieceRun, bool, error) {
	if inspector, ok := m.engineFor(hash).(TorrentPieceInspector); ok {
		return inspector.PieceRuns(hash)
	}
	return nil, false, backendCapabilityError(m.NetworkOf(hash), "piece_diagnostics")
}

// ed2kHashFromLink extracts the MD4 hash from an ed2k:// file link.
func ed2kHashFromLink(link string) (string, bool) {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(strings.ToLower(link), "ed2k://|file|") {
		return "", false
	}
	parts := strings.Split(link, "|")
	if len(parts) < 5 {
		return "", false
	}
	hash := strings.ToLower(strings.TrimSpace(parts[4]))
	if len(hash) != 32 {
		return "", false
	}
	for _, r := range hash {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return "", false
		}
	}
	return hash, true
}
