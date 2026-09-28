package gextto

// Managed qBittorrent runtime support. The downloaded binary is intentionally
// private to Gextto: it lives below DATA_DIR and is never installed in /usr,
// /opt or any other system location.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

const (
	qbittorrentGitHubAPI  = "https://api.github.com/repos/userdocs/qbittorrent-nox-static"
	qbittorrentGitHubRepo = "https://github.com/userdocs/qbittorrent-nox-static"
	qbittorrentBinaryName = "qbittorrent-nox"
	qbittorrentMaxBinary  = 256 << 20
)

var qbittorrentInstallMu sync.Mutex
var qbittorrentLatestMu sync.Mutex
var qbittorrentLatestFetchMu sync.Mutex
var qbittorrentLatestCache struct {
	at      time.Time
	release qbittorrentRelease
	asset   qbittorrentAsset
	err     error
}

type qbittorrentRelease struct {
	TagName string             `json:"tag_name"`
	Name    string             `json:"name"`
	HTMLURL string             `json:"html_url"`
	Body    string             `json:"body"`
	Assets  []qbittorrentAsset `json:"assets"`
}

type qbittorrentAsset struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Digest      string `json:"digest"`
	DownloadURL string `json:"browser_download_url"`
}

type qbittorrentInstallMetadata struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Asset       string `json:"asset"`
	Libtorrent  string `json:"libtorrent"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installed_at"`
}

type qbittorrentRuntimeStatus struct {
	Managed         bool   `json:"managed"`
	Directory       string `json:"directory"`
	Binary          string `json:"binary"`
	Installed       bool   `json:"installed"`
	InstalledTag    string `json:"installed_tag,omitempty"`
	InstalledName   string `json:"installed_name,omitempty"`
	LatestTag       string `json:"latest_tag,omitempty"`
	LatestName      string `json:"latest_name,omitempty"`
	LatestURL       string `json:"latest_url,omitempty"`
	Asset           string `json:"asset,omitempty"`
	Architecture    string `json:"architecture"`
	Libtorrent      string `json:"libtorrent"`
	UpdateAvailable bool   `json:"update_available"`
	RestartRequired bool   `json:"restart_required,omitempty"`
}

func cloneConfigWithSettings(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	copyCfg := *cfg
	copyCfg.Settings = make(map[string]string, len(cfg.Settings))
	for key, value := range cfg.Settings {
		copyCfg.Settings[key] = value
	}
	return &copyCfg
}

func managedQbittorrentConfig(cfg *Config) (*Config, error) {
	prepared := cloneConfigWithSettings(cfg)
	if prepared == nil {
		return nil, errors.New("configurazione Gextto assente")
	}
	baseURL := strings.TrimSpace(prepared.Settings["qbittorrent_url"])
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, errors.New("URL qBittorrent gestito non valido")
	}
	if parsed.Scheme != "http" {
		return nil, errors.New("qBittorrent gestito richiede Web API HTTP locale")
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() || ip == nil && !strings.EqualFold(host, "localhost") {
		return nil, errors.New("qBittorrent gestito richiede un URL locale (localhost/loopback)")
	}
	if parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return nil, errors.New("qBittorrent gestito non supporta un prefisso URL")
	}
	parsed.Path = ""
	parsed.RawPath = ""
	baseURL = strings.TrimRight(parsed.String(), "/")
	username := strings.TrimSpace(prepared.Settings["qbittorrent_username"])
	if username == "" {
		username = "admin"
	}
	password := prepared.Settings["qbittorrent_password"]
	if password == "" {
		bytes := make([]byte, 24)
		if _, err := rand.Read(bytes); err != nil {
			return nil, fmt.Errorf("generazione password qBittorrent: %w", err)
		}
		password = hex.EncodeToString(bytes)
	}
	prepared.Settings["qbittorrent_url"] = baseURL
	prepared.Settings["qbittorrent_username"] = username
	prepared.Settings["qbittorrent_password"] = password
	prepared.Settings["qbittorrent_managed"] = "true"
	return prepared, nil
}

// qbittorrentManagedDir keeps the managed runtime (binary + profile) next to
// the daemon, in the application directory, so it never clutters the data dir
// nor the media/download folders.
func qbittorrentManagedDir(cfg *Config) string {
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil && strings.TrimSpace(resolved) != "" {
			exe = resolved
		}
		return filepath.Join(filepath.Dir(exe), "qbittorrent")
	}
	// Fallback when the executable path is unavailable.
	if cfg == nil || strings.TrimSpace(cfg.DataDir) == "" {
		return ""
	}
	return filepath.Join(cfg.DataDir, "qbittorrent")
}

func qbittorrentManagedBinary(cfg *Config) string {
	dir := qbittorrentManagedDir(cfg)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, qbittorrentBinaryName)
}

func ensureQbittorrentManagedDir(cfg *Config) (string, error) {
	dir := qbittorrentManagedDir(cfg)
	if dir == "" {
		return "", errors.New("directory dati Gextto non configurata")
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		info, err = os.Lstat(dir)
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("cartella qBittorrent gestita non valida: %s", dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func qbittorrentManagedEnabled(cfg *Config) bool {
	return cfg != nil && settingsBool(cfg, "qbittorrent_managed", false)
}

func qbittorrentAssetArch() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("qBittorrent statico disponibile solo per Linux, non per %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64", nil
	case "386":
		return "x86", nil
	case "arm64":
		return "aarch64", nil
	case "arm":
		return "armv7", nil
	case "riscv64":
		return "riscv64", nil
	case "s390x":
		return "s390x", nil
	case "ppc64le":
		return "ppc64el", nil
	case "mips":
		return "mips", nil
	case "mipsle":
		return "mipsel", nil
	case "mips64":
		return "mips64", nil
	case "mips64le":
		return "mips64el", nil
	case "loong64":
		return "loongarch64", nil
	default:
		return "", fmt.Errorf("qBittorrent static build non disponibile per %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func qbittorrentIsLibtorrentV2Release(release qbittorrentRelease) bool {
	for _, value := range []string{release.TagName, release.Name, release.Body} {
		value = strings.ToLower(strings.ReplaceAll(value, " ", ""))
		if strings.Contains(value, "libtorrent2.") || strings.Contains(value, "libtorrentv2.") || strings.Contains(value, "_v2.") {
			return true
		}
	}
	return false
}

func qbittorrentReadInstallMetadata(cfg *Config) (qbittorrentInstallMetadata, bool) {
	path := filepath.Join(qbittorrentManagedDir(cfg), "install.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return qbittorrentInstallMetadata{}, false
	}
	var metadata qbittorrentInstallMetadata
	if json.Unmarshal(raw, &metadata) != nil || metadata.Tag == "" {
		return qbittorrentInstallMetadata{}, false
	}
	return metadata, true
}

func qbittorrentGitHubClient() *http.Client {
	return &http.Client{Timeout: 45 * time.Second}
}

func fetchQbittorrentLatest(ctx context.Context) (qbittorrentRelease, qbittorrentAsset, error) {
	qbittorrentLatestMu.Lock()
	if !qbittorrentLatestCache.at.IsZero() && time.Since(qbittorrentLatestCache.at) < 5*time.Minute {
		cached := qbittorrentLatestCache
		qbittorrentLatestMu.Unlock()
		return cached.release, cached.asset, cached.err
	}
	qbittorrentLatestMu.Unlock()
	qbittorrentLatestFetchMu.Lock()
	defer qbittorrentLatestFetchMu.Unlock()
	// Another concurrent caller may have filled the cache while this caller
	// waited for the fetch lock.
	qbittorrentLatestMu.Lock()
	if !qbittorrentLatestCache.at.IsZero() && time.Since(qbittorrentLatestCache.at) < 5*time.Minute {
		cached := qbittorrentLatestCache
		qbittorrentLatestMu.Unlock()
		return cached.release, cached.asset, cached.err
	}
	qbittorrentLatestMu.Unlock()
	release, asset, err := fetchQbittorrentLatestRemote(ctx)
	qbittorrentLatestMu.Lock()
	qbittorrentLatestCache = struct {
		at      time.Time
		release qbittorrentRelease
		asset   qbittorrentAsset
		err     error
	}{time.Now(), release, asset, err}
	qbittorrentLatestMu.Unlock()
	return release, asset, err
}

func fetchQbittorrentLatestRemote(ctx context.Context) (qbittorrentRelease, qbittorrentAsset, error) {
	arch, err := qbittorrentAssetArch()
	if err != nil {
		return qbittorrentRelease{}, qbittorrentAsset{}, err
	}
	client := qbittorrentGitHubClient()
	fetch := func(endpoint string, target any) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "gextto-qbittorrent-manager")
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GitHub API: HTTP %s", response.Status)
		}
		return json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(target)
	}
	var release qbittorrentRelease
	if err := fetch(qbittorrentGitHubAPI+"/releases/latest", &release); err != nil {
		return qbittorrentRelease{}, qbittorrentAsset{}, err
	}
	findAsset := func(item qbittorrentRelease) (qbittorrentAsset, bool) {
		for _, asset := range item.Assets {
			if asset.Name == arch+"-qbittorrent-nox" && asset.Size > 0 && asset.DownloadURL != "" {
				return asset, true
			}
		}
		return qbittorrentAsset{}, false
	}
	asset, ok := findAsset(release)
	if !ok || !qbittorrentIsLibtorrentV2Release(release) {
		// Keep the guarantee that the managed runtime is a v2 build even if the
		// repository's /latest pointer is temporarily moved to another channel.
		var releases []qbittorrentRelease
		if err := fetch(qbittorrentGitHubAPI+"/releases?per_page=30", &releases); err != nil {
			return qbittorrentRelease{}, qbittorrentAsset{}, err
		}
		for _, candidate := range releases {
			candidateAsset, candidateOK := findAsset(candidate)
			if candidateOK && qbittorrentIsLibtorrentV2Release(candidate) {
				release, asset, ok = candidate, candidateAsset, true
				break
			}
		}
	}
	if !ok {
		return qbittorrentRelease{}, qbittorrentAsset{}, fmt.Errorf("nessun asset qBittorrent libtorrent v2 per %s", arch)
	}
	if !strings.HasPrefix(asset.Digest, "sha256:") {
		return qbittorrentRelease{}, qbittorrentAsset{}, errors.New("GitHub non ha pubblicato il digest SHA-256 dell'asset")
	}
	parsed, err := url.Parse(asset.DownloadURL)
	digest := strings.TrimPrefix(asset.Digest, "sha256:")
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || !strings.HasPrefix(parsed.Path, "/userdocs/qbittorrent-nox-static/releases/download/") || len(digest) != sha256.Size*2 {
		return qbittorrentRelease{}, qbittorrentAsset{}, errors.New("URL asset GitHub non attendibile")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return qbittorrentRelease{}, qbittorrentAsset{}, errors.New("digest SHA-256 GitHub non valido")
	}
	return release, asset, nil
}

func getQbittorrentRuntimeStatus(ctx context.Context, cfg *Config) (qbittorrentRuntimeStatus, error) {
	if cfg == nil {
		return qbittorrentRuntimeStatus{}, errors.New("configurazione Gextto assente")
	}
	status := qbittorrentRuntimeStatus{
		Managed:      qbittorrentManagedEnabled(cfg),
		Directory:    qbittorrentManagedDir(cfg),
		Binary:       qbittorrentManagedBinary(cfg),
		Architecture: runtime.GOARCH,
		Libtorrent:   "2.x",
	}
	metadata, ok := qbittorrentReadInstallMetadata(cfg)
	status.Installed = ok && fileExists(status.Binary)
	if ok {
		status.InstalledTag, status.InstalledName = metadata.Tag, metadata.Name
	}
	release, asset, err := fetchQbittorrentLatest(ctx)
	if err != nil {
		return status, err
	}
	status.LatestTag, status.LatestName, status.LatestURL = release.TagName, release.Name, release.HTMLURL
	status.Asset = asset.Name
	status.UpdateAvailable = !status.Installed || status.InstalledTag != status.LatestTag
	return status, nil
}

func installQbittorrentRuntime(ctx context.Context, cfg *Config) (qbittorrentRuntimeStatus, error) {
	qbittorrentInstallMu.Lock()
	defer qbittorrentInstallMu.Unlock()
	if cfg == nil {
		return qbittorrentRuntimeStatus{}, errors.New("configurazione Gextto assente")
	}
	release, asset, err := fetchQbittorrentLatest(ctx)
	if err != nil {
		return qbittorrentRuntimeStatus{}, err
	}
	dir, err := ensureQbittorrentManagedDir(cfg)
	if err != nil {
		return qbittorrentRuntimeStatus{}, fmt.Errorf("creazione cartella qBittorrent: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".qbittorrent-nox.download-*")
	if err != nil {
		return qbittorrentRuntimeStatus{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	defer tmp.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.DownloadURL, nil)
	if err != nil {
		return qbittorrentRuntimeStatus{}, err
	}
	req.Header.Set("User-Agent", "gextto-qbittorrent-manager")
	response, err := qbittorrentGitHubClient().Do(req)
	if err != nil {
		return qbittorrentRuntimeStatus{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return qbittorrentRuntimeStatus{}, fmt.Errorf("download qBittorrent: HTTP %s", response.Status)
	}
	if asset.Size > qbittorrentMaxBinary {
		return qbittorrentRuntimeStatus{}, fmt.Errorf("asset qBittorrent troppo grande: %d byte", asset.Size)
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(response.Body, qbittorrentMaxBinary+1))
	if err != nil {
		return qbittorrentRuntimeStatus{}, err
	}
	if written != asset.Size {
		return qbittorrentRuntimeStatus{}, fmt.Errorf("download incompleto: %d/%d byte", written, asset.Size)
	}
	want := strings.TrimPrefix(asset.Digest, "sha256:")
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), want) {
		return qbittorrentRuntimeStatus{}, errors.New("verifica SHA-256 qBittorrent fallita")
	}
	if err := tmp.Chmod(0o755); err != nil {
		return qbittorrentRuntimeStatus{}, err
	}
	if err := tmp.Close(); err != nil {
		return qbittorrentRuntimeStatus{}, err
	}
	binary := qbittorrentManagedBinary(cfg)
	backupPath, err := backupQbittorrentBinary(binary)
	if err != nil {
		return qbittorrentRuntimeStatus{}, err
	}
	if err := os.Rename(tmpPath, binary); err != nil {
		return qbittorrentRuntimeStatus{}, fmt.Errorf("installazione binario qBittorrent: %w", err)
	}
	metadata := qbittorrentInstallMetadata{
		Tag: release.TagName, Name: release.Name, Asset: asset.Name,
		Libtorrent: "2.x", SHA256: want, InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	encoded, _ := json.MarshalIndent(metadata, "", "  ")
	metadataPath := filepath.Join(dir, "install.json")
	tmpMetadata := metadataPath + ".tmp"
	if err := os.WriteFile(tmpMetadata, encoded, 0o644); err != nil {
		_ = rollbackQbittorrentBinary(binary, backupPath)
		return qbittorrentRuntimeStatus{}, err
	}
	if err := os.Rename(tmpMetadata, metadataPath); err != nil {
		_ = os.Remove(tmpMetadata)
		_ = rollbackQbittorrentBinary(binary, backupPath)
		return qbittorrentRuntimeStatus{}, err
	}
	return getQbittorrentRuntimeStatus(ctx, cfg)
}

func qbittorrentPasswordHash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := pbkdf2.Key([]byte(password), salt, 100000, 64, sha512.New)
	return base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(derived), nil
}

func qbittorrentProfileConfigPath(cfg *Config) string {
	return filepath.Join(qbittorrentManagedDir(cfg), "profile", "qBittorrent", "config", "qBittorrent.conf")
}

// writeManagedQbittorrentConfig updates only the private portable profile. It
// uses qBittorrent's native INI sections and preserves unrelated preferences
// already created by qBittorrent.
func writeManagedQbittorrentConfig(cfg *Config, settings qbittorrentSettings) error {
	if cfg == nil {
		return errors.New("configurazione Gextto assente")
	}
	if settings.Client.Username == "" || settings.Client.Password == "" {
		return errors.New("credenziali qBittorrent gestito non configurate")
	}
	hash, err := qbittorrentPasswordHash(settings.Client.Password)
	if err != nil {
		return fmt.Errorf("hash password qBittorrent: %w", err)
	}
	path := qbittorrentProfileConfigPath(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("lettura profilo qBittorrent: %w", err)
	}
	content := string(data)
	content = qbittorrentINISet(content, "Preferences", "WebUI\\Enabled", "true")
	content = qbittorrentINISet(content, "Preferences", "WebUI\\Address", "127.0.0.1")
	content = qbittorrentINISet(content, "Preferences", "WebUI\\Port", managedQbittorrentPort(settings.Client.BaseURL))
	content = qbittorrentINISet(content, "Preferences", "WebUI\\Username", settings.Client.Username)
	// QSettings serializes QByteArray values using the @ByteArray(...) marker;
	// qBittorrent expects that marker when reading Password_PBKDF2 from disk.
	content = qbittorrentINISet(content, "Preferences", "WebUI\\Password_PBKDF2", "@ByteArray("+hash+")")
	content = qbittorrentINISet(content, "Preferences", "Downloads\\SavePath", cfg.LibtorrentDir)
	if cfg.LibtorrentTempDir != nil && strings.TrimSpace(*cfg.LibtorrentTempDir) != "" {
		content = qbittorrentINISet(content, "Preferences", "Downloads\\TempPathEnabled", "true")
		content = qbittorrentINISet(content, "Preferences", "Downloads\\TempPath", *cfg.LibtorrentTempDir)
	}
	content = qbittorrentINISet(content, "LegalNotice", "Accepted", "true")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return fmt.Errorf("scrittura profilo qBittorrent: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("installazione configurazione qBittorrent: %w", err)
	}
	return nil
}

func managedQbittorrentPort(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "8080"
	}
	if port := parsed.Port(); port != "" {
		return port
	}
	if parsed.Scheme == "https" {
		return "443"
	}
	return "8080"
}

func qbittorrentINISet(content, section, key, value string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	sectionHeader := "[" + section + "]"
	sectionStart := -1
	sectionEnd := len(lines)
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == sectionHeader {
			sectionStart = index
			continue
		}
		if sectionStart >= 0 && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionEnd = index
			break
		}
	}
	if sectionStart < 0 {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, sectionHeader, key+"="+value)
		return strings.Join(lines, "\n")
	}
	for index := sectionStart + 1; index < sectionEnd; index++ {
		trimmed := strings.TrimSpace(lines[index])
		if strings.HasPrefix(trimmed, key+"=") {
			prefix := lines[index][:len(lines[index])-len(strings.TrimLeft(lines[index], " \t"))]
			lines[index] = prefix + key + "=" + value
			return strings.Join(lines, "\n")
		}
	}
	lines = append(lines[:sectionEnd], append([]string{key + "=" + value}, lines[sectionEnd:]...)...)
	return strings.Join(lines, "\n")
}

// backupQbittorrentBinary preserves an installed managed binary before an
// update. A symlink or directory at the managed path is rejected instead of
// being replaced, so the installer cannot unexpectedly follow or destroy it.
func backupQbittorrentBinary(binary string) (string, error) {
	info, err := os.Lstat(binary)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("verifica binario qBittorrent esistente: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("il percorso del binario qBittorrent non è un file regolare: %s", binary)
	}
	backup := fmt.Sprintf("%s.backup-%s", binary, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Link(binary, backup); err == nil {
		return verifyQbittorrentBackup(binary, backup)
	}
	// Hard links can be unavailable on some filesystems. Copy into an
	// exclusively-created backup without ever overwriting an older backup.
	source, err := os.Open(binary)
	if err != nil {
		return "", fmt.Errorf("backup binario qBittorrent: %w", err)
	}
	defer source.Close()
	destination, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return "", fmt.Errorf("creazione backup binario qBittorrent: %w", err)
	}
	if _, copyErr := io.Copy(destination, source); copyErr != nil {
		_ = destination.Close()
		_ = os.Remove(backup)
		return "", fmt.Errorf("copia backup binario qBittorrent: %w", copyErr)
	}
	if err := destination.Close(); err != nil {
		_ = os.Remove(backup)
		return "", fmt.Errorf("chiusura backup binario qBittorrent: %w", err)
	}
	return verifyQbittorrentBackup(binary, backup)
}

func verifyQbittorrentBackup(original, backup string) (string, error) {
	originalHash, err := sha256QbittorrentFile(original)
	if err != nil {
		return "", fmt.Errorf("verifica binario qBittorrent originale: %w", err)
	}
	backupHash, err := sha256QbittorrentFile(backup)
	if err != nil {
		return "", fmt.Errorf("verifica backup binario qBittorrent: %w", err)
	}
	if originalHash != backupHash {
		return "", errors.New("backup binario qBittorrent non corrisponde all'originale")
	}
	return backup, nil
}

func sha256QbittorrentFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func rollbackQbittorrentBinary(binary, backup string) error {
	if backup == "" {
		return os.Remove(binary)
	}
	if err := os.Remove(binary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(backup, binary)
}

type managedQbittorrentProcess struct {
	cmd     *exec.Cmd
	logFile *os.File
	mu      sync.Mutex
	closed  bool
	// done receives the exit error once the process terminates, so the
	// supervisor can wait without racing the Wait goroutine.
	done chan error
}

// wait blocks until the managed process exits and returns its exit error.
func (p *managedQbittorrentProcess) wait() error {
	if p == nil || p.done == nil {
		return nil
	}
	err, ok := <-p.done
	if !ok {
		return nil
	}
	return err
}

func startManagedQbittorrent(cfg *Config, settings qbittorrentSettings) (*managedQbittorrentProcess, error) {
	if !settings.Managed {
		return nil, nil
	}
	binary := qbittorrentManagedBinary(cfg)
	if binary == "" || !fileExists(binary) {
		return nil, fmt.Errorf("qBittorrent gestito non installato: usa il pulsante di download nella configurazione")
	}
	parsed, err := url.Parse(settings.Client.BaseURL)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("URL qBittorrent non valido per l'avvio gestito")
	}
	if parsed.Scheme != "http" {
		return nil, errors.New("qBittorrent gestito richiede Web API HTTP locale")
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); ip != nil && !ip.IsLoopback() || ip == nil && !strings.EqualFold(host, "localhost") {
		return nil, errors.New("qBittorrent gestito richiede un URL locale (localhost/loopback)")
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
		if parsed.Scheme == "http" {
			port = "80"
		}
	}
	dir, err := ensureQbittorrentManagedDir(cfg)
	if err != nil {
		return nil, err
	}
	profile := filepath.Join(dir, "profile")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return nil, err
	}
	if err := writeManagedQbittorrentConfig(cfg, settings); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "qbittorrent.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	command := exec.Command(binary, "--profile="+profile, "--webui-port="+port, "--confirm-legal-notice")
	command.Dir = dir
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("avvio qBittorrent gestito: %w", err)
	}
	process := &managedQbittorrentProcess{cmd: command, logFile: logFile, done: make(chan error, 1)}
	go func() {
		err := command.Wait()
		_ = logFile.Close()
		process.done <- err
		close(process.done)
	}()
	return process, nil
}

func (p *managedQbittorrentProcess) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		_ = p.cmd.Process.Kill()
		return err
	}
	return nil
}
