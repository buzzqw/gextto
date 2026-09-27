package gextto

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/utils"
)

// Tests ported from the remaining reference scenarios that need a config
// database, a torrent_meta row or an HTTP round-trip.

func TestEffectiveListenInterfacesBindsKillswitch(t *testing.T) {
	settings := DefaultLibtorrentSettings()
	settings.PortMin = 6881
	if got := effectiveListenInterfaces(&settings); got != "" {
		t.Fatalf("no interface -> %q, want empty", got)
	}
	settings.ListenInterfaces = "wg0"
	if got := effectiveListenInterfaces(&settings); got != "wg0:6881" {
		t.Fatalf("bare interface -> %q, want wg0:6881", got)
	}
	settings.ListenInterfaces = ""
	settings.OutgoingInterface = "tun0"
	if got := effectiveListenInterfaces(&settings); got != "tun0:6881" {
		t.Fatalf("outgoing interface -> %q, want tun0:6881", got)
	}
	settings.ListenInterfaces = "0.0.0.0:6881-6891"
	if got := effectiveListenInterfaces(&settings); got != "0.0.0.0:6881-6891" {
		t.Fatalf("explicit port changed -> %q", got)
	}
}

func TestReadsImportedBandwidthLimitsWithoutChangingUnits(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gextto_config.db")
	db, err := OpenConfigDB(dbPath)
	if err != nil {
		t.Fatalf("open config db: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE torrent_limits (info_hash TEXT PRIMARY KEY, dl_bytes INTEGER, ul_bytes INTEGER)"); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := db.Exec("INSERT INTO torrent_limits VALUES (?1,?2,?3)", "abc123", int64(1_572_864), int64(-1)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	db.Close()

	client := &LibtorrentClient{configDB: dbPath}
	download, upload, ok, err := client.storedLimits("abc123")
	if err != nil || !ok {
		t.Fatalf("storedLimits ok=%v err=%v", ok, err)
	}
	if download != 1_572_864 || upload != -1 {
		t.Fatalf("limits = (%d,%d), byte units must be preserved", download, upload)
	}
	if _, _, ok, _ := client.storedLimits("missing"); ok {
		t.Fatal("missing hash reported as present")
	}
}

func TestRamdiskSettingsReadFromConfigDB(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Settings = map[string]string{
		"libtorrent_ramdisk_dir":            "/mnt/ramdisk",
		"libtorrent_ramdisk_enabled":        "yes",
		"libtorrent_ramdisk_threshold_gb":   "5.5",
		"libtorrent_ramdisk_margin_gb":      "0.5",
		"libtorrent_ramdisk_min_free_bytes": "1024",
	}
	if !cfg.RamdiskEnabled() {
		t.Fatal("ramdisk should be enabled")
	}
	if dir := cfg.RamdiskDir(); dir == nil || *dir != "/mnt/ramdisk" {
		t.Fatalf("ramdisk dir = %v", dir)
	}
	const gib = uint64(1024 * 1024 * 1024)
	if got := cfg.RamdiskThresholdBytes(); got != uint64(5.5*float64(gib)) {
		t.Fatalf("threshold = %d", got)
	}
	if got := cfg.RamdiskMarginBytes(); got != gib/2 {
		t.Fatalf("margin = %d", got)
	}
	if got := cfg.RamdiskMinFreeBytes(); got != 1024 {
		t.Fatalf("min free = %d", got)
	}
	cfg.Settings["libtorrent_ramdisk_enabled"] = "no"
	if cfg.RamdiskEnabled() {
		t.Fatal("ramdisk should be disabled")
	}
}

func TestArchivedPackSourceDisposableRequiresCopy(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDatabase(filepath.Join(dir, "series.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.db.Close()

	magnet := "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	release := ParseRelease("Show.S01.1080p.WEB-DL.ITA", magnet, "test")
	if release == nil || !release.IsPack {
		t.Fatalf("expected a pack release: %+v", release)
	}
	hash, ok := utils.MagnetHash(release.Magnet)
	if !ok {
		t.Fatal("no magnet hash")
	}
	if err := db.RegisterTorrent(release); err != nil {
		t.Fatalf("register: %v", err)
	}

	// Not completed yet: the source must never be removed.
	if tev_archivedPackSourceDisposable(db, hash, "/tmp/torrents") {
		t.Fatal("incomplete pack marked disposable")
	}
	if err := db.MarkPackCompleted(release, nil, "/nas/Show", 1); err != nil {
		t.Fatalf("mark pack completed: %v", err)
	}
	// Archived outside the torrent storage: the source is disposable.
	if !tev_archivedPackSourceDisposable(db, hash, "/tmp/torrents") {
		t.Fatal("archived pack not marked disposable")
	}
	// A processed path inside the torrent storage is not a copy.
	if _, err := db.db.Exec("UPDATE torrent_meta SET processed_path=?1 WHERE hash=?2", "/tmp/torrents/Show S01", hash); err != nil {
		t.Fatalf("update processed path: %v", err)
	}
	if tev_archivedPackSourceDisposable(db, hash, "/tmp/torrents") {
		t.Fatal("processed path inside the torrent storage must not be disposable")
	}
}

func TestEpisodeSearchMatchesPartialAndCompleteSeasonPacks(t *testing.T) {
	series := SeriesConfig{Name: "Example Show"}
	magnet := "magnet:?xt=urn:btih:0123456789012345678901234567890123456789"
	partial := ParseRelease("Example.Show.S01E01-08.1080p.WEB-DL.ITA", magnet, "test")
	complete := ParseRelease("Example.Show.S01.1080p.WEB-DL.ITA", magnet, "test")
	if partial == nil || complete == nil {
		t.Fatal("releases not parsed")
	}
	if !gh7_release_matches_series_episode(partial, series, 1, 5) {
		t.Fatal("partial pack should match episode 5")
	}
	if !gh7_release_matches_series_episode(complete, series, 1, 5) {
		t.Fatal("complete pack should match episode 5")
	}
	if gh7_release_matches_series_episode(partial, series, 1, 9) {
		t.Fatal("partial pack must not match episode 9")
	}
}

func TestBackupListReturnsEntries(t *testing.T) {
	state := newTestAppState(t)
	backups := filepath.Join(state.cfg.DataDir, "backups")
	if err := os.MkdirAll(backups, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backups, "snapshot-1.zip"), []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(Router(state))
	t.Cleanup(server.Close)

	code, _, body := webGet(t, server, "/api/backup")
	if code != 200 {
		t.Fatalf("backup list -> %d: %s", code, body)
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Items) != 1 {
		t.Fatalf("items = %v", decoded.Items)
	}
	if name, _ := decoded.Items[0]["name"].(string); name != "snapshot-1.zip" {
		t.Fatalf("name = %v", decoded.Items[0]["name"])
	}
	if label, _ := decoded.Items[0]["label"].(string); !strings.Contains(label, "/") {
		t.Fatalf("label missing the date: %v", decoded.Items[0]["label"])
	}
}
