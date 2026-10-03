package gextto

// Background workers : the dedicated backup scheduler,
// the scheduled MediaInfo backfill and the calendar warm-up. Sibling workers
// live in `web_background.go`.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// backupWorker implements `backup_worker` (the web module:7604): checks every
// minute so the daily time is accurate, and persists the last backup time so it
// is not repeated on every restart.
func backupWorker(state *AppState) {
	last := bwm_loadLastBackup(state.cfg.DataDir)
	for {
		cfg, err := LoadConfig(state.config_path)
		if err != nil {
			logging.Warn("backup scheduler config reload failed", "error", err)
			if !state.SleepBackground(60 * time.Second) {
				return
			}
			continue
		}
		if cfg.Active && bwm_backupDue(&cfg, last) {
			notifier := FromConfig(&cfg)
			if err := bwm_executeScheduledBackup(&cfg, notifier); err != nil {
				logging.Error("scheduled backup failed", "error", err)
			} else {
				now := time.Now()
				last = &now
				bwm_saveLastBackup(cfg.DataDir, now)
			}
		}
		if !state.SleepBackground(60 * time.Second) {
			return
		}
	}
}

// mediaInfoBackfillWorker implements `media_info_backfill_worker`
// (the web module:8495): a few archived files per run, only those without stored
// MediaInfo.
func mediaInfoBackfillWorker(state *AppState) {
	if !state.SleepBackground(300 * time.Second) {
		return
	}
	warnedUnavailable := false
	for {
		cfg := latestConfig(state)
		enabled := true
		if value, ok := cfg.Settings["media_info_backfill_enabled"]; ok {
			enabled = bwm_truthy(value)
		}
		intervalMinutes := int64(60)
		if parsed, err := strconv.ParseInt(strings.TrimSpace(cfg.Settings["media_info_backfill_interval_minutes"]), 10, 64); err == nil {
			intervalMinutes = parsed
		}
		if intervalMinutes < 5 {
			intervalMinutes = 5
		}
		if intervalMinutes > 24*60 {
			intervalMinutes = 24 * 60
		}
		batch := 10
		if parsed, err := strconv.Atoi(strings.TrimSpace(cfg.Settings["media_info_backfill_batch"])); err == nil {
			batch = parsed
		}
		if batch < 1 {
			batch = 1
		}
		if batch > 200 {
			batch = 200
		}
		if enabled {
			if Available() {
				warnedUnavailable = false
				report, err := gh0_runMediaInfoBackfill(state.bgContext, state, batch, nil)
				if err != nil {
					logging.Warn("MediaInfo backfill run failed", "error", err)
				} else {
					candidates := bwm_reportCount(report, "candidates")
					if candidates > 0 {
						missingFiles, _ := report["missing_files"].([]string)
						logging.Info(
							"🔬 MediaInfo backfill: esito analisi file archiviati",
							"candidates", candidates,
							"analyzed", bwm_reportCount(report, "probed"),
							"failed", bwm_reportCount(report, "failed"),
							"skipped_missing", bwm_reportCount(report, "skipped"),
							"missing_files", strings.Join(missingFiles, " | "),
						)
					}
				}
			} else if !warnedUnavailable {
				warnedUnavailable = true
				logging.Warn("ffprobe is not available: MediaInfo backfill paused until it is installed")
			}
		}
		if !state.SleepBackground(time.Duration(intervalMinutes) * 60 * time.Second) {
			return
		}
	}
}

// calendarWarmupWorker implements `calendar_warmup_worker` (the web module:6639):
// precomputes the calendar at startup and refreshes it when the cache expires,
// so the endpoint answers immediately.
func calendarWarmupWorker(state *AppState) {
	if !state.SleepBackground(3 * time.Second) {
		return
	}
	for {
		cfg := latestConfig(state)
		if cfg.TmdbAPIKey != nil {
			if _, ok := cacheGet("calendar", 120*time.Second); !ok {
				response := gh0_buildCalendar(context.Background(), cfg)
				cachePut("calendar", response)
			}
		}
		if !state.SleepBackground(60 * time.Second) {
			return
		}
	}
}

// flareSolverrSweeperWorker destroys the FlareSolverr browser sessions owned by
// Gextto once their reuse window expires, and the sessions orphaned by a
// previous Gextto run. Without it a Cloudflare-protected domain that is not
// queried again keeps a whole browser (hundreds of MB) alive indefinitely,
// which is how several gigabytes of Chromium can pile up between two cycles.
func flareSolverrSweeperWorker(state *AppState) {
	if !state.SleepBackground(30 * time.Second) {
		return
	}
	firstRun := true
	for {
		cfg := latestConfig(state)
		if endpoint := flareSolverrEndpointFromConfig(cfg); endpoint != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			if firstRun {
				// Sessions from the previous run are untracked after a restart
				// (the map is in-memory): reconcile them once at startup, before
				// the sweep, so the same id is not destroyed twice.
				reconcileFlareSolverrSessions(ctx, defaultHTTPClient, endpoint)
			}
			sweepStaleFlareSolverrSessions(ctx, defaultHTTPClient, endpoint)
			cancel()
		}
		firstRun = false
		if !state.SleepBackground(2 * time.Minute) {
			return
		}
	}
}

// ---------------------------------------------------------------------------
// bwm_ helpers
// ---------------------------------------------------------------------------

// bwm_truthy ports the `matches!(value, "yes" | "true" | "1")` setting check.
func bwm_truthy(value string) bool {
	return value == "yes" || value == "true" || value == "1"
}

// bwm_optionalString renders a nullable error string as the tracing
// `unwrap_or("none")` does.
func bwm_optionalString(value *string) string {
	if value == nil {
		return "none"
	}
	return *value
}

// bwm_backupStatePath is the file holding the last automatic backup time.
func bwm_backupStatePath(dataDir string) string {
	return filepath.Join(dataDir, ".gextto-backup-last")
}

// bwm_loadLastBackup mirrors the web module `load_last_backup`.
func bwm_loadLastBackup(dataDir string) *time.Time {
	text, err := os.ReadFile(bwm_backupStatePath(dataDir))
	if err != nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(string(text)))
	if err != nil {
		return nil
	}
	local := parsed.Local()
	return &local
}

// bwm_saveLastBackup mirrors the web module `save_last_backup`.
func bwm_saveLastBackup(dataDir string, when time.Time) {
	_ = os.WriteFile(bwm_backupStatePath(dataDir), []byte(when.Format(time.RFC3339)), 0o644)
}

// bwm_backupDue mirrors the web module `backup_due`: `backup_schedule_at` (HH:MM) takes
// precedence (daily at that local time, at most once a day), otherwise the
// `backup_schedule_hours` interval is measured from the last backup.
func bwm_backupDue(cfg *Config, last *time.Time) bool {
	if at := strings.TrimSpace(cfg.Settings["backup_schedule_at"]); at != "" {
		if parsed, err := time.Parse("15:04", at); err == nil {
			now := time.Now()
			alreadyToday := false
			if last != nil {
				local := last.Local()
				alreadyToday = local.Year() == now.Year() && local.YearDay() == now.YearDay()
			}
			nowMinutes := now.Hour()*60 + now.Minute()
			atMinutes := parsed.Hour()*60 + parsed.Minute()
			return !alreadyToday && nowMinutes >= atMinutes
		}
	}
	hours := int64(0)
	if parsed, err := strconv.ParseInt(strings.TrimSpace(cfg.Settings["backup_schedule_hours"]), 10, 64); err == nil {
		hours = parsed
	}
	if hours <= 0 {
		return false
	}
	if last == nil {
		return true
	}
	return int64(time.Since(last.Local()).Seconds()) >= hours*3600
}

// bwm_executeScheduledBackup mirrors the web module `execute_scheduled_backup`: local
// snapshot, FTP, cloud copy and notifications.
func bwm_executeScheduledBackup(cfg *Config, notifier *Notifier) error {
	dataDir := cfg.DataDir
	root := filepath.Join(dataDir, "backups")
	retain := gh5_backupRetention(cfg)
	ftp := gh5_backupFtpConfig(cfg)
	cloudDir := gh5_backupCloudDir(cfg)
	sendTelegram := bwm_truthy(cfg.Settings["backup_send_telegram"])

	steps, err := gh5_runBackupSteps(dataDir, root, retain, ftp, cloudDir)
	if err != nil {
		return err
	}
	telegramUploaded := false
	if sendTelegram {
		caption := fmt.Sprintf("Gextto backup: %s", filepath.Base(steps.path))
		uploaded, uploadErr := notifier.NotifyBackupDocument(steps.path, caption)
		if uploadErr == nil {
			telegramUploaded = uploaded
		}
	}
	logging.Info(
		"scheduled backup completed",
		"path", steps.path,
		"ftp_uploaded", steps.ftpUploaded,
		"ftp_host", steps.ftpHost,
		"ftp_remote", steps.ftpRemote,
		"ftp_error", bwm_optionalString(steps.ftpError),
		"cloud_copied", steps.cloudCopied,
		"cloud_destination", steps.cloudDestination,
		"cloud_error", bwm_optionalString(steps.cloudError),
		"telegram_uploaded", telegramUploaded,
	)
	_ = notifier.NotifyEvent("backup_completed", map[string]any{
		"path":              steps.path,
		"size_bytes":        steps.sizeBytes,
		"scheduled":         true,
		"ftp_uploaded":      steps.ftpUploaded,
		"ftp_host":          steps.ftpHost,
		"ftp_remote":        steps.ftpRemote,
		"ftp_error":         bwm_optionalString(steps.ftpError),
		"cloud_copied":      steps.cloudCopied,
		"cloud_destination": steps.cloudDestination,
		"cloud_error":       bwm_optionalString(steps.cloudError),
		"telegram_uploaded": telegramUploaded,
	})
	return nil
}

// bwm_reportCount reads a numeric counter from a backfill report the way
// `as_u64().unwrap_or(0)` does.
func bwm_reportCount(report map[string]any, key string) uint64 {
	switch value := report[key].(type) {
	case int:
		if value < 0 {
			return 0
		}
		return uint64(value)
	case int64:
		if value < 0 {
			return 0
		}
		return uint64(value)
	case uint64:
		return value
	case float64:
		if value < 0 {
			return 0
		}
		return uint64(value)
	default:
		return 0
	}
}
