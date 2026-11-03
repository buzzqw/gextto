// audio_cleanup_queue.go runs the audio cleanup one file at a time in the
// background. The queue has no size limit (a 100-episode pack queues 100
// files) and is saved in the data folder after every change, so files waiting
// or being cleaned when Gextto stops are cleaned after the restart.

package gextto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/buzzqw/gextto/internal/logging"
)

// audioCleanupQueueFile holds the pending jobs, inside the data folder.
const audioCleanupQueueFile = "audio_cleanup_queue.json"

// audioCleanupJob is one file to clean. It is what the queue file stores.
type audioCleanupJob struct {
	Path    string              `json:"path"`
	Hash    string              `json:"hash,omitempty"`
	Series  *string             `json:"series,omitempty"`
	Options AudioCleanupOptions `json:"options"`
}

// audioCleanupQueueState is the queue and its single worker. The first job is
// the one being cleaned: it leaves the list (and the file) only when done.
type audioCleanupQueueState struct {
	mu        sync.Mutex
	jobs      []audioCleanupJob
	queued    map[string]bool
	statePath string
	cfg       *Config
	db        *Database
	running   bool
	wake      chan struct{}
	// process cleans one file; a variable so tests can drive the queue.
	process func(audioCleanupJob, *Config, *Database)
}

func newAudioCleanupQueue() *audioCleanupQueueState {
	return &audioCleanupQueueState{
		queued:  make(map[string]bool),
		wake:    make(chan struct{}, 1),
		process: audioCleanupProcess,
	}
}

var audioCleanupQueue = newAudioCleanupQueue()

var audioCleanupToolWarn sync.Once

func audioCleanupJobKey(path string) string {
	if absolute, err := filepath.Abs(path); err == nil {
		return absolute
	}
	return path
}

// configure records where the queue is saved and what the jobs report to.
func (q *audioCleanupQueueState) configure(cfg *Config, db *Database) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cfg = cfg
	if db != nil {
		q.db = db
	}
	if cfg != nil && strings.TrimSpace(cfg.DataDir) != "" {
		q.statePath = filepath.Join(cfg.DataDir, audioCleanupQueueFile)
	}
}

// enqueue adds the jobs not already waiting and starts the worker.
func (q *audioCleanupQueueState) enqueue(jobs []audioCleanupJob) int {
	q.mu.Lock()
	added := 0
	for _, job := range jobs {
		key := audioCleanupJobKey(job.Path)
		if q.queued[key] {
			continue
		}
		q.queued[key] = true
		q.jobs = append(q.jobs, job)
		added++
	}
	if added > 0 {
		q.saveLocked()
		if !q.running {
			q.running = true
			go q.worker()
		}
	}
	q.mu.Unlock()
	if added > 0 {
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}
	return added
}

func (q *audioCleanupQueueState) worker() {
	for {
		q.mu.Lock()
		if len(q.jobs) == 0 {
			q.mu.Unlock()
			<-q.wake
			continue
		}
		job, cfg, db := q.jobs[0], q.cfg, q.db
		q.mu.Unlock()

		q.process(job, cfg, db)

		q.mu.Lock()
		q.jobs = q.jobs[1:]
		delete(q.queued, audioCleanupJobKey(job.Path))
		q.saveLocked()
		q.mu.Unlock()
	}
}

// pending returns a copy of the waiting jobs, the running one first.
func (q *audioCleanupQueueState) pending() []audioCleanupJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]audioCleanupJob(nil), q.jobs...)
}

// saveLocked writes the queue next to the databases (atomically) or removes
// the file when the queue is empty. Without a data folder nothing is written.
func (q *audioCleanupQueueState) saveLocked() {
	if q.statePath == "" {
		return
	}
	if len(q.jobs) == 0 {
		if err := os.Remove(q.statePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			logging.Warn("the audio cleanup queue could not be cleared", "error", err.Error())
		}
		return
	}
	data, err := json.MarshalIndent(q.jobs, "", "  ")
	if err != nil {
		return
	}
	temporary := q.statePath + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		logging.Warn("the audio cleanup queue could not be saved; files waiting now are not cleaned again after a restart", "error", err.Error())
		return
	}
	if err := os.Rename(temporary, q.statePath); err != nil {
		_ = os.Remove(temporary)
		logging.Warn("the audio cleanup queue could not be saved; files waiting now are not cleaned again after a restart", "error", err.Error())
	}
}

// resume loads the jobs saved by the previous run. Files that are gone are
// dropped; with the cleanup switched off the whole queue is.
func (q *audioCleanupQueueState) resume(cfg *Config, db *Database) {
	q.configure(cfg, db)
	q.mu.Lock()
	statePath := q.statePath
	q.mu.Unlock()
	if statePath == "" {
		return
	}
	data, err := os.ReadFile(statePath)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		logging.Warn("the audio cleanup queue of the previous run could not be read", "error", err.Error())
		return
	}
	var saved []audioCleanupJob
	if err := json.Unmarshal(data, &saved); err != nil {
		logging.Warn("the audio cleanup queue of the previous run is damaged and was discarded", "error", err.Error())
		_ = os.Remove(statePath)
		return
	}
	if _, enabled := audioCleanupSettings(cfg); !enabled || !AudioCleanupAvailable() {
		if len(saved) > 0 {
			logging.Info(fmt.Sprintf("%d file(s) waiting for the audio cleanup were left as they are: the cleanup is switched off or its tools are missing", len(saved)))
		}
		_ = os.Remove(statePath)
		return
	}
	jobs := make([]audioCleanupJob, 0, len(saved))
	for _, job := range saved {
		if strings.TrimSpace(job.Path) == "" {
			continue
		}
		if _, err := os.Stat(job.Path); err != nil {
			continue
		}
		jobs = append(jobs, job)
	}
	if len(jobs) == 0 {
		_ = os.Remove(statePath)
		return
	}
	added := q.enqueue(jobs)
	logging.Info(fmt.Sprintf("🎧 %d file(s) waiting for the audio cleanup before the restart are queued again", added))
}

// ResumeAudioCleanup queues again the files the previous run did not finish
// cleaning. It is called once at startup, after the databases are open.
func ResumeAudioCleanup(cfg *Config, db *Database) {
	audioCleanupQueue.resume(cfg, db)
}

// scheduleAudioCleanup queues the files of an imported release. It is a no-op
// unless the feature is enabled in the settings, and it never blocks the
// caller: a slow remux must not stall the torrent event loop.
func scheduleAudioCleanup(cfg *Config, db *Database, path, hash string, series *string) {
	opts, enabled := audioCleanupSettings(cfg)
	if !enabled || strings.TrimSpace(path) == "" {
		return
	}
	if !AudioCleanupAvailable() {
		audioCleanupToolWarn.Do(func() {
			logging.Warn(fmt.Sprintf("audio cleanup is enabled but %s is missing, so no file will be cleaned",
				strings.Join(AudioCleanupMissingTools(), ", ")))
		})
		return
	}

	files := []string{}
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			listed, listErr := VideoFiles(path)
			if listErr != nil {
				logging.Warn("audio cleanup could not list the folder", "path", path, "error", listErr.Error())
				return
			}
			files = listed
		} else if info.Mode().IsRegular() {
			files = []string{path}
		}
	}
	if len(files) == 0 {
		return
	}
	jobs := make([]audioCleanupJob, 0, len(files))
	for _, file := range files {
		jobs = append(jobs, audioCleanupJob{Path: file, Hash: hash, Series: series, Options: opts})
	}
	audioCleanupQueue.configure(cfg, db)
	audioCleanupQueue.enqueue(jobs)
}

// audioCleanupProcess cleans one queued file and reports the outcome.
func audioCleanupProcess(job audioCleanupJob, cfg *Config, db *Database) {
	if cfg != nil {
		if _, enabled := audioCleanupSettings(cfg); !enabled {
			logging.Debug("audio cleanup switched off: queued file left as it is", "path", job.Path)
			return
		}
	}
	// One heavy job at a time: a night job running now finishes first.
	release := acquireMaintenanceHeavy(context.Background())
	defer release()
	guard := AcquireArchiveImport(job.Series)
	defer guard.Release()

	ctx, cancel := context.WithTimeout(context.Background(), audioCleanupTimeout)
	defer cancel()
	report, err := RunAudioCleanup(ctx, job.Path, job.Options)
	switch {
	case err != nil:
		logging.Warn(fmt.Sprintf("🎧 «%s»: audio cleanup failed, the file was left untouched: %v",
			filepath.Base(job.Path), err), "path", job.Path)
	case report.Applied:
		if db != nil {
			if err := db.UpdateArchivedSizeAfterCleanup(job.Hash, job.Path, report.NewSize); err != nil {
				logging.Warn("audio cleanup removed the tracks but the archived size was not updated",
					"path", job.Path, "error", err.Error())
			}
		}
		if cfg != nil {
			notifyAudioCleanup(FromConfig(cfg), job.Path, report)
		}
		logging.Info(fmt.Sprintf("🎧 «%s»: %d audio track(s) in another language removed, %s saved",
			filepath.Base(job.Path), len(report.Dropped), logging.HumanBytesI64(report.SavedBytes)),
			"path", job.Path, "tool", report.Backend)
	default:
		logging.Debug("audio cleanup skipped", "path", job.Path, "reason", report.SkipReason)
	}
}
