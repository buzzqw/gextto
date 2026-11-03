package gextto

// Backup handling: creation, listing and validation of the local snapshots,
// their retention, the copies to FTP, to a cloud directory and to Telegram,
// and the backup settings the UI reads and writes.

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

func BackupSendTelegram(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	notifier := FromConfig(cfg)
	dataDir := cfg.DataDir
	root := filepath.Join(dataDir, "backups")
	retain := 5
	if value, ok := cfg.Settings["backup_retention"]; ok {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			retain = parsed
		}
	}
	if retain < 1 {
		retain = 1
	}
	if retain > 100 {
		retain = 100
	}
	path, err := CreateSnapshotWithExtras(dataDir, root, retain, BackupExtraPaths(cfg.Settings["backup_extra_paths"]))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	parts, err := notifier.NotifyBackupDocument(path, "Gextto backup: "+filepath.Base(path))
	response := map[string]any{"ok": true, "path": path, "sent": err == nil && parts > 0, "parts": parts}
	if err != nil {
		logging.Warn("backup Telegram upload failed", "parts_sent", parts, "error", err)
		response["error"] = err.Error()
	}
	jsonStatus(w, http.StatusOK, response)
}

// Converts a JSON-decoded setting value (string, bool, number or raw JSON) to
// the string stored in the settings file; ok is false for any other type.
func gh2_settingsValue(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		if typed {
			return "true", true
		}
		return "false", true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case json.Number:
		return typed.String(), true
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err != nil {
			return "", false
		}
		return gh2_settingsValue(decoded)
	default:
		return "", false
	}
}

// isJSONNull reports whether a flattened setting value is absent or JSON null;
// a null value is skipped instead of being stored.
func isJSONNull(value any) bool {
	if value == nil {
		return true
	}
	if raw, ok := value.(json.RawMessage); ok {
		return string(bytes.TrimSpace(raw)) == "null"
	}
	return false
}

// BackupSettings returns the backup options stored in settings, with their
// defaults, and tells whether an FTP password is set without revealing it.
func BackupSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	value := func(key, fallback string) string {
		if current, ok := cfg.Settings[key]; ok {
			return current
		}
		return fallback
	}
	password := cfg.Settings["backup_ftp_password"]
	_, passwordPresent := cfg.Settings["backup_ftp_password"]
	jsonResponse(w, map[string]any{
		"ok":                      true,
		"retention":               value("backup_retention", "5"),
		"schedule_hours":          value("backup_schedule_hours", "0"),
		"schedule_at":             value("backup_schedule_at", ""),
		"send_telegram":           settingTruthy(cfg.Settings["backup_send_telegram"]),
		"ftp_host":                value("backup_ftp_host", ""),
		"ftp_user":                value("backup_ftp_user", ""),
		"ftp_path":                value("backup_ftp_path", ""),
		"cloud_dir":               value("backup_cloud_dir", ""),
		"extra_paths":             value("backup_extra_paths", ""),
		"ftp_password_configured": passwordPresent && password != "",
	})
}

// SaveBackupSettings stores the backup options sent in the request, in key
// order: keys outside the backup section are ignored, an empty FTP password is
// skipped so the stored one is not erased, and an extra path must be absolute.
func SaveBackupSettings(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input SettingsPatch
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowed := map[string]struct{}{
		"backup_retention":      {},
		"backup_schedule_hours": {},
		"backup_schedule_at":    {},
		"backup_send_telegram":  {},
		"backup_ftp_host":       {},
		"backup_ftp_user":       {},
		"backup_ftp_password":   {},
		"backup_ftp_path":       {},
		"backup_cloud_dir":      {},
		"backup_extra_paths":    {},
	}
	keys := make([]string, 0, len(input.Values))
	for key := range input.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	saved := []string{}
	for _, key := range keys {
		raw := input.Values[key]
		if _, ok := allowed[key]; !ok || isJSONNull(raw) {
			continue
		}
		value, ok := gh2_settingsValue(raw)
		if !ok {
			jsonError(w, http.StatusBadRequest, "invalid backup setting value")
			return
		}
		if key == "backup_ftp_password" && value == "" {
			continue
		}
		if len(value) > 4096 {
			jsonError(w, http.StatusBadRequest, "backup setting too long")
			return
		}
		if key == "backup_extra_paths" {
			for _, field := range strings.FieldsFunc(value, func(r rune) bool {
				return r == '\n' || r == '\r' || r == ','
			}) {
				path := strings.TrimSpace(field)
				if path != "" && !filepath.IsAbs(path) {
					jsonError(w, http.StatusBadRequest, "backup_extra_paths: usa percorsi assoluti: "+path)
					return
				}
			}
		}
		if err := SaveSetting(s.cfg.DataDir, key, value); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		saved = append(saved, key)
	}
	jsonStatus(w, http.StatusOK, map[string]any{"ok": true, "saved": saved})
}

// BackupTestFtp runs connection, login, path, upload and delete against the
// FTP server configured or sent in the request, and returns the outcome of
// every step.
func BackupTestFtp(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	var input FtpTestInput
	if !DecodeOptionalJSON(w, r, &input) {
		return
	}
	pick := func(inline *string, key string) string {
		if inline != nil {
			value := strings.TrimSpace(*inline)
			if value != "" {
				return value
			}
		}
		return cfg.Settings[key]
	}
	host := pick(input.Host, "backup_ftp_host")
	user := pick(input.User, "backup_ftp_user")
	password := pick(input.Password, "backup_ftp_password")
	path := pick(input.Path, "backup_ftp_path")
	if host == "" || user == "" || password == "" {
		logging.Info("FTP test not run: the server, user and password are all needed")
		jsonError(w, http.StatusBadRequest, "host, utente e password FTP sono obbligatori")
		return
	}
	// Always 200 so the UI can show exactly which step succeeded/failed.
	report := TestFTP(host, user, password, path)
	logging.Debug("FTP test result",
		"host", report.Host,
		"path", report.Path,
		"test_file", report.TestFile,
		"connected", report.Connected,
		"logged_in", report.LoggedIn,
		"entered_path", report.EnteredPath,
		"uploaded", report.Uploaded,
		"deleted", report.Deleted,
		"error", derefString(report.Error))
	var reportError any
	if report.Error != nil {
		reportError = *report.Error
	}
	jsonStatus(w, http.StatusOK, map[string]any{
		"ok":           report.OK(),
		"host":         report.Host,
		"user":         report.User,
		"path":         report.Path,
		"test_file":    report.TestFile,
		"connected":    report.Connected,
		"logged_in":    report.LoggedIn,
		"entered_path": report.EnteredPath,
		"uploaded":     report.Uploaded,
		"deleted":      report.Deleted,
		"error":        reportError,
	})
}

type ftpConfig struct {
	host     string
	user     string
	password string
	remote   string
}

type backupSteps struct {
	path             string
	sizeBytes        int64
	ftpUploaded      bool
	ftpHost          string
	ftpRemote        string
	ftpError         *string
	cloudCopied      bool
	cloudDestination string
	cloudError       *string
}

// backupLabel is the label shown for a snapshot: the timestamp in its name when
// the name carries one, otherwise the file's modification time.
func backupLabel(name string, modified *uint64) string {
	var seconds *int64
	if strings.HasPrefix(name, "snapshot-") && strings.HasSuffix(name, ".zip") {
		raw := name[len("snapshot-") : len(name)-len(".zip")]
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			seconds = &value
		}
	}
	if seconds == nil && modified != nil {
		value := int64(*modified)
		seconds = &value
	}
	if seconds != nil {
		return time.Unix(*seconds, 0).Format("02/01/2006 15:04")
	}
	return "data sconosciuta"
}

func modifiedSeconds(value any) uint64 {
	if number, ok := value.(uint64); ok {
		return number
	}
	return 0
}

// CreateBackup creates a snapshot, copies it to FTP, to the cloud directory and
// to Telegram as configured, sends the backup_completed notification and
// answers with the outcome of each step.
func CreateBackup(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	notifier := FromConfig(cfg)
	dataDir := cfg.DataDir
	root := filepath.Join(dataDir, "backups")
	retain := backupRetention(cfg)
	ftp := backupFtpConfig(cfg)
	cloudDir := backupCloudDir(cfg)
	sendTelegram := false
	if value, ok := cfg.Settings["backup_send_telegram"]; ok {
		sendTelegram = settingTruthy(value)
	}

	steps, err := runBackupSteps(dataDir, root, retain, ftp, cloudDir, BackupExtraPaths(cfg.Settings["backup_extra_paths"]))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	telegramUploaded := false
	telegramParts := 0
	telegramError := ""
	if sendTelegram {
		caption := "Gextto backup: " + filepath.Base(steps.path)
		parts, uploadErr := notifier.NotifyBackupDocument(steps.path, caption)
		telegramParts = parts
		telegramUploaded = uploadErr == nil && parts > 0
		if uploadErr != nil {
			telegramError = uploadErr.Error()
			logging.Warn("backup Telegram upload failed", "parts_sent", parts, "error", uploadErr)
		} else {
			logging.Debug("backup Telegram send completed", "uploaded", telegramUploaded, "parts", parts)
		}
	}
	logging.Info(backupLogSummary(steps, false, telegramParts, telegramError))
	// The notification gets an empty error for a step that was not configured
	// (nil): the "none" placeholder would read as a failure.
	_ = notifier.NotifyEvent("backup_completed", map[string]any{
		"path":              steps.path,
		"size_bytes":        steps.sizeBytes,
		"ftp_uploaded":      steps.ftpUploaded,
		"ftp_host":          steps.ftpHost,
		"ftp_remote":        steps.ftpRemote,
		"ftp_error":         bg_derefStr(steps.ftpError),
		"cloud_copied":      steps.cloudCopied,
		"cloud_destination": steps.cloudDestination,
		"cloud_error":       bg_derefStr(steps.cloudError),
		"telegram_uploaded": telegramUploaded,
		"telegram_parts":    telegramParts,
		"telegram_error":    telegramError,
	})
	jsonResponse(w, map[string]any{
		"ok":                true,
		"path":              steps.path,
		"size_bytes":        steps.sizeBytes,
		"ftp_uploaded":      steps.ftpUploaded,
		"ftp_host":          steps.ftpHost,
		"ftp_remote":        steps.ftpRemote,
		"ftp_error":         steps.ftpError,
		"cloud_copied":      steps.cloudCopied,
		"cloud_destination": steps.cloudDestination,
		"cloud_error":       steps.cloudError,
		"telegram_uploaded": telegramUploaded,
	})
}

// ListBackups returns the snapshots stored in the backups directory, newest
// first.
func ListBackups(w http.ResponseWriter, r *http.Request, s *AppState) {
	root := filepath.Join(s.cfg.DataDir, "backups")
	items := []map[string]any{}
	entries, err := os.ReadDir(root)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".zip" {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				continue
			}
			name := entry.Name()
			var modified any
			var modifiedSeconds *uint64
			modTime := info.ModTime()
			if !modTime.IsZero() && !modTime.Before(time.Unix(0, 0)) {
				seconds := uint64(modTime.Unix())
				modified = seconds
				modifiedSeconds = &seconds
			}
			items = append(items, map[string]any{
				"name":       name,
				"path":       filepath.Join(root, name),
				"size_bytes": info.Size(),
				"modified":   modified,
				"label":      backupLabel(name, modifiedSeconds),
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left := modifiedSeconds(items[i]["modified"])
		right := modifiedSeconds(items[j]["modified"])
		if left != right {
			return left > right
		}
		return items[i]["name"].(string) > items[j]["name"].(string)
	})
	jsonResponse(w, map[string]any{"items": items})
}

// ValidateBackup verifies a named local backup before a recovery operation.
// It never extracts the archive: ZIP names are untrusted and a validation must
// not be able to write into the data directory.
func ValidateBackup(w http.ResponseWriter, r *http.Request, s *AppState) {
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, http.StatusBadRequest, "Nome backup non valido")
		return
	}
	name := filepath.Base(strings.TrimSpace(input.Name))
	if name == "." || name == "" || name != strings.TrimSpace(input.Name) || filepath.Ext(name) != ".zip" {
		jsonError(w, http.StatusBadRequest, "Seleziona un file ZIP dalla lista dei backup")
		return
	}
	path := filepath.Join(latestConfig(s).DataDir, "backups", name)
	if err := verifyZipArchive(path); err != nil {
		logging.Warn("backup verification failed", "path", path, "error", err)
		jsonError(w, http.StatusUnprocessableEntity, "Il backup non è leggibile o è danneggiato; usa un’altra copia.")
		return
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		jsonError(w, http.StatusUnprocessableEntity, "Il backup non è leggibile o è danneggiato; usa un’altra copia.")
		return
	}
	defer reader.Close()
	archiveDatabases := make([]string, 0, len(databases))
	known := make(map[string]struct{}, len(databases))
	for _, database := range databases {
		known[database] = struct{}{}
	}
	for _, entry := range reader.File {
		if _, ok := known[entry.Name]; ok {
			archiveDatabases = append(archiveDatabases, entry.Name)
		}
	}
	sort.Strings(archiveDatabases)
	jsonResponse(w, map[string]any{
		"ok":        true,
		"name":      name,
		"valid":     true,
		"entries":   len(reader.File),
		"databases": archiveDatabases,
		"message":   "Backup verificato: lo ZIP è leggibile e tutti i file interni hanno superato il controllo d’integrità.",
	})
}

// backupFtpConfig returns the FTP target for the backups, or nil when host,
// user or password is missing.
func backupFtpConfig(cfg *Config) *ftpConfig {
	host := cfg.Settings["backup_ftp_host"]
	user := cfg.Settings["backup_ftp_user"]
	password := cfg.Settings["backup_ftp_password"]
	remote := cfg.Settings["backup_ftp_path"]
	if strings.TrimSpace(host) != "" && strings.TrimSpace(user) != "" && password != "" {
		return &ftpConfig{host: host, user: user, password: password, remote: remote}
	}
	return nil
}

// backupCloudDir returns the configured cloud directory, or nil when it is
// empty.
func backupCloudDir(cfg *Config) *string {
	value := strings.TrimSpace(cfg.Settings["backup_cloud_dir"])
	if value == "" {
		return nil
	}
	return &value
}

// backupRetention returns how many snapshots to keep, between 1 and 100.
func backupRetention(cfg *Config) int {
	retain := 5
	if parsed, err := strconv.Atoi(strings.TrimSpace(cfg.Settings["backup_retention"])); err == nil {
		retain = parsed
	}
	if retain < 1 {
		retain = 1
	}
	if retain > 100 {
		retain = 100
	}
	return retain
}

// runBackupSteps creates the snapshot, then copies it to FTP and to the cloud
// directory when they are configured. A failed copy is recorded in the returned
// steps; only a failed snapshot is an error.
func runBackupSteps(dataDir, root string, retain int, ftp *ftpConfig, cloudDir *string, extras []string) (backupSteps, error) {
	path, err := CreateSnapshotWithExtras(dataDir, root, retain, extras)
	if err != nil {
		return backupSteps{}, err
	}
	var size int64
	if info, statErr := os.Stat(path); statErr == nil {
		size = info.Size()
	}
	logging.Debug("backup snapshot created", "path", path, "size_bytes", size)

	steps := backupSteps{path: path, sizeBytes: size}
	if ftp != nil {
		steps.ftpHost = ftp.host
		steps.ftpRemote = ftp.remote
		if err := UploadFTP(path, ftp.host, ftp.user, ftp.password, ftp.remote); err != nil {
			message := err.Error()
			steps.ftpError = &message
			logging.Warn("backup FTP upload failed", "path", path, "host", ftp.host, "error", message)
		} else {
			steps.ftpUploaded = true
		}
	}
	if cloudDir != nil {
		if destination, err := CopySnapshot(path, *cloudDir); err != nil {
			message := err.Error()
			steps.cloudError = &message
			logging.Warn("backup cloud copy failed", "directory", *cloudDir, "error", message)
		} else {
			steps.cloudCopied = true
			steps.cloudDestination = destination
		}
	}
	return steps, nil
}

// backupLogSummary is the single INFO line of a finished backup: size,
// path and the outcome of each copy that is configured. Steps that are not
// configured are not mentioned; failures also have their own WARN line.
func backupLogSummary(steps backupSteps, scheduled bool, telegramParts int, telegramError string) string {
	label := "💾 Backup saved"
	if scheduled {
		label = "💾 Scheduled backup saved"
	}
	parts := []string{fmt.Sprintf("%s (%s): %s", label, logging.HumanBytesI64(steps.sizeBytes), steps.path)}
	switch {
	case steps.ftpUploaded:
		parts = append(parts, "FTP "+steps.ftpHost+" ✓")
	case steps.ftpError != nil:
		parts = append(parts, "FTP "+steps.ftpHost+" ✗")
	}
	switch {
	case steps.cloudCopied:
		parts = append(parts, "cloud copy ✓")
	case steps.cloudError != nil:
		parts = append(parts, "cloud copy ✗")
	}
	switch {
	case telegramError != "":
		parts = append(parts, "Telegram ✗")
	case telegramParts > 1:
		parts = append(parts, fmt.Sprintf("Telegram ✓ (%d parts)", telegramParts))
	case telegramParts == 1:
		parts = append(parts, "Telegram ✓")
	}
	return strings.Join(parts, " · ")
}
