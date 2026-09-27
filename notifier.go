package gextto

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

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
	emailEnabled     bool
	emailSMTP        string
	emailFrom        *string
	emailTo          *string
	emailPassword    *string
	lastTelegram     *telegramThrottle
	// External event hooks (see hooks.go). Reloadable at runtime when the user
	// edits them in the UI.
	hooks *eventHookStore
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

// NotifyBackupDocument uploads a backup archive to Telegram. It returns false
// (without an error) when Telegram is unavailable or the file is too large.
func (n *Notifier) NotifyBackupDocument(path, caption string) (bool, error) {
	if n.telegramBotToken == nil || n.telegramChatID == nil {
		return false, nil
	}
	// `notify_telegram` gates the *notification* messages; uploading a backup
	// is an explicit action driven by `backup_send_telegram` (or a manual
	// click), so it only needs the Telegram credentials, not that switch.
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.Size() > 49*1024*1024 {
		return false, nil
	}
	n.throttleTelegram()
	filename := filepath.Base(path)
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "gextto-backup.zip"
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("chat_id", *n.telegramChatID)
	_ = writer.WriteField("caption", caption)
	part, err := writer.CreateFormFile("document", filename)
	if err != nil {
		return false, err
	}
	if _, err := part.Write(content); err != nil {
		return false, err
	}
	if err := writer.Close(); err != nil {
		return false, err
	}
	rawURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", *n.telegramBotToken)
	request, err := http.NewRequest(http.MethodPost, rawURL, &body)
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := n.httpClient().Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode >= 400 {
		return false, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return true, nil
}

// NotifyEvent formats and delivers one event to every enabled channel and
// dispatches the external hooks.
func (n *Notifier) NotifyEvent(event string, data map[string]any) error {
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
				result = send()
				if result != nil {
					time.Sleep(time.Duration(uint64(1)<<uint(attempt)) * time.Second)
				}
			}
			if result != nil {
				firstError = result
			}
		}
	}
	if n.webhookURL != nil {
		url := *n.webhookURL
		body, err := json.Marshal(map[string]any{"event": event, "data": data})
		if err != nil {
			return err
		}
		headers := map[string]string{}
		if n.webhookSecret != nil {
			mac := hmac.New(sha256.New, []byte(*n.webhookSecret))
			mac.Write(body)
			headers["x-gextto-signature"] = "sha256=" + hexBytes(mac.Sum(nil))
		}
		send := func() error {
			return n.postJSON(url, headers, body)
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
		if err := n.sendEmail(event, formatEvent(event, data)); err != nil {
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
		return err
	}
	if status >= 400 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
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
func (n *Notifier) sendEmail(event, body string) error {
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
	subject := sanitizeEmailHeader(fmt.Sprintf("Gextto [%s]", event))
	message := buildEmailMessage(from, recipients, subject, body)
	address := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", from, password, host)
	return smtp.SendMail(address, auth, from, recipients, message)
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
	message.WriteString("\r\n")
	message.WriteString(body)
	return []byte(message.String())
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
		return fmt.Sprintf(
			"%s\n\n📺 %s\n\n💾 %s%s\n%s: %s",
			messages.Pick("✅ DOWNLOAD COMPLETATO", "✅ DOWNLOAD COMPLETE"),
			seriesEpisode(),
			formatBytes(size),
			stats,
			messages.Pick("📂 Archiviato in", "📂 Archived to"),
			text("path"),
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
		return fmt.Sprintf(
			"Gextto: %s — %s",
			messages.Pick("errore torrent", "torrent error"),
			text("error"),
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
		return fmt.Sprintf(
			"Gextto: %s — %s",
			messages.Pick("download fallito", "download failed"),
			text("title"),
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
		return fmt.Sprintf(
			"Gextto: %s — %s",
			messages.Pick("backup completato", "backup completed"),
			text("path"),
		)
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
