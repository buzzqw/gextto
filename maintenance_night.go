// maintenance_night.go is the night maintenance window: one time window (01:00
// to 07:00 by default) in which the heavy background work runs, only while
// Gextto is idle (nothing downloading, no import being archived, no library
// check, a low system load) and one job at a time. The window serves:
//
//   - the database maintenance (maintenance_db.go), once per night;
//   - the library report (maintenance_library.go), once per night;
//   - the audio cleanup of the existing library (maintenance_library.go),
//     file by file until the window closes or nothing is left;
//   - the media integrity check and the MediaInfo backfill, which wait for the
//     window when maintenance_heavy_in_window is on.
//
// The work done in a night is summed up in one notification when the window
// closes, and shown in the Maintenance panel.

package gextto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/buzzqw/gextto/internal/bwschedule"
	"github.com/buzzqw/gextto/internal/logging"
)

const (
	maintenanceWindowStartSetting = "maintenance_window_start"
	maintenanceWindowEndSetting   = "maintenance_window_end"
	maintenanceHeavySetting       = "maintenance_heavy_in_window"
	maintenanceDBSetting          = "maintenance_db"
	maintenanceAudioSetting       = "maintenance_audio_library"

	maintenanceDefaultStart = 1 * 60
	maintenanceDefaultEnd   = 7 * 60

	// maintenanceIdleDownloadRate is the total download rate above which
	// Gextto is considered busy: a stalled torrent trickling a few bytes must
	// not keep the maintenance away forever.
	maintenanceIdleDownloadRate = 100 << 10
	// maintenancePoll is how often the worker looks at the window and the load.
	maintenancePoll = 2 * time.Minute
	// maintenanceStatusFile keeps the last results across restarts.
	maintenanceStatusFile = "maintenance_status.json"
)

// maintenanceWindow is a time-of-day range in minutes; end before start wraps
// over midnight.
type maintenanceWindow struct {
	Start int
	End   int
}

// maintenanceWindowFrom reads the window from the settings.
func maintenanceWindowFrom(cfg *Config) maintenanceWindow {
	window := maintenanceWindow{Start: maintenanceDefaultStart, End: maintenanceDefaultEnd}
	if cfg == nil {
		return window
	}
	if minutes, ok := bwschedule.ParseClock(cfg.Settings[maintenanceWindowStartSetting]); ok {
		window.Start = minutes
	}
	if minutes, ok := bwschedule.ParseClock(cfg.Settings[maintenanceWindowEndSetting]); ok {
		window.End = minutes
	}
	return window
}

// Contains reports whether t falls inside the window. The same start and end
// mean the whole day.
func (w maintenanceWindow) Contains(t time.Time) bool {
	minute := t.Hour()*60 + t.Minute()
	switch {
	case w.Start == w.End:
		return true
	case w.Start < w.End:
		return minute >= w.Start && minute < w.End
	default:
		return minute >= w.Start || minute < w.End
	}
}

// Night names the window that contains t by the date it opened, so a window
// across midnight is one night, not two.
func (w maintenanceWindow) Night(t time.Time) string {
	minute := t.Hour()*60 + t.Minute()
	if w.Start > w.End && minute < w.End {
		t = t.AddDate(0, 0, -1)
	}
	return t.Format("2006-01-02")
}

// NextOpening is the next time the window opens after t.
func (w maintenanceWindow) NextOpening(t time.Time) time.Time {
	opening := time.Date(t.Year(), t.Month(), t.Day(), w.Start/60, w.Start%60, 0, 0, t.Location())
	if !opening.After(t) {
		opening = opening.AddDate(0, 0, 1)
	}
	return opening
}

// Label is the window as the panel shows it.
func (w maintenanceWindow) Label() string {
	return fmt.Sprintf("%02d:%02d–%02d:%02d", w.Start/60, w.Start%60, w.End/60, w.End%60)
}

// maintenanceHeavyInWindow reports whether the integrity check and the
// MediaInfo backfill wait for the window.
func maintenanceHeavyInWindow(cfg *Config) bool {
	return settingsBool(cfg, maintenanceHeavySetting, true)
}

// maintenanceWaitFor is how long a periodic heavy job waits before looking at
// the window again: its own interval, but never more than five minutes.
func maintenanceWaitFor(intervalMinutes int64) time.Duration {
	wait := time.Duration(intervalMinutes) * time.Minute
	if wait <= 0 || wait > 5*time.Minute {
		wait = 5 * time.Minute
	}
	return wait
}

// maintenanceHeavy lets one heavy job run at a time: the night jobs, the
// MediaInfo backfill and every audio cleanup take it for each unit of work.
var maintenanceHeavy = make(chan struct{}, 1)

// acquireMaintenanceHeavy waits for the slot; it returns a release function, or
// nil when ctx ends first.
func acquireMaintenanceHeavy(ctx context.Context) func() {
	select {
	case maintenanceHeavy <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-maintenanceHeavy }) }
	case <-ctx.Done():
		return nil
	}
}

// maintenanceHeavyBusy reports whether a heavy job holds the slot right now.
func maintenanceHeavyBusy() bool {
	select {
	case maintenanceHeavy <- struct{}{}:
		<-maintenanceHeavy
		return false
	default:
		return true
	}
}

// libraryCheckRunning is set while the periodic library check (rename repair)
// walks the archive: the night jobs wait for it.
var libraryCheckRunning atomic.Bool

// maintenanceLoadAverage is a variable so tests can simulate a busy machine.
var maintenanceLoadAverage = healthLoadAverage

// maintenanceBusyReason says why Gextto is not idle, in the words the panel
// shows; "" means idle. Uploads do not count: seeding goes on all night.
func maintenanceBusyReason(state *AppState) string {
	if state != nil {
		var rate uint64
		for _, torrent := range SessionSnapshot(state.activeEngine()) {
			rate += torrent.DownloadRate
		}
		if rate > maintenanceIdleDownloadRate {
			return "Un download sta trasferendo"
		}
	}
	if ArchiveImportAnyBusy() {
		return "Un download viene archiviato"
	}
	if libraryCheckRunning.Load() {
		return "Il controllo della libreria è in corso"
	}
	if state != nil && mediaIntegrityStatusFor(state).Running {
		return "Il controllo integrità è in corso"
	}
	if load := maintenanceLoadAverage(); load != nil && *load > float64(runtime.NumCPU())*0.75 {
		return "Il sistema è sotto carico"
	}
	return ""
}

// maintenanceAllowedNow reports whether a heavy job may start now: inside the
// window and idle. With the window switch off it is always allowed.
func maintenanceAllowedNow(state *AppState, cfg *Config, now time.Time) bool {
	if !maintenanceHeavyInWindow(cfg) {
		return true
	}
	if !maintenanceWindowFrom(cfg).Contains(now) {
		return false
	}
	return maintenanceBusyReason(state) == ""
}

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

// maintenanceDBResult is the outcome of the last database maintenance.
type maintenanceDBResult struct {
	At           time.Time `json:"at"`
	BytesBefore  int64     `json:"bytes_before"`
	BytesAfter   int64     `json:"bytes_after"`
	Compacted    []string  `json:"compacted,omitempty"`
	CheckedAt    time.Time `json:"checked_at,omitempty"`
	Problems     []string  `json:"problems,omitempty"`
	ErrorMessage string    `json:"error,omitempty"`
}

// maintenanceMissingFile is one library file the database knows and the disk
// no longer has.
type maintenanceMissingFile struct {
	Series  string `json:"series"`
	Season  int64  `json:"season"`
	Episode int64  `json:"episode"`
	Path    string `json:"path"`
}

// maintenanceLibraryResult is the last library report.
type maintenanceLibraryResult struct {
	At          time.Time                `json:"at"`
	Files       int                      `json:"files"`
	Missing     []maintenanceMissingFile `json:"missing,omitempty"`
	Unreachable []string                 `json:"unreachable,omitempty"`
}

// maintenanceLibraryCheck is the last pass of the periodic library check.
type maintenanceLibraryCheck struct {
	At         time.Time `json:"at"`
	Series     int       `json:"series"`
	Correct    int64     `json:"correct"`
	Renamed    int64     `json:"renamed"`
	Discarded  int64     `json:"discarded"`
	Duplicates int64     `json:"duplicates"`
	Errors     int64     `json:"errors"`
}

// maintenanceAudioResult is the progress of the audio cleanup of the library.
type maintenanceAudioResult struct {
	At       time.Time `json:"at"`
	Checked  int       `json:"checked"`
	Total    int       `json:"total"`
	Cleaned  int       `json:"cleaned"`
	Saved    int64     `json:"saved"`
	Failed   int       `json:"failed"`
	LastFail string    `json:"last_fail,omitempty"`
}

// maintenanceNightSummary is what the current night did, for the notification.
type maintenanceNightSummary struct {
	Night        string `json:"night"`
	Notified     bool   `json:"notified"`
	DBDone       bool   `json:"db_done"`
	DBProblems   int    `json:"db_problems"`
	DBFreed      int64  `json:"db_freed"`
	ReportDone   bool   `json:"report_done"`
	Missing      int    `json:"missing"`
	AudioCleaned int    `json:"audio_cleaned"`
	AudioSaved   int64  `json:"audio_saved"`
	AudioFailed  int    `json:"audio_failed"`
	// AudioDone is set when the library sweep found nothing left for tonight.
	AudioDone bool `json:"audio_done"`
}

func (n maintenanceNightSummary) audioDone() bool { return n.AudioDone }

// maintenanceStatus is the persisted state of the night maintenance.
type maintenanceStatus struct {
	DB           maintenanceDBResult      `json:"db"`
	Library      maintenanceLibraryResult `json:"library"`
	LibraryCheck maintenanceLibraryCheck  `json:"library_check"`
	Audio        maintenanceAudioResult   `json:"audio"`
	Night        maintenanceNightSummary  `json:"night"`
	// Activity and Waiting are live only: what runs now, or why nothing does.
	Activity string `json:"-"`
	Waiting  string `json:"-"`
}

var maintenanceState = struct {
	sync.Mutex
	status maintenanceStatus
	path   string
	loaded bool
}{}

// maintenanceStatusLoad reads the persisted status once.
func maintenanceStatusLoad(dataDir string) {
	maintenanceState.Lock()
	defer maintenanceState.Unlock()
	if maintenanceState.loaded && maintenanceState.path == maintenanceStatusPath(dataDir) {
		return
	}
	maintenanceState.loaded = true
	maintenanceState.path = maintenanceStatusPath(dataDir)
	maintenanceState.status = maintenanceStatus{}
	if maintenanceState.path == "" {
		return
	}
	data, err := os.ReadFile(maintenanceState.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			logging.Debug("the night maintenance status could not be read", "error", err.Error())
		}
		return
	}
	_ = json.Unmarshal(data, &maintenanceState.status)
}

func maintenanceStatusPath(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	return filepath.Join(dataDir, maintenanceStatusFile)
}

// maintenanceStatusSnapshot returns a copy of the status.
func maintenanceStatusSnapshot() maintenanceStatus {
	maintenanceState.Lock()
	defer maintenanceState.Unlock()
	status := maintenanceState.status
	status.DB.Compacted = append([]string(nil), status.DB.Compacted...)
	status.DB.Problems = append([]string(nil), status.DB.Problems...)
	status.Library.Missing = append([]maintenanceMissingFile(nil), status.Library.Missing...)
	status.Library.Unreachable = append([]string(nil), status.Library.Unreachable...)
	return status
}

// maintenanceStatusUpdate changes the status and saves it.
func maintenanceStatusUpdate(change func(*maintenanceStatus)) {
	maintenanceState.Lock()
	defer maintenanceState.Unlock()
	change(&maintenanceState.status)
	if maintenanceState.path == "" {
		return
	}
	data, err := json.MarshalIndent(maintenanceState.status, "", "  ")
	if err != nil {
		return
	}
	temporary := maintenanceState.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		logging.Debug("the night maintenance status could not be saved", "error", err.Error())
		return
	}
	if err := os.Rename(temporary, maintenanceState.path); err != nil {
		_ = os.Remove(temporary)
	}
}

// maintenanceSetLive records what runs now, or why nothing does (not saved).
func maintenanceSetLive(activity, waiting string) {
	maintenanceState.Lock()
	maintenanceState.status.Activity = activity
	maintenanceState.status.Waiting = waiting
	maintenanceState.Unlock()
}

// recordLibraryCheck keeps the outcome of the periodic library check for the
// panel.
func recordLibraryCheck(series int, correct, renamed, discarded, duplicates, errs int64) {
	maintenanceStatusUpdate(func(status *maintenanceStatus) {
		status.LibraryCheck = maintenanceLibraryCheck{
			At: time.Now(), Series: series, Correct: correct, Renamed: renamed,
			Discarded: discarded, Duplicates: duplicates, Errors: errs,
		}
	})
}

// ---------------------------------------------------------------------------
// Worker
// ---------------------------------------------------------------------------

// maintenanceRunNow asks the worker for one pass now, outside the window.
var maintenanceRunNow = make(chan struct{}, 1)

// requestMaintenanceRun wakes the worker for a pass that ignores the window
// (the load and the downloads still count).
func requestMaintenanceRun() {
	select {
	case maintenanceRunNow <- struct{}{}:
	default:
	}
}

// nightMaintenanceWorker runs the night jobs inside the window.
func nightMaintenanceWorker(state *AppState) {
	maintenanceStatusLoad(state.cfg.DataDir)
	ctx, cancel := state.BackgroundContext()
	defer cancel()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		forced := false
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-maintenanceRunNow:
			forced = true
		}
		nightMaintenancePass(ctx, state, time.Now(), forced)
		timer.Reset(maintenancePoll)
	}
}

// nightMaintenancePass is one look at the window: it runs what is due, or
// sends the night summary when the window has just closed.
func nightMaintenancePass(ctx context.Context, state *AppState, now time.Time, forced bool) {
	cfg := latestConfig(state)
	window := maintenanceWindowFrom(cfg)
	if !forced && !window.Contains(now) {
		maintenanceSetLive("", "")
		maintenanceNotifyNight(cfg)
		return
	}
	night := window.Night(now)
	maintenanceStatusUpdate(func(status *maintenanceStatus) {
		if status.Night.Night != night {
			status.Night = maintenanceNightSummary{Night: night}
		}
	})
	inWindow := func() bool {
		return forced || maintenanceWindowFrom(latestConfig(state)).Contains(time.Now())
	}
	idle := func() bool {
		reason := maintenanceBusyReason(state)
		maintenanceSetLive("", reason)
		return reason == ""
	}
	if !idle() {
		return
	}
	defer maintenanceSetLive("", "")

	status := maintenanceStatusSnapshot()
	if settingsBool(cfg, maintenanceDBSetting, true) && (forced || !status.Night.DBDone) && inWindow() && idle() {
		runMaintenanceStep(ctx, "Manutenzione del database", func(ctx context.Context) {
			runDatabaseMaintenance(ctx, state, now)
		})
	}
	if (forced || !maintenanceStatusSnapshot().Night.ReportDone) && inWindow() && idle() {
		runMaintenanceStep(ctx, "Rapporto della libreria", func(ctx context.Context) {
			runLibraryReport(ctx, state)
		})
	}
	if maintenanceAudioLibraryEnabled(cfg) {
		runLibraryAudioCleanup(ctx, state, func() bool { return inWindow() && idle() })
	}
}

// runMaintenanceStep runs one job holding the heavy slot.
func runMaintenanceStep(ctx context.Context, label string, job func(context.Context)) {
	release := acquireMaintenanceHeavy(ctx)
	if release == nil {
		return
	}
	defer release()
	maintenanceSetLive(label, "")
	job(ctx)
}

// maintenanceNotifyNight sends the summary of the last night once, after its
// window closed, when it did something.
func maintenanceNotifyNight(cfg *Config) {
	summary := maintenanceStatusSnapshot().Night
	if summary.Night == "" || summary.Notified {
		return
	}
	if !summary.DBDone && !summary.ReportDone && summary.AudioCleaned == 0 && summary.AudioFailed == 0 {
		return
	}
	maintenanceStatusUpdate(func(status *maintenanceStatus) { status.Night.Notified = true })
	text := maintenanceNightText(summary)
	logging.Info("🌙 Night maintenance: " + text)
	if summary.DBProblems == 0 && summary.Missing == 0 && summary.AudioCleaned == 0 && summary.AudioFailed == 0 && summary.DBFreed < 64<<20 {
		// A quiet night is in the log, not on the phone.
		return
	}
	if cfg == nil {
		return
	}
	if err := FromConfig(cfg).NotifyEvent("maintenance_report", map[string]any{
		"title":         "Night maintenance",
		"db_problems":   summary.DBProblems,
		"db_freed":      summary.DBFreed,
		"missing":       summary.Missing,
		"audio_cleaned": summary.AudioCleaned,
		"audio_saved":   summary.AudioSaved,
		"audio_failed":  summary.AudioFailed,
	}); err != nil {
		logging.Warn("the night maintenance notification could not be sent", "error", err.Error())
	}
}

// maintenanceNightText is the one-line summary of a night, for the log.
func maintenanceNightText(summary maintenanceNightSummary) string {
	parts := []string{}
	if summary.DBDone {
		switch {
		case summary.DBProblems > 0:
			parts = append(parts, fmt.Sprintf("the database check found %d problem(s)", summary.DBProblems))
		case summary.DBFreed > 0:
			parts = append(parts, fmt.Sprintf("databases optimised, %s freed", logging.HumanBytesI64(summary.DBFreed)))
		default:
			parts = append(parts, "databases optimised")
		}
	}
	if summary.ReportDone {
		if summary.Missing > 0 {
			parts = append(parts, fmt.Sprintf("%d library file(s) missing from the disk", summary.Missing))
		} else {
			parts = append(parts, "no library file missing")
		}
	}
	if summary.AudioCleaned > 0 || summary.AudioFailed > 0 {
		text := fmt.Sprintf("%d file(s) cleaned of other audio languages, %s saved", summary.AudioCleaned, logging.HumanBytesI64(summary.AudioSaved))
		if summary.AudioFailed > 0 {
			text += fmt.Sprintf(", %d left as they were after a failed check", summary.AudioFailed)
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "; ")
}
