// Package notify delivers the gx-torrent daemon's notifications on the same
// channels Gextto offers — Telegram, a webhook (generic JSON or the Discord,
// Slack, Gotify, ntfy and Pushover formats) and e-mail over SMTP — and on the
// two further channels of indexarr's notification page: an Apprise API server
// and a custom script run on this machine. Every configured channel receives
// every event; a channel left empty is off.
package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Config is the set of channels. Empty fields switch their channel off.
type Config struct {
	// App names the sender in titles and in the JSON payload.
	App string

	TelegramToken string
	TelegramChat  string

	WebhookURL string
	// WebhookFormat is "json" (default: {app, event, time, …fields}),
	// "discord", "slack", "gotify", "ntfy" or "pushover".
	WebhookFormat string
	// WebhookToken is the provider credential (ntfy Bearer, Gotify key,
	// Pushover application token); WebhookUser is Pushover's user key.
	WebhookToken string
	WebhookUser  string

	// AppriseURL posts {title, body, type} to an Apprise API server. The
	// key is optional and selects one Apprise configuration.
	AppriseURL string
	AppriseKey string

	// DiscordURL and SlackURL are the incoming webhook URLs of the two
	// services. They live beside the generic webhook, so a page can offer
	// every channel at once.
	DiscordURL string
	SlackURL   string

	// GotifyURL is the server address and GotifyToken the application
	// token; notifications are posted to <server>/message.
	GotifyURL   string
	GotifyToken string

	// NtfyURL is the server address, NtfyTopic the topic and NtfyToken an
	// optional access token; notifications are posted to <server>/<topic>.
	NtfyURL   string
	NtfyTopic string
	NtfyToken string

	// PushoverToken is the application token and PushoverUser the user or
	// group key.
	PushoverToken string
	PushoverUser  string

	// SMTP is "host:port" (587 when the port is missing, STARTTLS when the
	// server offers it).
	SMTP     string
	From     string
	To       string // comma separated
	Password string

	// Script is executed with ScriptArgs for every event; the event travels
	// in the GX_TORRENT_* environment variables.
	Script     string
	ScriptArgs string
}

// Formats lists the webhook formats, in the order a page offers them.
var Formats = []string{"json", "discord", "slack", "gotify", "ntfy", "pushover"}

// PushoverAPIBase is a variable so tests can point it at a fake server.
var PushoverAPIBase = "https://api.pushover.net/1/messages.json"

// Enabled reports whether at least one channel is configured.
func (c Config) Enabled() bool {
	return c.telegramOn() || c.webhookOn() || strings.TrimSpace(c.DiscordURL) != "" ||
		strings.TrimSpace(c.SlackURL) != "" || c.gotifyOn() || c.ntfyOn() ||
		c.pushoverOn() || c.appriseOn() || c.emailOn() || c.scriptOn()
}

func (c Config) telegramOn() bool {
	return strings.TrimSpace(c.TelegramToken) != "" && strings.TrimSpace(c.TelegramChat) != ""
}

func (c Config) webhookOn() bool {
	return strings.TrimSpace(c.WebhookURL) != ""
}

func (c Config) gotifyOn() bool {
	return strings.TrimSpace(c.GotifyURL) != "" && strings.TrimSpace(c.GotifyToken) != ""
}

func (c Config) ntfyOn() bool {
	return strings.TrimSpace(c.NtfyURL) != "" && strings.TrimSpace(c.NtfyTopic) != ""
}

func (c Config) pushoverOn() bool {
	return strings.TrimSpace(c.PushoverToken) != "" && strings.TrimSpace(c.PushoverUser) != ""
}

func (c Config) appriseOn() bool {
	return strings.TrimSpace(c.AppriseURL) != ""
}

func (c Config) emailOn() bool {
	return strings.TrimSpace(c.SMTP) != "" && strings.TrimSpace(c.From) != "" && strings.TrimSpace(c.To) != ""
}

func (c Config) scriptOn() bool {
	return strings.TrimSpace(c.Script) != ""
}

// Event is one notification: a machine name, a short title, a readable text
// and the raw fields (sent as they are in the JSON webhook).
type Event struct {
	Name   string
	Title  string
	Text   string
	Fields map[string]any
}

const timeout = 20 * time.Second

// channel is one configured delivery and the function that performs it.
type channel struct {
	name string
	run  func(context.Context) error
}

// channelsFor builds the list of configured channels.
func channelsFor(cfg Config, event Event) []channel {
	var out []channel
	if cfg.telegramOn() {
		out = append(out, channel{"telegram", func(ctx context.Context) error {
			return sendTelegram(ctx, cfg, event)
		}})
	}
	if cfg.webhookOn() {
		out = append(out, channel{"webhook", func(ctx context.Context) error {
			return sendWebhook(ctx, cfg, event, cfg.WebhookURL, cfg.WebhookFormat, cfg.WebhookToken, cfg.WebhookUser)
		}})
	}
	if strings.TrimSpace(cfg.DiscordURL) != "" {
		out = append(out, channel{"discord", func(ctx context.Context) error {
			return sendWebhook(ctx, cfg, event, cfg.DiscordURL, "discord", "", "")
		}})
	}
	if strings.TrimSpace(cfg.SlackURL) != "" {
		out = append(out, channel{"slack", func(ctx context.Context) error {
			return sendWebhook(ctx, cfg, event, cfg.SlackURL, "slack", "", "")
		}})
	}
	if cfg.gotifyOn() {
		out = append(out, channel{"gotify", func(ctx context.Context) error {
			endpoint := strings.TrimRight(strings.TrimSpace(cfg.GotifyURL), "/") + "/message"
			return sendWebhook(ctx, cfg, event, endpoint, "gotify", cfg.GotifyToken, "")
		}})
	}
	if cfg.ntfyOn() {
		out = append(out, channel{"ntfy", func(ctx context.Context) error {
			endpoint := strings.TrimRight(strings.TrimSpace(cfg.NtfyURL), "/") + "/" +
				url.PathEscape(strings.Trim(cfg.NtfyTopic, "/"))
			return sendWebhook(ctx, cfg, event, endpoint, "ntfy", cfg.NtfyToken, "")
		}})
	}
	if cfg.pushoverOn() {
		out = append(out, channel{"pushover", func(ctx context.Context) error {
			return sendWebhook(ctx, cfg, event, PushoverAPIBase, "pushover", cfg.PushoverToken, cfg.PushoverUser)
		}})
	}
	if cfg.appriseOn() {
		out = append(out, channel{"apprise", func(ctx context.Context) error {
			return sendApprise(ctx, cfg, event)
		}})
	}
	if cfg.emailOn() {
		out = append(out, channel{"e-mail", func(ctx context.Context) error {
			return sendEmail(cfg, event)
		}})
	}
	if cfg.scriptOn() {
		out = append(out, channel{"script", func(ctx context.Context) error {
			return sendScript(ctx, cfg, event)
		}})
	}
	return out
}

// Send delivers the event to every configured channel, concurrently, and
// returns the errors joined (nil when every channel succeeded or none is
// configured). Each channel has its own timeout, so one slow service never
// holds back the others.
func Send(ctx context.Context, cfg Config, event Event) error {
	app := strings.TrimSpace(cfg.App)
	if app == "" {
		app = "gx-torrent"
	}
	cfg.App = app
	channels := channelsFor(cfg, event)
	results := make([]error, len(channels))
	var wait sync.WaitGroup
	for index := range channels {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			results[index] = channels[index].run(ctx)
		}(index)
	}
	wait.Wait()
	var errs []error
	for index, err := range results {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", channels[index].name, err))
		}
	}
	return errors.Join(errs...)
}

func post(ctx context.Context, rawURL, contentType string, headers map[string]string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The URL may carry a credential (Telegram bot token): keep it out of
		// the error text.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return urlErr.Err
		}
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func sendTelegram(ctx context.Context, cfg Config, event Event) error {
	body, err := json.Marshal(map[string]string{"chat_id": strings.TrimSpace(cfg.TelegramChat), "text": event.Text})
	if err != nil {
		return err
	}
	return post(ctx, "https://api.telegram.org/bot"+strings.TrimSpace(cfg.TelegramToken)+"/sendMessage", "application/json", nil, body)
}

// sendWebhook posts the event to a webhook URL in the payload of one service.
func sendWebhook(ctx context.Context, cfg Config, event Event, rawURL, format, token, user string) error {
	body, contentType, headers, err := webhookBody(format, cfg.App, token, user, event)
	if err != nil {
		return err
	}
	return post(ctx, strings.TrimSpace(rawURL), contentType, headers, body)
}

// webhookBody renders the payload of a webhook-style channel: the generic
// JSON one, or the body the chosen service expects.
func webhookBody(format, app, token, user string, event Event) (body []byte, contentType string, headers map[string]string, err error) {
	contentType = "application/json"
	headers = map[string]string{}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "discord":
		body, err = json.Marshal(map[string]any{"content": truncate(event.Text, 1900)})
	case "slack":
		body, err = json.Marshal(map[string]any{"text": truncate(event.Text, 3900)})
	case "gotify":
		if token != "" {
			headers["X-Gotify-Key"] = token
		}
		body, err = json.Marshal(map[string]any{"title": app, "message": event.Text})
	case "ntfy":
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
		headers["Title"] = app
		body, contentType = []byte(event.Text), "text/plain; charset=utf-8"
	case "pushover":
		form := url.Values{"title": {app}, "message": {truncate(event.Text, 1024)}, "token": {token}, "user": {strings.TrimSpace(user)}}
		body, contentType = []byte(form.Encode()), "application/x-www-form-urlencoded"
	default: // json
		payload := map[string]any{"app": app, "event": event.Name, "text": event.Text, "time": time.Now().UTC().Format(time.RFC3339)}
		for key, value := range event.Fields {
			payload[key] = value
		}
		body, err = json.Marshal(payload)
	}
	return body, contentType, headers, err
}

// sendApprise posts {title, body, type} to an Apprise API server, which
// forwards the message to the services configured there.
func sendApprise(ctx context.Context, cfg Config, event Event) error {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.AppriseURL), "/")
	if !strings.HasSuffix(strings.ToLower(endpoint), "/notify") {
		endpoint += "/notify"
	}
	if key := strings.TrimSpace(cfg.AppriseKey); key != "" {
		endpoint += "/" + url.PathEscape(key)
	}
	payload := map[string]string{
		"title": eventTitle(cfg.App, event),
		"body":  event.Text,
		"type":  appriseType(event),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return post(ctx, endpoint, "application/json", nil, body)
}

// eventTitle is the short subject Apprise, the e-mail and the script see. The
// message itself stays the same for every channel.
func eventTitle(app string, event Event) string {
	if title := strings.TrimSpace(event.Title); title != "" {
		return title
	}
	if first, _, _ := strings.Cut(event.Text, "\n"); strings.TrimSpace(first) != "" {
		return app + ": " + strings.TrimSpace(first)
	}
	return app
}

// appriseType is the notification type Apprise understands.
func appriseType(event Event) string {
	switch event.Name {
	case "feed_error", "download_failed", "torrent_error", "comic_error",
		"health_degraded", "cycle_warning", "indexer_failing",
		"provider_login_failed", "daemon_error", "engine_crashed",
		"engine_error", "engine_rollback", "engine_unresponsive", "engine_unstable":
		return "failure"
	case "download_complete", "feed_match", "test", "torrent_completed",
		"season_pack_completed", "comic_completed", "health_recovered",
		"cycle_recovered", "provider_login_recovered", "backup_completed":
		return "success"
	}
	return "info"
}

// truncate keeps a message within the length a service accepts.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

// headerSafe strips CR/LF and NUL so a value cannot inject mail headers.
func headerSafe(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return -1
		}
		return r
	}, strings.TrimSpace(value))
}

func sendEmail(cfg Config, event Event) error {
	host, port := strings.TrimSpace(cfg.SMTP), 587
	if h, p, err := net.SplitHostPort(host); err == nil {
		host = h
		if n, convErr := strconv.Atoi(p); convErr == nil {
			port = n
		}
	}
	from := headerSafe(cfg.From)
	var to []string
	for _, recipient := range strings.Split(cfg.To, ",") {
		if recipient = headerSafe(recipient); recipient != "" {
			to = append(to, recipient)
		}
	}
	subject := cfg.App + ": " + strings.SplitN(event.Text, "\n", 2)[0]
	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	msg.WriteString("Subject: =?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte(headerSafe(subject))) + "?=\r\n")
	msg.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	encoded := base64.StdEncoding.EncodeToString([]byte(event.Text))
	for len(encoded) > 76 {
		msg.WriteString(encoded[:76] + "\r\n")
		encoded = encoded[76:]
	}
	msg.WriteString(encoded + "\r\n")

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = client.Close() }()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if cfg.Password != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", from, cfg.Password, host)); err != nil {
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
	if _, err := writer.Write([]byte(msg.String())); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// sendScript runs the configured executable with a bounded context and passes
// the event through the GX_TORRENT_* environment variables, never on the
// command line.
func sendScript(ctx context.Context, cfg Config, event Event) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, strings.TrimSpace(cfg.Script), splitArgs(cfg.ScriptArgs)...)
	command.Env = append(os.Environ(), scriptEnvironment(cfg, event)...)
	var stderr limitedBuffer
	command.Stderr = &stderr
	// A killed shell can leave a child holding the stderr pipe open. Bound that
	// wait too, so a misbehaving script cannot outlive the notification.
	command.WaitDelay = 250 * time.Millisecond
	err := command.Run()
	if err == nil {
		return nil
	}
	if detail := strings.TrimSpace(stderr.String()); detail != "" {
		err = fmt.Errorf("%w: %s", err, detail)
	}
	return err
}

// scriptEnvironment is the event handed to the custom script.
func scriptEnvironment(cfg Config, event Event) []string {
	// Both prefixes carry the same values: GX_TORRENT_* is the one gx-torrent
	// and its documentation use, GEXTTO_* the one Gextto uses, so a script can
	// be written once for either program.
	values := map[string]string{
		"GX_TORRENT_APP":     cfg.App,
		"GX_TORRENT_EVENT":   event.Name,
		"GX_TORRENT_TITLE":   eventTitle(cfg.App, event),
		"GX_TORRENT_MESSAGE": event.Text,
		"GEXTTO_APP":         cfg.App,
		"GEXTTO_EVENT":       event.Name,
		"GEXTTO_TITLE":       eventTitle(cfg.App, event),
		"GEXTTO_MESSAGE":     event.Text,
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys)+2)
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	if event.Name == "test" {
		env = append(env, "GX_TORRENT_TEST=true", "GEXTTO_TEST=true")
	}
	return env
}

// splitArgs splits the optional argument string. Single and double quotes keep
// a value with spaces together; a backslash escapes the next character.
func splitArgs(raw string) []string {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}
	for _, r := range raw {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return args
}

// limitedBuffer keeps the first 8 KiB a script writes on stderr, enough to
// name the cause of a failure without keeping a runaway output in memory.
type limitedBuffer struct {
	data []byte
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	const limit = 8192
	if len(b.data) < limit {
		remaining := limit - len(b.data)
		if len(p) > remaining {
			b.data = append(b.data, p[:remaining]...)
		} else {
			b.data = append(b.data, p...)
		}
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return string(b.data) }
