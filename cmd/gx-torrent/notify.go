package main

// notify.go: optional notifications, on the same channels Gextto offers
// (internal/notify): Telegram, a webhook (generic JSON, Discord, Slack,
// Gotify, ntfy, Pushover) and e-mail. The daemon notifies completed downloads,
// feed matches and feed errors. The webhook URL also comes from the
// -notify-url flag or GX_TORRENT_NOTIFY_URL; in standalone the rest lives in
// settings.json and is edited on the settings page.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/notify"
)

// Settings keys of the notification channels (standalone settings.json).
const (
	notifyURLSettingKey    = "notify-url"
	notifyFormatKey        = "notify-webhook-format"
	notifyWebhookTokenKey  = "notify-webhook-token"
	notifyWebhookUserKey   = "notify-webhook-user"
	notifyTelegramTokenKey = "notify-telegram-token"
	notifyTelegramChatKey  = "notify-telegram-chat"
	notifyEmailSMTPKey     = "notify-email-smtp"
	notifyEmailFromKey     = "notify-email-from"
	notifyEmailToKey       = "notify-email-to"
	notifyEmailPasswordKey = "notify-email-password"
	notifyOnCompleteKey    = "notify-on-complete"
	notifyOnFeedMatchKey   = "notify-on-feed-match"
	notifyOnFeedErrorKey   = "notify-on-feed-error"
)

// notifyURL returns the configured webhook, or "" when it is off. The flag
// wins; in standalone the settings.json key is the fallback.
func (d *Daemon) notifyURL() string {
	if u := strings.TrimSpace(d.opts.NotifyURL); u != "" {
		return u
	}
	if d.opts.Settings != nil {
		return strings.TrimSpace(d.opts.Settings.Get(notifyURLSettingKey, ""))
	}
	return ""
}

// notifyConfig gathers the channels from the flag and settings.json.
func (d *Daemon) notifyConfig() notify.Config {
	cfg := notify.Config{App: "gx-torrent", WebhookURL: d.notifyURL()}
	if s := d.opts.Settings; s != nil {
		cfg.WebhookFormat = s.Get(notifyFormatKey, "json")
		cfg.WebhookToken = s.Get(notifyWebhookTokenKey, "")
		cfg.WebhookUser = s.Get(notifyWebhookUserKey, "")
		cfg.TelegramToken = s.Get(notifyTelegramTokenKey, "")
		cfg.TelegramChat = s.Get(notifyTelegramChatKey, "")
		cfg.SMTP = s.Get(notifyEmailSMTPKey, "")
		cfg.From = s.Get(notifyEmailFromKey, "")
		cfg.To = s.Get(notifyEmailToKey, "")
		cfg.Password = s.Get(notifyEmailPasswordKey, "")
	}
	return cfg
}

// notifyEventOn reports whether an event kind is enabled (all are by default).
func (d *Daemon) notifyEventOn(event string) bool {
	key := ""
	switch event {
	case "download_complete":
		key = notifyOnCompleteKey
	case "feed_match":
		key = notifyOnFeedMatchKey
	case "feed_error":
		key = notifyOnFeedErrorKey
	}
	if key == "" || d.opts.Settings == nil {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(d.opts.Settings.Get(key, "true")), "false")
}

// notifyText is the readable message of an event.
func notifyText(event string, fields map[string]any) string {
	switch event {
	case "download_complete":
		return fmt.Sprintf("✅ Download complete: %v", fields["name"])
	case "feed_match":
		text := fmt.Sprintf("📥 Feed %v: %v torrent(s) added", fields["feed"], fields["added"])
		if titles, ok := fields["titles"].([]string); ok {
			for _, title := range titles {
				text += "\n• " + title
			}
		}
		return text
	case "feed_error":
		text := fmt.Sprintf("⚠️ Feed %v: %v", fields["feed"], fields["error"])
		if title, ok := fields["title"]; ok {
			text += fmt.Sprintf(" (%v)", title)
		}
		return text
	case "test":
		return "🔔 gx-torrent: test notification"
	}
	return event
}

// notify sends an event to the configured channels, best effort and without
// blocking the caller.
func (d *Daemon) notify(event string, fields map[string]any) {
	cfg := d.notifyConfig()
	if !cfg.Enabled() || !d.notifyEventOn(event) {
		return
	}
	go func() {
		if err := notify.Send(context.Background(), cfg, notify.Event{Name: event, Text: notifyText(event, fields), Fields: fields}); err != nil {
			logf("notify %s: %v", event, err)
		}
	}()
}

// handleUINotifyTest sends a test notification with the saved channels and
// renders the outcome for the settings page.
func (d *Daemon) handleUINotifyTest(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	lang := uiDictionary(d.uiLang(r))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	cfg := d.notifyConfig()
	if !cfg.Enabled() {
		_, _ = w.Write([]byte(uiTranslateText("No notification channel is configured: save one first.", lang)))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := notify.Send(ctx, cfg, notify.Event{Name: "test", Text: notifyText("test", nil)}); err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(uiTranslateText("Not delivered:", lang) + " " + err.Error()))
		return
	}
	_, _ = w.Write([]byte(uiTranslateText("Test notification sent.", lang)))
}
