package gextto

// update_check.go is the in-app side of updates: it downloads the release
// manifest (release.json, published next to the archives by CI), tells
// whether a newer build exists and what changed (the commits since the
// running one), and turns the "Update" button into a request file that the
// root `gextto-update.path` unit (written by install.sh) acts on. The web UI
// runs as an unprivileged user: it never replaces the program itself.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
)

const (
	// updateRequestFile, in the data directory, is watched by gextto-update.path.
	updateRequestFile = "update-request"
	// updateLogFile is where gextto-update.service writes its output.
	updateLogFile = "/var/log/gextto-update.log"
	// updateCheckInterval spaces the automatic checks (one small download).
	updateCheckInterval = 6 * time.Hour
	updateManifestName  = "release.json"
	updateCommitBaseURL = "https://github.com/" + DefaultRepo + "/commit/"
)

// updateManifest is release.json, written by scripts/release-manifest.sh.
type updateManifest struct {
	Channel    string         `json:"channel"`
	Label      string         `json:"label"`
	AppVersion string         `json:"app_version"`
	Build      int64          `json:"build"`
	Commit     string         `json:"commit"`
	BuiltAt    string         `json:"built_at"`
	Commits    []UpdateCommit `json:"commits"`
}

// UpdateCommit is one changelog entry.
type UpdateCommit struct {
	Sha     string `json:"sha"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

// Short is the abbreviated commit hash shown in the UI.
func (c UpdateCommit) Short() string {
	if len(c.Sha) > 7 {
		return c.Sha[:7]
	}
	return c.Sha
}

// Day is the commit date (YYYY-MM-DD).
func (c UpdateCommit) Day() string {
	if len(c.Date) >= 10 {
		return c.Date[:10]
	}
	return c.Date
}

// URL links the commit on GitHub.
func (c UpdateCommit) URL() string { return updateCommitBaseURL + c.Sha }

// UpdateStatus is what /api/update and the maintenance panel show.
type UpdateStatus struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
	Marker  string `json:"marker"`
	Channel string `json:"channel"`
	// Managed is true for an installation from a release archive (installer or
	// `gexttod --update`); a build from a source checkout updates with git.
	Managed bool `json:"managed"`
	// Updater is true when the root path unit that applies updates exists.
	Updater      bool   `json:"updater"`
	CheckEnabled bool   `json:"check_enabled"`
	CheckedAt    string `json:"checked_at,omitempty"`
	Error        string `json:"error,omitempty"`

	Available       bool           `json:"available"`
	LatestVersion   string         `json:"latest_version,omitempty"`
	LatestCommit    string         `json:"latest_commit,omitempty"`
	LatestBuiltAt   string         `json:"latest_built_at,omitempty"`
	LatestLabel     string         `json:"latest_label,omitempty"`
	Changes         []UpdateCommit `json:"changes,omitempty"`
	ChangesComplete bool           `json:"changes_complete"`

	// Requested is true while a request waits for the updater.
	Requested bool   `json:"requested"`
	LastLog   string `json:"last_log,omitempty"`
}

// CanApply reports whether the "Update" button can work here.
func (u UpdateStatus) CanApply() bool { return u.Managed && u.Updater && u.Available && !u.Requested }

// Reasons why the update cannot start; the UI shows them translated.
var (
	errUpdateSourceBuild = errors.New("Gextto runs from a source checkout: update it with git pull and make build")
	errUpdateNoUpdater   = errors.New("in-app updates are not set up: run the installer again (install.sh) to enable them")
	errUpdateInProgress  = errors.New("an update is already in progress")
	errUpdateNone        = errors.New("no update available")
	errUpdateBackup      = errors.New("backup before the update failed")
)

// updatePathUnit exists when the installer set up in-app updates; the marker
// source tells a release installation from a source checkout. Both are
// variables so tests can stand in for an installed system.
var (
	updatePathUnit     = "/etc/systemd/system/gextto-update.path"
	updateMarkerSource = updateInstalledMarker
)

type updateChecker struct {
	mu        sync.Mutex
	manifest  *updateManifest
	checkedAt time.Time
	err       string
}

var updates = &updateChecker{}

// updateInstalledCommit is the commit of the running binary: stamped at build
// time, or taken from a "continuous-<sha>" marker of older builds.
func updateInstalledCommit() string {
	if constants.Commit != "" {
		return constants.Commit
	}
	if marker, ok := updateInstalledMarker(); ok {
		if sha, found := strings.CutPrefix(marker, "continuous-"); found {
			return sha
		}
	}
	return ""
}

func updateManifestURL(channel string) string {
	if channel == "latest" || channel == "stable" {
		return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s", DefaultRepo, updateManifestName)
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", DefaultRepo, channel, updateManifestName)
}

func updateCheckEnabled(cfg *Config) bool {
	return cfg == nil || settingsBool(cfg, "update_check", true)
}

// fetchManifest downloads release.json for the installed channel.
func (u *updateChecker) fetch(ctx context.Context, channel string) (*updateManifest, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, updateManifestURL(channel), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", updateUserAgent())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, errors.New("this release does not publish update information yet")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release information: HTTP %d", response.StatusCode)
	}
	var manifest updateManifest
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("release information: %w", err)
	}
	if manifest.Commit == "" {
		return nil, errors.New("release information without a commit")
	}
	return &manifest, nil
}

// Check downloads the manifest now and stores the result.
func (u *updateChecker) Check(ctx context.Context) {
	channel := ReleaseName(UpdateOptions{})
	manifest, err := u.fetch(ctx, channel)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checkedAt = time.Now()
	if err != nil {
		u.err = err.Error()
		logging.Debug("update check failed", "error", err)
		return
	}
	u.err = ""
	u.manifest = manifest
}

// Status combines the installation, the last check and the request state.
func (u *updateChecker) Status(cfg *Config) UpdateStatus {
	marker, managed := updateMarkerSource()
	status := UpdateStatus{
		Version:      constants.AppVersion(),
		Commit:       updateInstalledCommit(),
		BuiltAt:      constants.BuiltAt,
		Marker:       marker,
		Channel:      ReleaseName(UpdateOptions{}),
		Managed:      managed,
		Updater:      updateIsFile(updatePathUnit),
		CheckEnabled: updateCheckEnabled(cfg),
	}
	if cfg != nil && cfg.DataDir != "" {
		status.Requested = updateIsFile(filepath.Join(cfg.DataDir, updateRequestFile))
	}
	// The updater removes the request as soon as it starts: while it runs
	// (download, install, restart) its unit is "activating".
	if !status.Requested && status.Updater {
		status.Requested = updateServiceRunning()
	}
	status.LastLog = updateLogTail(updateLogFile, 40)

	u.mu.Lock()
	manifest, checkedAt, lastErr := u.manifest, u.checkedAt, u.err
	u.mu.Unlock()
	if !checkedAt.IsZero() {
		status.CheckedAt = checkedAt.UTC().Format(time.RFC3339)
	}
	status.Error = lastErr
	if manifest == nil {
		return status
	}
	status.LatestVersion = manifest.AppVersion
	status.LatestCommit = manifest.Commit
	status.LatestBuiltAt = manifest.BuiltAt
	status.LatestLabel = manifest.Label
	status.Available, status.Changes, status.ChangesComplete = updateCompare(status.Commit, manifest)
	// A source checkout updates with git: no badge, no button.
	status.Available = status.Available && status.Managed
	return status
}

// updateCompare decides whether the manifest is newer than the running
// commit and lists the commits in between (newest first). The manifest always
// describes the newest published build, so a different commit means newer;
// without a known commit (a local build) nothing is offered.
func updateCompare(installed string, manifest *updateManifest) (bool, []UpdateCommit, bool) {
	if installed == "" || manifest == nil || manifest.Commit == "" {
		return false, nil, false
	}
	if strings.HasPrefix(manifest.Commit, installed) || strings.HasPrefix(installed, manifest.Commit) {
		return false, nil, true
	}
	var changes []UpdateCommit
	for _, commit := range manifest.Commits {
		if strings.HasPrefix(commit.Sha, installed) || strings.HasPrefix(installed, commit.Sha) {
			return true, changes, true
		}
		changes = append(changes, commit)
	}
	// The installed commit is older than the listed history.
	return true, changes, false
}

// updateServiceRunning reports whether gextto-update.service is running. The
// query is read-only, so the unprivileged service user may make it.
func updateServiceRunning() bool {
	output, err := exec.Command("systemctl", "is-active", "gextto-update.service").Output()
	state := strings.TrimSpace(string(output))
	if err != nil && state == "" {
		return false
	}
	return state == "activating" || state == "active" || state == "reloading"
}

// updateLogTail returns the last lines of the updater log, if readable.
func updateLogTail(path string, lines int) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	const window = 16 << 10
	offset := info.Size() - window
	if offset < 0 {
		offset = 0
	}
	buffer := make([]byte, info.Size()-offset)
	if _, err := file.ReadAt(buffer, offset); err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	parts := strings.Split(strings.TrimRight(string(buffer), "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

// RequestUpdate takes a backup of the databases, then asks the root updater to
// install the newest build of the installed channel.
func (u *updateChecker) RequestUpdate(cfg *Config) error {
	status := u.Status(cfg)
	switch {
	case !status.Managed:
		return errUpdateSourceBuild
	case !status.Updater:
		return errUpdateNoUpdater
	case status.Requested:
		return errUpdateInProgress
	case !status.Available:
		return errUpdateNone
	}
	// A database snapshot first: an update that migrates the schema can be
	// undone by restoring it.
	if path, err := CreateSnapshot(cfg.DataDir, filepath.Join(cfg.DataDir, "backups"), gh5_backupRetention(cfg)); err != nil {
		return fmt.Errorf("%w: %w", errUpdateBackup, err)
	} else {
		logging.Info(fmt.Sprintf("💾 Backup before the update: %s", filepath.Base(path)))
	}
	request := filepath.Join(cfg.DataDir, updateRequestFile)
	// The content is informative only: the updater ignores it.
	body := fmt.Sprintf("requested_at=%s\nfrom=%s\nto=%s\n", time.Now().UTC().Format(time.RFC3339), status.Version, status.LatestVersion)
	if err := os.WriteFile(request, []byte(body), 0o644); err != nil {
		return fmt.Errorf("cannot request the update: %w", err)
	}
	logging.Info(fmt.Sprintf("⬆️ Update requested: %s → %s; Gextto restarts when it is installed", status.Version, status.LatestVersion))
	return nil
}

// updateCheckWorker checks for updates shortly after start and then every
// updateCheckInterval, unless `update_check` is off or this is a source build.
func updateCheckWorker(state *AppState) {
	if !state.SleepBackground(2 * time.Minute) {
		return
	}
	for {
		cfg := latestConfig(state)
		if _, managed := updateMarkerSource(); managed && updateCheckEnabled(cfg) {
			updates.Check(context.Background())
		}
		if !state.SleepBackground(updateCheckInterval) {
			return
		}
	}
}

// UpdateStatusApi returns the update state (GET /api/update).
func UpdateStatusApi(w http.ResponseWriter, r *http.Request, s *AppState) {
	jsonResponse(w, updates.Status(latestConfig(s)))
}

// UpdateCheckApi checks now (POST /api/update/check).
func UpdateCheckApi(w http.ResponseWriter, r *http.Request, s *AppState) {
	updates.Check(r.Context())
	jsonResponse(w, updates.Status(latestConfig(s)))
}

// UpdateApplyApi requests the update (POST /api/update/apply).
func UpdateApplyApi(w http.ResponseWriter, r *http.Request, s *AppState) {
	if err := updates.RequestUpdate(latestConfig(s)); err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}
