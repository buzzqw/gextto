package main

// update.go is the in-app update of the standalone daemon, in the
// Maintenance section of the settings page. It reads the release manifest CI
// publishes (release.json, internal/updatecheck) on a timer or on demand; an
// update is offered only when a commit after the running one changed
// gx-torrent's own code. "Update" downloads the standalone archive of the
// release channel the binary came from, checks its SHA-256, extracts the new
// binary next to the running one, tries it (--version), swaps it in (the old
// one stays as <binary>.prev) and restarts the daemon in place. The new
// version shows a popup with the commits it brought; one that does not start
// three times in a row is rolled back to <binary>.prev.
//
// Managed by Gextto the daemon never updates itself: Gextto ships and
// replaces it. A local build (no release channel stamped) is not updated
// either, since a checkout may be newer than the published build.

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/updatecheck"
)

const (
	updateCheckKey = "update-check"       // automatic check on/off (default on)
	updateHoursKey = "update-check-hours" // hours between automatic checks
	// updateDefaultHours spaces the automatic checks (one small download).
	updateDefaultHours = 6
	updateMaxHours     = 24 * 30
	// updateNotesFile, in the data directory, carries what an update brought
	// across the restart, for the popup of the new version.
	updateNotesFile = "update-notes.json"
	// updateMaxArchive bounds the download (the archive is ~15 MB).
	updateMaxArchive = 256 << 20
	// updateBootAttempts is how many starts a new version gets before the
	// previous binary is restored.
	updateBootAttempts = 3
	// updateHealthyAfter is how long a new version must run to count as good.
	updateHealthyAfter = 30 * time.Second
)

// gxUpdater is the update state of one daemon. The zero value is ready.
type gxUpdater struct {
	mu        sync.Mutex
	manifest  *updatecheck.Manifest
	checkedAt time.Time
	err       string
	applying  bool
	applyErr  string
	// restart is closed when the daemon must restart into the new binary.
	restartOnce sync.Once
	restart     chan struct{}

	// Test hooks: the manifest/asset URL of a channel, the HTTP client and
	// the running binary.
	assetURL func(channel, asset string) string
	client   *http.Client
	exe      string
}

func (u *gxUpdater) restartC() chan struct{} {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.restart == nil {
		u.restart = make(chan struct{})
	}
	return u.restart
}

// updateMirrorEnv names a folder (URL) holding the release assets in place
// of GitHub: a mirror, or a local copy for testing.
const updateMirrorEnv = "GX_TORRENT_UPDATE_URL"

func (u *gxUpdater) url(channel, asset string) string {
	if u.assetURL != nil {
		return u.assetURL(channel, asset)
	}
	if mirror := strings.TrimRight(strings.TrimSpace(os.Getenv(updateMirrorEnv)), "/"); mirror != "" {
		return mirror + "/" + asset
	}
	return updatecheck.AssetURL(channel, asset)
}

func (u *gxUpdater) httpClient() *http.Client {
	if u.client != nil {
		return u.client
	}
	return http.DefaultClient
}

// gxUpdateStatus is what the Maintenance section shows.
type gxUpdateStatus struct {
	Version, Commit, BuiltAt, Channel string
	// Reason is why updates are off here ("" = they work).
	Reason       string
	CanApply     bool // the platform can install by itself (Linux)
	CheckEnabled bool
	Hours        int
	CheckedAt    time.Time
	Err          string
	Available    bool
	Complete     bool
	Changes      []updatecheck.Commit
	Latest       string // newest gx-torrent commit of the release
	LatestDate   string
	Applying     bool
	ApplyErr     string
}

// updateAsset is the standalone archive of this platform ("" = none).
func updateAsset() string {
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	if arch == "" {
		return ""
	}
	switch runtime.GOOS {
	case "linux":
		return "gx-torrent-linux-" + arch + ".tar.gz"
	case "windows":
		return "gx-torrent-windows-" + runtime.GOARCH + ".zip"
	}
	return ""
}

// updateUnavailable is why this daemon cannot update itself ("" = it can).
func (d *Daemon) updateUnavailable() string {
	switch {
	case d.opts.Mode != ModeStandalone:
		return "Managed by Gextto: gx-torrent is updated together with Gextto."
	case constants.GxReleaseChannel == "" || constants.Commit == "":
		return "Local build: update it from the source code, or install a release to update from here."
	case updateAsset() == "":
		return "No release is published for this platform."
	}
	return ""
}

func (d *Daemon) updateHours() int {
	h := updateDefaultHours
	if d.opts.Settings != nil {
		if v, err := strconv.Atoi(strings.TrimSpace(d.opts.Settings.Get(updateHoursKey, ""))); err == nil && v > 0 {
			h = v
		}
	}
	return min(h, updateMaxHours)
}

func (d *Daemon) updateCheckEnabled() bool {
	return d.opts.Settings == nil || !strings.EqualFold(strings.TrimSpace(d.opts.Settings.Get(updateCheckKey, "true")), "false")
}

// updateStatus combines the binary, the last check and a running update.
func (d *Daemon) updateStatus() gxUpdateStatus {
	st := gxUpdateStatus{
		Version: runtimeVersion(), Commit: constants.Commit, BuiltAt: constants.BuiltAt,
		Channel: constants.GxReleaseChannel, Reason: d.updateUnavailable(),
		CanApply: runtime.GOOS == "linux", CheckEnabled: d.updateCheckEnabled(), Hours: d.updateHours(),
	}
	u := &d.updater
	u.mu.Lock()
	m := u.manifest
	st.CheckedAt, st.Err, st.Applying, st.ApplyErr = u.checkedAt, u.err, u.applying, u.applyErr
	u.mu.Unlock()
	if m != nil && st.Reason == "" {
		st.Available, st.Changes, st.Complete = updatecheck.Changes(st.Commit, m, true)
		st.Latest = m.GxCommit
		for _, c := range m.Commits {
			if c.Sha == m.GxCommit {
				st.LatestDate = c.Day()
			}
		}
	}
	return st
}

// checkUpdate downloads the manifest of the binary's channel now.
func (d *Daemon) checkUpdate(ctx context.Context) error {
	if reason := d.updateUnavailable(); reason != "" {
		return errors.New(reason)
	}
	u := &d.updater
	m, err := updatecheck.Fetch(ctx, u.httpClient(), u.url(constants.GxReleaseChannel, updatecheck.ManifestName), "gx-torrent/"+runtimeVersion())
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checkedAt = time.Now()
	if err != nil {
		u.err = err.Error()
		logf("update check failed: %v", err)
		return err
	}
	u.err, u.manifest = "", m
	if available, changes, _ := updatecheck.Changes(constants.Commit, m, true); available {
		logf("update available: %d change(s) to gx-torrent since %s", len(changes), shortCommit(constants.Commit))
	}
	return nil
}

// updateWorker checks shortly after start, then every update-check-hours
// (read again at each round, so a change applies without restart).
func (d *Daemon) updateWorker(stop <-chan struct{}) {
	if d.updateUnavailable() != "" {
		return
	}
	wait := 2 * time.Minute
	for {
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
		if d.updateCheckEnabled() {
			_ = d.checkUpdate(context.Background())
		}
		wait = time.Duration(d.updateHours()) * time.Hour
	}
}

// startUpdate starts the installation in the background; the page follows it
// and reloads on the new version.
func (d *Daemon) startUpdate() error {
	if reason := d.updateUnavailable(); reason != "" {
		return errors.New(reason)
	}
	if runtime.GOOS != "linux" {
		return errors.New("Automatic installation is only available on Linux: download the new version from the release page.")
	}
	st := d.updateStatus()
	if !st.Available {
		return errors.New("No update available.")
	}
	u := &d.updater
	u.mu.Lock()
	if u.applying {
		u.mu.Unlock()
		return errors.New("An update is already in progress.")
	}
	u.applying, u.applyErr = true, ""
	u.mu.Unlock()
	go func() {
		err := d.installUpdate(context.Background(), st)
		u.mu.Lock()
		u.applying = false
		if err != nil {
			u.applyErr = err.Error()
		}
		u.mu.Unlock()
		if err != nil {
			logf("update failed: %v", err)
			return
		}
		u.restartOnce.Do(func() { close(u.restartC()) })
	}()
	return nil
}

// updateNotes is update-notes.json.
type updateNotes struct {
	FromVersion string               `json:"from_version"`
	FromCommit  string               `json:"from_commit"`
	ToVersion   string               `json:"to_version,omitempty"`
	ToCommit    string               `json:"to_commit,omitempty"`
	Changes     []updatecheck.Commit `json:"changes"`
	At          time.Time            `json:"at"`
	// Pending is true until the new version has run for updateHealthyAfter;
	// Attempts counts its starts meanwhile.
	Pending    bool `json:"pending"`
	Attempts   int  `json:"attempts"`
	RolledBack bool `json:"rolled_back,omitempty"`
	// Seen is set when the popup was closed; the file goes once the update
	// is also confirmed (not Pending).
	Seen  bool   `json:"seen,omitempty"`
	Error string `json:"error,omitempty"`
}

func updateNotesPath(dataDir string) string { return filepath.Join(dataDir, updateNotesFile) }

func readUpdateNotes(dataDir string) (*updateNotes, bool) {
	raw, err := os.ReadFile(updateNotesPath(dataDir))
	if err != nil {
		return nil, false
	}
	var n updateNotes
	if json.Unmarshal(raw, &n) != nil {
		return nil, false
	}
	return &n, true
}

func writeUpdateNotes(dataDir string, n *updateNotes) error {
	raw, err := json.MarshalIndent(n, "", "  ")
	if err != nil {
		return err
	}
	tmp := updateNotesPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, updateNotesPath(dataDir))
}

// executable is the running binary, symlinks resolved.
func (u *gxUpdater) executable() (string, error) {
	if u.exe != "" {
		return u.exe, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// installUpdate downloads, verifies and swaps in the new binary.
func (d *Daemon) installUpdate(ctx context.Context, st gxUpdateStatus) error {
	u := &d.updater
	exe, err := u.executable()
	if err != nil {
		return fmt.Errorf("cannot locate the running program: %w", err)
	}
	dir := filepath.Dir(exe)
	probe, err := os.CreateTemp(dir, ".gx-torrent-update-*")
	if err != nil {
		return fmt.Errorf("the program folder %s is not writable by the service user: %w", dir, err)
	}
	archivePath := probe.Name()
	defer os.Remove(archivePath)

	asset := updateAsset()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	sum, err := d.fetchChecksum(ctx, u.url(st.Channel, asset+".sha256"))
	if err != nil {
		probe.Close()
		return err
	}
	logf("update: downloading %s", u.url(st.Channel, asset))
	got, err := d.download(ctx, u.url(st.Channel, asset), probe)
	probe.Close()
	if err != nil {
		return err
	}
	if got != sum {
		return fmt.Errorf("the downloaded archive is damaged (SHA-256 %s, expected %s)", got, sum)
	}
	newPath := exe + ".new"
	if err := extractBinary(archivePath, "gx-torrent", newPath); err != nil {
		os.Remove(newPath)
		return err
	}
	// The new binary must at least run here before it replaces this one.
	tryCtx, tryCancel := context.WithTimeout(ctx, 15*time.Second)
	out, err := exec.CommandContext(tryCtx, newPath, "--version").Output()
	tryCancel()
	if err != nil || !strings.Contains(string(out), "1.1.") {
		os.Remove(newPath)
		return fmt.Errorf("the new version does not run on this system: %v", err)
	}
	notes := &updateNotes{
		FromVersion: st.Version, FromCommit: st.Commit, ToCommit: st.Latest,
		Changes: st.Changes, At: time.Now().UTC(), Pending: true,
	}
	if err := writeUpdateNotes(d.opts.DataDir, notes); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("cannot record the update: %w", err)
	}
	prev := exe + ".prev"
	_ = os.Remove(prev)
	if err := os.Link(exe, prev); err != nil {
		// No hard links (another filesystem feature): copy instead.
		if err := copyFile(exe, prev, 0o755); err != nil {
			os.Remove(newPath)
			return fmt.Errorf("cannot keep the previous version: %w", err)
		}
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Remove(newPath)
		return fmt.Errorf("cannot replace the program: %w", err)
	}
	logf("update: %s installed (%s → %s), restarting", strings.TrimSpace(string(out)), shortCommit(st.Commit), shortCommit(st.Latest))
	return nil
}

// fetchChecksum reads a "<sha256>  <file>" checksum file.
func (d *Daemon) fetchChecksum(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := d.updater.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("checksum: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum: HTTP %d", resp.StatusCode)
	}
	line, _ := bufio.NewReader(io.LimitReader(resp.Body, 4096)).ReadString('\n')
	fields := strings.Fields(line)
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", errors.New("checksum: unreadable")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("checksum: unreadable")
	}
	return strings.ToLower(fields[0]), nil
}

// download writes url to f and returns its SHA-256.
func (d *Daemon) download(ctx context.Context, url string, f *os.File) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "gx-torrent/"+runtimeVersion())
	resp, err := d.updater.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, updateMaxArchive+1))
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if n > updateMaxArchive {
		return "", errors.New("download: the archive is too large")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractBinary copies the entry called name (at the archive root) of a
// .tar.gz to dst, executable.
func extractBinary(archive, name, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("the archive is not readable: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s is missing from the archive", name)
		}
		if err != nil {
			return fmt.Errorf("the archive is not readable: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Clean(strings.TrimPrefix(hdr.Name, "./")) != name {
			continue
		}
		if hdr.Size > updateMaxArchive {
			return errors.New("the program in the archive is too large")
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, hdr.Size)); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

// ---------------------------------------------------------------------------
// Boot guard
// ---------------------------------------------------------------------------

// updateBootGuard runs at start, before the daemon opens anything. After an
// update it counts the new version's starts; past updateBootAttempts the
// previous binary is put back and started instead. It reports the binary to
// re-execute ("" = go on with this one).
func updateBootGuard(dataDir, exe string) string {
	n, ok := readUpdateNotes(dataDir)
	if !ok || !n.Pending {
		return ""
	}
	n.Attempts++
	prev := exe + ".prev"
	if n.Attempts > updateBootAttempts {
		if _, err := os.Stat(prev); err == nil {
			_ = os.Rename(exe, exe+".failed")
			if err := os.Rename(prev, exe); err == nil {
				n.Pending, n.RolledBack = false, true
				n.Error = "The new version did not start: the previous one was restored."
				_ = writeUpdateNotes(dataDir, n)
				logf("update: the new version failed to start %d times, previous version restored", updateBootAttempts)
				return exe
			}
		}
		n.Pending = false
		n.Error = "The new version did not start and the previous one could not be restored."
	}
	_ = writeUpdateNotes(dataDir, n)
	return ""
}

// updateMarkHealthy records, once the new version has run for a while, that
// the update succeeded (no more rollback) and which version it is.
func (d *Daemon) updateMarkHealthy(stop <-chan struct{}) {
	n, ok := readUpdateNotes(d.opts.DataDir)
	if !ok || !n.Pending {
		return
	}
	select {
	case <-stop:
		return
	case <-time.After(updateHealthyAfter):
	}
	if n, ok = readUpdateNotes(d.opts.DataDir); !ok || !n.Pending {
		return
	}
	n.Pending = false
	n.ToVersion, n.ToCommit = runtimeVersion(), constants.Commit
	if n.Seen {
		_ = os.Remove(updateNotesPath(d.opts.DataDir))
	} else {
		_ = writeUpdateNotes(d.opts.DataDir, n)
	}
	logf("update: %s is running (was %s)", n.ToVersion, n.FromVersion)
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

// handleUIUpdate is the Maintenance section's button: action=check checks
// now, action=apply starts the update.
func (d *Daemon) handleUIUpdate(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	var message string
	var err error
	switch r.FormValue("action") {
	case "check":
		if err = d.checkUpdate(r.Context()); err == nil {
			if d.updateStatus().Available {
				message = "A new version is available."
			} else {
				message = "gx-torrent is up to date."
			}
		}
	case "apply":
		if err = d.startUpdate(); err == nil {
			message = "Installing the update: gx-torrent restarts by itself."
		}
	default:
		err = errors.New("Unknown action.")
	}
	ok := "1"
	if err != nil {
		ok, message = "0", err.Error()
	}
	http.Redirect(w, r, uiSettingsPath+"?msg="+url.QueryEscape(message)+"&ok="+ok+"#maintenance", http.StatusSeeOther)
}

// handleUIUpdateNotes closes the "updated" popup for good.
func (d *Daemon) handleUIUpdateNotes(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	// While the new version is still on probation the file keeps counting
	// its starts: it is only marked as seen.
	if n, ok := readUpdateNotes(d.opts.DataDir); ok {
		if n.Pending {
			n.Seen = true
			_ = writeUpdateNotes(d.opts.DataDir, n)
		} else {
			_ = os.Remove(updateNotesPath(d.opts.DataDir))
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateSectionHTML is the dynamic part of the Maintenance section: the
// running version, the state of the check and the one button, which reads
// "Check for updates" until a new version is found and "Update" then.
func (d *Daemon) updateSectionHTML() string {
	st := d.updateStatus()
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString(`<p class="muted"><span>Version</span> <strong>` + esc(st.Version) + `</strong>`)
	if st.Commit != "" {
		b.WriteString(` · <code>` + esc(shortCommit(st.Commit)) + `</code>`)
	}
	if st.BuiltAt != "" && len(st.BuiltAt) >= 10 {
		b.WriteString(` · ` + esc(st.BuiltAt[:10]))
	}
	if st.Channel != "" {
		b.WriteString(` · <span>channel</span> ` + esc(st.Channel))
	}
	b.WriteString(`</p>`)
	if st.Reason != "" {
		b.WriteString(`<p class="muted">` + esc(st.Reason) + `</p>`)
		return b.String()
	}
	if st.Applying {
		// The page follows the installation and reloads on the new version.
		b.WriteString(`<p class="muted" role="status" data-update-applying><span>Installing the update: gx-torrent restarts by itself.</span></p>`)
		return b.String()
	}
	if st.ApplyErr != "" {
		b.WriteString(`<p class="err" role="alert"><span>Update failed:</span> ` + esc(st.ApplyErr) + `</p>`)
	}
	if st.Err != "" {
		b.WriteString(`<p class="err" role="alert"><span>Update check failed:</span> ` + esc(st.Err) + `</p>`)
	}
	switch {
	case st.Available:
		b.WriteString(`<p role="status"><span class="badge b-downloading">New version</span> <code>` + esc(shortCommit(st.Latest)) + `</code> ` + esc(st.LatestDate) + `</p>`)
		if len(st.Changes) > 0 {
			b.WriteString(`<p class="muted">What changes:</p>` + updateChangesHTML(st.Changes))
		}
		if !st.Complete {
			b.WriteString(`<p class="muted">Partial list: the running version is older than the changes shown.</p>`)
		}
	case !st.CheckedAt.IsZero() && st.Err == "":
		b.WriteString(`<p role="status"><span class="badge b-seeding">Up to date</span> <span>gx-torrent is up to date.</span></p>`)
	}
	b.WriteString(`<div class="actions">`)
	if st.Available && st.CanApply {
		b.WriteString(`<button type="button" class="primary sm" data-formaction="/ui/update" data-update="apply" title="Download the new version, install it and restart gx-torrent">Update</button>`)
		b.WriteString(`<button type="button" class="secondary sm" data-formaction="/ui/update" data-update="check" title="Look for a newer version now">Check again</button>`)
	} else {
		b.WriteString(`<button type="button" class="primary sm" data-formaction="/ui/update" data-update="check" title="Look for a newer version now">Check for updates</button>`)
	}
	if st.Available && !st.CanApply {
		b.WriteString(` <a class="btn sm" href="https://github.com/` + updatecheck.Repo + `/releases" target="_blank" rel="noopener noreferrer" title="Open the release page to download the new version">Download</a>`)
	}
	b.WriteString(`</div>`)
	if !st.CheckedAt.IsZero() {
		b.WriteString(`<p class="muted"><span>Last check:</span> ` + esc(st.CheckedAt.Local().Format("2006-01-02 15:04")) + `</p>`)
	}
	return b.String()
}

func updateChangesHTML(changes []updatecheck.Commit) string {
	var b strings.Builder
	b.WriteString(`<ul class="update-changes">`)
	for _, c := range changes {
		b.WriteString(`<li><a href="` + html.EscapeString(c.URL()) + `" target="_blank" rel="noopener noreferrer"><code>` +
			html.EscapeString(c.Short()) + `</code></a> <span class="muted">` + html.EscapeString(c.Day()) + `</span> ` +
			html.EscapeString(c.Subject) + `</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

// updateNotesDialogHTML is the popup the new version shows once: what the
// update brought (or why it was rolled back). "" when there is nothing to
// show.
func (d *Daemon) updateNotesDialogHTML() string {
	if d.opts.Mode != ModeStandalone {
		return ""
	}
	n, ok := readUpdateNotes(d.opts.DataDir)
	// Shown by the new version (or after a rollback), once: the old process,
	// in the moment before it restarts, has the "from" commit.
	if !ok || n.Seen || (!n.RolledBack && n.Error == "" && constants.Commit == n.FromCommit) {
		return ""
	}
	if n.ToVersion == "" {
		n.ToVersion, n.ToCommit = runtimeVersion(), constants.Commit
	}
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<dialog id="dlg-updated" aria-labelledby="dlg-updated-title"><div class="dlg-head"><h2 id="dlg-updated-title">`)
	if n.RolledBack || n.Error != "" {
		b.WriteString(`Update not installed</h2></div><div class="dlg-body"><p class="err" role="alert">` + esc(n.Error) + `</p>`)
	} else {
		b.WriteString(`gx-torrent updated</h2></div><div class="dlg-body"><p><strong>` + esc(n.FromVersion) +
			`</strong> <code>` + esc(shortCommit(n.FromCommit)) + `</code> → <strong>` + esc(n.ToVersion) +
			`</strong> <code>` + esc(shortCommit(n.ToCommit)) + `</code></p>`)
		if len(n.Changes) > 0 {
			b.WriteString(`<p class="muted">What changed:</p>` + updateChangesHTML(n.Changes))
		}
	}
	b.WriteString(`</div><div class="dlg-foot"><button type="button" class="primary" data-updated-close title="Close this message">OK</button></div></dialog>`)
	b.WriteString(`<script>(function(){var d=document.getElementById('dlg-updated');if(!d||typeof d.showModal!=='function')return;` +
		`function done(){try{fetch('/ui/update-notes',{method:'POST',credentials:'same-origin'});}catch(e){}}` +
		`d.querySelector('[data-updated-close]').addEventListener('click',function(){d.close();});` +
		`d.addEventListener('close',done);d.showModal();})();</script>`)
	return b.String()
}
