package gextto

// null_engine.go provides the placeholder engine used when no transfer backend
// could be activated (an activation refused at startup): it lists nothing and
// refuses every operation with a capability error, so the UI and the automation
// keep working while the transfers are stopped. It replaced the old "embedded
// engine with a dry-run session" fallback.

import "github.com/buzzqw/gextto/internal/models"

// nullEngine is the engine of last resort.
type nullEngine struct{}

var _ TorrentEngine = nullEngine{}
var _ interface{ Close() error } = nullEngine{}

func (nullEngine) Name() string { return "none" }

func (nullEngine) Capabilities() map[string]bool { return capabilitiesFor("none") }

func (nullEngine) Stats() map[string]any { return map[string]any{} }

func (nullEngine) List() []models.TorrentView { return nil }

func (nullEngine) PollEvents() []models.TorrentEvent { return nil }

func (nullEngine) AdjustQueue(*Config, int64) {}

func (nullEngine) Close() error { return nil }

func (nullEngine) SessionHealthy() bool { return false }

func (nullEngine) unsupported(capability string) error {
	return ErrCapabilityUnavailable{Backend: "none", Capability: capability}
}

func (n nullEngine) Pause(string) (bool, error)   { return false, n.unsupported("pause") }
func (n nullEngine) Resume(string) (bool, error)  { return false, n.unsupported("resume") }
func (n nullEngine) Restart(string) (bool, error) { return false, n.unsupported("restart") }
func (n nullEngine) Remove(string, bool) (bool, error) {
	return false, n.unsupported("remove")
}
func (n nullEngine) ForceRecheck(string) (bool, error) { return false, n.unsupported("recheck") }
func (n nullEngine) Reannounce(string) (bool, error)   { return false, n.unsupported("reannounce") }
func (n nullEngine) MoveStorage(string, string) (bool, error) {
	return false, n.unsupported("move")
}
func (n nullEngine) MarkStalled(string) (bool, error) { return false, n.unsupported("stalled") }
func (nullEngine) ClearStalled(string)                {}
func (nullEngine) RamdiskUncommittedBytes(string, string) uint64 {
	return 0
}

func (n nullEngine) Add(string, *Config) (bool, error) { return false, n.unsupported("add") }
func (n nullEngine) AddWithPath(string, *Config, *string) (bool, error) {
	return false, n.unsupported("add")
}
func (n nullEngine) AddWithOptions(string, *Config, *string, AddOptions) (bool, error) {
	return false, n.unsupported("add")
}
func (n nullEngine) AddFileWithPath(string, *Config, *string) (bool, error) {
	return false, n.unsupported("add")
}
func (n nullEngine) AddTorrentFile(string, string) (*string, error) {
	return nil, n.unsupported("add")
}
func (n nullEngine) AddTorrentFileEx(string, string, AddOptions) (*string, error) {
	return nil, n.unsupported("add")
}
func (n nullEngine) AddTorrentFileWithOptions(string, *Config, *string, AddOptions) (*string, error) {
	return nil, n.unsupported("add")
}
func (n nullEngine) TorrentFilePath(string) (string, bool) { return "", false }

func (n nullEngine) Files(string) ([]models.FileView, bool, error) {
	return nil, false, n.unsupported("files")
}
func (n nullEngine) Peers(string) ([]models.PeerView, bool, error) {
	return nil, false, n.unsupported("peers")
}
func (n nullEngine) Trackers(string) ([]models.TrackerView, bool, error) {
	return nil, false, n.unsupported("trackers")
}
func (n nullEngine) SetFilePriorities(string, []int32) (bool, error) {
	return false, n.unsupported("file_priorities")
}
func (n nullEngine) SetTrackers(string, []TrackerEntry) (bool, error) {
	return false, n.unsupported("trackers")
}
func (n nullEngine) WebSeeds(string, string, bool) (bool, error) {
	return false, n.unsupported("web_seeds")
}
func (n nullEngine) SetLimits(string, int64, int64, float64, int64) (bool, error) {
	return false, n.unsupported("limits")
}
func (n nullEngine) SetGlobalSpeedLimits(int64, int64) (bool, error) {
	return false, n.unsupported("limits")
}
func (n nullEngine) SetMaxConnections(string, int) (bool, error) {
	return false, n.unsupported("connections")
}
func (n nullEngine) SetMaxUploads(string, int) (bool, error) { return false, n.unsupported("uploads") }
func (n nullEngine) SetPin(string, bool) (bool, error)       { return false, n.unsupported("pin") }
func (n nullEngine) SetSequential(bool) (bool, error)        { return false, n.unsupported("sequential") }
func (nullEngine) SetFirstLastDefault(bool)                  {}
func (n nullEngine) AssociateStorage(string, string) (bool, error) {
	return false, n.unsupported("associate")
}
