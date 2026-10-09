package gextto

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
// bundled libtorrent.
func VersionString() string {
	value := fmt.Sprintf("%s %s (build %s)", constants.AppName, constants.Version, constants.Build)
	if marker, ok := updateInstalledMarker(); ok {
		value += fmt.Sprintf(" [%s]", marker)
	}
	libtorrent := LibtorrentVersion()
	if libtorrent != "" {
		value += fmt.Sprintf(" · libtorrent %s", libtorrent)
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
	// up the repository's product-version file during development.
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
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
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

	if err := updateInstallRelease(root, installDir); err != nil {
		return err
	}
	// The payload's own marker ("continuous-<sha>", "v1.2.3") says exactly
	// what is installed; the release name is only a fallback.
	marker := []byte(release + "\n")
	if payload, err := os.ReadFile(filepath.Join(root, "VERSION")); err == nil && strings.TrimSpace(string(payload)) != "" {
		marker = payload
	}
	// The previous marker goes back in place if the new version is rolled back.
	if previous, err := os.ReadFile(filepath.Join(installDir, "VERSION")); err == nil {
		_ = os.WriteFile(filepath.Join(installDir, "VERSION.prev"), previous, 0o644)
	}
	if err := os.WriteFile(filepath.Join(installDir, "VERSION"), marker, 0o644); err != nil {
		return err
	}
	fmt.Printf("Gextto updated to %s in %s\n", release, installDir)

	return updateRestartService(opts)
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
	return "gexttod/" + constants.Version
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
	file, err := os.Create(destination)
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

// verifyRemoteChecksum verifies the archive against a `.sha256` asset when the
// release publishes one. A missing checksum is tolerated because not every build
// publishes it.
func verifyRemoteChecksum(ctx context.Context, archive, checksumURL string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		fmt.Println("  checksum:   not published by this release (continuing)")
		return nil
	}
	request.Header.Set("User-Agent", updateUserAgent())
	response, err := client.Do(request)
	if err != nil {
		fmt.Println("  checksum:   not published by this release (continuing)")
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		fmt.Println("  checksum:   not published by this release (continuing)")
		return nil
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
		fmt.Println("  checksum:   not published by this release (continuing)")
		return nil
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
	cmd := exec.Command("tar", "-xzf", archive, "-C", destination)
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
	binary := filepath.Join(root, constants.AppName)
	if !updateIsFile(binary) {
		return fmt.Errorf("release is missing the %s executable", binary)
	}
	return nil
}

// updateInstallRelease installs the staged payload. Everything is copied next to the
// final destination first; the live installation is only touched once every
// piece is staged, and any failure during the swap restores the previous binary
// and directories.
func updateInstallRelease(root, installDir string) error {
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("cannot create %s: %w", installDir, err)
	}
	suffix := fmt.Sprintf(".%d.new", os.Getpid())

	// Stage the executable and the directories before touching what is live.
	binary := filepath.Join(installDir, constants.AppName)
	newBinary := filepath.Join(installDir, constants.AppName+suffix)
	if err := updateCopyFileMode(filepath.Join(root, constants.AppName), newBinary); err != nil {
		return fmt.Errorf("cannot stage the new executable: %w", err)
	}
	if err := updateSetExecutable(newBinary); err != nil {
		return err
	}

	type stagedDir struct {
		name        string
		staged      string
		previous    string
		destination string
	}
	var stagedDirs []stagedDir
	for _, directory := range []string{"ui", "lib"} {
		source := filepath.Join(root, directory)
		if !updateIsDir(source) {
			continue
		}
		staged := filepath.Join(installDir, "."+directory+suffix)
		updateRemovePath(staged)
		if err := updateCopyDir(source, staged); err != nil {
			return fmt.Errorf("cannot stage the %s directory: %w", directory, err)
		}
		previous := filepath.Join(installDir, fmt.Sprintf(".%s.old%d", directory, os.Getpid()))
		updateRemovePath(previous)
		stagedDirs = append(stagedDirs, stagedDir{name: directory, staged: staged, previous: previous})
	}

	// Snapshot the current executable so the whole swap can be undone.
	backup := filepath.Join(installDir, constants.AppName+".bak")
	hadBinary := updateIsFile(binary)
	if hadBinary {
		if err := updateCopyFileMode(binary, backup); err != nil {
			return fmt.Errorf("cannot back up the current executable: %w", err)
		}
	}
	rollback := func(committed []stagedDir) {
		for index := len(committed) - 1; index >= 0; index-- {
			item := committed[index]
			if updatePathExists(item.previous) {
				updateRemovePath(item.destination)
				_ = os.Rename(item.previous, item.destination)
			}
		}
		if hadBinary {
			_ = updateCopyFileMode(backup, binary)
		}
		updateRemovePath(backup)
	}

	if err := os.Rename(newBinary, binary); err != nil {
		updateRemovePath(newBinary)
		rollback(nil)
		return fmt.Errorf("cannot replace the executable: %w", err)
	}

	var committed []stagedDir
	for _, item := range stagedDirs {
		destination := filepath.Join(installDir, item.name)
		if updatePathExists(destination) {
			if err := os.Rename(destination, item.previous); err != nil {
				updateRemovePath(item.staged)
				rollback(committed)
				return fmt.Errorf("cannot move the current %s aside: %w", item.name, err)
			}
		}
		if err := os.Rename(item.staged, destination); err != nil {
			if updatePathExists(item.previous) {
				_ = os.Rename(item.previous, destination)
			}
			updateRemovePath(item.staged)
			rollback(committed)
			return fmt.Errorf("cannot install the %s directory: %w", item.name, err)
		}
		item.destination = destination
		committed = append(committed, item)
	}

	// Success: the previous directories are no longer needed. The previous
	// executable is kept as gexttod.prev (as the installer does): if the new
	// version does not start, updateRestartService puts it back.
	for _, item := range committed {
		updateRemovePath(item.previous)
	}
	if hadBinary {
		if err := os.Rename(backup, filepath.Join(installDir, constants.AppName+".prev")); err != nil {
			updateRemovePath(backup)
		}
	}

	// The gx-torrent daemon may be running (managed mode): install it
	// through a rename so the running executable is never overwritten in place.
	if source := filepath.Join(root, "gx-torrent"); updateIsFile(source) {
		destination := filepath.Join(installDir, "gx-torrent")
		staged := destination + ".new"
		if err := updateCopyFileMode(source, staged); err != nil {
			return fmt.Errorf("cannot install gx-torrent: %w", err)
		}
		if err := updateSetExecutable(staged); err != nil {
			return err
		}
		if err := os.Rename(staged, destination); err != nil {
			return fmt.Errorf("cannot install gx-torrent: %w", err)
		}
	} else {
		logging.Warn("gx-torrent is missing from the update payload: Gextto will fall back to embedded libtorrent")
	}

	// Keep the launcher and the release notes alongside the payload when shipped.
	for _, file := range []string{"run.sh", "README.md"} {
		source := filepath.Join(root, file)
		if updateIsFile(source) {
			destination := filepath.Join(installDir, file)
			if err := updateCopyFileMode(source, destination); err != nil {
				return fmt.Errorf("cannot install %s: %w", file, err)
			}
			if file == "run.sh" {
				if err := updateSetExecutable(destination); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func updateRestartService(opts UpdateOptions) error {
	if !opts.Restart {
		fmt.Println("Service not restarted (--no-restart).")
		return nil
	}
	if syscall.Geteuid() != 0 {
		fmt.Printf("Run `sudo systemctl restart %s` to start the new version.\n", ServiceName)
		return nil
	}
	if err := updateSystemctl("restart", ServiceName); err != nil {
		fmt.Fprintf(os.Stderr, "Could not restart %s; run `systemctl restart %s` manually.\n", ServiceName, ServiceName)
		return nil
	}
	if updateWaitActive(30 * time.Second) {
		fmt.Printf("Service %s restarted.\n", ServiceName)
		return nil
	}
	// The new version does not start: put the previous executable back.
	installDir, err := updateResolveInstallDir(opts)
	if err != nil {
		return fmt.Errorf("%s did not start after the update: %w", ServiceName, err)
	}
	previous := filepath.Join(installDir, constants.AppName+".prev")
	if !updateIsFile(previous) {
		return fmt.Errorf("%s did not start after the update and no previous version is available; inspect: journalctl -u %s -n 100", ServiceName, ServiceName)
	}
	fmt.Fprintf(os.Stderr, "%s did not start after the update: restoring the previous version.\n", ServiceName)
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
		_ = os.WriteFile(filepath.Join(installDir, "VERSION"), previousMarker, 0o644)
	} else {
		_ = os.WriteFile(filepath.Join(installDir, "VERSION"), []byte("rollback\n"), 0o644)
	}
	if err := updateSystemctl("restart", ServiceName); err != nil || !updateWaitActive(30*time.Second) {
		return fmt.Errorf("the update failed and the previous version did not start either; inspect: journalctl -u %s -n 100", ServiceName)
	}
	return fmt.Errorf("the new version did not start: the previous one was restored and is running")
}

func updateSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// updateWaitActive polls the service until it is active, for at most limit.
// `systemctl restart` returns once the unit started; a binary that crashes
// right away shows up as "activating"/"failed" within a few seconds.
func updateWaitActive(limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	stable := 0
	for time.Now().Before(deadline) {
		if exec.Command("systemctl", "is-active", "--quiet", ServiceName).Run() == nil {
			// Active for five consecutive seconds: not a crash loop.
			stable++
			if stable >= 5 {
				return true
			}
		} else {
			stable = 0
		}
		time.Sleep(time.Second)
	}
	return false
}

func updateSetExecutable(path string) error {
	return os.Chmod(path, 0o755)
}

func updateCopyDir(from, to string) error {
	if err := os.MkdirAll(to, 0o755); err != nil {
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
	return out.Close()
}

func updateRemovePath(path string) {
	info, err := os.Stat(path)
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
	_, err := os.Stat(path)
	return err == nil
}

// WorkDir is a temporary working directory removed on cleanup.
type WorkDir struct {
	path string
}

// NewWorkDir creates a fresh temporary working directory.
func NewWorkDir() (*WorkDir, error) {
	stamp := time.Now().UnixMilli()
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("gextto-update-%d-%d", os.Getpid(), stamp))
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
