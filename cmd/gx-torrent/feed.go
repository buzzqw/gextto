package main

// feed.go is the standalone RSS engine: it follows the configured feeds
// (Jackett, Prowlarr, MIRCrew and any RSS listing), applies include/exclude
// rules and adds the matching torrents. It runs only in standalone mode: in
// managed mode Gextto owns acquisition, so the loop never starts.

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/rss"
)

const (
	feedsSettingKey     = "feeds"
	feedIntervalKey     = "feed-interval-secs"
	defaultFeedInterval = 15 * time.Minute
	feedSeenRetention   = 30 * 24 * time.Hour
	feedRequestTimeout  = 30 * time.Second
	feedMaxAddsPerPoll  = 25
)

// feedConfig is one configured feed with its rules.
type feedConfig struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Include  string `json:"include,omitempty"` // comma-separated
	Exclude  string `json:"exclude,omitempty"`
	Category string `json:"category,omitempty"`
	SavePath string `json:"savepath,omitempty"`
	Paused   bool   `json:"paused,omitempty"`
}

func (c feedConfig) rule() rss.Rule {
	return rss.Rule{Include: splitList(c.Include), Exclude: splitList(c.Exclude)}
}

// feedStatus is the last outcome of one feed, for the page.
type feedStatus struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Items     int    `json:"items"`
	Added     int    `json:"added"`
	LastError string `json:"last_error,omitempty"`
	LastCheck int64  `json:"last_check,omitempty"`
}

func splitList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

// feeds returns the configured feeds (empty when none/invalid).
func (d *Daemon) feeds() []feedConfig {
	if d.opts.Settings == nil {
		return nil
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(feedsSettingKey, ""))
	if raw == "" {
		return nil
	}
	var list []feedConfig
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		logf("cannot parse the feeds setting: %v", err)
		return nil
	}
	return list
}

func (d *Daemon) feedInterval() time.Duration {
	if d.opts.Settings == nil {
		return defaultFeedInterval
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(feedIntervalKey, ""))
	if raw == "" {
		return defaultFeedInterval
	}
	secs, err := time.ParseDuration(raw + "s")
	if err != nil || secs < 60*time.Second {
		return defaultFeedInterval
	}
	return secs
}

// runFeeds polls the feeds on an interval until stop is closed. It is a no-op
// unless the daemon is standalone and at least one feed is configured.
func (d *Daemon) runFeeds(stop <-chan struct{}) {
	if d.opts.Mode != ModeStandalone || len(d.feeds()) == 0 {
		return
	}
	// A short initial delay lets the session settle before the first poll.
	select {
	case <-stop:
		return
	case <-time.After(30 * time.Second):
	}
	for {
		d.pollFeeds()
		select {
		case <-stop:
			return
		case <-time.After(d.feedInterval()):
		}
	}
}

// pollFeeds fetches every feed once, applies its rules and adds the matching
// torrents that were not added before.
func (d *Daemon) pollFeeds() {
	feeds := d.feeds()
	if len(feeds) == 0 {
		return
	}
	client := &http.Client{Timeout: feedRequestTimeout}
	for _, feed := range feeds {
		if strings.TrimSpace(feed.URL) == "" {
			continue
		}
		name := feed.Name
		if name == "" {
			name = feed.URL
		}
		ctx, cancel := context.WithTimeout(context.Background(), feedRequestTimeout)
		items, err := rss.Fetch(ctx, client, feed.URL, name)
		cancel()
		status := feedStatus{Name: name, URL: feed.URL, LastCheck: time.Now().Unix()}
		if err != nil {
			status.LastError = err.Error()
			logf("feed %s: %v", name, err)
			d.setFeedStatus(status)
			continue
		}
		status.Items = len(items)
		added := 0
		for _, item := range feed.rule().Filter(items) {
			if added >= feedMaxAddsPerPoll {
				break
			}
			if d.feedSeen(name, item.GUID) {
				continue
			}
			if err := d.addFeedItem(feed, item); err != nil {
				status.LastError = err.Error()
				logf("feed %s: cannot add %q: %v", name, item.Title, err)
				continue
			}
			d.markFeedSeen(name, item.GUID)
			added++
		}
		status.Added = added
		if added > 0 {
			logf("feed %s: added %d torrent(s)", name, added)
		}
		d.setFeedStatus(status)
	}
}

// addFeedItem adds one feed item: a magnet directly, a .torrent URL downloaded
// first.
func (d *Daemon) addFeedItem(feed feedConfig, item rss.Item) error {
	req := addRequest{
		Destination: strings.TrimSpace(feed.SavePath),
		Paused:      feed.Paused,
		Category:    strings.TrimSpace(feed.Category),
	}
	if item.Magnet != "" {
		req.Magnet = item.Magnet
	} else {
		data, err := d.fetchTorrentURL(item.Link)
		if err != nil {
			return err
		}
		req.TorrentData = data
	}
	_, _, err := d.add(req)
	return err
}

// feedSeenKey is the persisted key of one item.
func feedSeenKey(feed, guid string) string { return feed + "\x00" + guid }

// feedSeen reports whether an item was already added.
func (d *Daemon) feedSeen(feed, guid string) bool {
	if guid == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.state.FeedSeen[feedSeenKey(feed, guid)]
	return ok
}

// markFeedSeen records an item as added and prunes old entries.
func (d *Daemon) markFeedSeen(feed, guid string) {
	if guid == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.FeedSeen == nil {
		d.state.FeedSeen = map[string]int64{}
	}
	now := time.Now()
	d.state.FeedSeen[feedSeenKey(feed, guid)] = now.Unix()
	for key, at := range d.state.FeedSeen {
		if now.Sub(time.Unix(at, 0)) > feedSeenRetention {
			delete(d.state.FeedSeen, key)
		}
	}
	d.dirty = true
	d.saveLocked()
}

func (d *Daemon) setFeedStatus(status feedStatus) {
	d.feedMu.Lock()
	defer d.feedMu.Unlock()
	if d.feedStatuses == nil {
		d.feedStatuses = map[string]feedStatus{}
	}
	d.feedStatuses[status.Name] = status
}

// feedStatusesSnapshot returns the feeds' last outcomes, ordered by name.
func (d *Daemon) feedStatusesSnapshot() []feedStatus {
	d.feedMu.Lock()
	defer d.feedMu.Unlock()
	out := make([]feedStatus, 0, len(d.feedStatuses))
	for _, status := range d.feedStatuses {
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
