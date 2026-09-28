package gextto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedQbittorrentConfigDefaultsAndPrivateProfile(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.LibtorrentDir = filepath.Join(cfg.DataDir, "downloads")
	tempDir := filepath.Join(cfg.DataDir, "incomplete")
	cfg.LibtorrentTempDir = &tempDir

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
		"Downloads\\SavePath=" + cfg.LibtorrentDir,
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
