package gextto

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedQbittorrentConfigDefaultsAndPrivateProfile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.DownloadDir = filepath.Join(cfg.DataDir, "downloads")
	tempDir := filepath.Join(cfg.DataDir, "incomplete")
	cfg.DownloadTempDir = &tempDir

	prepared, err := managedQbittorrentConfig(&cfg)
	if err != nil {
		t.Fatalf("managedQbittorrentConfig: %v", err)
	}
	if got := prepared.Settings["qbittorrent_url"]; got != "http://127.0.0.1:8080" {
		t.Fatalf("URL = %q", got)
	}
	if got := prepared.Settings["qbittorrent_username"]; got != "admin" {
		t.Fatalf("username = %q", got)
	}
	if len(prepared.Settings["qbittorrent_password"]) < 32 {
		t.Fatalf("generated password is too short")
	}

	settings, err := qbittorrentSettingsFromConfig(prepared)
	if err != nil {
		t.Fatalf("qbittorrentSettingsFromConfig: %v", err)
	}
	if err := writeManagedQbittorrentConfig(prepared, settings); err != nil {
		t.Fatalf("writeManagedQbittorrentConfig: %v", err)
	}
	raw, err := os.ReadFile(qbittorrentProfileConfigPath(prepared))
	if err != nil {
		t.Fatalf("read private profile: %v", err)
	}
	content := string(raw)
	for _, want := range []string{
		"WebUI\\Enabled=true",
		"WebUI\\Port=8080",
		"WebUI\\Username=admin",
		"WebUI\\Password_PBKDF2=@ByteArray(",
		"Downloads\\SavePath=" + cfg.DownloadDir,
		"Downloads\\TempPath=" + tempDir,
		"[LegalNotice]",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("private qBittorrent profile does not contain %q:\n%s", want, content)
		}
	}
}

func TestBackupQbittorrentBinaryNeverOverwritesBackup(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, qbittorrentBinaryName)
	if err := os.WriteFile(binary, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	backup, err := backupQbittorrentBinary(binary)
	if err != nil {
		t.Fatalf("backupQbittorrentBinary: %v", err)
	}
	if backup == "" {
		t.Fatal("backup path is empty")
	}
	newBinary := binary + ".new"
	if err := os.WriteFile(newBinary, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(newBinary, binary); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(old) != "old" {
		t.Fatalf("backup contains %q, want old", old)
	}
}

func TestQbittorrentReleaseV2Detection(t *testing.T) {
	if !qbittorrentIsLibtorrentV2Release(qbittorrentRelease{TagName: "release-5.2.3_v2.0.15"}) {
		t.Fatal("v2 release was not detected")
	}
	if qbittorrentIsLibtorrentV2Release(qbittorrentRelease{Name: "qbittorrent 5.2.3 libtorrent 1.2.19"}) {
		t.Fatal("v1 release was detected as v2")
	}
}

// TestFetchQbittorrentLatestNetwork exercises the real GitHub lookup the
// fallback uses to download the managed runtime. It is opt-in because it needs
// the network: run it with GEXTTO_TEST_NETWORK=1.
func TestFetchQbittorrentLatestNetwork(t *testing.T) {
	if os.Getenv("GEXTTO_TEST_NETWORK") != "1" {
		t.Skip("set GEXTTO_TEST_NETWORK=1 to run the network test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	release, asset, err := fetchQbittorrentLatestRemote(ctx)
	if err != nil {
		t.Fatalf("fetchQbittorrentLatestRemote: %v", err)
	}
	if asset.Name == "" || !strings.Contains(asset.Name, "qbittorrent-nox") {
		t.Fatalf("asset = %q, want a qbittorrent-nox binary", asset.Name)
	}
	if !strings.HasPrefix(asset.Digest, "sha256:") {
		t.Fatalf("asset digest = %q, want a sha256 digest", asset.Digest)
	}
	if !strings.Contains(asset.DownloadURL, "/userdocs/qbittorrent-nox-static/releases/download/") {
		t.Fatalf("asset URL = %q, want a userdocs release download", asset.DownloadURL)
	}
	t.Logf("latest qBittorrent-nox: %s · %s · %d bytes · %s", release.TagName, asset.Name, asset.Size, asset.Digest)
}

// TestInstallQbittorrentRuntimeNetwork downloads, verifies and installs the
// managed qBittorrent runtime for real (the fallback's on-demand download). It
// is opt-in and installs into the managed directory next to the test binary, so
// the machine is not touched: run it with GEXTTO_TEST_NETWORK=1.
func TestInstallQbittorrentRuntimeNetwork(t *testing.T) {
	if os.Getenv("GEXTTO_TEST_NETWORK") != "1" {
		t.Skip("set GEXTTO_TEST_NETWORK=1 to run the network test")
	}
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	status, err := installQbittorrentRuntime(ctx, &cfg)
	if err != nil {
		t.Fatalf("installQbittorrentRuntime: %v", err)
	}
	if !status.Installed {
		t.Fatalf("status = %+v, want installed", status)
	}
	binary := qbittorrentManagedBinary(&cfg)
	if !fileExists(binary) {
		t.Fatalf("managed binary missing at %s", binary)
	}
	metadata, ok := qbittorrentReadInstallMetadata(&cfg)
	if !ok || metadata.Tag == "" || !strings.HasPrefix(metadata.SHA256, "") {
		t.Fatalf("install metadata = %+v (ok=%v)", metadata, ok)
	}
	t.Logf("installed managed qBittorrent %s at %s", status.InstalledTag, binary)
}
