// Package notify delivers the gx-torrent daemon's notifications on the same
// channels Gextto offers: Telegram, a webhook (generic JSON or the Discord,
// Slack, Gotify, ntfy and Pushover formats) and e-mail over SMTP. Every
// configured channel receives every event; a channel left empty is off.
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
	"strconv"
	"strings"
	"time"
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

	// SMTP is "host:port" (587 when the port is missing, STARTTLS when the
	// server offers it).
	SMTP     string
	From     string
	To       string // comma separated
	Password string
}

// Formats lists the webhook formats, in the order a page offers them.
var Formats = []string{"json", "discord", "slack", "gotify", "ntfy", "pushover"}

// Enabled reports whether at least one channel is configured.
func (c Config) Enabled() bool {
	return c.telegramOn() || strings.TrimSpace(c.WebhookURL) != "" || c.emailOn()
}

func (c Config) telegramOn() bool {
	return strings.TrimSpace(c.TelegramToken) != "" && strings.TrimSpace(c.TelegramChat) != ""
}

func (c Config) emailOn() bool {
	return strings.TrimSpace(c.SMTP) != "" && strings.TrimSpace(c.From) != "" && strings.TrimSpace(c.To) != ""
}

// Event is one notification: a machine name, a readable text and the raw
// fields (sent as they are in the JSON webhook).
type Event struct {
	Name   string
	Text   string
	Fields map[string]any
}

const timeout = 20 * time.Second

// Send delivers the event to every configured channel and returns the errors
// joined (nil when every channel succeeded or none is configured).
func Send(ctx context.Context, cfg Config, event Event) error {
	app := strings.TrimSpace(cfg.App)
	if app == "" {
		app = "gx-torrent"
	}
	cfg.App = app
	var errs []error
	if cfg.telegramOn() {
		if err := sendTelegram(ctx, cfg, event); err != nil {
			errs = append(errs, fmt.Errorf("telegram: %w", err))
		}
	}
	if strings.TrimSpace(cfg.WebhookURL) != "" {
		if err := sendWebhook(ctx, cfg, event); err != nil {
			errs = append(errs, fmt.Errorf("webhook: %w", err))
		}
	}
	if cfg.emailOn() {
		if err := sendEmail(cfg, event); err != nil {
			errs = append(errs, fmt.Errorf("e-mail: %w", err))
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

func sendWebhook(ctx context.Context, cfg Config, event Event) error {
	headers := map[string]string{}
	token := strings.TrimSpace(cfg.WebhookToken)
	var body []byte
	var err error
	contentType := "application/json"
	switch strings.ToLower(strings.TrimSpace(cfg.WebhookFormat)) {
	case "discord":
		body, err = json.Marshal(map[string]any{"content": event.Text})
	case "slack":
		body, err = json.Marshal(map[string]any{"text": event.Text})
	case "gotify":
		if token != "" {
			headers["X-Gotify-Key"] = token
		}
		body, err = json.Marshal(map[string]any{"title": cfg.App, "message": event.Text})
	case "ntfy":
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
		headers["Title"] = cfg.App
		body, contentType = []byte(event.Text), "text/plain; charset=utf-8"
	case "pushover":
		form := url.Values{"title": {cfg.App}, "message": {event.Text}, "token": {token}, "user": {strings.TrimSpace(cfg.WebhookUser)}}
		body, contentType = []byte(form.Encode()), "application/x-www-form-urlencoded"
	default: // json
		payload := map[string]any{"app": cfg.App, "event": event.Name, "text": event.Text, "time": time.Now().UTC().Format(time.RFC3339)}
		for key, value := range event.Fields {
			payload[key] = value
		}
		body, err = json.Marshal(payload)
	}
	if err != nil {
		return err
	}
	return post(ctx, strings.TrimSpace(cfg.WebhookURL), contentType, headers, body)
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
	defer client.Close()
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
