package gextto

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/messages"
)

// Notifier delivers notifications over Telegram, an HTTP webhook and SMTP,
// and dispatches the configured external event hooks. It is a faithful implementation of
// gextto's `notifier::Notifier`.
type Notifier struct {
	client           *http.Client
	telegramEnabled  bool
	telegramBotToken *string
	telegramChatID   *string
	webhookURL       *string
	webhookSecret    *string
	webhookFormat    string
	webhookToken     *string
	webhookUser      *string
	emailEnabled     bool
	emailSMTP        string
	emailFrom        *string
	emailTo          *string
	emailPassword    *string
	lastTelegram     *telegramThrottle
	// External event hooks (see hooks.go). Reloadable at runtime when the user
	// edits them in the UI.
	hooks *eventHookStore
	// async makes NotifyEvent enqueue the delivery instead of performing it
	// (see Async).
	async bool
}

// telegramMessage is the Telegram `sendMessage` JSON body.
type telegramMessage struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// telegramThrottle mirrors `Arc<Mutex<Option<Instant>>>`: at most one Telegram
// message per second.
type telegramThrottle struct {
	mu   sync.Mutex
	last *time.Time
}

// eventHookStore mirrors `Arc<RwLock<Vec<EventHook>>>`.
type eventHookStore struct {
	mu    sync.RWMutex
	hooks []EventHook
}

func (s *eventHookStore) load() []EventHook {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.hooks) == 0 {
		return nil
	}
	out := make([]EventHook, len(s.hooks))
	for index, hook := range s.hooks {
		out[index] = hook
		if hook.Events != nil {
			out[index].Events = append([]string(nil), hook.Events...)
		}
	}
	return out
}

func (s *eventHookStore) store(hooks []EventHook) {
	s.mu.Lock()
	s.hooks = hooks
	s.mu.Unlock()
}

// NewNotifier is the `Notifier::new()` constructor: everything disabled.
func NewNotifier() *Notifier {
	return &Notifier{
		client:       defaultHTTPClient,
		emailSMTP:    "smtp.gmail.com:587",
		lastTelegram: &telegramThrottle{},
		hooks:        &eventHookStore{},
	}
}

// FromConfig builds a notifier from the runtime configuration.
func FromConfig(cfg *Config) *Notifier {
	notifier := NewNotifier()
	notifier.telegramEnabled = cfg.NotifyTelegram
	notifier.telegramBotToken = cfg.TelegramBotToken
	notifier.telegramChatID = cfg.TelegramChatID
	notifier.webhookURL = cfg.NotifyWebhookURL
	notifier.webhookSecret = cfg.NotifyWebhookSecret
	notifier.webhookFormat = cfg.NotifyWebhookFormat
	notifier.webhookToken = cfg.NotifyWebhookToken
	notifier.webhookUser = cfg.NotifyWebhookUser
	notifier.emailEnabled = cfg.NotifyEmail
	notifier.emailSMTP = cfg.EmailSMTP
	notifier.emailFrom = cfg.EmailFrom
	notifier.emailTo = cfg.EmailTo
	notifier.emailPassword = cfg.EmailPassword
	notifier.hooks.store(LoadHooks(cfg.Settings))
	return notifier
}

// ReloadHooks replaces the event hooks, e.g. after the settings API saved them.
func (n *Notifier) ReloadHooks(hooks []EventHook) {
	if n.hooks == nil {
		n.hooks = &eventHookStore{}
	}
	n.hooks.store(hooks)
}

// EventHooks returns a copy of the configured event hooks.
func (n *Notifier) EventHooks() []EventHook {
	if n.hooks == nil {
		return nil
	}
	return n.hooks.load()
}

// Enabled reports whether at least one delivery channel is fully configured.
func (n *Notifier) Enabled() bool {
	return (n.telegramEnabled && n.telegramBotToken != nil && n.telegramChatID != nil) ||
		n.webhookURL != nil ||
		(n.emailEnabled && n.emailFrom != nil && n.emailTo != nil && n.emailPassword != nil)
}

// Notify sends a plain text message (`message` event).
func (n *Notifier) Notify(text string) error {
	return n.NotifyEvent("message", map[string]any{"text": text})
}

// NotifyComicComplete emits a `comic_completed` event.
func (n *Notifier) NotifyComicComplete(title, path string, sizeBytes uint64, method string) error {
	return n.NotifyEvent("comic_completed", map[string]any{
		"title":      title,
		"path":       path,
		"size_bytes": sizeBytes,
		"method":     method,
	})
}

// telegramBackupPartSize is the size of each piece a backup is split into for
// Telegram: the Bot API refuses uploads above 50 MB, so stay well below it.
// A variable only so tests can use small pieces.
var telegramBackupPartSize int64 = 45 * 1024 * 1024

// telegramBackupMaxParts caps how many pieces one backup may be sent in
// (about 1.8 GB): beyond that Telegram is not a sensible destination.
const telegramBackupMaxParts = 40

// telegramBackupPartCount is how many pieces a backup of this size is sent in.
func telegramBackupPartCount(size int64) int {
	if size <= telegramBackupPartSize {
		return 1
	}
	return int((size + telegramBackupPartSize - 1) / telegramBackupPartSize)
}

// telegramBackupPartName names piece index (1-based) of total: the archive
// name itself when it fits in one piece, otherwise name.001, name.002, ...
// which `cat name.0* > name` joins back into the original zip.
func telegramBackupPartName(filename string, index, total int) string {
	if total <= 1 {
		return filename
	}
	return fmt.Sprintf("%s.%03d", filename, index)
}

// NotifyBackupDocument uploads a backup archive to Telegram, split into
// pieces below the Bot API upload limit when it is larger. It returns how many
// pieces were sent (0 without an error when Telegram is not configured).
func (n *Notifier) NotifyBackupDocument(path, caption string) (int, error) {
	if n.telegramBotToken == nil || n.telegramChatID == nil {
		return 0, nil
	}
	// `notify_telegram` gates the *notification* messages; uploading a backup
	// is an explicit action driven by `backup_send_telegram` (or a manual
	// click), so it only needs the Telegram credentials, not that switch.
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	total := telegramBackupPartCount(info.Size())
	if total > telegramBackupMaxParts {
		return 0, fmt.Errorf("backup too large for Telegram (%s, more than %d parts)", formatBytes(info.Size()), telegramBackupMaxParts)
	}
	filename := filepath.Base(path)
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "gextto-backup.zip"
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	buffer := make([]byte, telegramBackupPartSize)
	for index := 1; index <= total; index++ {
		read, err := io.ReadFull(file, buffer)
		// The last piece is shorter (ErrUnexpectedEOF); an empty file is EOF.
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return index - 1, err
		}
		partCaption := caption
		if total > 1 {
			partCaption = fmt.Sprintf("%s — %s %d/%d", caption, messages.Pick("parte", "part"), index, total)
		}
		partName := telegramBackupPartName(filename, index, total)
		sendErr := n.sendTelegramDocument(partName, partCaption, buffer[:read])
		for attempt := 0; sendErr != nil && attempt < 2; attempt++ {
			time.Sleep(time.Duration(uint64(2)<<uint(attempt)) * time.Second)
			sendErr = n.sendTelegramDocument(partName, partCaption, buffer[:read])
		}
		if sendErr != nil {
			if total > 1 {
				return index - 1, fmt.Errorf("%s %d/%d: %w", messages.Pick("parte", "part"), index, total, sendErr)
			}
			return 0, sendErr
		}
	}
	return total, nil
}

// sendTelegramDocument uploads one file to the configured chat.
func (n *Notifier) sendTelegramDocument(filename, caption string, content []byte) error {
	n.throttleTelegram()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", *n.telegramChatID)
	_ = writer.WriteField("caption", caption)
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(content); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	rawURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", *n.telegramBotToken)
	request, err := http.NewRequest(http.MethodPost, rawURL, &body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	// A 45 MB piece takes minutes on a slow uplink: the default 90 s client
	// timeout would cut it off.
	client := *n.httpClient()
	client.Timeout = 15 * time.Minute
	response, err := client.Do(request)
	if err != nil {
		return redactRequestError(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

// NotifyEvent formats and delivers one event to every enabled channel and
// dispatches the external hooks.
func (n *Notifier) NotifyEvent(event string, data map[string]any) error {
	if n.async {
		n.enqueue(event, data)
		return nil
	}
	return n.deliverEvent(event, data)
}

// deliverEvent sends one event to every enabled channel, synchronously.
func (n *Notifier) deliverEvent(event string, data map[string]any) error {
	// External hooks run on a detached goroutine so they never delay the
	// notification delivery or the caller.
	hooks := n.EventHooks()
	if len(hooks) > 0 {
		Dispatch(hooks, event, data)
	}
	var firstError error
	if n.telegramEnabled {
		if n.telegramBotToken != nil && n.telegramChatID != nil {
			n.throttleTelegram()
			token := *n.telegramBotToken
			chat := *n.telegramChatID
			rawURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
			send := func() error {
				body, err := json.Marshal(telegramMessage{ChatID: chat, Text: formatEvent(event, data)})
				if err != nil {
					return err
				}
				return n.postJSON(rawURL, nil, body)
			}
			result := send()
			for attempt := 0; attempt < 2; attempt++ {
				if result == nil {
					break
				}
				// Back off before retrying (1 s, then 2 s), like the webhook.
				time.Sleep(time.Duration(uint64(1)<<uint(attempt)) * time.Second)
				result = send()
			}
			if result != nil {
				firstError = result
			}
		}
	}
	if n.webhookURL != nil {
		target := *n.webhookURL
		body, contentType, headers, err := n.webhookRequest(event, data)
		if err != nil {
			return err
		}
		send := func() error {
			return n.postRaw(target, contentType, headers, body)
		}
		result := send()
		for attempt := 0; attempt < 2; attempt++ {
			if result == nil {
				break
			}
			time.Sleep(time.Duration(uint64(1)<<uint(attempt)) * time.Second)
			result = send()
		}
		if result != nil && firstError == nil {
			firstError = result
		}
	}
	if n.emailEnabled {
		if err := n.sendEmail(event, formatEmailSubject(event, data), formatEvent(event, data)); err != nil {
			if firstError == nil {
				firstError = err
			}
		}
	}
	return firstError
}

func (n *Notifier) httpClient() *http.Client {
	if n.client == nil {
		return defaultHTTPClient
	}
	return n.client
}

// postJSON POSTs a JSON body and treats any 4xx/5xx status as an error, like
// reqwest's `error_for_status()`.
func (n *Notifier) postJSON(rawURL string, headers map[string]string, payload []byte) error {
	_, status, err := HTTPPostJSON(context.Background(), rawURL, headers, payload)
	if err != nil {
		return redactRequestError(err)
	}
	if status >= 400 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

// postRaw POSTs a body with an explicit content type and treats any 4xx/5xx
// status as an error.
func (n *Notifier) postRaw(rawURL, contentType string, headers map[string]string, payload []byte) error {
	response, err := HTTPRequest(context.Background(), http.MethodPost, rawURL, headers, payload, contentType)
	if err != nil {
		return redactRequestError(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return nil
}

// webhookRequest builds the payload, content type and headers for the
// configured webhook format. The default `gextto` format is the signed JSON
// envelope; the other formats adapt the same text to a provider API. The URL
// carries only the endpoint: the provider credential goes in the dedicated
// "Webhook token" field (ntfy Bearer, gotify X-Gotify-Key, pushover token) and
// Pushover's user key in "Webhook user".
func (n *Notifier) webhookRequest(event string, data map[string]any) ([]byte, string, map[string]string, error) {
	headers := map[string]string{}
	switch strings.ToLower(strings.TrimSpace(n.webhookFormat)) {
	case "discord":
		body, err := json.Marshal(map[string]any{"content": formatEvent(event, data)})
		return body, "application/json", headers, err
	case "slack":
		body, err := json.Marshal(map[string]any{"text": formatEvent(event, data)})
		return body, "application/json", headers, err
	case "gotify":
		if n.webhookToken != nil {
			headers["X-Gotify-Key"] = *n.webhookToken
		}
		body, err := json.Marshal(map[string]any{"title": "Gextto", "message": formatEvent(event, data)})
		return body, "application/json", headers, err
	case "ntfy":
		if n.webhookToken != nil {
			headers["Authorization"] = "Bearer " + *n.webhookToken
		}
		return []byte(formatEvent(event, data)), "text/plain; charset=utf-8", headers, nil
	case "pushover":
		form := url.Values{}
		form.Set("title", "Gextto")
		form.Set("message", formatEvent(event, data))
		if n.webhookToken != nil {
			form.Set("token", *n.webhookToken)
		}
		if n.webhookUser != nil {
			form.Set("user", *n.webhookUser)
		}
		return []byte(form.Encode()), "application/x-www-form-urlencoded", headers, nil
	default: // gextto
		body, err := json.Marshal(map[string]any{"event": event, "data": data})
		if err != nil {
			return nil, "", nil, err
		}
		if n.webhookSecret != nil {
			mac := hmac.New(sha256.New, []byte(*n.webhookSecret))
			mac.Write(body)
			headers["x-gextto-signature"] = "sha256=" + hexBytes(mac.Sum(nil))
		}
		return body, "application/json", headers, nil
	}
}

// throttleTelegram enforces at most one Telegram API call per second.
func (n *Notifier) throttleTelegram() {
	if n.lastTelegram == nil {
		return
	}
	n.lastTelegram.mu.Lock()
	defer n.lastTelegram.mu.Unlock()
	now := time.Now()
	if n.lastTelegram.last != nil {
		elapsed := now.Sub(*n.lastTelegram.last)
		if elapsed < time.Second {
			time.Sleep(time.Second - elapsed)
		}
	}
	n.lastTelegram.last = &now
}

// sendEmail delivers the formatted event over SMTP. A missing recipient/from
// configuration is a silent no-op (matching gextto).
func (n *Notifier) sendEmail(event, subject, body string) error {
	if n.emailFrom == nil || n.emailTo == nil || n.emailPassword == nil {
		return nil
	}
	from := sanitizeEmailHeader(*n.emailFrom)
	to := *n.emailTo
	password := *n.emailPassword
	host := n.emailSMTP
	port := 587
	if index := strings.LastIndex(n.emailSMTP, ":"); index >= 0 {
		host = n.emailSMTP[:index]
		if parsed, err := strconv.Atoi(n.emailSMTP[index+1:]); err == nil {
			port = parsed
		} else {
			port = 587
		}
	}
	recipients := []string{}
	for _, recipient := range strings.Split(to, ",") {
		recipient = sanitizeEmailHeader(strings.TrimSpace(recipient))
		if recipient != "" {
			recipients = append(recipients, recipient)
		}
	}
	message := buildEmailMessage(from, recipients, subject, body)
	address := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", from, password, host)
	return sendMailWithTimeout(address, host, auth, from, recipients, message, smtpTimeout)
}

// smtpTimeout bounds a whole SMTP delivery. Notifications are sent from the
// torrent event worker: an unreachable or silent mail server must not freeze
// stall monitoring, seed policy and post-processing.
const smtpTimeout = 30 * time.Second

// sendMailWithTimeout is smtp.SendMail (same STARTTLS and AUTH behaviour) with a
// deadline on the connection and on the whole exchange: the standard function
// dials without a timeout and can block forever.
func sendMailWithTimeout(address, host string, auth smtp.Auth, from string, to []string, message []byte, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return err
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close()
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// sanitizeEmailHeader strips CR/LF and NUL so a configured or event-derived
// value cannot inject extra MIME headers (email header injection).
func sanitizeEmailHeader(value string) string {
	return strings.Map(func(character rune) rune {
		switch character {
		case '\r', '\n', 0:
			return -1
		default:
			return character
		}
	}, value)
}

// buildEmailMessage renders a minimal text/plain MIME message.
// The body is base64-encoded to guarantee that untrusted/event-derived content
// cannot inject headers, MIME boundaries, or raw SMTP commands.
func buildEmailMessage(from string, recipients []string, subject, body string) []byte {
	from = sanitizeEmailHeader(from)
	subject = sanitizeEmailHeader(subject)
	safeRecipients := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		safeRecipients = append(safeRecipients, sanitizeEmailHeader(recipient))
	}
	recipients = safeRecipients
	var message strings.Builder
	message.WriteString("From: " + from + "\r\n")
	message.WriteString("To: " + strings.Join(recipients, ", ") + "\r\n")
	message.WriteString("Subject: " + subject + "\r\n")
	message.WriteString("MIME-Version: 1.0\r\n")
	message.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	message.WriteString("Content-Transfer-Encoding: base64\r\n")
	message.WriteString("\r\n")
	b64Body := base64.StdEncoding.EncodeToString([]byte(body))
	for len(b64Body) > 76 {
		message.WriteString(b64Body[:76] + "\r\n")
		b64Body = b64Body[76:]
	}
	message.WriteString(b64Body + "\r\n")
	return []byte(message.String())
}

// formatEmailSubject builds a clean, readable email subject for an event.
func formatEmailSubject(event string, data map[string]any) string {
	text := func(key string) string {
		return jsonString(mapLookup(data, key))
	}
	title := text("title")
	if title == "" {
		title = text("name")
	}
	series := text("series")
	season, hasSeason := jsonInt(mapLookup(data, "season"))
	episode, hasEpisode := jsonInt(mapLookup(data, "episode"))
	itemLabel := title
	if series != "" && hasSeason && hasEpisode {
		itemLabel = fmt.Sprintf("%s S%02dE%02d", series, season, episode)
	} else if series != "" {
		itemLabel = series
	}

	switch event {
	case "download_started":
		if text("kind") == "movie" {
			return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Film in download", "Movie download started"), itemLabel)
		}
		return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Nuovo episodio in download", "Episode download started"), itemLabel)
	case "torrent_completed":
		if seeding, ok := mapLookup(data, "seeding").(bool); ok && seeding {
			return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Download completato (in seed)", "Download complete (seeding)"), itemLabel)
		}
		return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Download completato", "Download complete"), itemLabel)
	case "season_pack_completed":
		if series != "" && hasSeason {
			return fmt.Sprintf("Gextto: %s — %s S%02d", messages.Pick("Season Pack completato", "Season pack complete"), series, season)
		}
		return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Season Pack completato", "Season pack complete"), itemLabel)
	case "backup_completed":
		if sched, ok := mapLookup(data, "scheduled").(bool); ok && sched {
			return fmt.Sprintf("Gextto: %s", messages.Pick("Backup programmato completato", "Scheduled backup completed"))
		}
		return fmt.Sprintf("Gextto: %s", messages.Pick("Backup completato", "Backup completed"))
	case "torrent_error":
		if itemLabel != "" {
			return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Errore torrent", "Torrent error"), itemLabel)
		}
		return fmt.Sprintf("Gextto: %s", messages.Pick("Errore torrent", "Torrent error"))
	case "download_failed":
		if itemLabel != "" {
			return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Download fallito", "Download failed"), itemLabel)
		}
		return fmt.Sprintf("Gextto: %s", messages.Pick("Download fallito", "Download failed"))
	case "comic_queued":
		return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Fumetto in download", "Comic download started"), itemLabel)
	case "comic_completed":
		return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Fumetto scaricato", "Comic downloaded"), itemLabel)
	case "comic_error":
		return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Errore fumetto", "Comic error"), itemLabel)
	case "comic_pending":
		return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Fumetto in attesa", "Comic pending"), itemLabel)
	case "gap_filled":
		return fmt.Sprintf("Gextto: %s — %s", messages.Pick("Gap riempito", "Gap filled"), itemLabel)
	default:
		return fmt.Sprintf("Gextto [%s]", event)
	}
}

// formatEvent renders a human notification text for an event, picking the
// Italian or English wording like the UI.
func formatEvent(event string, data map[string]any) string {
	text := func(key string) string {
		return jsonString(mapLookup(data, key))
	}
	valueText := func(value any, key, fallback string) string {
		if raw := mapLookup(value, key); raw != nil {
			if textValue, ok := deref(raw).(string); ok {
				return textValue
			}
		}
		return fallback
	}
	seriesEpisode := func() string {
		series := text("series")
		season, hasSeason := jsonInt(mapLookup(data, "season"))
		episode, hasEpisode := jsonInt(mapLookup(data, "episode"))
		if series != "" && hasSeason && hasEpisode {
			return fmt.Sprintf("%s S%02dE%02d", series, season, episode)
		}
		if series != "" {
			return series
		}
		title := text("title")
		if title == "" {
			return text("name")
		}
		return title
	}
	episodeCode := func() string {
		season, hasSeason := jsonInt(mapLookup(data, "season"))
		episode, hasEpisode := jsonInt(mapLookup(data, "episode"))
		if hasSeason && hasEpisode {
			return fmt.Sprintf("S%02dE%02d", season, episode)
		}
		return "-"
	}
	reasonCode := text("reason")
	var reason string
	switch reasonCode {
	case "upgrade":
		reason = messages.Pick("⬆️ Qualità superiore trovata", "⬆️ Better quality found")
	case "gap_filled":
		reason = messages.Pick("🔎 Episodio mancante trovato", "🔎 Missing episode found")
	case "restored":
		reason = messages.Pick("♻️ Download ripristinato", "♻️ Download restored")
	case "approved":
		reason = messages.Pick("🆕 Nuovo episodio approvato", "🆕 New episode approved")
	case "":
		reason = messages.Pick("✅ Release approvata", "✅ Release approved")
	default:
		reason = reasonCode
	}
	switch event {
	case "message":
		return text("text")
	case "download_started":
		qualityScore, _ := jsonInt(mapLookup(data, "quality_score"))
		if text("kind") == "movie" {
			return fmt.Sprintf(
				"%s\n\n🏷️ Release: %s\n🏆 %s %d\n⚙️ %s: %s",
				messages.Pick("🎬 FILM IN DOWNLOAD!", "🎬 MOVIE DOWNLOAD STARTED!"),
				text("title"),
				messages.Pick("Score Qualità:", "Quality score:"),
				qualityScore,
				messages.Pick("Motivo", "Reason"),
				reason,
			)
		}
		return fmt.Sprintf(
			"%s\n\n🎬 %s: %s\n▶️ %s: %s\n🏷️ Release: %s\n🏆 %s %d\n⚙️ %s: %s",
			messages.Pick("📺 NUOVO EPISODIO IN DOWNLOAD!", "📺 NEW EPISODE DOWNLOAD STARTED!"),
			messages.Pick("Serie", "Series"),
			text("series"),
			messages.Pick("Episodio", "Episode"),
			episodeCode(),
			text("title"),
			messages.Pick("Score Qualità:", "Quality score:"),
			qualityScore,
			messages.Pick("Motivo", "Reason"),
			reason,
		)
	case "torrent_completed":
		duration, hasDuration := jsonInt(mapLookup(data, "duration_seconds"))
		if !hasDuration || duration <= 0 {
			hasDuration = false
		}
		speed, hasSpeed := jsonInt(mapLookup(data, "average_speed_bps"))
		if !hasSpeed || speed <= 0 {
			hasSpeed = false
		}
		var stats string
		switch {
		case hasDuration && hasSpeed:
			stats = fmt.Sprintf("  ⏱️ %s  🚀 %s/s", formatDuration(duration), formatBytes(speed))
		case hasDuration:
			stats = fmt.Sprintf("  ⏱️ %s", formatDuration(duration))
		case hasSpeed:
			stats = fmt.Sprintf("  🚀 %s/s", formatBytes(speed))
		default:
			stats = ""
		}
		size, _ := jsonInt(mapLookup(data, "size_bytes"))
		if manuallyAdded, _ := mapLookup(data, "manual").(bool); manuallyAdded {
			return fmt.Sprintf(
				"%s\n\n📁 %s: %s\n💾 %s: %s\n📂 %s: %s",
				messages.Pick("✅ DOWNLOAD COMPLETATO (AGGIUNTO MANUALMENTE)", "✅ DOWNLOAD COMPLETE (ADDED MANUALLY)"),
				messages.Pick("File", "File"),
				text("title"),
				messages.Pick("Dimensione", "Size"),
				formatBytes(size),
				messages.Pick("Percorso", "Path"),
				text("path"),
			)
		}
		kind := text("kind")
		mediaIcon := "📺"
		if kind == "movie" {
			mediaIcon = "🎬"
		}
		if seeding, ok := mapLookup(data, "seeding").(bool); ok && seeding {
			return fmt.Sprintf(
				"%s\n\n%s %s\n\n💾 %s%s\n%s",
				messages.Pick("📥 DOWNLOAD COMPLETATO — IN SEED", "📥 DOWNLOAD COMPLETE — SEEDING"),
				mediaIcon,
				seriesEpisode(),
				formatBytes(size),
				stats,
				messages.Pick("⏳ Sarà archiviato a fine seed", "⏳ Will be archived when seeding ends"),
			)
		}
		replacedTitle := text("replaced_title")
		var upgradeNote string
		if replacedTitle != "" {
			upgradeNote = fmt.Sprintf("\n\n♻️ %s: «%s»", messages.Pick("Sostituisce versione inferiore", "Replaces inferior version"), replacedTitle)
		}
		return fmt.Sprintf(
			"%s\n\n%s %s\n\n💾 %s%s\n%s: %s%s",
			messages.Pick("✅ DOWNLOAD COMPLETATO", "✅ DOWNLOAD COMPLETE"),
			mediaIcon,
			seriesEpisode(),
			formatBytes(size),
			stats,
			messages.Pick("📂 Archiviato in", "📂 Archived to"),
			text("path"),
			upgradeNote,
		)
	case "season_pack_completed":
		season, _ := jsonInt(mapLookup(data, "season"))
		size, _ := jsonInt(mapLookup(data, "size_bytes"))
		newCount, _ := jsonInt(mapLookup(data, "new_count"))
		var discarded string
		if count, ok := jsonInt(mapLookup(data, "discarded_count")); ok && count > 0 {
			discarded = fmt.Sprintf(" 🗑️ %d %s", count, messages.Pick("inferiori scartati", "inferior discarded"))
		}
		return fmt.Sprintf(
			"%s\n\n🎬 %s S%02d\n\n💾 %s\n\n✅ %d %s %s\n%s\n\n📂 %s",
			messages.Pick(
				"✅ DOWNLOAD COMPLETATO — SEASON PACK ARCHIVIATO",
				"✅ DOWNLOAD COMPLETE — SEASON PACK ARCHIVED",
			),
			valueText(data, "series", "Release"),
			season,
			formatBytes(size),
			newCount,
			messages.Pick("nuovi", "new"),
			discarded,
			formatSeasonEpisodes(mapLookup(data, "episodes")),
			valueText(data, "path", ""),
		)
	case "torrent_error":
		name := text("name")
		if name == "" {
			name = text("title")
		}
		var restoredText string
		if restored, ok := mapLookup(data, "upgrade_restored").(bool); ok && restored {
			restoredText = messages.Pick(" (versione precedente ripristinata)", " (previous version restored)")
		}
		if name != "" {
			return fmt.Sprintf(
				"Gextto: %s «%s» — %s%s",
				messages.Pick("errore torrent", "torrent error"),
				name,
				text("error"),
				restoredText,
			)
		}
		return fmt.Sprintf(
			"Gextto: %s — %s%s",
			messages.Pick("errore torrent", "torrent error"),
			text("error"),
			restoredText,
		)
	case "gap_filled":
		return fmt.Sprintf(
			"Gextto: %s — %s",
			messages.Pick("gap riempito", "gap filled"),
			seriesEpisode(),
		)
	case "series_complete":
		return fmt.Sprintf(
			"Gextto: %s — %s",
			messages.Pick("serie completata", "series complete"),
			text("series"),
		)
	case "movie":
		return fmt.Sprintf(
			"Gextto: %s «%s» %s",
			messages.Pick("film", "movie"),
			text("title"),
			messages.Pick("disponibile", "available"),
		)
	case "download_failed":
		title := text("title")
		if title == "" {
			title = text("name")
		}
		errText := text("error")
		var restoredText string
		if restored, ok := mapLookup(data, "upgrade_restored").(bool); ok && restored {
			restoredText = messages.Pick(" (versione precedente ripristinata)", " (previous version restored)")
		}
		if errText != "" && title != "" {
			return fmt.Sprintf(
				"Gextto: %s «%s» — %s%s",
				messages.Pick("download fallito", "download failed"),
				title,
				errText,
				restoredText,
			)
		}
		if title != "" {
			return fmt.Sprintf(
				"Gextto: %s «%s»%s",
				messages.Pick("download fallito", "download failed"),
				title,
				restoredText,
			)
		}
		return fmt.Sprintf(
			"Gextto: %s — %s%s",
			messages.Pick("download fallito", "download failed"),
			errText,
			restoredText,
		)
	case "comic_queued":
		return fmt.Sprintf(
			"%s\n\n🏷️ %s: %s\n⚙️ %s: %s\n✨ %s",
			messages.Pick("📚 NUOVO FUMETTO IN DOWNLOAD!", "📚 NEW COMIC DOWNLOAD STARTED!"),
			messages.Pick("Titolo", "Title"),
			text("title"),
			messages.Pick("Metodo", "Method"),
			text("method"),
			messages.Pick("Download avviato con successo.", "Download started successfully."),
		)
	case "comic_completed":
		size, _ := jsonInt(mapLookup(data, "size_bytes"))
		return fmt.Sprintf(
			"%s\n\n🏷️ %s: %s\n⚙️ %s: %s\n💾 %s: %s\n📂 %s: %s",
			messages.Pick("✅ FUMETTO SCARICATO!", "✅ COMIC DOWNLOADED!"),
			messages.Pick("Titolo", "Title"),
			text("title"),
			messages.Pick("Metodo", "Method"),
			text("method"),
			messages.Pick("Dimensione", "Size"),
			formatBytes(size),
			messages.Pick("File salvato nella cartella fumetti", "File saved in the comics folder"),
			text("path"),
		)
	case "comic_error":
		return fmt.Sprintf(
			"Gextto: %s «%s» — %s",
			messages.Pick("errore fumetto", "comic error"),
			text("title"),
			text("error"),
		)
	case "comic_pending":
		return fmt.Sprintf(
			"%s\n\n🏷️ %s: %s\n🔗 %s",
			messages.Pick(
				"⏳ FUMETTO PRESENTE MA NON ANCORA SCARICABILE",
				"⏳ COMIC FOUND BUT NOT YET DOWNLOADABLE",
			),
			messages.Pick("Titolo", "Title"),
			text("title"),
			messages.Pick(
				"GetComics non ha ancora pubblicato un pulsante Download Now o un link torrent. Gextto riproverà al prossimo ciclo.",
				"GetComics has not published a Download Now button or torrent link yet. Gextto will retry on the next cycle.",
			),
		)
	case "backup_completed":
		path := text("path")
		if path == "" {
			return messages.Pick("Gextto: backup completato", "Gextto: backup completed")
		}
		size, hasSize := jsonInt(mapLookup(data, "size_bytes"))
		if !hasSize || size <= 0 {
			if info, err := os.Stat(path); err == nil {
				size = info.Size()
				hasSize = true
			}
		}
		var lines []string
		header := messages.Pick("💾 BACKUP COMPLETATO", "💾 BACKUP COMPLETED")
		if sched, ok := mapLookup(data, "scheduled").(bool); ok && sched {
			header = messages.Pick("💾 BACKUP PROGRAMMATO COMPLETATO", "💾 SCHEDULED BACKUP COMPLETED")
		}
		lines = append(lines, header, "")
		lines = append(lines, fmt.Sprintf("📦 %s: %s", messages.Pick("File", "File"), path))
		if hasSize && size > 0 {
			lines = append(lines, fmt.Sprintf("📊 %s: %s", messages.Pick("Dimensione", "Size"), formatBytes(size)))
		}

		cloudCopied, _ := mapLookup(data, "cloud_copied").(bool)
		cloudDest := text("cloud_destination")
		cloudErr := text("cloud_error")
		if cloudCopied && cloudDest != "" {
			lines = append(lines, fmt.Sprintf("☁️ %s: %s", messages.Pick("Copia cloud", "Cloud copy"), cloudDest))
		} else if cloudErr != "" {
			lines = append(lines, fmt.Sprintf("⚠️ %s: %s", messages.Pick("Copia cloud non riuscita", "Cloud copy failed"), cloudErr))
		}

		ftpUploaded, _ := mapLookup(data, "ftp_uploaded").(bool)
		ftpHost := text("ftp_host")
		ftpRemote := text("ftp_remote")
		ftpErr := text("ftp_error")
		if ftpUploaded && ftpHost != "" {
			dest := ftpHost
			baseName := filepath.Base(path)
			if ftpRemote != "" {
				cleanRemote := "/" + strings.Trim(ftpRemote, "/")
				dest = fmt.Sprintf("%s (%s/%s)", ftpHost, cleanRemote, baseName)
			} else {
				dest = fmt.Sprintf("%s (%s)", ftpHost, baseName)
			}
			lines = append(lines, fmt.Sprintf("🌐 %s: %s", messages.Pick("Caricato via FTP", "Uploaded via FTP"), dest))
		} else if ftpErr != "" {
			dest := ftpHost
			if dest == "" {
				dest = "FTP"
			}
			lines = append(lines, fmt.Sprintf("⚠️ %s (%s): %s", messages.Pick("Caricamento FTP non riuscito", "FTP upload failed"), dest, ftpErr))
		}

		tgParts, _ := jsonInt(mapLookup(data, "telegram_parts"))
		if tgUploaded, _ := mapLookup(data, "telegram_uploaded").(bool); tgUploaded {
			if tgParts > 1 {
				baseName := filepath.Base(path)
				lines = append(lines, fmt.Sprintf("📱 %s %d %s: cat %s.0* > %s",
					messages.Pick("Inviato su Telegram in", "Sent to Telegram in"), tgParts,
					messages.Pick("parti; per ricomporlo", "parts; to rebuild it"), baseName, baseName))
			} else {
				lines = append(lines, fmt.Sprintf("📱 %s", messages.Pick("Inviato anche come allegato Telegram", "Also sent as a Telegram document")))
			}
		} else if tgErr := text("telegram_error"); tgErr != "" {
			sent := ""
			if tgParts > 0 {
				sent = fmt.Sprintf(" (%s %d)", messages.Pick("parti inviate:", "parts sent:"), tgParts)
			}
			lines = append(lines, fmt.Sprintf("⚠️ %s%s: %s", messages.Pick("Invio Telegram non riuscito", "Telegram upload failed"), sent, tgErr))
		}

		return strings.Join(lines, "\n")
	default:
		if value, ok := mapLookup(data, "text").(string); ok {
			return fmt.Sprintf("Gextto [%s] %s", event, value)
		}
		return fmt.Sprintf("Gextto [%s] %s", event, jsonDisplay(data))
	}
}

// formatSeasonEpisodes renders the `episodes` array of a season pack.
func formatSeasonEpisodes(value any) string {
	value = deref(value)
	if value == nil {
		return ""
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return ""
	}
	parts := make([]string, 0, rv.Len())
	for index := 0; index < rv.Len(); index++ {
		item := rv.Index(index).Interface()
		series := jsonString(mapLookup(item, "series"))
		season, _ := jsonInt(mapLookup(item, "season"))
		episode, _ := jsonInt(mapLookup(item, "episode"))
		path := jsonString(mapLookup(item, "path"))
		parts = append(parts, fmt.Sprintf("✅ %s - S%02dE%02d - %s", series, season, episode, path))
	}
	return strings.Join(parts, "\n")
}

// hexBytes renders bytes as lowercase hexadecimal.
func hexBytes(bytes []byte) string {
	return hex.EncodeToString(bytes)
}

// formatBytes renders a byte amount with binary units, e.g. `1.50 GB`.
func formatBytes(value int64) string {
	amount := float64(value)
	if amount < 0 {
		amount = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	index := 0
	for amount >= 1024.0 && index < len(units)-1 {
		amount /= 1024.0
		index++
	}
	if index == 0 {
		return fmt.Sprintf("%d %s", int64(amount), units[index])
	}
	return fmt.Sprintf("%.2f %s", amount, units[index])
}

// formatDuration renders a human duration, e.g. "6h 36m 55s".
func formatDuration(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, secs)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// redactRequestError hides the URL of a failed notification request. net/http
// errors embed the full URL, and notification URLs carry secrets in the path
// (the Telegram bot token, Discord/Slack webhook tokens) that the query-string
// redaction does not cover. Only the scheme and host are kept.
func redactRequestError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	target := "request"
	if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil && parsed.Host != "" {
		target = parsed.Scheme + "://" + parsed.Host
	}
	return fmt.Errorf("%s %s: %w", urlErr.Op, target, urlErr.Err)
}

// Async returns a copy of the notifier whose NotifyEvent only enqueues the
// event and returns at once. The background workers (torrent events, cycle) use
// it: a slow or unreachable Telegram, webhook or SMTP server must not stall
// stall monitoring, seed policy or post-processing. Delivery errors are logged
// by the delivery goroutine. Interactive callers (the "test notification"
// button) keep the synchronous notifier so they can report the error.
func (n *Notifier) Async() *Notifier {
	if n == nil {
		return nil
	}
	copied := *n
	copied.async = true
	return &copied
}

type queuedNotification struct {
	notifier *Notifier
	event    string
	data     map[string]any
}

// notificationQueueSize bounds the pending notifications. When it is full new
// events are dropped with a warning rather than blocking a worker.
const notificationQueueSize = 256

var (
	notificationQueue     = make(chan queuedNotification, notificationQueueSize)
	notificationQueueOnce sync.Once
)

func (n *Notifier) enqueue(event string, data map[string]any) {
	notificationQueueOnce.Do(func() {
		// A single delivery goroutine keeps events in order and honours the
		// Telegram throttle; safeGoLoop restarts it after a panic.
		go safeGoLoop("notification delivery", nil, deliverQueuedNotifications)
	})
	synchronous := *n
	synchronous.async = false
	// The payload is copied so a caller mutating its map later cannot race
	// with the delivery.
	owned, _ := cloneJSON(data).(map[string]any)
	if owned == nil {
		owned = data
	}
	select {
	case notificationQueue <- queuedNotification{notifier: &synchronous, event: event, data: owned}:
	default:
		logging.Warn("notification queue full; event dropped", "event", event)
	}
}

func deliverQueuedNotifications() {
	for item := range notificationQueue {
		if err := item.notifier.deliverEvent(item.event, item.data); err != nil {
			logging.Warn("notification delivery failed", "event", item.event, "error", err)
		} else {
			logging.Debug("notification delivered", "event", item.event)
		}
	}
}
