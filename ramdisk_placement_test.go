package gextto

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestReleaseFitsRamdisk(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LibtorrentDir = t.TempDir()
	temp := t.TempDir()
	cfg.LibtorrentTempDir = &temp
	cfg.Settings = map[string]string{"libtorrent_ramdisk_dir": t.TempDir()}

	if !ReleaseFitsRamdisk(&models.Release{SizeBytes: 1_000_000_000}, &cfg) {
		t.Fatal("a 1 GB release must fit the default 3.5 GB threshold")
	}
	if ReleaseFitsRamdisk(&models.Release{SizeBytes: 100_000_000_000}, &cfg) {
		t.Fatal("a 100 GB season pack must not fit the RAM disk")
	}
	// Unknown size: this predicate alone treats it as fitting; the add path
	// pairs it with RamdiskNeedsMetadata, so it is actually staged off the RAM
	// disk and decided at metadata time.
	if !ReleaseFitsRamdisk(&models.Release{}, &cfg) {
		t.Fatal("an unknown size must be treated as fitting by this predicate")
	}

	// With the RAM disk disabled there is nothing to skip.
	off := DefaultConfig()
	off.Settings = map[string]string{
		"libtorrent_ramdisk_enabled": "false",
		"libtorrent_ramdisk_dir":     t.TempDir(),
	}
	if !ReleaseFitsRamdisk(&models.Release{SizeBytes: 100_000_000_000}, &off) {
		t.Fatal("a disabled RAM disk must never divert the download")
	}
}

func TestRamdiskOverflowDirPrefersTempDir(t *testing.T) {
	main := t.TempDir()
	temp := t.TempDir()
	cfg := DefaultConfig()
	cfg.LibtorrentDir = main
	cfg.LibtorrentTempDir = &temp
	if dir, ok := ramdiskOverflowDir(&cfg); !ok || dir != temp {
		t.Fatalf("overflow dir = %q (%v), want the temp dir", dir, ok)
	}

	cfg.LibtorrentTempDir = nil
	if dir, ok := ramdiskOverflowDir(&cfg); !ok || dir != main {
		t.Fatalf("overflow dir = %q (%v), want the main dir", dir, ok)
	}
}

// TestRamdiskNeedsMetadata pins the rule: a release whose size is unknown must
// not start on the RAM disk. It is staged off the tmpfs and decided when the
// metadata arrives; a disabled RAM disk never diverts anything.
func TestRamdiskNeedsMetadata(t *testing.T) {
	cfg := DefaultConfig()
	cfg.LibtorrentDir = t.TempDir()
	cfg.Settings = map[string]string{
		"libtorrent_ramdisk_dir":     t.TempDir(),
		"libtorrent_ramdisk_enabled": "yes",
	}
	if !RamdiskNeedsMetadata(&models.Release{}, &cfg) {
		t.Fatal("an unknown size must be staged off the RAM disk until metadata")
	}
	if RamdiskNeedsMetadata(&models.Release{SizeBytes: 1_000_000_000}, &cfg) {
		t.Fatal("a known size is decided before the download starts")
	}

	off := DefaultConfig()
	off.Settings = map[string]string{
		"libtorrent_ramdisk_enabled": "false",
		"libtorrent_ramdisk_dir":     t.TempDir(),
	}
	if RamdiskNeedsMetadata(&models.Release{}, &off) {
		t.Fatal("a disabled RAM disk must never divert the download")
	}
}

// TestAutomaticDownloadPathRamdiskPolicy pins where an automatic acquisition is
// sent: a release whose known size fits targets the RAM disk, an oversized or
// unknown-size one is forced to the overflow dir, and a backend without the
// `ramdisk` capability (qBittorrent) never uses it.
func TestAutomaticDownloadPathRamdiskPolicy(t *testing.T) {
	temp := t.TempDir()
	ramdisk := t.TempDir()
	cfg := DefaultConfig()
	cfg.LibtorrentDir = t.TempDir()
	cfg.LibtorrentTempDir = &temp
	cfg.Settings = map[string]string{
		"libtorrent_ramdisk_dir":          ramdisk,
		"libtorrent_ramdisk_enabled":      "yes",
		"libtorrent_ramdisk_threshold_gb": "3.5",
	}

	// A known size that fits: the RAM disk, chosen explicitly.
	if got := automaticDownloadPath(&models.Release{SizeBytes: 1 << 30}, &cfg, true); got == nil || *got != ramdisk {
		t.Fatalf("a fitting release must target the RAM disk, got %v", got)
	}
	if got := automaticDownloadPath(&models.Release{SizeBytes: 8 << 30}, &cfg, true); got == nil || *got != temp {
		t.Fatalf("an oversized release must go to the overflow dir, got %v", got)
	}
	if got := automaticDownloadPath(&models.Release{}, &cfg, true); got == nil || *got != temp {
		t.Fatalf("an unknown-size release must go to the overflow dir, got %v", got)
	}

	// qBittorrent-like backend: no RAM disk tier, the engine default wins.
	if got := automaticDownloadPath(&models.Release{SizeBytes: 1 << 30}, &cfg, false); got != nil {
		t.Fatalf("a backend without ramdisk support must not target the RAM disk, got %v", got)
	}
	if got := automaticDownloadPath(&models.Release{SizeBytes: 8 << 30}, &cfg, false); got != nil {
		t.Fatalf("a backend without ramdisk support must use the engine default, got %v", got)
	}
	if got := automaticDownloadPath(&models.Release{}, &cfg, false); got != nil {
		t.Fatalf("a backend without ramdisk support must use the engine default, got %v", got)
	}

	// The implicit default never picks the RAM disk, so a manual add (no size
	// known) stays off the tmpfs until the metadata decides.
	if got := preferredDownloadPath(&cfg); got != temp {
		t.Fatalf("the engine default must not be the RAM disk: got %q want %q", got, temp)
	}
	if got := resolveSavePath(nil, &cfg); got != temp {
		t.Fatalf("a manual add must not start on the RAM disk: got %q want %q", got, temp)
	}
}

// ramdiskCapStub is a TorrentSession that also declares capabilities, like the
// real backends do.
type ramdiskCapStub struct {
	stubTorrentSession
	ramdisk bool
}

func (s *ramdiskCapStub) Capabilities() map[string]bool {
	return map[string]bool{"ramdisk": s.ramdisk}
}

// TestEngineSupportsRamdiskUsesCapability pins the backend gate: qBittorrent
// (ramdisk: none) must never use the RAM disk, while an undeclared backend keeps
// the historical behavior.
func TestEngineSupportsRamdiskUsesCapability(t *testing.T) {
	if !engineSupportsRamdisk(&stubTorrentSession{}) {
		t.Fatal("a backend without declared capabilities keeps the historical RAM-disk behavior")
	}
	if engineSupportsRamdisk(&ramdiskCapStub{ramdisk: false}) {
		t.Fatal("a backend with ramdisk: none must not use the RAM disk")
	}
	if !engineSupportsRamdisk(&ramdiskCapStub{ramdisk: true}) {
		t.Fatal("a ramdisk-capable backend must use the RAM disk")
	}
}

// TestRamdiskAdmissionMovesFittingTorrentOntoTheRamdisk pins the metadata-time
// decision: a torrent staged off the RAM disk because its size was unknown is
// moved onto it once the real size fits.
func TestRamdiskAdmissionMovesFittingTorrentOntoTheRamdisk(t *testing.T) {
	ramdisk := t.TempDir()
	disk := t.TempDir()
	cfg := DefaultConfig()
	cfg.LibtorrentDir = disk
	cfg.Settings = map[string]string{
		"libtorrent_ramdisk_dir":          ramdisk,
		"libtorrent_ramdisk_enabled":      "yes",
		"libtorrent_ramdisk_threshold_gb": "3.5",
		"libtorrent_ramdisk_margin_gb":    "0.5",
	}
	hash := strings.Repeat("a", 40)
	torrents := &stubTorrentSession{list: []models.TorrentView{{
		Hash: hash, Name: "fits", SavePath: disk, TotalSize: 1 << 30,
	}}}
	event := &models.TorrentEvent{Kind: "metadata_received", Hash: hash, Name: "fits", SavePath: disk}
	tev_admitToRamdisk(&cfg, torrents, event)
	if torrents.moved[hash] != ramdisk {
		t.Fatalf("fitting torrent not admitted to the RAM disk: %v", torrents.moved)
	}
}

// TestRamdiskAdmissionRejectsOversizedOrStartedTorrent pins the guard rails: a
// torrent above the threshold stays on disk, and so does one that has already
// started downloading.
func TestRamdiskAdmissionRejectsOversizedOrStartedTorrent(t *testing.T) {
	ramdisk := t.TempDir()
	disk := t.TempDir()
	cfg := DefaultConfig()
	cfg.LibtorrentDir = disk
	cfg.Settings = map[string]string{
		"libtorrent_ramdisk_dir":          ramdisk,
		"libtorrent_ramdisk_enabled":      "yes",
		"libtorrent_ramdisk_threshold_gb": "3.5",
		"libtorrent_ramdisk_margin_gb":    "0.5",
	}
	oversized := strings.Repeat("b", 40)
	started := strings.Repeat("c", 40)
	torrents := &stubTorrentSession{list: []models.TorrentView{
		{Hash: oversized, Name: "big", SavePath: disk, TotalSize: 8 << 30},
		{Hash: started, Name: "running", SavePath: disk, TotalSize: 1 << 30, TotalDone: 1 << 30, Progress: 50},
	}}
	tev_admitToRamdisk(&cfg, torrents, &models.TorrentEvent{Kind: "metadata_received", Hash: oversized, Name: "big", SavePath: disk})
	tev_admitToRamdisk(&cfg, torrents, &models.TorrentEvent{Kind: "metadata_received", Hash: started, Name: "running", SavePath: disk})
	if len(torrents.moved) != 0 {
		t.Fatalf("oversized or started torrents must not be admitted: %v", torrents.moved)
	}
}

// TestRamdiskAdmissionDisabledForBackendsWithoutRamdisk pins the backend gate on
// the metadata decision too: qBittorrent must never move a torrent onto the RAM
// disk, even when the idle path would fit.
func TestRamdiskAdmissionDisabledForBackendsWithoutRamdisk(t *testing.T) {
	ramdisk := t.TempDir()
	disk := t.TempDir()
	cfg := DefaultConfig()
	cfg.LibtorrentDir = disk
	cfg.Settings = map[string]string{
		"libtorrent_ramdisk_dir":          ramdisk,
		"libtorrent_ramdisk_enabled":      "yes",
		"libtorrent_ramdisk_threshold_gb": "3.5",
		"libtorrent_ramdisk_margin_gb":    "0.5",
	}
	hash := strings.Repeat("d", 40)
	torrents := &ramdiskCapStub{
		ramdisk: false,
		stubTorrentSession: stubTorrentSession{list: []models.TorrentView{{
			Hash: hash, Name: "fits", SavePath: disk, TotalSize: 1 << 30,
		}}},
	}
	tev_admitToRamdisk(&cfg, torrents, &models.TorrentEvent{Kind: "metadata_received", Hash: hash, Name: "fits", SavePath: disk})
	if len(torrents.moved) != 0 {
		t.Fatalf("a backend without ramdisk support must not admit to the RAM disk: %v", torrents.moved)
	}
}
