package main

// notify.go: optional notifications, on the same channels Gextto and indexarr
// offer (internal/notify): Telegram, a webhook (generic JSON, Discord, Slack,
// Gotify, ntfy, Pushover), the dedicated Discord, Slack, Gotify, ntfy and
// Pushover channels, Apprise, e-mail and a custom script. The daemon notifies
// completed downloads, feed matches and feed errors. The webhook URL also
// comes from the -notify-url flag or GX_TORRENT_NOTIFY_URL; in standalone the
// rest lives in settings.json and is edited on the settings page.

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
	// Dedicated channels, the same ones indexarr offers: they can be filled
	// together, so a user reaches several services at once.
	notifyDiscordURLKey    = "notify-discord-url"
	notifySlackURLKey      = "notify-slack-url"
	notifyGotifyURLKey     = "notify-gotify-url"
	notifyGotifyTokenKey   = "notify-gotify-token"
	notifyNtfyURLKey       = "notify-ntfy-url"
	notifyNtfyTopicKey     = "notify-ntfy-topic"
	notifyNtfyTokenKey     = "notify-ntfy-token"
	notifyPushoverTokenKey = "notify-pushover-token"
	notifyPushoverUserKey  = "notify-pushover-user"
	notifyAppriseURLKey    = "notify-apprise-url"
	notifyAppriseKeyKey    = "notify-apprise-key"
	notifyScriptKey        = "notify-script"
	notifyScriptArgsKey    = "notify-script-args"
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
		cfg.DiscordURL = s.Get(notifyDiscordURLKey, "")
		cfg.SlackURL = s.Get(notifySlackURLKey, "")
		cfg.GotifyURL = s.Get(notifyGotifyURLKey, "")
		cfg.GotifyToken = s.Get(notifyGotifyTokenKey, "")
		cfg.NtfyURL = s.Get(notifyNtfyURLKey, "")
		cfg.NtfyTopic = s.Get(notifyNtfyTopicKey, "")
		cfg.NtfyToken = s.Get(notifyNtfyTokenKey, "")
		cfg.PushoverToken = s.Get(notifyPushoverTokenKey, "")
		cfg.PushoverUser = s.Get(notifyPushoverUserKey, "")
		cfg.AppriseURL = s.Get(notifyAppriseURLKey, "")
		cfg.AppriseKey = s.Get(notifyAppriseKeyKey, "")
		cfg.Script = s.Get(notifyScriptKey, "")
		cfg.ScriptArgs = s.Get(notifyScriptArgsKey, "")
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

// notifyTitle is the short subject some channels show on their own (the
// Apprise title) and the custom script receives.
func notifyTitle(event string, fields map[string]any) string {
	switch event {
	case "download_complete":
		return fmt.Sprintf("Download complete: %v", fields["name"])
	case "feed_match":
		return fmt.Sprintf("Feed %v: %v torrent(s) added", fields["feed"], fields["added"])
	case "feed_error":
		return fmt.Sprintf("Feed %v: error", fields["feed"])
	case "test":
		return "gx-torrent: test notification"
	}
	return "gx-torrent"
}

// notify sends an event to the configured channels, best effort and without
// blocking the caller.
func (d *Daemon) notify(event string, fields map[string]any) {
	cfg := d.notifyConfig()
	if !cfg.Enabled() || !d.notifyEventOn(event) {
		return
	}
	go func() {
		payload := notify.Event{Name: event, Title: notifyTitle(event, fields), Text: notifyText(event, fields), Fields: fields}
		if err := notify.Send(context.Background(), cfg, payload); err != nil {
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
	if err := notify.Send(ctx, cfg, notify.Event{Name: "test", Title: notifyTitle("test", nil), Text: notifyText("test", nil)}); err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(uiTranslateText("Not delivered:", lang) + " " + err.Error()))
		return
	}
	_, _ = w.Write([]byte(uiTranslateText("Test notification sent.", lang)))
}
