package gextto

// torrent_engine.go introduces the transfer-plane contract between Gextto and
// the torrent engine that actually moves bytes.
//
// Gextto stays the owner of the queue, the automation and the post-processing;
// the engine only provides the transfer plan. Two implementations exist:
//
//   - embeddedEngine  — the official libtorrent backend (default, unchanged);
//   - qbittorrentEngine — an adapter over the qBittorrent-nox Web API.
//
// The refactor is deliberately behavior-preserving: `embeddedEngine` embeds the
// existing *LibtorrentClient, so every promoted method behaves exactly as
// before. The abstraction exists to let the daemon select a different engine
// without changing the HTTP contract or the automation policy (see
// the backend-specific implementation notes).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/models"
	"github.com/buzzqw/gextto/internal/qbittorrent"
)

// TorrentSession is the narrow, engine-agnostic surface the automation layer
// (queue, stalled/retry, storage moves, seed policy) needs. It is intentionally
// small so a backend only has to implement what Gextto actually automates.
type TorrentSession interface {
	List() []models.TorrentView
	PollEvents() []models.TorrentEvent

	Pause(hash string) (bool, error)
	Resume(hash string) (bool, error)
	Restart(hash string) (bool, error)
	Remove(hash string, deleteFiles bool) (bool, error)
	ForceRecheck(hash string) (bool, error)
	Reannounce(hash string) (bool, error)
	MoveStorage(hash, destination string) (bool, error)

	MarkStalled(hash string) (bool, error)
	ClearStalled(hash string)
	RamdiskUncommittedBytes(ramdisk string, excludeHash string) uint64
}

// TorrentSessionHealth is optionally implemented by backends whose List result
// can be stale or empty while their external daemon is unavailable. Callers
// that perform destructive reconciliation must skip it until such a backend
// has a confirmed live snapshot.
type TorrentSessionHealth interface {
	SessionHealthy() bool
}

// TorrentPieceRun is a compact run of consecutive pieces that share a state.
// Exposed by GET /api/torrents/{hash}/pieces for backends that support it.
type TorrentPieceRun struct {
	Begin int    `json:"begin"`
	End   int    `json:"end"`
	State string `json:"state"`
}

// TorrentPieceInspector is implemented by backends that can expose per-piece
// diagnostics. Backends without it must answer with a capability error, never
// with fabricated data.
type TorrentPieceInspector interface {
	PieceRuns(hash string) ([]TorrentPieceRun, bool, error)
}

// TorrentEngine is the full surface shared by the HTTP API and the acquisition
// path. Backends that cannot implement an operation must return an explicit
// error (ErrCapabilityUnavailable) instead of pretending success.
type TorrentEngine interface {
	TorrentSession

	Name() string
	Capabilities() map[string]bool

	Stats() map[string]any
	// AdjustQueue enforces Gextto's active-download slots on the backend. The
	// embedded engine applies libtorrent's dynamic queue; the external engines
	// implement the same policy with pause/start.
	AdjustQueue(cfg *Config, effectiveDownloadKib int64)

	Add(magnet string, cfg *Config) (bool, error)
	AddWithPath(magnet string, cfg *Config, preferredPath *string) (bool, error)
	AddWithOptions(magnet string, cfg *Config, preferredPath *string, options AddOptions) (bool, error)
	AddTorrentFile(torrentPath, savePath string) (*string, error)
	AddTorrentFileEx(torrentPath, savePath string, options AddOptions) (*string, error)
	AddTorrentFileWithOptions(torrentPath string, cfg *Config, preferredPath *string, options AddOptions) (*string, error)
	AddFileWithPath(torrentPath string, cfg *Config, preferredPath *string) (bool, error)

	Files(hash string) ([]models.FileView, bool, error)
	Peers(hash string) ([]models.PeerView, bool, error)
	Trackers(hash string) ([]models.TrackerView, bool, error)
	SetFilePriorities(hash string, priorities []int32) (bool, error)
	SetTrackers(hash string, trackers []TrackerEntry) (bool, error)
	WebSeeds(hash, urls string, remove bool) (bool, error)
	SetLimits(hash string, downloadLimit, uploadLimit int64, seedRatio float64, seedDays int64) (bool, error)
	SetGlobalSpeedLimits(downloadKib, uploadKib int64) (bool, error)
	SetMaxConnections(hash string, value int) (bool, error)
	SetMaxUploads(hash string, value int) (bool, error)
	SetPin(hash string, pinned bool) (bool, error)
	SetSequential(enabled bool) (bool, error)
	AssociateStorage(hash, destination string) (bool, error)
	TorrentFilePath(hash string) (string, bool)
}

// ErrCapabilityUnavailable is returned by a backend for an operation it does
// not support. Callers must surface it instead of simulating success.
type ErrCapabilityUnavailable struct {
	Backend    string
	Capability string
}

func (e ErrCapabilityUnavailable) Error() string {
	return fmt.Sprintf("torrent backend %q does not support %q", e.Backend, e.Capability)
}

// embeddedEngine adapts the libtorrent client to TorrentEngine. Because it
// embeds the concrete client, every method is promoted unchanged: switching the
// default path through this adapter cannot alter behavior.
type embeddedEngine struct{ *LibtorrentClient }

func (embeddedEngine) Name() string { return BackendEmbedded }

func (embeddedEngine) Capabilities() map[string]bool {
	return capabilitiesFor(BackendEmbedded)
}

var _ TorrentEngine = embeddedEngine{}

// Backend identifiers accepted by `torrent_backend`.
const (
	BackendEmbedded    = "embedded"
	BackendQbittorrent = "qbittorrent"
	BackendGxTorrent   = "gx-torrent"
)

// DefaultTorrentBackend is the engine a fresh installation uses when no
// `torrent_backend` has ever been saved. gx-torrent is the default since it is
// pure Go, needs no libtorrent-rasterbar and runs in its own supervised process;
// `embedded` (libtorrent) stays available and remains the automatic fallback
// when gx-torrent cannot be activated.
const DefaultTorrentBackend = BackendGxTorrent

// capabilityLevels is the single source of truth for the parity matrix. It is
// intentionally explicit: pretending a backend supports an operation it cannot
// apply is worse than returning a clear "unavailable".
//
// categories/tags are Gextto-level (the torrent tag set in Scarico and the
// "NAS per categoria" rules), not an engine feature: they work with every
// backend, provided the UI/API path, so all of them report "full".
var capabilityLevels = map[string]map[string]string{
	BackendEmbedded: {
		"add": "full", "list": "full", "pause": "full", "resume": "full", "remove": "full",
		"recheck": "full", "move": "full", "limits": "full", "files": "full", "peers": "full",
		"trackers": "full", "events": "full", "stats": "full", "sequential": "full",
		"first_last": "full", "seed_policy": "full", "ramdisk": "full", "fastresume": "full",
		"piece_diagnostics": "none", "categories": "full", "tags": "full", "sync": "none",
		"preferences": "full", "super_seeding": "full", "upload_mode": "full",
		"ip_filter": "full", "session_stats": "full", "web_seeds": "full",
	},
	BackendQbittorrent: {
		"add": "full", "list": "full", "pause": "full", "resume": "full", "remove": "full",
		"recheck": "full", "move": "full", "limits": "full", "files": "full", "peers": "full",
		"trackers": "full", "events": "full", "stats": "full", "sequential": "full",
		"first_last": "full", "seed_policy": "partial", "ramdisk": "none", "fastresume": "none",
		"piece_diagnostics": "none", "categories": "full", "tags": "full", "sync": "full",
		"preferences": "partial", "super_seeding": "partial", "upload_mode": "none",
		"ip_filter": "partial", "session_stats": "partial", "web_seeds": "partial",
	},
	BackendGxTorrent: {
		"add": "full", "list": "full", "pause": "full", "resume": "full", "remove": "full",
		"recheck": "full", "move": "full", "limits": "full", "files": "full", "peers": "full",
		"trackers": "full", "events": "full", "stats": "full", "sequential": "full",
		"first_last": "partial", "seed_policy": "full", "ramdisk": "full", "fastresume": "full",
		"piece_diagnostics": "full", "categories": "full", "tags": "full", "sync": "full",
		"preferences": "partial", "super_seeding": "full", "upload_mode": "none",
		"ip_filter": "full", "session_stats": "partial", "web_seeds": "full",
	},
}

// capabilityNames is the stable, sorted list used by the API and the tests.
func capabilityNames() []string {
	seen := map[string]struct{}{}
	for _, levels := range capabilityLevels {
		for name := range levels {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// capabilitiesFor reports boolean capabilities for a backend. Partial support
// counts as available (true) but is flagged in CapabilityMatrix.
func capabilitiesFor(backend string) map[string]bool {
	levels := capabilityLevels[backend]
	out := map[string]bool{}
	for _, name := range capabilityNames() {
		out[name] = levels[name] == "full" || levels[name] == "partial"
	}
	return out
}

// CapabilityMatrix returns the explicit capability/level map for a backend.
func CapabilityMatrix(backend string) map[string]string {
	levels := capabilityLevels[backend]
	if levels == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(levels))
	for name, level := range levels {
		out[name] = level
	}
	return out
}

// CapabilityParity returns, for every capability, the level of each backend.
// It backs the parity test and the UI capability table.
func CapabilityParity() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, name := range capabilityNames() {
		row := map[string]string{}
		for backend := range capabilityLevels {
			level := capabilityLevels[backend][name]
			if level == "" {
				level = "none"
			}
			row[backend] = level
		}
		out[name] = row
	}
	return out
}

// activeEngine returns the engine currently driving the daemon. When no
// alternative engine was installed at startup (or after a settings change), the
// embedded libtorrent adapter is returned, preserving the historic behavior.
func (s *AppState) activeEngine() TorrentEngine {
	if s == nil {
		return embeddedEngine{nil}
	}
	s.engine_mu.RLock()
	engine := s.torrent_engine
	s.engine_mu.RUnlock()
	if engine != nil {
		return engine
	}
	return embeddedEngine{s.torrents}
}

// setActiveEngine installs an alternative engine. A nil engine restores the
// embedded libtorrent backend.
func (s *AppState) setActiveEngine(engine TorrentEngine) {
	s.engine_mu.Lock()
	s.torrent_engine = engine
	s.engine_mu.Unlock()
}

// hasTorrentEngine reports whether any engine (embedded or alternative) is
// wired. It guards handlers that must not dereference a nil embedded client in
// a partially constructed AppState (route smoke tests).
func (s *AppState) hasTorrentEngine() bool {
	if s == nil {
		return false
	}
	s.engine_mu.RLock()
	defer s.engine_mu.RUnlock()
	return s.torrent_engine != nil || s.torrents != nil
}

// ---------------------------------------------------------------------------
// path mapping (Gextto paths <-> backend paths)
// ---------------------------------------------------------------------------

// PathMapping translates between the path as Gextto sees it and the path as the
// backend process sees it. Empty/equal pairs are allowed and mean "identical".
type PathMapping struct {
	Gextto  string `json:"gextto"`
	Backend string `json:"backend"`
}

// ParsePathMappings decodes the `backend_path_mappings` setting, which is a
// newline-separated list of `gextto_path=backend_path` pairs. Lines starting
// with `#` and blank lines are ignored. Invalid lines are reported.
func ParsePathMappings(raw string) ([]PathMapping, error) {
	var mappings []PathMapping
	for index, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		left, right, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("path mapping line %d: expected 'gextto_path=backend_path'", index+1)
		}
		left = filepath.Clean(strings.TrimSpace(left))
		right = filepath.Clean(strings.TrimSpace(right))
		if left == "." || right == "." || left == "" || right == "" {
			return nil, fmt.Errorf("path mapping line %d: empty path", index+1)
		}
		mappings = append(mappings, PathMapping{Gextto: left, Backend: right})
	}
	return mappings, nil
}

// pathWithin reports whether path is inside root (component-wise).
func pathWithin(path, root string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, strings.TrimRight(root, string(os.PathSeparator))+string(os.PathSeparator))
}

// translatePath maps a path from one root to another using the first matching
// mapping, choosing the best (longest root) match. It returns the original path
// and false when no mapping applies.
func translatePath(path string, mappings []PathMapping, from, to func(PathMapping) string) (string, bool) {
	cleaned := filepath.Clean(path)
	best := -1
	for index, mapping := range mappings {
		root := from(mapping)
		if !pathWithin(cleaned, root) {
			continue
		}
		if best == -1 || len(from(mappings[best])) < len(root) {
			best = index
		}
	}
	if best == -1 {
		return path, false
	}
	mapping := mappings[best]
	base := from(mapping)
	rest := strings.TrimPrefix(cleaned, base)
	rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	translated := filepath.Join(to(mapping), rest)
	return translated, true
}

// TranslateGexttoToBackend maps a Gextto path into the active backend's view.
func TranslateGexttoToBackend(path string, mappings []PathMapping) (string, bool) {
	return translatePath(path, mappings,
		func(m PathMapping) string { return m.Gextto },
		func(m PathMapping) string { return m.Backend })
}

// TranslateBackendToGextto maps a backend-reported path into the Gextto view.
func TranslateBackendToGextto(path string, mappings []PathMapping) (string, bool) {
	return translatePath(path, mappings,
		func(m PathMapping) string { return m.Backend },
		func(m PathMapping) string { return m.Gextto })
}

// ValidatePathMappings is the mandatory preflight: when a backend runs with a
// different filesystem namespace (Docker, separate mount), every Gextto path
// that can be handed to it must be covered by an explicit mapping. It also
// refuses overlapping mappings that would make translation ambiguous.
func ValidatePathMappings(mappings []PathMapping, required []string) error {
	// Overlapping roots (one contained in the other) make translation
	// order-dependent; reject them outright.
	for index, mapping := range mappings {
		for other, candidate := range mappings {
			if index >= other {
				continue
			}
			// Identical root on both sides is a valid identity mapping and must
			// not be flagged as overlapping with itself.
			if mapping.Gextto == candidate.Gextto && mapping.Backend == candidate.Backend {
				continue
			}
			if pathWithin(mapping.Gextto, candidate.Gextto) || pathWithin(candidate.Gextto, mapping.Gextto) {
				return fmt.Errorf("overlapping path mappings: %q and %q", mapping.Gextto, candidate.Gextto)
			}
		}
	}
	for _, path := range required {
		if strings.TrimSpace(path) == "" {
			continue
		}
		cleaned := filepath.Clean(path)
		if _, ok := TranslateGexttoToBackend(cleaned, mappings); ok {
			continue
		}
		// An identity mapping is acceptable only when the backend is known to
		// share the same filesystem, which cannot be assumed. Require coverage.
		return fmt.Errorf("path %q is not covered by backend_path_mappings", cleaned)
	}
	return nil
}

// torrentEngineEmbeddedExtras are the libtorrent-only operations the worker
// uses. A non-embedded backend simply does not implement them and the
// corresponding blocks are skipped: the queue and metadata machinery are
// Gextto's, but only the embedded engine exposes them in-process.
type torrentEngineEmbeddedExtras interface {
	PromoteMetadata()
	EnsureAutoManaged() int
	EnforceDeferredOptions(torrents []models.TorrentView)
	recentlyRechecked(hash string, window time.Duration) bool
	RequestResumeSave() int
}

// requireEmbedded returns the embedded libtorrent client when it is the active
// backend. When another engine is active it returns an explicit capability
// error so libtorrent-only endpoints never act on the wrong session.
func (s *AppState) requireEmbedded(capability string) (*LibtorrentClient, error) {
	if s == nil {
		return nil, backendCapabilityError(BackendEmbedded, capability)
	}
	s.engine_mu.RLock()
	engine := s.torrent_engine
	s.engine_mu.RUnlock()
	if engine != nil {
		return nil, backendCapabilityError(engine.Name(), capability)
	}
	if s.torrents == nil {
		return nil, backendCapabilityError(BackendEmbedded, capability)
	}
	return s.torrents, nil
}

// backendCapabilityError builds the canonical explicit "unsupported" error.
func backendCapabilityError(backend, capability string) error {
	return ErrCapabilityUnavailable{Backend: backend, Capability: capability}
}

// torrentNotFoundError reports whether an operation failed because the torrent
// is no longer in the backend session. That is a benign race (the torrent was
// removed between a listing and a command) and must not be logged as a warning.
func torrentNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, qbittorrent.ErrNotFound) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "torrent not found")
}

// TorrentTransferring reports whether a torrent is really moving data right
// now (downloading or uploading). Only these count as "active": a torrent in
// the downloading state at 0 B/s both ways is waiting, not working.
func TorrentTransferring(torrent models.TorrentView) bool {
	return torrent.DownloadRate > 0 || torrent.UploadRate > 0
}

// TorrentIdle reports whether an unfinished, not paused torrent is stuck at
// 0 B/s both ways (no peer has the missing pieces, a recheck, a stall).
func TorrentIdle(torrent models.TorrentView) bool {
	if TorrentTransferring(torrent) || torrent.Progress >= 100 {
		return false
	}
	switch torrent.State {
	case "downloading", "downloading_metadata", "stalled", "checking_files", "checking_resume_data":
		return true
	}
	return false
}
