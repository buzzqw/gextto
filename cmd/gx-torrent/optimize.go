package main

// optimize.go is the settings page's Optimization section, the standalone
// counterpart of Gextto's "Optimize" button: one click puts the queue, the
// disk cache or the bandwidth on values that work well, and the page explains
// what was set. Each action only changes its own settings.

import (
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strconv"
)

const (
	// optimizeUploadShare keeps the upload below the measured line capacity:
	// a saturated upstream delays the ACKs of the downloads and slows them.
	optimizeUploadShare = 0.8
	// optimizeMinUploadPeak is the least upload (bytes/s) worth measuring.
	optimizeMinUploadPeak = 50 * 1024
)

// handleUIOptimize applies one optimization (queue, cache or bandwidth).
func (d *Daemon) handleUIOptimize(w http.ResponseWriter, r *http.Request) {
	if d.opts.Mode != ModeStandalone || d.opts.Settings == nil {
		http.NotFound(w, r)
		return
	}
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	var message string
	var err error
	switch r.FormValue("optimize") {
	case "queue":
		message, err = d.optimizeQueue()
	case "cache":
		message, err = d.optimizeCache()
	case "bandwidth":
		message, err = d.optimizeBandwidth()
	default:
		err = errors.New("Unknown optimization.")
	}
	ok := "1"
	if err != nil {
		ok, message = "0", err.Error()
	}
	http.Redirect(w, r, uiSettingsPath+"?msg="+url.QueryEscape(message)+"&ok="+ok+"#optimize", http.StatusSeeOther)
}

func rawJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

// optimizeQueue applies Gextto's queue baseline: 3 downloads, 3 seeds, 5
// active in all, the dynamic queue between 1 and 10 slots, slow torrents not
// counted, and gx-torrent's self-management on.
func (d *Daemon) optimizeQueue() (string, error) {
	_, err := d.setConfig(map[string]json.RawMessage{
		"active_downloads": rawJSON(3),
		"active_seeds":     rawJSON(3),
		"active_limit":     rawJSON(5),
		"dont_count_slow":  rawJSON(true),
		"dynamic_queue":    rawJSON(true),
		"dynamic_min":      rawJSON(1),
		"dynamic_max":      rawJSON(10),
		"auto":             rawJSON(true),
	})
	if err != nil {
		return "", err
	}
	return "Queue optimized: 3 downloads, 3 seeds, 5 active; the dynamic queue adapts between 1 and 10 slots and slow torrents do not take a slot.", nil
}

// optimizeCache sets the disk cache back to automatic: sized from the RAM and
// the storage class, adapted while running.
func (d *Daemon) optimizeCache() (string, error) {
	_, err := d.setConfig(map[string]json.RawMessage{
		"cache_mb":       rawJSON(-1),
		"cache_ttl_secs": rawJSON(300),
		"auto":           rawJSON(true),
	})
	if err != nil {
		return "", err
	}
	return "Disk cache optimized: automatic, sized from the RAM and adapted while running.", nil
}

// optimizeBandwidth sets the limits from the rates measured since start: the
// download unlimited, the upload at 80% of the highest upload seen, and the
// latency auto-speed on so the upload backs off when the line gets busy.
func (d *Daemon) optimizeBandwidth() (string, error) {
	d.mu.Lock()
	peakUp := d.peakUp
	d.mu.Unlock()
	cfg, _, _ := d.config()
	if peakUp < optimizeMinUploadPeak {
		return "", errors.New("Not enough upload measured yet: let some torrents seed for a while, then try again.")
	}
	peakKiB := peakUp / 1024
	if cfg.SpeedLimitUpload > 0 && peakKiB >= cfg.SpeedLimitUpload*9/10 {
		// The upload ran into the current limit: the line was not measured.
		// Keep the limit and let the auto-speed protect the line.
		d.enableAutoSpeed(cfg.SpeedLimitUpload)
		return "", errors.New("The upload reached the current limit, so the line capacity is unknown: the limit is kept and latency auto-speed is on. Set the upload to 0, let it seed, then optimize again.")
	}
	upload := int64(float64(peakKiB) * optimizeUploadShare)
	if _, err := d.setConfig(map[string]json.RawMessage{
		"speed_limit_download": rawJSON(0),
		"speed_limit_upload":   rawJSON(upload),
	}); err != nil {
		return "", err
	}
	d.enableAutoSpeed(upload)
	d.applyScheduleNow()
	return "Bandwidth optimized: download unlimited, upload at 80% of the measured peak, latency auto-speed on.", nil
}

// enableAutoSpeed turns the latency auto-speed on with a floor of a tenth of
// the upload limit.
func (d *Daemon) enableAutoSpeed(uploadKiB int64) {
	store := d.opts.Settings
	store.Set(autospeedEnabledKey, "true")
	store.Set(autospeedMinKey, strconv.FormatInt(max(uploadKiB/10, autoSpeedDefaultMinKiB), 10))
	if err := store.Save(); err != nil {
		logf("cannot save the auto-speed settings: %v", err)
	}
}

// optimizationStatusHTML shows the measured peaks the bandwidth optimization
// starts from.
func (d *Daemon) optimizationStatusHTML() string {
	d.mu.Lock()
	down, up := d.peakDown, d.peakUp
	d.mu.Unlock()
	return `<span>Peak rates measured since start</span>: ↓ ` + uiRate(down) + ` · ↑ ` + uiRate(up)
}

// ipFilterStatusHTML renders the filter status: rules, source, when it was
// loaded and when the next refresh is due.
func (d *Daemon) ipFilterStatusHTML() string {
	status := d.ipFilterStatusSnapshot()
	var out string
	if status.Rules > 0 {
		out = `<span class="ok">` + strconv.Itoa(status.Rules) + `</span> <span>rules loaded</span>`
		if !status.LoadedAt.IsZero() {
			out += ` · <span>loaded at</span> ` + status.LoadedAt.Format("02/01 15:04")
		}
		if status.Path != "" {
			out += ` · <span class="mono">` + html.EscapeString(status.Path) + `</span>`
		}
	} else {
		out = `<span>No IP filter loaded</span>`
	}
	if !status.Next.IsZero() {
		out += ` · <span>next update</span> ` + status.Next.Format("02/01 15:04")
	} else if src := d.ipFilterSource(); src != "" && !isIPFilterURL(src) {
		out += ` · <span>reloaded when the file changes</span>`
	}
	if status.Error != "" {
		out += ` · <span class="err" role="status">` + html.EscapeString(status.Error) + `</span>`
	}
	return out
}
