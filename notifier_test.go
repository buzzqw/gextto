package gextto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/buzzqw/gextto/internal/messages"
)

// TestNotifierDisabledWhenNothingConfigured ports the
// `disabled_notifier_is_a_noop` test and checks a partially configured
// Telegram channel does not count as enabled.
func TestNotifierDisabledWhenNothingConfigured(t *testing.T) {
	if (&Notifier{}).Enabled() {
		t.Fatal("a zero Notifier should be disabled")
	}
	if NewNotifier().Enabled() {
		t.Fatal("NewNotifier should be disabled")
	}

	cfg := DefaultConfig()
	if FromConfig(&cfg).Enabled() {
		t.Fatal("DefaultConfig should not enable the notifier")
	}

	// Telegram enabled without credentials is still disabled.
	cfg.NotifyTelegram = true
	if FromConfig(&cfg).Enabled() {
		t.Fatal("Telegram without token/chat id should not be enabled")
	}
}

// TestNotifierHexBytes ports the `hmac_encoding_is_lowercase_hex` test.
func TestNotifierHexBytes(t *testing.T) {
	if got, want := hexBytes([]byte{0, 15, 255}), "000fff"; got != want {
		t.Fatalf("hexBytes = %q, want %q", got, want)
	}
}

// TestNotifierWebhookSignatureAndBody posts an event to an httptest webhook and
// verifies the HMAC-SHA256 signature header and the expected JSON envelope.
func TestNotifierWebhookSignatureAndBody(t *testing.T) {
	secret := "top-secret"
	bodyCh := make(chan []byte, 1)
	signatureCh := make(chan string, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			http.Error(w, "content-type", http.StatusBadRequest)
			return
		}
		bodyCh <- body
		signatureCh <- r.Header.Get("x-gextto-signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	cfg.NotifyWebhookSecret = &secret
	notifier := FromConfig(&cfg)
	if !notifier.Enabled() {
		t.Fatal("a configured webhook should enable the notifier")
	}

	event := "comic_completed"
	data := map[string]any{"title": "Batman", "size_bytes": 1234, "method": "direct"}
	if err := notifier.NotifyEvent(event, data); err != nil {
		t.Fatalf("NotifyEvent: %v", err)
	}

	body := <-bodyCh
	signature := <-signatureCh

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	wantSignature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if signature != wantSignature {
		t.Fatalf("x-gextto-signature = %q, want %q", signature, wantSignature)
	}

	var envelope struct {
		Event string         `json:"event"`
		Data  map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode webhook body %q: %v", body, err)
	}
	if envelope.Event != event {
		t.Fatalf("event = %q, want %q", envelope.Event, event)
	}
	if envelope.Data["title"] != "Batman" {
		t.Fatalf("data.title = %#v", envelope.Data["title"])
	}
	if got, ok := envelope.Data["size_bytes"].(float64); !ok || got != 1234 {
		t.Fatalf("data.size_bytes = %#v, want 1234", envelope.Data["size_bytes"])
	}
	if envelope.Data["method"] != "direct" {
		t.Fatalf("data.method = %#v", envelope.Data["method"])
	}
}

// TestNotifierNoSignatureWithoutSecret checks the header is absent when no
// secret is configured.
func TestNotifierNoSignatureWithoutSecret(t *testing.T) {
	headerCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		headerCh <- r.Header.Get("x-gextto-signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	notifier := FromConfig(&cfg)
	if err := notifier.NotifyEvent("message", map[string]any{"text": "hi"}); err != nil {
		t.Fatalf("NotifyEvent: %v", err)
	}
	if signature := <-headerCh; signature != "" {
		t.Fatalf("expected no signature header, got %q", signature)
	}
}

// TestNotifierRetriesAfterServerError checks that a 500 response is retried.
// The first request fails, the retry succeeds, so the delivered count is two.
func TestNotifierRetriesAfterServerError(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		if atomic.AddInt32(&requests, 1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	notifier := FromConfig(&cfg)

	if err := notifier.NotifyEvent("message", map[string]any{"text": "retry me"}); err != nil {
		t.Fatalf("NotifyEvent should recover on retry: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 2 {
		t.Fatalf("requests = %d, want 2 (one failure + one retry)", got)
	}
}

// TestNotifierReportsPersistentServerError checks the error surfaces after the
// retries are exhausted. It costs the built-in backoff (1s + 2s).
func TestNotifierReportsPersistentServerError(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		atomic.AddInt32(&requests, 1)
		http.Error(w, "still broken", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := DefaultConfig()
	cfg.NotifyWebhookURL = &server.URL
	notifier := FromConfig(&cfg)

	err := notifier.NotifyEvent("message", map[string]any{"text": "always fails"})
	if err == nil {
		t.Fatal("expected an error after retries are exhausted")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("error = %v, want HTTP 500", err)
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("requests = %d, want 3 (initial + 2 retries)", got)
	}
}

// TestNotifierFormatEventItalian ports the
// `formats_known_events_in_italian` test.
func TestNotifierFormatEventItalian(t *testing.T) {
	previous := messages.Language()
	t.Cleanup(func() { messages.SetLanguage(previous) })
	messages.SetLanguage("it")

	episode := map[string]any{"series": "FBI", "season": 2, "episode": 5}
	started := formatEvent("download_started", episode)
	if !strings.Contains(started, "Serie: FBI") {
		t.Fatalf("download_started missing series: %q", started)
	}
	if !strings.Contains(started, "S02E05") {
		t.Fatalf("download_started missing episode code: %q", started)
	}
	if got := formatEvent("gap_filled", episode); !strings.HasPrefix(got, "Gextto: gap riempito") {
		t.Fatalf("gap_filled = %q", got)
	}
	if got := formatEvent("message", map[string]any{"text": "ciao"}); got != "ciao" {
		t.Fatalf("message = %q, want ciao", got)
	}
	movie := map[string]any{"kind": "movie", "title": "Dune"}
	if got := formatEvent("download_started", movie); !strings.Contains(got, "Dune") {
		t.Fatalf("movie download_started missing title: %q", got)
	}
}

// TestNotifierFormatHelpers covers the human formatting helpers used in the
// notification bodies.
func TestNotifierFormatHelpers(t *testing.T) {
	if got, want := formatBytes(0), "0 B"; got != want {
		t.Fatalf("formatBytes(0) = %q, want %q", got, want)
	}
	if got, want := formatBytes(1536), "1.50 KB"; got != want {
		t.Fatalf("formatBytes(1536) = %q, want %q", got, want)
	}
	if got, want := formatBytes(-5), "0 B"; got != want {
		t.Fatalf("formatBytes(-5) = %q, want %q", got, want)
	}
	if got, want := formatDuration(3661), "1h 1m 1s"; got != want {
		t.Fatalf("formatDuration(3661) = %q, want %q", got, want)
	}
	if got, want := formatDuration(61), "1m 1s"; got != want {
		t.Fatalf("formatDuration(61) = %q, want %q", got, want)
	}
	if got, want := formatDuration(9), "9s"; got != want {
		t.Fatalf("formatDuration(9) = %q, want %q", got, want)
	}
}

// TestFormatEventSeedingNotification pins the wording split: a completion that
// keeps seeding uses the same event but a different message than the final
// "archived" one.
func TestFormatEventSeedingNotification(t *testing.T) {
	seeding := formatEvent("torrent_completed", map[string]any{
		"series":     "Show",
		"season":     int64(1),
		"episode":    int64(2),
		"size_bytes": int64(1000),
		"path":       "/downloads/x.mkv",
		"seeding":    true,
	})
	if !strings.Contains(seeding, "SEED") {
		t.Fatalf("seeding message = %q", seeding)
	}
	if strings.Contains(seeding, "Archiviato") || strings.Contains(seeding, "Archived") {
		t.Fatalf("seeding message must not say archived: %q", seeding)
	}

	archived := formatEvent("torrent_completed", map[string]any{
		"series":     "Show",
		"season":     int64(1),
		"episode":    int64(2),
		"size_bytes": int64(1000),
		"path":       "/nas/x.mkv",
	})
	if strings.Contains(archived, "SEED") {
		t.Fatalf("archived message must not say seeding: %q", archived)
	}
	if !strings.Contains(archived, "/nas/x.mkv") {
		t.Fatalf("archived message must carry the path: %q", archived)
	}
}

func TestFormatEventManualTorrentCompletion(t *testing.T) {
	message := formatEvent("torrent_completed", map[string]any{
		"title":      "Minions.&.Monsters.2026.iTA-ENG.WEBDL.2160p.HEVC.HDR.x265-CYBER.mkv",
		"path":       "/downloads/Minions.&.Monsters.mkv",
		"size_bytes": int64(1024),
		"manual":     true,
	})
	for _, want := range []string{"AGGIUNTO MANUALMENTE", "Minions.&.Monsters.2026", "1.00 KB", "/downloads/Minions.&.Monsters.mkv"} {
		if !strings.Contains(message, want) {
			t.Fatalf("manual completion message missing %q: %q", want, message)
		}
	}
	if strings.Contains(message, "Archiviato") {
		t.Fatalf("manual completion message must not claim the file was archived: %q", message)
	}
}

func TestFormatEventBackupNotification(t *testing.T) {
	previous := messages.Language()
	t.Cleanup(func() { messages.SetLanguage(previous) })

	// Italian test
	messages.SetLanguage("it")
	msg := formatEvent("backup_completed", map[string]any{
		"path":              "/home/andres/backups/gextto-backup.zip",
		"size_bytes":        int64(15 * 1024 * 1024),
		"scheduled":         true,
		"cloud_copied":      true,
		"cloud_destination": "/mnt/cloud/backups/gextto-backup.zip",
		"ftp_uploaded":      true,
		"ftp_host":          "ftp.example.com",
		"ftp_remote":        "/remote/backups",
		"telegram_uploaded": true,
	})

	if !strings.Contains(msg, "BACKUP PROGRAMMATO COMPLETATO") {
		t.Fatalf("missing scheduled header: %q", msg)
	}
	if !strings.Contains(msg, "File: /home/andres/backups/gextto-backup.zip") {
		t.Fatalf("missing local path: %q", msg)
	}
	if !strings.Contains(msg, "Dimensione: 15.00 MB") {
		t.Fatalf("missing size: %q", msg)
	}
	if !strings.Contains(msg, "Copia cloud: /mnt/cloud/backups/gextto-backup.zip") {
		t.Fatalf("missing cloud destination: %q", msg)
	}
	if !strings.Contains(msg, "Caricato via FTP: ftp.example.com (/remote/backups/gextto-backup.zip)") {
		t.Fatalf("missing ftp info: %q", msg)
	}
	if !strings.Contains(msg, "allegato Telegram") {
		t.Fatalf("missing telegram document marker: %q", msg)
	}

	// English test with errors
	messages.SetLanguage("en")
	msgEn := formatEvent("backup_completed", map[string]any{
		"path":        "/backups/backup.zip",
		"cloud_error": "access denied",
		"ftp_host":    "ftp.bad.com",
		"ftp_error":   "connection refused",
	})
	if !strings.Contains(msgEn, "BACKUP COMPLETED") {
		t.Fatalf("missing English header: %q", msgEn)
	}
	if !strings.Contains(msgEn, "Cloud copy failed: access denied") {
		t.Fatalf("missing cloud error: %q", msgEn)
	}
	if !strings.Contains(msgEn, "FTP upload failed (ftp.bad.com): connection refused") {
		t.Fatalf("missing ftp error: %q", msgEn)
	}
}

func TestFormatEventMovieAndErrors(t *testing.T) {
	previous := messages.Language()
	t.Cleanup(func() { messages.SetLanguage(previous) })
	messages.SetLanguage("it")

	// Movie completion uses movie icon 🎬 instead of 📺
	movieDone := formatEvent("torrent_completed", map[string]any{
		"kind":       "movie",
		"title":      "Minions 2026",
		"size_bytes": int64(4500000000),
		"path":       "/movies/Minions (2026).mkv",
	})
	if !strings.Contains(movieDone, "🎬 Minions 2026") {
		t.Fatalf("movie completion missing movie icon: %q", movieDone)
	}

	// Torrent error with release name and restored upgrade
	errDone := formatEvent("torrent_error", map[string]any{
		"name":             "Show S01E01",
		"error":            "metadata timeout",
		"upgrade_restored": true,
	})
	if !strings.Contains(errDone, "«Show S01E01» — metadata timeout (versione precedente ripristinata)") {
		t.Fatalf("torrent error formatting unexpected: %q", errDone)
	}

	// Download failed with release title and error
	failDone := formatEvent("download_failed", map[string]any{
		"title":            "Show S01E02",
		"error":            "stalled download",
		"upgrade_restored": false,
	})
	if !strings.Contains(failDone, "«Show S01E02» — stalled download") {
		t.Fatalf("download failed formatting unexpected: %q", failDone)
	}
}

func TestFormatEmailSubject(t *testing.T) {
	previous := messages.Language()
	t.Cleanup(func() { messages.SetLanguage(previous) })
	messages.SetLanguage("it")

	subj1 := formatEmailSubject("download_started", map[string]any{"series": "FBI", "season": 3, "episode": 1})
	if subj1 != "Gextto: Nuovo episodio in download — FBI S03E01" {
		t.Fatalf("subj1 = %q", subj1)
	}

	subj2 := formatEmailSubject("torrent_completed", map[string]any{"kind": "movie", "title": "Minions 2026"})
	if subj2 != "Gextto: Download completato — Minions 2026" {
		t.Fatalf("subj2 = %q", subj2)
	}

	subj3 := formatEmailSubject("backup_completed", map[string]any{"scheduled": true})
	if subj3 != "Gextto: Backup programmato completato" {
		t.Fatalf("subj3 = %q", subj3)
	}
}
