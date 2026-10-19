package gextto

// ed2k_engine.go is the Gextto-side adapter for the gx-ed2k transfer plane.
// It exposes the daemon's /api/v1/transfers rows through the same
// TorrentEngine surface as the torrent backends, so multiEngine can merge
// them and the existing download list, events and post-processing keep
// working unchanged.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/models"
)

type ed2kTransfer struct {
	ID      string `json:"id"`
	Hash    string `json:"hash"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	State   string `json:"state"`
	Done    int64  `json:"done"`
	Sources int    `json:"sources"`
	Error   string `json:"error"`
	Path    string `json:"path"`
}

type ed2kEngine struct {
	nullEngine
	settings ed2kSettings
	client   *http.Client

	mu       sync.Mutex
	previous map[string]string
	healthy  bool
}

func newEd2kEngine(cfg *Config) (*ed2kEngine, error) {
	settings, err := ed2kSettingsFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &ed2kEngine{
		settings: settings,
		client:   &http.Client{Timeout: 10 * time.Second},
		previous: map[string]string{},
	}, nil
}

func (e *ed2kEngine) Name() string { return "gx-ed2k" }

func (e *ed2kEngine) Capabilities() map[string]bool {
	out := map[string]bool{}
	for _, name := range capabilityNames() {
		out[name] = false
	}
	for _, name := range []string{"add", "list", "remove", "stats", "events", "files"} {
		out[name] = true
	}
	return out
}

func (e *ed2kEngine) SessionHealthy() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.healthy
}

func (e *ed2kEngine) request(method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, e.settings.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if e.settings.Token != "" {
		request.Header.Set("X-Gx-Token", e.settings.Token)
	}
	return e.client.Do(request)
}

func (e *ed2kEngine) transfers() ([]ed2kTransfer, error) {
	response, err := e.request(http.MethodGet, "/api/v1/transfers", nil)
	if err != nil {
		e.setHealthy(false)
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		e.setHealthy(false)
		return nil, fmt.Errorf("gx-ed2k transfers: HTTP %d", response.StatusCode)
	}
	var payload struct {
		Transfers []ed2kTransfer `json:"transfers"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&payload); err != nil {
		e.setHealthy(false)
		return nil, err
	}
	e.setHealthy(true)
	return payload.Transfers, nil
}

func (e *ed2kEngine) setHealthy(value bool) {
	e.mu.Lock()
	e.healthy = value
	e.mu.Unlock()
}

func (e *ed2kEngine) List() []models.TorrentView {
	transfers, err := e.transfers()
	if err != nil {
		return nil
	}
	out := make([]models.TorrentView, 0, len(transfers))
	for _, transfer := range transfers {
		out = append(out, e.view(transfer))
	}
	return out
}

func (e *ed2kEngine) view(transfer ed2kTransfer) models.TorrentView {
	progress := 0.0
	if transfer.Size > 0 {
		progress = float64(transfer.Done) * 100 / float64(transfer.Size)
		if progress > 100 {
			progress = 100
		}
	}
	state := "downloading"
	switch transfer.State {
	case "discovering", "downloading":
		state = "downloading"
	case "queued":
		state = "queued"
	case "completed":
		state = "finished"
	case "error":
		state = "error"
	}
	return models.TorrentView{
		Hash:         strings.ToLower(transfer.Hash),
		Name:         transfer.Name,
		Progress:     progress,
		State:        state,
		Network:      "ed2k",
		Sources:      transfer.Sources,
		TotalSize:    transfer.Size,
		TotalDone:    transfer.Done,
		HasMetadata:  transfer.Size > 0,
		AutoManaged:  true,
		SavePath:     directoryOf(transfer.Path),
		Error:        transfer.Error,
		DownloadRate: 0,
		UploadRate:   0,
	}
}

func directoryOf(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if index := strings.LastIndexAny(path, "/\\"); index > 0 {
		return path[:index]
	}
	return ""
}

func (e *ed2kEngine) Stats() map[string]any {
	transfers, err := e.transfers()
	if err != nil {
		return map[string]any{"transfers": 0, "downloading": 0}
	}
	counts := map[string]int{}
	for _, transfer := range transfers {
		counts[transfer.State]++
	}
	return map[string]any{
		"transfers":   len(transfers),
		"downloading": counts["downloading"] + counts["discovering"],
		"queued":      counts["queued"],
		"completed":   counts["completed"],
		"error":       counts["error"],
		"backend":     "gx-ed2k",
	}
}

func (e *ed2kEngine) PollEvents() []models.TorrentEvent {
	transfers, err := e.transfers()
	if err != nil {
		return nil
	}
	var events []models.TorrentEvent
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, transfer := range transfers {
		hash := strings.ToLower(transfer.Hash)
		previous, seen := e.previous[hash]
		if !seen {
			e.previous[hash] = transfer.State
			continue
		}
		if previous == transfer.State {
			continue
		}
		if transfer.State == "completed" {
			events = append(events, models.TorrentEvent{
				Kind: "torrent_finished", Hash: hash, Name: transfer.Name, SavePath: directoryOf(transfer.Path),
			})
		}
		if transfer.State == "error" {
			events = append(events, models.TorrentEvent{
				Kind: "torrent_error", Hash: hash, Name: transfer.Name, SavePath: directoryOf(transfer.Path),
				Message: transfer.Error,
			})
		}
		e.previous[hash] = transfer.State
	}
	return events
}

func (e *ed2kEngine) Add(link string, cfg *Config) (bool, error) {
	return e.AddWithOptions(link, cfg, nil, AddOptions{})
}

func (e *ed2kEngine) AddWithPath(link string, cfg *Config, preferredPath *string) (bool, error) {
	return e.AddWithOptions(link, cfg, preferredPath, AddOptions{})
}

func (e *ed2kEngine) AddWithOptions(link string, cfg *Config, preferredPath *string, options AddOptions) (bool, error) {
	link = strings.TrimSpace(link)
	if !strings.HasPrefix(strings.ToLower(link), "ed2k://|file|") {
		return false, errors.New("gx-ed2k: not an ed2k file link")
	}
	response, err := e.request(http.MethodPost, "/api/v1/transfers", map[string]string{"link": link})
	if err != nil {
		e.setHealthy(false)
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusCreated || response.StatusCode == http.StatusOK {
		e.setHealthy(true)
		return true, nil
	}
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	return false, fmt.Errorf("gx-ed2k add: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
}

func (e *ed2kEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	response, err := e.request(http.MethodDelete, "/api/v1/transfers/"+hash, nil)
	if err != nil {
		e.setHealthy(false)
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		e.mu.Lock()
		delete(e.previous, hash)
		e.mu.Unlock()
		return true, nil
	}
	return false, fmt.Errorf("gx-ed2k remove: HTTP %d", response.StatusCode)
}

func (e *ed2kEngine) Files(hash string) ([]models.FileView, bool, error) {
	transfers, err := e.transfers()
	if err != nil {
		return nil, false, err
	}
	hash = strings.ToLower(strings.TrimSpace(hash))
	for _, transfer := range transfers {
		if strings.ToLower(transfer.Hash) != hash {
			continue
		}
		return []models.FileView{{
			Path: transfer.Name, Size: transfer.Size, Downloaded: transfer.Done, Priority: 1,
		}}, true, nil
	}
	return nil, false, nil
}
