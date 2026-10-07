package gextto

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"time"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
)

var ErrGxTorrentNotImplemented = errors.New("gxtorrent: not fully implemented yet")

type gxTorrentEngine struct {
	baseURL string
	client  *http.Client
}

func newGxTorrentEngine(cfg *Config) (*gxTorrentEngine, error) {
	apiURL := cfg.Settings["gxtorrent_url"]
	if apiURL == "" {
		apiURL = "http://127.0.0.1:8890"
	}
	return &gxTorrentEngine{
		baseURL: apiURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}, nil
}

func (e *gxTorrentEngine) Name() string { return BackendGxTorrent }
func (e *gxTorrentEngine) Capabilities() map[string]bool {
	return capabilitiesFor(BackendGxTorrent)
}

type gxTorrentItem struct {
	Hash     string `json:"hash"`
	Name     string `json:"name"`
	State    string `json:"state"`
	Progress int64  `json:"progress"`
	Size     int64  `json:"size"`
	DownloadedRate int64 `json:"download_rate"`
	UploadedRate   int64 `json:"upload_rate"`
	Uploaded       int64 `json:"uploaded"`
}

func (e *gxTorrentEngine) List() []models.TorrentView {
	resp, err := e.client.Get(e.baseURL + "/api/v1/torrents")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var items []gxTorrentItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil
	}

	var views []models.TorrentView
	for _, item := range items {
		views = append(views, models.TorrentView{
			Hash:  item.Hash,
			Name:  item.Name,
			State: item.State,
			AllTimeDownload: item.Progress,
			AllTimeUpload:   item.Uploaded,
		})
	}
	return views
}

func (e *gxTorrentEngine) Add(magnet string, cfg *Config) (bool, error) {
	return e.AddWithOptions(magnet, cfg, nil, AddOptions{})
}

func (e *gxTorrentEngine) AddWithPath(magnet string, cfg *Config, preferredPath *string) (bool, error) {
	return e.AddWithOptions(magnet, cfg, preferredPath, AddOptions{})
}

func (e *gxTorrentEngine) AddWithOptions(magnet string, cfg *Config, preferredPath *string, options AddOptions) (bool, error) {
	target, err := url.Parse(e.baseURL + "/api/v1/add")
	if err != nil {
		return false, err
	}
	q := target.Query()
	q.Set("magnet", magnet)
	if preferredPath != nil {
		q.Set("destination", *preferredPath)
	}
	target.RawQuery = q.Encode()

	req, err := http.NewRequest("POST", target.String(), nil)
	if err != nil {
		return false, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("gxtorrent returned status: %d", resp.StatusCode)
	}
	return true, nil
}

func (e *gxTorrentEngine) PollEvents() []models.TorrentEvent { return nil }
func (e *gxTorrentEngine) Restart(hash string) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) Reannounce(hash string) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) MarkStalled(hash string) (bool, error) { return false, nil }
func (e *gxTorrentEngine) ClearStalled(hash string) {}
func (e *gxTorrentEngine) RamdiskUncommittedBytes(ramdisk string, excludeHash string) uint64 { return 0 }
func (e *gxTorrentEngine) SessionHealthy() bool { return true }
func (e *gxTorrentEngine) Stats() map[string]any { return nil }
func (e *gxTorrentEngine) AdjustQueue(cfg *Config, effectiveDownloadKib int64) {
	limit := cfg.Libtorrent.ActiveDownloads
	if limit <= 0 {
		return
	}
	torrents := e.List()
	
	active := int64(0)
	var queued []models.TorrentView
	for _, t := range torrents {
		if t.State == "downloading" {
			active++
		} else if t.State == "paused" {
			queued = append(queued, t)
		}
	}
	
	if active > limit {
		toPause := active - limit
		for _, t := range torrents {
			if t.State == "downloading" && toPause > 0 {
				e.Pause(t.Hash)
				toPause--
			}
		}
	} else if active < limit && len(queued) > 0 {
		toResume := limit - active
		for i := 0; i < len(queued) && toResume > 0; i++ {
			e.Resume(queued[i].Hash)
			toResume--
		}
	}
}

func (e *gxTorrentEngine) AddFileWithPath(torrentPath string, cfg *Config, preferredPath *string) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) SetFilePriorities(hash string, priorities []int32) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) SetTrackers(hash string, trackers []TrackerEntry) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) WebSeeds(hash, urls string, remove bool) (bool, error) { return false, ErrGxTorrentNotImplemented }

func (e *gxTorrentEngine) SetLimits(hash string, downloadLimit, uploadLimit int64, seedRatio float64, seedDays int64) (bool, error) {
	return true, nil 
}

func (e *gxTorrentEngine) SetGlobalSpeedLimits(downloadKib, uploadKib int64) (bool, error) {
	body := fmt.Sprintf(`{"speed_limit_download": %d, "speed_limit_upload": %d}`, downloadKib, uploadKib)
	req, err := http.NewRequest("POST", e.baseURL+"/api/v1/config", strings.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

func (e *gxTorrentEngine) SetMaxConnections(hash string, value int) (bool, error) {
	return true, nil 
}
func (e *gxTorrentEngine) SetMaxUploads(hash string, value int) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) SetPin(hash string, pinned bool) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) SetSequential(enabled bool) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) AssociateStorage(hash, destination string) (bool, error) { return false, ErrGxTorrentNotImplemented }
func (e *gxTorrentEngine) TorrentFilePath(hash string) (string, bool) { return "", false }
func (e *gxTorrentEngine) AddTorrentFile(torrentPath, savePath string) (*string, error) {
	return e.AddTorrentFileWithOptions(torrentPath, nil, &savePath, AddOptions{})
}

func (e *gxTorrentEngine) AddTorrentFileEx(torrentPath, savePath string, options AddOptions) (*string, error) {
	return e.AddTorrentFileWithOptions(torrentPath, nil, &savePath, options)
}

func (e *gxTorrentEngine) AddTorrentFileWithOptions(torrentPath string, cfg *Config, preferredPath *string, options AddOptions) (*string, error) {
	file, err := os.Open(torrentPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("torrent", "file.torrent")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", e.baseURL+"/api/v1/add-file", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gxtorrent returned status: %d", resp.StatusCode)
	}

	var result struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result.Hash, nil
}
func (e *gxTorrentEngine) sendAction(hash, action string) error {
	req, err := http.NewRequest("POST", e.baseURL+"/api/v1/torrents/"+hash+"/"+action, nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gxtorrent returned status: %d", resp.StatusCode)
	}
	return nil
}

func (e *gxTorrentEngine) Pause(hash string) (bool, error) {
	return true, e.sendAction(hash, "pause")
}

func (e *gxTorrentEngine) Resume(hash string) (bool, error) {
	return true, e.sendAction(hash, "resume")
}

func (e *gxTorrentEngine) Remove(hash string, deleteFiles bool) (bool, error) {
	req, err := http.NewRequest("DELETE", e.baseURL+"/api/v1/torrents/"+hash, nil)
	if err != nil {
		return false, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("gxtorrent returned status: %d", resp.StatusCode)
	}
	// TODO: delete files if deleteFiles == true
	return true, nil
}

func (e *gxTorrentEngine) ForceRecheck(hash string) (bool, error) {
	return true, e.sendAction(hash, "verify")
}

func (e *gxTorrentEngine) MoveStorage(hash, destination string) (bool, error) {
	req, err := http.NewRequest("POST", e.baseURL+"/api/v1/torrents/"+hash+"/move?destination="+url.QueryEscape(destination), nil)
	if err != nil {
		return false, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("gxtorrent returned status: %d", resp.StatusCode)
	}
	return true, nil
}

func (e *gxTorrentEngine) getActionData(hash, action string, target interface{}) error {
	resp, err := e.client.Get(e.baseURL + "/api/v1/torrents/" + hash + "/" + action)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gxtorrent returned status: %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

type gxFile struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Progress int64  `json:"progress"`
}

func (e *gxTorrentEngine) Files(hash string) ([]models.FileView, bool, error) {
	var files []gxFile
	if err := e.getActionData(hash, "files", &files); err != nil {
		return nil, false, err
	}
	var views []models.FileView
	for _, f := range files {
		views = append(views, models.FileView{
			Path:       f.Path,
			Size:       f.Size,
			Downloaded: f.Progress,
			Priority:   4,
		})
	}
	return views, true, nil
}

type gxPeer struct {
	IP string `json:"ip"`
}

func (e *gxTorrentEngine) Peers(hash string) ([]models.PeerView, bool, error) {
	var peers []gxPeer
	if err := e.getActionData(hash, "peers", &peers); err != nil {
		return nil, false, err
	}
	var views []models.PeerView
	for _, p := range peers {
		views = append(views, models.PeerView{
			Address: p.IP,
		})
	}
	return views, true, nil
}

type gxTracker struct {
	URL string `json:"url"`
}

func (e *gxTorrentEngine) Trackers(hash string) ([]models.TrackerView, bool, error) {
	var trackers []gxTracker
	if err := e.getActionData(hash, "trackers", &trackers); err != nil {
		return nil, false, err
	}
	var views []models.TrackerView
	for _, t := range trackers {
		views = append(views, models.TrackerView{
			URL: t.URL,
		})
	}
	return views, true, nil
}

