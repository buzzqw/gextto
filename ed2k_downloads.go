package gextto

// ed2k_downloads.go follows aMule's download queue: it lists the files for
// the Scarico page, runs pause/resume/remove, adds ed2k:// links and turns a
// completed file into the same "torrent_finished" event a torrent produces,
// so renaming, archiving, MediaInfo checks and notifications are shared.
//
// The files are not part of the torrent engine: they are shown next to the
// torrents, like the HTTP downloads, and their actions go straight to aMule.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/amule"
	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/models"
)

// Ed2kDownloads returns aMule's download queue, or nil when the integration
// is off or the core cannot be reached.
func Ed2kDownloads(cfg *Config) []amule.Download {
	if !ed2kEnabled(cfg) {
		return nil
	}
	var downloads []amule.Download
	if err := ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		var callErr error
		downloads, callErr = client.Downloads(ctx)
		return callErr
	}); err != nil {
		return nil
	}
	return downloads
}

// AddEd2kLink queues one ed2k:// file link in aMule.
func AddEd2kLink(cfg *Config, link string) error {
	parsed, err := ed2k.ParseFileLink(link)
	if err != nil {
		return err
	}
	return ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		return client.AddLink(ctx, parsed.String(), 0)
	})
}

// addEd2kRelease starts an approved eD2k release found by the automatic gap
// filler. It reports false, nil when aMule already has the file, like a
// torrent engine does for a duplicate.
func addEd2kRelease(cfg *Config, release *models.Release) (bool, error) {
	if err := AddEd2kLink(cfg, release.Ed2kLink); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Ed2kDownloadAction pauses, resumes or removes a download by MD4 hash. A
// completed file is only taken off the list: its data stays in aMule's
// incoming folder (and in the library, if it was imported).
func Ed2kDownloadAction(cfg *Config, action, hash string) error {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if !ed2k.ValidHash(hash) {
		return errors.New("invalid eD2k hash")
	}
	return ed2kCall(cfg, func(ctx context.Context, client *amule.Client) error {
		switch action {
		case "pause":
			return client.Pause(ctx, hash)
		case "resume":
			return client.Resume(ctx, hash)
		case "remove":
			downloads, err := client.Downloads(ctx)
			if err != nil {
				return err
			}
			for _, download := range downloads {
				if download.Hash == hash && download.Complete() {
					return client.ClearCompleted(ctx)
				}
			}
			return client.Delete(ctx, hash)
		}
		return errors.New("unknown eD2k action")
	})
}

// ed2kDownloadState maps aMule's file status to the states the UI uses.
func ed2kDownloadState(download amule.Download) string {
	switch {
	case download.Complete():
		return "finished"
	case download.Paused():
		return "paused"
	case download.Status == amule.StatusError:
		return "error"
	case download.Status == amule.StatusHashing || download.Status == amule.StatusWaitingHash ||
		download.Status == amule.StatusCompleting || download.Status == amule.StatusAllocating:
		return "checking"
	case download.Speed > 0:
		return "downloading"
	}
	return "queued"
}

// ed2kSession presents aMule's queue to the completion handler as a
// TorrentSession. Only what post-processing needs is implemented; the
// torrent-only operations (recheck, reannounce, storage moves) report false.
type ed2kSession struct {
	nullEngine
	cfg *Config
}

var (
	ed2kEventsMu   sync.Mutex
	ed2kLastStatus = map[string]bool{}
	ed2kEventsKey  string
	ed2kLastPoll   time.Time
)

// ed2kPollInterval spaces the completion checks: the event loop ticks far more
// often than an eD2k file can complete, and each check reads aMule's whole
// download queue.
const ed2kPollInterval = 5 * time.Second

func (s ed2kSession) List() []models.TorrentView {
	downloads := Ed2kDownloads(s.cfg)
	incoming := ed2kIncomingDir(s.cfg)
	out := make([]models.TorrentView, 0, len(downloads))
	for _, download := range downloads {
		state := ed2kDownloadState(download)
		out = append(out, models.TorrentView{
			Hash:         download.Hash,
			Name:         download.Name,
			Progress:     download.Progress(),
			State:        state,
			Network:      networkEd2k,
			Sources:      int(download.Sources),
			TotalSize:    download.Size,
			TotalDone:    download.Done,
			HasMetadata:  download.Size > 0,
			SavePath:     incoming,
			DownloadRate: uint64(download.Speed),
		})
	}
	return out
}

// PollEvents reports the files that completed since the previous call. A
// file already complete the first time it is seen (after a Gextto restart)
// is reported too: the completion handler ignores what it already archived.
func (s ed2kSession) PollEvents() []models.TorrentEvent {
	ed2kEventsMu.Lock()
	due := time.Since(ed2kLastPoll) >= ed2kPollInterval
	if due {
		ed2kLastPoll = time.Now()
	}
	ed2kEventsMu.Unlock()
	if !due {
		return nil
	}
	downloads := Ed2kDownloads(s.cfg)
	if downloads == nil {
		return nil
	}
	incoming := ed2kIncomingDir(s.cfg)
	if incoming == "" {
		// Without the incoming folder the completed file cannot be found;
		// wait for the next poll instead of reporting a wrong path.
		return nil
	}
	settings, _ := ed2kSettingsFromConfig(s.cfg)
	ed2kEventsMu.Lock()
	defer ed2kEventsMu.Unlock()
	if ed2kEventsKey != settings.Address {
		ed2kLastStatus = map[string]bool{}
		ed2kEventsKey = settings.Address
	}
	var events []models.TorrentEvent
	present := make(map[string]struct{}, len(downloads))
	for _, download := range downloads {
		present[download.Hash] = struct{}{}
		complete := download.Complete()
		wasComplete, seen := ed2kLastStatus[download.Hash]
		ed2kLastStatus[download.Hash] = complete
		if complete && (!seen || !wasComplete) {
			events = append(events, models.TorrentEvent{
				Kind:     "torrent_finished",
				Hash:     download.Hash,
				Name:     download.Name,
				SavePath: incoming,
			})
		}
	}
	for hash := range ed2kLastStatus {
		if _, ok := present[hash]; !ok {
			delete(ed2kLastStatus, hash)
		}
	}
	return events
}

func (s ed2kSession) Pause(hash string) (bool, error) {
	return true, Ed2kDownloadAction(s.cfg, "pause", hash)
}

func (s ed2kSession) Resume(hash string) (bool, error) {
	return true, Ed2kDownloadAction(s.cfg, "resume", hash)
}

func (s ed2kSession) Remove(hash string, deleteFiles bool) (bool, error) {
	if err := Ed2kDownloadAction(s.cfg, "remove", hash); err != nil {
		return false, err
	}
	return true, nil
}

// ed2kImportConfig is the configuration the completion handler uses for an
// eD2k file: the episode is always imported by link or copy, never moved, so
// aMule keeps sharing the original from its incoming folder (a hardlink costs
// no extra space when library and incoming folder share a filesystem).
func ed2kImportConfig(cfg *Config) *Config {
	copy := *cfg
	copy.Settings = make(map[string]string, len(cfg.Settings)+1)
	for key, value := range cfg.Settings {
		copy.Settings[key] = value
	}
	copy.Settings["move_episodes"] = "false"
	return &copy
}
