package main

// ipfilter_refresh.go keeps the standalone IP filter current. The configured
// source (settings "ipfilter") is a local file or a URL: a URL is downloaded at
// start-up (the session cannot read it directly) and again every refresh
// interval; a file is reloaded when it changed on disk. Managed mode is left to
// Gextto, which refreshes the list itself.

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// ipFilterRefreshKey is the refresh interval in hours (0 = never).
	ipFilterRefreshKey          = "ipfilter-refresh-hours"
	ipFilterDefaultRefreshHours = 24
	ipFilterStartDelay          = 20 * time.Second
	ipFilterCheckInterval       = 10 * time.Minute
)

// ipFilterState is the filter status shown on the settings page.
type ipFilterState struct {
	Rules    int
	Path     string
	LoadedAt time.Time
	Next     time.Time
	Error    string
}

func isIPFilterURL(source string) bool {
	lower := strings.ToLower(strings.TrimSpace(source))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// ipFilterSource is the configured filter (settings first, then the flag).
func (d *Daemon) ipFilterSource() string {
	if d.opts.Settings != nil {
		if value := strings.TrimSpace(d.opts.Settings.Get("ipfilter", "")); value != "" {
			return value
		}
	}
	return strings.TrimSpace(d.opts.Network.IPFilter)
}

// ipFilterRefreshEvery is the configured refresh interval (0 = never).
func (d *Daemon) ipFilterRefreshEvery() time.Duration {
	hours := ipFilterDefaultRefreshHours
	if d.opts.Settings != nil {
		if raw := strings.TrimSpace(d.opts.Settings.Get(ipFilterRefreshKey, "")); raw != "" {
			if value, err := strconv.Atoi(raw); err == nil && value >= 0 {
				hours = value
			}
		}
	}
	return time.Duration(hours) * time.Hour
}

// loadIPFilterSource downloads (a URL) and loads the filter, recording the
// outcome for the settings page.
func (d *Daemon) loadIPFilterSource(source string) (int, error) {
	path := source
	if isIPFilterURL(source) {
		fetched, err := d.fetchIPFilterURL(source)
		if err != nil {
			d.recordIPFilter(0, "", err)
			return 0, err
		}
		path = fetched
	}
	rules, err := d.loadIPFilter(path)
	d.recordIPFilter(rules, path, err)
	return rules, err
}

func (d *Daemon) recordIPFilter(rules int, path string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		d.ipFilterStatus.Error = err.Error()
		return
	}
	d.ipFilterStatus = ipFilterState{Rules: rules, Path: path, LoadedAt: time.Now(), Next: d.ipFilterStatus.Next}
}

// ipFilterStatusSnapshot returns the filter status for the page.
func (d *Daemon) ipFilterStatusSnapshot() ipFilterState {
	d.mu.Lock()
	defer d.mu.Unlock()
	status := d.ipFilterStatus
	if status.Rules == 0 && d.ipFilterRules > 0 {
		status.Rules, status.Path = d.ipFilterRules, d.ipFilterPath
	}
	return status
}

// runIPFilterRefresh loads a URL filter at start-up and keeps the filter
// current until stop is closed (standalone only).
func (d *Daemon) runIPFilterRefresh(stop <-chan struct{}) {
	if d.opts.Mode != ModeStandalone {
		return
	}
	select {
	case <-stop:
		return
	case <-time.After(ipFilterStartDelay):
	}
	var lastFetch time.Time
	var lastStamp string
	first := true
	for {
		source := d.ipFilterSource()
		every := d.ipFilterRefreshEvery()
		now := time.Now()
		switch {
		case source == "":
		case isIPFilterURL(source):
			// The session cannot open a URL: the first round always fetches it,
			// later rounds when the interval elapsed.
			if first || (every > 0 && now.Sub(lastFetch) >= every) {
				lastFetch = now
				if rules, err := d.loadIPFilterSource(source); err != nil {
					logf("IP filter %s: %v", source, err)
				} else {
					logf("IP filter refreshed: %d rules from %s", rules, source)
				}
			}
		default:
			// A local file: reload only when it changed (size or mtime).
			if info, err := os.Stat(source); err == nil {
				stamp := strconv.FormatInt(info.Size(), 10) + "|" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
				if !first && stamp != lastStamp && every > 0 {
					if rules, err := d.loadIPFilterSource(source); err == nil {
						logf("IP filter reloaded after a change: %d rules from %s", rules, source)
					}
				}
				lastStamp = stamp
			}
		}
		first = false
		d.mu.Lock()
		if source != "" && isIPFilterURL(source) && every > 0 {
			d.ipFilterStatus.Next = lastFetch.Add(every)
		} else {
			d.ipFilterStatus.Next = time.Time{}
		}
		d.mu.Unlock()
		select {
		case <-stop:
			return
		case <-time.After(ipFilterCheckInterval):
		}
	}
}
