package gextto

// ed2k_api.go is the JSON API of the aMule integration: aMule's download
// queue and its actions, the bandwidth limits and aMule's log. The TUI and
// the Scarico bandwidth menu use it; the eD2k page renders the same data.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/logging"
)

// ed2kDownloadJSON is one download of GET /api/ed2k/downloads.
type ed2kDownloadJSON struct {
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	Size          int64   `json:"size"`
	Done          int64   `json:"done"`
	Progress      float64 `json:"progress"`
	Speed         int64   `json:"speed"`
	State         string  `json:"state"`
	Sources       int64   `json:"sources"`
	ActiveSources int64   `json:"active_sources"`
	Priority      string  `json:"priority"`
	Link          string  `json:"link"`
	LastReceived  int64   `json:"last_received"`
}

func ed2kDownloadJSONFrom(download amule.Download) ed2kDownloadJSON {
	link := download.Link
	if link == "" {
		link = ed2k.FileLink{Name: download.Name, Size: download.Size, Hash: download.Hash}.String()
	}
	return ed2kDownloadJSON{
		Hash:          download.Hash,
		Name:          download.Name,
		Size:          download.Size,
		Done:          download.Done,
		Progress:      download.Progress(),
		Speed:         download.Speed,
		State:         ed2kDownloadState(download),
		Sources:       download.Sources,
		ActiveSources: download.Active,
		Priority:      amule.PriorityName(download.Priority),
		Link:          link,
		LastReceived:  download.LastRecv,
	}
}

// Ed2kDownloadsAPI implements `GET /api/ed2k/downloads`.
func Ed2kDownloadsAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if !ed2kEnabled(cfg) {
		jsonResponse(w, map[string]any{"ok": true, "enabled": false, "downloads": []ed2kDownloadJSON{}})
		return
	}
	var downloads []amule.Download
	if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		downloads, callErr = client.Downloads(ctx)
		return callErr
	}); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := make([]ed2kDownloadJSON, 0, len(downloads))
	for _, download := range downloads {
		out = append(out, ed2kDownloadJSONFrom(download))
	}
	jsonResponse(w, map[string]any{"ok": true, "enabled": true, "downloads": out})
}

// Ed2kDownloadActionAPI implements `POST /api/ed2k/downloads/{hash}/{action}`
// for pause, resume and remove.
func Ed2kDownloadActionAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	action := pathParam(r, "action")
	switch action {
	case "pause", "resume", "remove":
	default:
		jsonError(w, http.StatusNotFound, "unknown eD2k action")
		return
	}
	cfg := latestConfig(s)
	if !ed2kEnabled(cfg) {
		jsonError(w, http.StatusConflict, "eD2k/aMule is disabled")
		return
	}
	if err := Ed2kDownloadAction(cfg, action, pathParam(r, "hash")); err != nil {
		jsonError(w, ed2kErrorStatus(err), err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// Ed2kPriorityInput is the body of `POST /api/ed2k/downloads/{hash}/priority`.
type Ed2kPriorityInput struct {
	Priority string `json:"priority"`
}

// Ed2kPriorityAPI implements `POST /api/ed2k/downloads/{hash}/priority`.
func Ed2kPriorityAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input Ed2kPriorityInput
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := latestConfig(s)
	if err := Ed2kSetPriority(cfg, pathParam(r, "hash"), input.Priority); err != nil {
		jsonError(w, ed2kErrorStatus(err), err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}

// Ed2kSetPriority changes the priority of one download (low, normal, high or
// auto).
func Ed2kSetPriority(cfg *Config, hash, name string) error {
	if !ed2kEnabled(cfg) {
		return errEd2kDisabled
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	if !ed2k.ValidHash(hash) {
		return errEd2kBadInput("invalid eD2k hash")
	}
	priority, ok := amule.ParsePriority(name)
	if !ok {
		return errEd2kBadInput("priority must be low, normal, high or auto")
	}
	return ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		return client.SetPriority(ctx, hash, priority)
	})
}

// Ed2kLimitsInput is the body of `POST /api/ed2k/limits` (KiB/s, 0 =
// unlimited).
type Ed2kLimitsInput struct {
	DownloadKib int64 `json:"download_kib"`
	UploadKib   int64 `json:"upload_kib"`
}

// Ed2kLimits returns aMule's global bandwidth limits in KiB/s.
func Ed2kLimits(cfg *Config) (amule.Preferences, error) {
	var prefs amule.Preferences
	err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		prefs, callErr = client.Preferences(ctx)
		return callErr
	})
	return prefs, err
}

// Ed2kLimitsAPI implements `GET` and `POST /api/ed2k/limits`.
func Ed2kLimitsAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if !ed2kEnabled(cfg) {
		jsonError(w, http.StatusConflict, "eD2k/aMule is disabled")
		return
	}
	if r.Method == http.MethodPost {
		var input Ed2kLimitsInput
		if err := decodeJSON(r, &input); err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		if input.DownloadKib < 0 || input.UploadKib < 0 || input.DownloadKib > 10_000_000 || input.UploadKib > 10_000_000 {
			jsonError(w, http.StatusBadRequest, "limits must be between 0 and 10000000 KiB/s")
			return
		}
		if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
			return client.SetLimits(ctx, input.DownloadKib, input.UploadKib)
		}); err != nil {
			jsonError(w, ed2kErrorStatus(err), err.Error())
			return
		}
		ed2kInvalidateHealth()
	}
	prefs, err := Ed2kLimits(cfg)
	if err != nil {
		jsonError(w, ed2kErrorStatus(err), err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "download_kib": prefs.MaxDownload, "upload_kib": prefs.MaxUpload})
}

// Ed2kLogLines returns aMule's log, newest last.
func Ed2kLogLines(cfg *Config) ([]string, error) {
	var lines []string
	err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		lines, callErr = client.LogLines(ctx)
		return callErr
	})
	return lines, err
}

// Ed2kLogAPI implements `GET /api/ed2k/log`.
func Ed2kLogAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if !ed2kEnabled(cfg) {
		jsonError(w, http.StatusConflict, "eD2k/aMule is disabled")
		return
	}
	lines, err := Ed2kLogLines(cfg)
	if err != nil {
		jsonError(w, ed2kErrorStatus(err), err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true, "lines": lines})
}

var errEd2kDisabled = errEd2kBadInput("eD2k/aMule is disabled")

// errEd2kBadInput is a request error (400/409), not an aMule failure.
type errEd2kBadInput string

func (e errEd2kBadInput) Error() string { return string(e) }

// ed2kErrorStatus maps an error to an HTTP status: a bad request, a file aMule
// does not have, or aMule unreachable / refusing.
func ed2kErrorStatus(err error) int {
	switch {
	case err == errEd2kDisabled:
		return http.StatusConflict
	case isEd2kBadInput(err):
		return http.StatusBadRequest
	case err == errEd2kNotQueued || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "invalid eD2k hash"):
		return http.StatusNotFound
	}
	return http.StatusBadGateway
}

func isEd2kBadInput(err error) bool {
	_, ok := err.(errEd2kBadInput)
	return ok
}

// ed2kSpeedState remembers the limits last sent to aMule.
var ed2kSpeedState struct {
	sync.Mutex
	key  string
	sent time.Time
}

// ed2kSpeedResend re-sends unchanged limits now and then, in case they were
// changed from aMule's own interface.
const ed2kSpeedResend = 10 * time.Minute

// applyEd2kSpeedLimits gives aMule the bandwidth limits in force for the
// torrents (rule, exception, schedule or Auto; KiB/s, 0 = unlimited). aMule
// saves its preferences at every change, so the limits are sent only when
// they change or every ed2kSpeedResend.
func applyEd2kSpeedLimits(cfg *Config, downloadKib, uploadKib int64, now time.Time) {
	if !ed2kEnabled(cfg) {
		return
	}
	if downloadKib < 0 {
		downloadKib = 0
	}
	if uploadKib < 0 {
		uploadKib = 0
	}
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return
	}
	key := fmt.Sprintf("%s|%d|%d", settings.Address, downloadKib, uploadKib)
	ed2kSpeedState.Lock()
	unchanged := ed2kSpeedState.key == key && now.Sub(ed2kSpeedState.sent) < ed2kSpeedResend
	ed2kSpeedState.Unlock()
	if unchanged {
		return
	}
	if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		return client.SetLimits(ctx, downloadKib, uploadKib)
	}); err != nil {
		logging.Debug("cannot apply the speed limits to aMule", "error", err)
		return
	}
	ed2kSpeedState.Lock()
	changed := ed2kSpeedState.key != key
	ed2kSpeedState.key = key
	ed2kSpeedState.sent = now
	ed2kSpeedState.Unlock()
	if changed {
		logging.Debug("aMule follows the torrent speed limits", "download_kib", downloadKib, "upload_kib", uploadKib)
	}
}

// ---------------------------------------------------------------------------
// Shared files
// ---------------------------------------------------------------------------

// ed2kSharedSnapshot is the shared-file view of the core: the files, what
// aMule has uploaded for them and the folders it shares.
type ed2kSharedSnapshot struct {
	Enabled     bool
	Incoming    string
	SharedDirs  []string
	UploadRate  int64
	UploadQueue int64
	Files       []amule.SharedFile
	Uploads     []amule.UploadClient
}

// ed2kSharedSnapshotFor reads the shared files from aMule. The status and the
// preferences are best effort: the file list is the data the caller wants.
func ed2kSharedSnapshotFor(cfg *Config) (ed2kSharedSnapshot, error) {
	snapshot := ed2kSharedSnapshot{Enabled: ed2kEnabled(cfg)}
	if !snapshot.Enabled {
		return snapshot, nil
	}
	err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		if status, statusErr := client.Status(ctx); statusErr == nil {
			snapshot.UploadRate = status.UploadRate
			snapshot.UploadQueue = status.UploadQueue
		}
		if prefs, prefsErr := client.Preferences(ctx); prefsErr == nil {
			snapshot.Incoming = prefs.IncomingDir
			snapshot.SharedDirs = prefs.SharedDirs
		}
		files, filesErr := client.SharedFiles(ctx)
		if filesErr != nil {
			return filesErr
		}
		snapshot.Files = files
		if uploads, uploadsErr := client.UploadQueue(ctx); uploadsErr == nil {
			snapshot.Uploads = uploads
		}
		return nil
	})
	// What aMule uploaded in this session comes first: that is the answer to
	// "what is it uploading now".
	sort.SliceStable(snapshot.Files, func(i, j int) bool {
		if snapshot.Files[i].Transferred != snapshot.Files[j].Transferred {
			return snapshot.Files[i].Transferred > snapshot.Files[j].Transferred
		}
		return strings.ToLower(snapshot.Files[i].Name) < strings.ToLower(snapshot.Files[j].Name)
	})
	return snapshot, err
}

// ed2kReloadShared asks the core to rescan its shared folders.
func ed2kReloadShared(cfg *Config) error {
	return ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		return client.ReloadSharedFiles(ctx)
	})
}

// ed2kSharedFileJSON is one entry of GET /api/ed2k/shared.
type ed2kSharedFileJSON struct {
	Hash           string `json:"hash"`
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	Dir            string `json:"dir"`
	Transferred    int64  `json:"transferred"`
	TransferredAll int64  `json:"transferred_all"`
	Requests       int64  `json:"requests"`
	RequestsAll    int64  `json:"requests_all"`
	Accepts        int64  `json:"accepts"`
	AcceptsAll     int64  `json:"accepts_all"`
	Queued         int64  `json:"queued"`
	Priority       string `json:"priority"`
	AutoPriority   bool   `json:"auto_priority"`
	Link           string `json:"link"`
}

// Ed2kSharedAPI implements `GET /api/ed2k/shared`.
func Ed2kSharedAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	snapshot, err := ed2kSharedSnapshotFor(cfg)
	if !snapshot.Enabled {
		jsonResponse(w, map[string]any{"ok": true, "enabled": false, "files": []ed2kSharedFileJSON{}})
		return
	}
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	files := make([]ed2kSharedFileJSON, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		priority := "normal"
		if file.AutoPriority {
			priority = "auto"
		} else if value := amule.PriorityName(file.Priority); value != "" {
			priority = value
		}
		files = append(files, ed2kSharedFileJSON{
			Hash:           file.Hash,
			Name:           file.Name,
			Size:           file.Size,
			Dir:            file.Dir,
			Transferred:    file.Transferred,
			TransferredAll: file.TransferredAll,
			Requests:       file.Requests,
			RequestsAll:    file.RequestsAll,
			Accepts:        file.Accepts,
			AcceptsAll:     file.AcceptsAll,
			Queued:         file.Queued,
			Priority:       priority,
			AutoPriority:   file.AutoPriority,
			Link:           ed2k.FileLink{Name: file.Name, Size: file.Size, Hash: file.Hash}.String(),
		})
	}
	jsonResponse(w, map[string]any{
		"ok":           true,
		"enabled":      true,
		"incoming":     snapshot.Incoming,
		"shared_dirs":  snapshot.SharedDirs,
		"upload_rate":  snapshot.UploadRate,
		"upload_queue": snapshot.UploadQueue,
		"files":        files,
		"uploads":      snapshot.Uploads,
	})
}

// Ed2kSharedReloadAPI implements `POST /api/ed2k/shared/reload`.
func Ed2kSharedReloadAPI(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	if !ed2kEnabled(cfg) {
		jsonError(w, http.StatusConflict, "the aMule integration is off")
		return
	}
	if err := ed2kReloadShared(cfg); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	logging.Info("eD2k shared files reload requested")
	jsonResponse(w, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// v2Ed2kHealth is the aMule card on the Health page.
type v2Ed2kHealth struct {
	Enabled      bool
	Reachable    bool
	Error        string
	Version      string
	Connected    bool
	Server       string
	HighID       bool
	Kad          string
	DownloadRate int64
	UploadRate   int64
	TotalDown    int64
	TotalUp      int64
	UploadQueue  int64
	Warnings     []string
	DirIssues    []amuleDirIssue
}

// v2Ed2kHealthFrom reads aMule's state and the recent warnings from its log.
func v2Ed2kHealthFrom(s *AppState) *v2Ed2kHealth {
	cfg := latestConfig(s)
	card := &v2Ed2kHealth{Enabled: ed2kEnabled(cfg)}
	if !card.Enabled {
		return card
	}
	status, err := ed2kStatus(cfg)
	if err != nil {
		card.Error = err.Error()
		return card
	}
	card.Reachable = true
	card.Version = status.Version
	card.Connected = status.Ed2kConnected
	card.Server = status.ServerName
	if card.Server == "" {
		card.Server = status.ServerAddress
	}
	card.HighID = status.HighID
	switch {
	case status.KadConnected && status.KadFirewalled:
		card.Kad = "firewalled"
	case status.KadConnected:
		card.Kad = "connected"
	case status.KadRunning:
		card.Kad = "connecting"
	default:
		card.Kad = "stopped"
	}
	card.DownloadRate = status.DownloadRate
	card.UploadRate = status.UploadRate
	card.TotalDown = status.TotalRecv
	card.TotalUp = status.TotalSent
	card.UploadQueue = status.UploadQueue
	if prefs, prefsErr := ed2kPreferences(cfg); prefsErr == nil {
		card.DirIssues = amuleDirectoryIssues(cfg, prefs)
	}
	if lines, logErr := Ed2kLogLines(cfg); logErr == nil {
		card.Warnings = ed2kLogWarnings(lines, 5)
	}
	return card
}

// ed2kLogWarnings keeps the most recent aMule log lines that look like a
// problem. It is deliberately conservative: only clear markers are reported,
// and the last `limit` lines are kept.
func ed2kLogWarnings(lines []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	markers := []string{"error", "failed", "failure", "cannot", "can't", "lost", "disconnect", "no valid server", "refused"}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				out = append(out, strings.TrimSpace(line))
				break
			}
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// amuleDirIssue is one aMule folder that cannot be used, with the scope that
// says what the operator has to fix.
type amuleDirIssue struct {
	Path string `json:"path"`
	// Kind is "host" (missing on this machine: Gextto can create it),
	// "container" (missing inside aMule's container: fix it there) or
	// "unmapped" (it exists in the container but not at the same path here,
	// so the import cannot work).
	Kind string `json:"kind"`
}

// amuleDirectoryIssues checks aMule's incoming and temporary folders. With
// aMule in a container the paths belong to the container's filesystem, so they
// are checked through /proc/<pid>/root and never created from here.
func amuleDirectoryIssues(cfg *Config, prefs amule.Preferences) []amuleDirIssue {
	if cfg == nil || !hostIsLocal(uiSettingString(cfg, "amule_host")) {
		return nil
	}
	pid := 0
	if pids := amuleProcessPIDs(); len(pids) > 0 {
		pid = pids[0]
	}
	containerized := processRunsInContainer(pid)
	out := make([]amuleDirIssue, 0, 2)
	for _, dir := range []string{prefs.IncomingDir, prefs.TempDir} {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		hostExists := dirExists(dir)
		if !containerized {
			if !hostExists {
				out = append(out, amuleDirIssue{Path: dir, Kind: "host"})
			}
			continue
		}
		containerExists, known := false, false
		if pid > 0 {
			root := filepath.Join("/proc", strconv.Itoa(pid), "root")
			if _, err := os.Stat(root); err == nil {
				known = true
				containerExists = dirExists(filepath.Join(root, dir))
			}
		}
		switch {
		case known && !containerExists:
			out = append(out, amuleDirIssue{Path: dir, Kind: "container"})
		case known && !hostExists:
			// The container has it but this machine does not: the completed
			// files land where Gextto cannot read them, so the import fails.
			out = append(out, amuleDirIssue{Path: dir, Kind: "unmapped"})
		}
	}
	return out
}

// amuleProcessPIDs finds aMule's local process. It is a variable so tests can
// decide whether this machine has one (and whether it is containerized).
var amuleProcessPIDs = func() []int { return localProcessPIDs("amuled") }

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// amuleCreateMissingDirectories creates the folders missing on this machine
// (kind "host"). Container paths are never created: they belong to the
// container.
func amuleCreateMissingDirectories(cfg *Config) ([]string, error) {
	prefs, err := ed2kPreferences(cfg)
	if err != nil {
		return nil, err
	}
	created := []string{}
	for _, issue := range amuleDirectoryIssues(cfg, prefs) {
		if issue.Kind != "host" {
			continue
		}
		if mkErr := os.MkdirAll(issue.Path, 0o775); mkErr != nil {
			return created, mkErr
		}
		created = append(created, issue.Path)
	}
	return created, nil
}
