package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// The tracker list: a public list of trackers, one URL per line, downloaded
// from trackerListURLKey every day and appended to the new public torrents
// (qBittorrent 5's "add trackers from URL"). A private torrent never gets
// them: a magnet is marked pending at add and served only once its metadata
// shows it is public.

const (
	// trackerListKey turns the list on (default on); trackerListURLKey
	// names it (empty = defaultTrackerListURL).
	trackerListKey    = "tracker-list"
	trackerListURLKey = "tracker-list-url"
	// defaultTrackerListURL is the list used when none is named: the
	// "best" public trackers of the trackerslist project, kept current
	// upstream.
	defaultTrackerListURL = "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt"
	// trackerListEvery is how often the list is downloaded again.
	trackerListEvery = 24 * time.Hour
	// trackerListRetry is the wait after a failed download.
	trackerListRetry = time.Hour
	// trackerListMax bounds the trackers taken from one list.
	trackerListMax = 200
	// trackerListFile keeps the last list across restarts.
	trackerListFile = "tracker-list.txt"
)

// trackerListState is the last list read and how the last download went.
type trackerListState struct {
	mu        sync.Mutex
	url       string
	trackers  []string
	fetchedAt time.Time
	err       string
}

// snapshot returns a copy of the state.
func (s *trackerListState) snapshot() (string, []string, time.Time, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.url, append([]string(nil), s.trackers...), s.fetchedAt, s.err
}

// trackerListEnabled reports whether the list is appended (default on).
func (d *Daemon) trackerListEnabled() bool {
	return d.settingBool(trackerListKey, d.opts.Network.TrackerList)
}

// trackerListAddress returns the named list or the default one.
func (d *Daemon) trackerListAddress() string {
	if d.opts.Settings != nil {
		if v := strings.TrimSpace(d.opts.Settings.Get(trackerListURLKey, "")); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(d.opts.Network.TrackerListURL); v != "" {
		return v
	}
	return defaultTrackerListURL
}

// trackerListURL returns the list in use, or "" when the list is off.
func (d *Daemon) trackerListURL() string {
	if !d.trackerListEnabled() {
		return ""
	}
	return d.trackerListAddress()
}

// parseTrackerList keeps the valid tracker URLs of a list (http, https, udp,
// ws, wss), without duplicates, in their order.
func parseTrackerList(r io.Reader) []string {
	seen := map[string]bool{}
	var out []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 64<<10)
	for scanner.Scan() && len(out) < trackerListMax {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Host == "" {
			continue
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "udp", "ws", "wss":
		default:
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

// fetchTrackerList downloads the list through the engine's transport (proxy
// and VPN interface included).
func fetchTrackerList(rawURL string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gx-torrent")
	client := &http.Client{Transport: torrent.HTTPTransport()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	list := parseTrackerList(io.LimitReader(resp.Body, 1<<20))
	if len(list) == 0 {
		return nil, fmt.Errorf("the list holds no tracker address")
	}
	return list, nil
}

// loadCachedTrackerList reads the list kept from the last run.
func (d *Daemon) loadCachedTrackerList(rawURL string) {
	f, err := os.Open(filepath.Join(d.opts.DataDir, trackerListFile))
	if err != nil {
		return
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	first, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.TrimPrefix(first, "# ")) != rawURL {
		return
	}
	list := parseTrackerList(reader)
	info, _ := f.Stat()
	d.trackerList.mu.Lock()
	d.trackerList.url, d.trackerList.trackers = rawURL, list
	if info != nil {
		d.trackerList.fetchedAt = info.ModTime()
	}
	d.trackerList.mu.Unlock()
}

// refreshTrackerList downloads the list and keeps it, in memory and on disk.
func (d *Daemon) refreshTrackerList(rawURL string) {
	list, err := fetchTrackerList(rawURL)
	d.trackerList.mu.Lock()
	defer d.trackerList.mu.Unlock()
	d.trackerList.url = rawURL
	if err != nil {
		d.trackerList.err = err.Error()
		logf("The tracker list at %s could not be downloaded: %v", rawURL, err)
		return
	}
	d.trackerList.trackers, d.trackerList.fetchedAt, d.trackerList.err = list, time.Now(), ""
	logf("Tracker list downloaded: %d trackers from %s", len(list), rawURL)
	body := "# " + rawURL + "\n" + strings.Join(list, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(d.opts.DataDir, trackerListFile), []byte(body), 0o644); err != nil {
		logfError("The tracker list could not be saved: %v", err)
	}
}

// runTrackerList keeps the list current while a URL is configured.
func (d *Daemon) runTrackerList(stop <-chan struct{}) {
	var lastURL string
	for {
		wait := time.Minute
		if rawURL := d.trackerListURL(); rawURL != "" {
			current, _, fetchedAt, lastErr := d.trackerList.snapshot()
			if rawURL != lastURL && current != rawURL {
				d.loadCachedTrackerList(rawURL)
				current, _, fetchedAt, lastErr = d.trackerList.snapshot()
			}
			lastURL = rawURL
			due := current != rawURL || time.Since(fetchedAt) >= trackerListEvery
			if lastErr != "" && time.Since(fetchedAt) < trackerListEvery {
				due = false
			}
			if due {
				d.refreshTrackerList(rawURL)
				if _, _, _, failed := d.trackerList.snapshot(); failed != "" {
					wait = trackerListRetry
				}
			}
		} else {
			lastURL = ""
		}
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
	}
}

// applyTrackerListLocked appends the list to the torrents still waiting for
// it whose metadata is known: public ones get it, private ones are cleared.
func (d *Daemon) applyTrackerListLocked() {
	rawURL, list, _, _ := d.trackerList.snapshot()
	if d.session == nil || rawURL == "" || rawURL != d.trackerListURL() {
		return
	}
	for _, t := range d.session.ListTorrents() {
		meta, ok := d.state.Torrents[t.ID()]
		if !ok || !meta.TrackerListPending {
			continue
		}
		if len(list) == 0 {
			continue
		}
		stats := t.Stats()
		if stats.Status == torrent.DownloadingMetadata || stats.Bytes.Total == 0 {
			continue
		}
		meta.TrackerListPending = false
		d.dirty = true
		if stats.Private {
			continue
		}
		added := 0
		for _, tracker := range list {
			if err := t.AddTracker(tracker); err == nil {
				added++
			}
		}
		if added > 0 {
			logf("%d trackers from the tracker list added to %s", added, stats.Name)
		}
	}
}
