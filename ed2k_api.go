package gextto

// ed2k_api.go is the JSON API of the aMule integration: aMule's download
// queue and its actions, the bandwidth limits and aMule's log. The TUI and
// the Scarico bandwidth menu use it; the eD2k page renders the same data.

import (
	"context"
	"fmt"
	"net/http"
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
