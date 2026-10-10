package main

// notify.go: optional webhook notifications. When a URL is configured (the
// -notify-url flag, GX_TORRENT_NOTIFY_URL, or the "notify-url" setting), the
// daemon posts a small JSON payload on feed matches and feed errors, so an
// external tool (ntfy, Gotify, a chat webhook, a script) can alert the user.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// notifyURLSettingKey is the standalone settings key holding the webhook URL.
const notifyURLSettingKey = "notify-url"

// notifyURL returns the configured webhook, or "" when notifications are off.
// The flag wins; in standalone the settings.json key is the fallback.
func (d *Daemon) notifyURL() string {
	if u := strings.TrimSpace(d.opts.NotifyURL); u != "" {
		return u
	}
	if d.opts.Settings != nil {
		return strings.TrimSpace(d.opts.Settings.Get(notifyURLSettingKey, ""))
	}
	return ""
}

// notify posts a JSON event to the configured webhook, best effort and without
// blocking the caller. The known events are "feed_match" and "feed_error".
func (d *Daemon) notify(event string, fields map[string]any) {
	url := d.notifyURL()
	if url == "" {
		return
	}
	payload := map[string]any{
		"app":   "gx-torrent",
		"event": event,
		"time":  time.Now().UTC().Format(time.RFC3339),
	}
	for k, v := range fields {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		logf("notify: %v", err)
		return
	}
	go func() {
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			logf("notify: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			logf("notify: %v", err)
			return
		}
		resp.Body.Close()
	}()
}
