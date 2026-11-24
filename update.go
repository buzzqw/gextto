package gextto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
)

// DefaultRepo is the repository used when none is configured.
const DefaultRepo = "buzzqw/gextto"

// ServiceName is the systemd unit updated/restarted after an install.
const ServiceName = "gextto.service"

// ArchiveStem is the prefix of the per-architecture release asset.
const ArchiveStem = "gextto-linux"

// VersionString returns the installed version, including the release marker
// written next to the executable by the installer (when present) and the
// compiled-in build and commit.
func VersionString() string {
	value := fmt.Sprintf("%s %s", constants.AppName, constants.VersionLabel())
	if marker, ok := updateInstalledMarker(); ok {
		value += fmt.Sprintf(" [%s]", marker)
	}
	return value
}

func updateInstalledMarker() (string, bool) {
	executable, err := os.Executable()
	if err != nil {
		return "", false
	}
	// Only the marker next to the executable counts: installers and the release
	// archive write `VERSION` there. Looking at the parent directory would pick
	// up an unrelated VERSION file during development.
	markerPath := filepath.Join(filepath.Dir(executable), "VERSION")
	text, err := os.ReadFile(markerPath)
	if err != nil {
		return "", false
	}
	trimmed := strings.TrimSpace(string(text))
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

// ReleaseName resolves the release name to download.
func ReleaseName(opts UpdateOptions) string {
	if opts.Release != nil {
		return *opts.Release
	}
	if opts.Channel != nil {
		return *opts.Channel
	}
	if value, ok := os.LookupEnv("GEXTTO_VERSION"); ok {
		return value
	}
	if value, ok := os.LookupEnv("GEXTTO_CHANNEL"); ok {
		return value
	}
	if marker, ok := updateInstalledMarker(); ok {
		return ChannelFromMarker(marker)
	}
	return "continuous"
}

// ChannelFromMarker maps the installed release marker to the channel that
// updates it: a tagged release ("v1.2.3") follows the stable releases
// ("latest"), anything else ("continuous", "continuous-<sha>") the continuous
// build. An update never silently switches channel.
func ChannelFromMarker(marker string) string {
	marker = strings.TrimSpace(marker)
	if len(marker) > 1 && marker[0] == 'v' && marker[1] >= '0' && marker[1] <= '9' {
		return "latest"
	}
	if marker == "latest" || marker == "stable" {
		return "latest"
	}
	return "continuous"
}

// ArchiveURL builds the release asset URL for a repository, release and
// architecture.
func ArchiveURL(repo, release, arch string) string {
	asset := fmt.Sprintf("%s-%s.tar.gz", ArchiveStem, arch)
	if release == "latest" || release == "stable" {
		return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", repo, asset)
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, release, asset)
}

func updateTargetArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("unsupported CPU architecture for updates: %s", runtime.GOARCH)
	}
}

// Run executes `gexttod --update`.
func Run(ctx context.Context, opts UpdateOptions) error {
	repo := DefaultRepo
	if opts.Repo != nil {
		repo = *opts.Repo
	} else if value, ok := os.LookupEnv("GEXTTO_REPO"); ok {
		repo = value
	}
	release := ReleaseName(opts)
	installDir, err := updateResolveInstallDir(opts)
	if err != nil {
		return err
	}

	fmt.Println("Gextto updater")
	fmt.Printf("  release:    %s\n", release)
	fmt.Printf("  repository: %s\n", repo)
	fmt.Printf("  install:    %s\n", installDir)

	if marker, ok := updateInstalledMarker(); ok {
		if opts.Release != nil && marker == release && !opts.Force {
			fmt.Printf("Already at release %s; nothing to do (use --force to reinstall).\n", release)
			return nil
		}
	}

	work, err := NewWorkDir()
	if err != nil {
		return err
	}
	defer work.Cleanup()

	var archive string
	if opts.Archive != nil {
		archive = *opts.Archive
		if !updateIsFile(archive) {
			return fmt.Errorf("local archive not found: %s", archive)
		}
		fmt.Printf("  source:     local archive %s\n", archive)
		checksum := archive + ".sha256"
		if updateIsFile(checksum) {
			if err := verifyLocalChecksum(archive, checksum); err != nil {
				return err
			}
		} else {
			fmt.Println("  checksum:   no .sha256 next to the local archive")
		}
	} else {
		arch, err := updateTargetArch()
		if err != nil {
			return err
		}
		url := ArchiveURL(repo, release, arch)
		destination := filepath.Join(work.Path(), path.Base(url))
		fmt.Printf("  downloading %s\n", url)
		if err := updateDownload(ctx, url, destination); err != nil {
			return err
		}
		if err := verifyRemoteChecksum(ctx, destination, url+".sha256"); err != nil {
			return err
		}
		archive = destination
	}

	extractDir := filepath.Join(work.Path(), "payload")
	if err := os.MkdirAll(extractDir, 0o700); err != nil {
		return err
	}
	if err := updateExtract(archive, extractDir); err != nil {
		return err
	}

	root, ok := updateFindReleaseRoot(extractDir)
	if !ok {
		return fmt.Errorf("release archive does not contain a %s executable", constants.AppName)
	}
	if err := updateValidateRelease(root); err != nil {
		return err
	}

	transaction, err := updateInstallReleaseTransaction(root, installDir)
	if err != nil {
		return err
	}
	if err := updateRestartServiceWithTransaction(opts, transaction); err != nil {
		return err
	}
	fmt.Printf("Gextto updated to %s in %s\n", release, installDir)
	return nil
}

func updateResolveInstallDir(opts UpdateOptions) (string, error) {
	if opts.InstallDir != nil {
		return *opts.InstallDir, nil
	}
	if value, ok := os.LookupEnv("GEXTTO_INSTALL_DIR"); ok {
		return value, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate the running executable: %w", err)
	}
	if executable == "" {
		return "", fmt.Errorf("cannot determine the installation directory from the executable path")
	}
	return filepath.Dir(executable), nil
}

func updateUserAgent() string {
	return "gexttod/" + constants.AppVersion()
}

func updateDownload(ctx context.Context, url, destination string) error {
	client := &http.Client{Timeout: 1800 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	request.Header.Set("User-Agent", updateUserAgent())
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("download failed for %s: HTTP %s", url, response.Status)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", destination, err)
	}
	// Stream to disk with a cap instead of buffering the whole archive in
	// memory. A failed/oversized download leaves no partial file behind.
	if err := copyLimited(file, response.Body, maxUpdateBytes); err != nil {
		_ = file.Close()
		_ = os.Remove(destination)
		return fmt.Errorf("cannot read the download from %s: %w", url, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(destination)
		return fmt.Errorf("cannot write %s: %w", destination, err)
	}
	return nil
}

// verifyRemoteChecksum verifies the archive against the release's `.sha256`
// asset. The checksum is required: every published payload ships one, and an
// update that cannot verify what it installs is refused (the installer has the
// same rule). A local `--archive` without a `.sha256` is still allowed.
func verifyRemoteChecksum(ctx context.Context, archive, checksumURL string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return fmt.Errorf("checksum: cannot request %s: %w", checksumURL, err)
	}
	request.Header.Set("User-Agent", updateUserAgent())
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("checksum: cannot download %s: %w", checksumURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return fmt.Errorf("checksum: this release does not publish %s; refusing to install an unverified payload", checksumURL)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("checksum: HTTP %d for %s", response.StatusCode, checksumURL)
	}
	body, err := readLimitedBody(response.Body, maxAPIResponseBytes)
	if err != nil {
		return err
	}
	expected := ""
	if fields := strings.Fields(string(body)); len(fields) > 0 {
		expected = strings.ToLower(fields[0])
	}
	if len(expected) != 64 {
		return fmt.Errorf("checksum: malformed .sha256 for %s", filepath.Base(archive))
	}
	return updateCompareChecksum(archive, expected)
}

func verifyLocalChecksum(archive, checksum string) error {
	body, err := os.ReadFile(checksum)
	if err != nil {
		return err
	}
	expected := ""
	if fields := strings.Fields(string(body)); len(fields) > 0 {
		expected = fields[0]
	}
	if len(expected) != 64 {
		return fmt.Errorf("malformed checksum file: %s", checksum)
	}
	return updateCompareChecksum(archive, expected)
}

func updateCompareChecksum(archive, expected string) error {
	actual, err := updateSha256File(archive)
	if err != nil {
		return err
	}
	if actual != strings.ToLower(expected) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archive, expected, actual)
	}
	fmt.Println("  checksum:   verified")
	return nil
}

func updateSha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	buffer := make([]byte, 64*1024)
	for {
		read, err := file.Read(buffer)
		if read > 0 {
			hasher.Write(buffer[:read])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func updateExtract(archive, destination string) error {
	// --no-same-owner: the archive is checksum-verified, but the updater runs as
	// root and must never hand files the numeric owner stored at build time.
	cmd := exec.Command("tar", "--no-same-owner", "-xzf", archive, "-C", destination)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("cannot run tar (is it installed?): %w", err)
		}
		return fmt.Errorf("tar failed to extract %s", archive)
	}
	return nil
}

// updateFindReleaseRoot finds the directory that contains the `gexttod` executable.
func updateFindReleaseRoot(directory string) (string, bool) {
	direct := filepath.Join(directory, constants.AppName)
	if updateIsFile(direct) {
		return directory, true
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			if found, ok := updateFindReleaseRoot(filepath.Join(directory, entry.Name())); ok {
				return found, true
			}
		}
	}
	return "", false
}

func updateValidateRelease(root string) error {
	for _, name := range []string{constants.AppName, "gx-torrent", "run.sh", "VERSION"} {
		if !updateIsFile(filepath.Join(root, name)) {
			return fmt.Errorf("release is missing %s", name)
		}
	}
	return nil
}

// updateInstallRelease installs a complete payload and keeps the previous
// binaries for the engine supervisor. Run uses updateInstallReleaseTransaction
// directly so a failed health check can restore every installed component.
func updateInstallRelease(root, installDir string) error {
	transaction, err := updateInstallReleaseTransaction(root, installDir)
	if err != nil {
		return err
	}
	transaction.finish()
	return nil
}

var updatePayloadFiles = []string{
	constants.AppName, "gx-torrent", "run.sh", "VERSION",
	"README.md", "README.it.md", "LICENSE", "THIRD-PARTY-NOTICES",
	"install-service.sh", "uninstall-service.sh", "gextto.service",
}

var updatePayloadDirs = []string{"ui", "lib"}

type updateInstallEntry struct {
	name        string
	staged      string
	destination string
	previous    string
	removeOnly  bool
	touched     bool
	installed   bool
}

// updateInstallTransaction owns the snapshots taken while replacing the live
// payload. They stay beside the installation (same filesystem) until the new
// service has answered its expected health response.
type updateInstallTransaction struct {
	installDir  string
	staging     string
	rollbackDir string
	entries     []updateInstallEntry
	finished    bool
}

func updateInstallReleaseTransaction(root, installDir string) (*updateInstallTransaction, error) {
	return updateInstallReleaseWithCheckpoint(root, installDir, nil)
}

// updateInstallReleaseWithCheckpoint is the transaction boundary used by the
// focused rollback tests. A non-nil checkpoint runs after each live entry has
// been committed; its error rolls the whole payload back.
func updateInstallReleaseWithCheckpoint(root, installDir string, checkpoint func(int) error) (*updateInstallTransaction, error) {
	if err := updateValidateRelease(root); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", installDir, err)
	}
	staging, err := os.MkdirTemp(installDir, ".gextto-update-stage-")
	if err != nil {
		return nil, fmt.Errorf("cannot create the update staging directory: %w", err)
	}
	rollback, err := os.MkdirTemp(installDir, ".gextto-update-rollback-")
	if err != nil {
		updateRemovePath(staging)
		return nil, fmt.Errorf("cannot create the update rollback directory: %w", err)
	}
	transaction := &updateInstallTransaction{installDir: installDir, staging: staging, rollbackDir: rollback}
	ready := false
	defer func() {
		if !ready {
			updateRemovePath(staging)
			// A failed rollback keeps its snapshots for manual recovery.
			if !updatePathExists(rollback) || updateDirEmpty(rollback) {
				updateRemovePath(rollback)
			}
		}
	}()

	// Stage every component, including the previous binaries, before the first
	// live path is moved. Missing optional files are removal entries so stale
	// payload files cannot survive an update.
	for _, name := range updatePayloadFiles {
		staged := filepath.Join(staging, name)
		source := filepath.Join(root, name)
		if !updatePathExists(source) {
			transaction.entries = append(transaction.entries, updateInstallEntry{
				name: name, destination: filepath.Join(installDir, name), previous: filepath.Join(rollback, name), removeOnly: true,
			})
			continue
		}
		if !updateIsFile(source) {
			updateRemovePath(rollback)
			return nil, fmt.Errorf("release entry %s is not a regular file", name)
		}
		if err := updateCopyFileMode(source, staged); err != nil {
			return nil, fmt.Errorf("cannot stage %s: %w", name, err)
		}
		if err := updateChmodPayloadFile(staged, name); err != nil {
			return nil, err
		}
		transaction.entries = append(transaction.entries, updateInstallEntry{
			name: name, staged: staged, destination: filepath.Join(installDir, name), previous: filepath.Join(rollback, name),
		})
	}
	for _, name := range updatePayloadDirs {
		staged := filepath.Join(staging, name)
		source := filepath.Join(root, name)
		if updatePathExists(source) {
			if !updateIsDir(source) {
				updateRemovePath(rollback)
				return nil, fmt.Errorf("release entry %s is not a directory", name)
			}
			if err := updateCopyDir(source, staged); err != nil {
				return nil, fmt.Errorf("cannot stage the %s directory: %w", name, err)
			}
		} else if err := os.MkdirAll(staged, 0o755); err != nil {
			return nil, fmt.Errorf("cannot stage an empty %s directory: %w", name, err)
		}
		if err := os.Chmod(staged, 0o755); err != nil {
			return nil, err
		}
		transaction.entries = append(transaction.entries, updateInstallEntry{
			name: name, staged: staged, destination: filepath.Join(installDir, name), previous: filepath.Join(rollback, name),
		})
	}

	// The running engine supervisor recognises .prev. Stage those copies before
	// committing and include them in the transaction, so a health-check rollback
	// restores the previous rollback point as well as the live payload.
	for _, name := range []string{constants.AppName, "gx-torrent"} {
		previousName := name + ".prev"
		source := filepath.Join(installDir, name)
		entry := updateInstallEntry{
			name: previousName, destination: filepath.Join(installDir, previousName),
			previous: filepath.Join(rollback, previousName), removeOnly: true,
		}
		if updateIsFile(source) {
			staged := filepath.Join(staging, previousName)
			if err := updateCopyFileMode(source, staged); err != nil {
				return nil, fmt.Errorf("cannot stage %s: %w", previousName, err)
			}
			if err := updateSetExecutable(staged); err != nil {
				return nil, err
			}
			entry.staged, entry.removeOnly = staged, false
		}
		transaction.entries = append(transaction.entries, entry)
	}
	versionPrev := "VERSION.prev"
	versionEntry := updateInstallEntry{
		name: versionPrev, destination: filepath.Join(installDir, versionPrev),
		previous: filepath.Join(rollback, versionPrev), removeOnly: true,
	}
	if updateIsFile(filepath.Join(installDir, "VERSION")) {
		staged := filepath.Join(staging, versionPrev)
		if err := updateCopyFileMode(filepath.Join(installDir, "VERSION"), staged); err != nil {
			return nil, fmt.Errorf("cannot stage %s: %w", versionPrev, err)
		}
		if err := os.Chmod(staged, 0o644); err != nil {
			return nil, err
		}
		versionEntry.staged, versionEntry.removeOnly = staged, false
	}
	transaction.entries = append(transaction.entries, versionEntry)
	for _, name := range []string{"gx-ed2k", "gx-ed2k.prev"} {
		transaction.entries = append(transaction.entries, updateInstallEntry{
			name: name, destination: filepath.Join(installDir, name), previous: filepath.Join(rollback, name), removeOnly: true,
		})
	}

	committed := 0
	for index := range transaction.entries {
		entry := &transaction.entries[index]
		if updatePathExists(entry.destination) {
			if err := os.Rename(entry.destination, entry.previous); err != nil {
				rollbackErr := transaction.rollback()
				return nil, errors.Join(fmt.Errorf("cannot move the current %s aside: %w", entry.name, err), rollbackErr)
			}
		}
		entry.touched = true
		if !entry.removeOnly {
			if err := os.Rename(entry.staged, entry.destination); err != nil {
				rollbackErr := transaction.rollback()
				return nil, errors.Join(fmt.Errorf("cannot install %s: %w", entry.name, err), rollbackErr)
			}
			entry.installed = true
		}
		committed++
		if checkpoint != nil {
			if err := checkpoint(committed); err != nil {
				rollbackErr := transaction.rollback()
				return nil, errors.Join(err, rollbackErr)
			}
		}
	}
	if err := os.RemoveAll(staging); err != nil {
		rollbackErr := transaction.rollback()
		return nil, errors.Join(fmt.Errorf("cannot remove the update staging directory: %w", err), rollbackErr)
	}
	ready = true
	return transaction, nil
}

func updateChmodPayloadFile(path, name string) error {
	switch name {
	case constants.AppName, "gx-torrent", "run.sh", "install-service.sh", "uninstall-service.sh":
		return updateSetExecutable(path)
	default:
		return os.Chmod(path, 0o644)
	}
}

func (transaction *updateInstallTransaction) rollback() error {
	if transaction == nil || transaction.finished {
		return nil
	}
	var rollbackErrors []error
	for index := len(transaction.entries) - 1; index >= 0; index-- {
		entry := &transaction.entries[index]
		if !entry.touched {
			continue
		}
		if entry.installed {
			if err := os.RemoveAll(entry.destination); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("cannot remove the new %s during rollback: %w", entry.name, err))
				continue
			}
		}
		if updatePathExists(entry.previous) {
			if err := os.Rename(entry.previous, entry.destination); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("cannot restore %s during rollback: %w", entry.name, err))
			}
		}
	}
	updateRemovePath(transaction.staging)
	if len(rollbackErrors) == 0 {
		updateRemovePath(transaction.rollbackDir)
		transaction.finished = true
		return nil
	}
	return fmt.Errorf("the previous payload is only partially restored; snapshots remain in %s: %w", transaction.rollbackDir, errors.Join(rollbackErrors...))
}

func (transaction *updateInstallTransaction) finish() {
	if transaction == nil || transaction.finished {
		return
	}
	if err := os.RemoveAll(transaction.staging); err != nil {
		logging.Warn("Could not remove the update staging directory", "path", transaction.staging, "error", err)
	}
	if err := os.RemoveAll(transaction.rollbackDir); err != nil {
		logging.Warn("Could not remove the update rollback snapshots", "path", transaction.rollbackDir, "error", err)
	}
	transaction.finished = true
}

func updateDirEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) == 0
}

func updateRestartService(opts UpdateOptions) error {
	return updateRestartServiceWithTransaction(opts, nil)
}

func updateRestartServiceWithTransaction(opts UpdateOptions, transaction *updateInstallTransaction) error {
	return updateRestartServiceWithHooks(opts, transaction, updateHealthLimit,
		func() bool { return syscall.Geteuid() == 0 },
		updateSystemctl,
		updateWaitHealthy,
	)
}

// updateRestartServiceWithHooks takes the health limit explicitly so focused
// tests can pass a millisecond deadline instead of the production three
// minutes. Nothing here starts a timer of its own: the limit only reaches the
// injected `healthy` hook (and the error text describing it).
func updateRestartServiceWithHooks(opts UpdateOptions, transaction *updateInstallTransaction, limit time.Duration, privileged func() bool, systemctl func(...string) error, healthy func(time.Duration) updateHealth) error {
	if !opts.Restart {
		transaction.finish()
		fmt.Println("Service not restarted (--no-restart).")
		return nil
	}
	if !privileged() {
		transaction.finish()
		fmt.Printf("Run `sudo systemctl restart %s` to start the new version.\n", ServiceName)
		return nil
	}
	if err := systemctl("restart", ServiceName); err != nil {
		return updateRollbackFailedUpdate(opts, transaction, fmt.Errorf("could not restart %s: %w", ServiceName, err), limit, systemctl, healthy)
	}
	if healthy(limit) == updateHealthy {
		transaction.finish()
		fmt.Printf("Service %s restarted and answering.\n", ServiceName)
		return nil
	}
	return updateRollbackFailedUpdate(opts, transaction, fmt.Errorf("%s did not return the expected status response within %s", ServiceName, limit), limit, systemctl, healthy)
}

func updateRollbackFailedUpdate(opts UpdateOptions, transaction *updateInstallTransaction, cause error, limit time.Duration, systemctl func(...string) error, healthy func(time.Duration) updateHealth) error {
	fmt.Fprintf(os.Stderr, "%v: restoring the previous version.\n", cause)
	var rollbackErr error
	if transaction != nil {
		rollbackErr = transaction.rollback()
	} else {
		rollbackErr = updateRollbackLegacyBinary(opts)
	}
	if rollbackErr != nil {
		return errors.Join(cause, rollbackErr)
	}
	if err := systemctl("restart", ServiceName); err != nil {
		return errors.Join(cause, fmt.Errorf("the previous version could not be restarted: %w", err))
	}
	if healthy(limit) != updateHealthy {
		return errors.Join(cause, fmt.Errorf("the previous version did not return the expected status response either; inspect: journalctl -u %s -n 100", ServiceName))
	}
	return fmt.Errorf("%w; the previous version was restored and is running", cause)
}

// updateRollbackLegacyBinary keeps the standalone helper usable for callers
// that do not have an updateInstallTransaction. The normal updater always has
// one and restores the complete payload, not only the executable.
func updateRollbackLegacyBinary(opts UpdateOptions) error {
	installDir, err := updateResolveInstallDir(opts)
	if err != nil {
		return err
	}
	previous := filepath.Join(installDir, constants.AppName+".prev")
	if !updateIsFile(previous) {
		return fmt.Errorf("no previous version is available in %s", installDir)
	}
	binary := filepath.Join(installDir, constants.AppName)
	staged := binary + ".rollback"
	if err := updateCopyFileMode(previous, staged); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}
	if err := os.Rename(staged, binary); err != nil {
		updateRemovePath(staged)
		return fmt.Errorf("rollback failed: %w", err)
	}
	if previousMarker, err := os.ReadFile(filepath.Join(installDir, "VERSION.prev")); err == nil {
		if err := os.WriteFile(filepath.Join(installDir, "VERSION"), previousMarker, 0o644); err != nil {
			return fmt.Errorf("rollback failed to restore VERSION: %w", err)
		}
	}
	return nil
}

func updateSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// updateHealthLimit is how long a restarted service gets to answer: the first
// start after an update may migrate the databases.
const updateHealthLimit = 3 * time.Minute

type updateHealth int

const (
	updateHealthy updateHealth = iota // active and the web interface answers
	updateCrashed                     // left "active": failed or restarting
	updateSlow                        // still active, not answering yet
)

// updateWaitHealthy watches the service after a restart. The expected
// /api/status JSON proves the new version works; a 401/403 proves the protected
// daemon is answering when the operator disabled the local login bypass. Any
// other HTTP answer (including a login page or a reverse-proxy error) is not.
func updateWaitHealthy(limit time.Duration) updateHealth {
	return updateWaitHealthyWith(limit, updateServiceStatusURL(), func() bool {
		return exec.Command("systemctl", "is-active", "--quiet", ServiceName).Run() == nil
	})
}

func updateWaitHealthyWith(limit time.Duration, statusURL string, active func() bool) updateHealth {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if active == nil || !active() {
			return updateCrashed
		}
		response, err := client.Get(statusURL)
		if err == nil {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			closeErr := response.Body.Close()
			if readErr == nil && closeErr == nil && (updateExpectedStatusResponse(body) && response.StatusCode == http.StatusOK ||
				response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) {
				return updateHealthy
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		delay := 2 * time.Second
		if remaining < delay {
			delay = remaining
		}
		if delay > 0 {
			time.Sleep(delay)
		}
	}
	return updateSlow
}

func updateExpectedStatusResponse(body []byte) bool {
	var status struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return false
	}
	return status.Name == "gextto" && strings.TrimSpace(status.Version) != ""
}

// updateServiceStatusURL is /api/status on the address the unit listens on
// (GEXTTO_LISTEN in its environment), reached through loopback.
func updateServiceStatusURL() string {
	listen := constants.DefaultListen
	if output, err := exec.Command("systemctl", "show", "-p", "Environment", "--value", ServiceName).Output(); err == nil {
		for _, field := range strings.Fields(string(output)) {
			if value, ok := strings.CutPrefix(field, "GEXTTO_LISTEN="); ok && value != "" {
				listen = value
			}
		}
	}
	return "http://" + updateLoopbackAddress(listen) + "/api/status"
}

// updateLoopbackAddress turns a listen address into one to connect to: a
// wildcard host (0.0.0.0, [::], empty) becomes 127.0.0.1.
func updateLoopbackAddress(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func updateSetExecutable(path string) error {
	return os.Chmod(path, 0o755)
}

func updateCopyDir(from, to string) error {
	if err := os.MkdirAll(to, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(to, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		source := filepath.Join(from, entry.Name())
		destination := filepath.Join(to, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := updateCopyDir(source, destination); err != nil {
				return err
			}
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(source)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, destination); err != nil {
				return err
			}
		} else {
			if err := updateCopyFileMode(source, destination); err != nil {
				return err
			}
		}
	}
	return nil
}

func updateCopyFileMode(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Chmod(info.Mode().Perm()); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func updateRemovePath(path string) {
	info, err := os.Lstat(path)
	if err != nil {
		return
	}
	if info.IsDir() {
		_ = os.RemoveAll(path)
	} else {
		_ = os.Remove(path)
	}
}

func updateIsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func updateIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func updatePathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// WorkDir is a temporary working directory removed on cleanup.
type WorkDir struct {
	path string
}

// NewWorkDir creates a fresh temporary working directory.
func NewWorkDir() (*WorkDir, error) {
	dir, err := os.MkdirTemp(os.TempDir(), "gextto-update-")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		updateRemovePath(dir)
		return nil, err
	}
	return &WorkDir{path: dir}, nil
}

// Path returns the working directory path.
func (w *WorkDir) Path() string {
	return w.path
}

// Cleanup removes the working directory and everything inside it.
func (w *WorkDir) Cleanup() {
	updateRemovePath(w.path)
}
