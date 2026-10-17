package main

// api_integrations.go is what lets other systems talk to the daemon beyond the
// torrent list: Prometheus metrics (GET /api/v1/metrics), the engine's event
// stream for automations (GET /api/v1/events, Server-Sent Events with the API
// token) and the qBittorrent Web API v2 calls a client can expect from
// qBittorrent 5: torrents/count, transfer/setSpeedLimitsMode and the web seed
// editing calls (addWebSeeds, editWebSeed, removeWebSeeds).

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// handleAPIEvents streams the engine events (torrent added, completed, error,
// peer connected, …) as Server-Sent Events, one JSON object per "data:" line.
// It is the token-authenticated twin of the page's /ui/events, for Home
// Assistant, Node-RED or a script; a comment line every 30 s keeps proxies from
// closing an idle stream.
func (d *Daemon) handleAPIEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	session := d.session
	if session == nil {
		http.Error(w, "session not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": gx-torrent events\n\n")
	flusher.Flush()

	ch, cancel := session.Subscribe()
	defer cancel()
	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// metric is one Prometheus sample family.
type metric struct {
	name, help, kind string
	samples          []metricSample
}

type metricSample struct {
	labels string // already formatted: `state="seeding"`, or ""
	value  float64
}

// handleMetrics serves the daemon's state in the Prometheus text format
// (version 0.0.4): transfer rates and totals, torrents by state, peers, queue,
// cache, disk, the IP filter, the uTP socket and the process itself.
func (d *Daemon) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	s := d.stats()
	gauge := func(name, help string, value float64) metric {
		return metric{name: name, help: help, kind: "gauge", samples: []metricSample{{value: value}}}
	}
	counter := func(name, help string, value float64) metric {
		return metric{name: name, help: help, kind: "counter", samples: []metricSample{{value: value}}}
	}
	boolValue := func(b bool) float64 {
		if b {
			return 1
		}
		return 0
	}
	states := metric{name: "gxtorrent_torrents", help: "Torrents in the session, by state.", kind: "gauge"}
	for _, state := range []struct {
		name  string
		count int
	}{
		{"downloading", s.Downloading}, {"seeding", s.Seeding}, {"queued", s.Queued},
		{"stalled", s.Stalled}, {"paused", s.Paused}, {"slow", s.Slow}, {"moving", s.Moving},
	} {
		states.samples = append(states.samples, metricSample{labels: `state="` + state.name + `"`, value: float64(state.count)})
	}
	metrics := []metric{
		{name: "gxtorrent_info", help: "The daemon version (value 1).", kind: "gauge",
			samples: []metricSample{{labels: `version="` + promLabel(s.Version) + `"`, value: 1}}},
		gauge("gxtorrent_uptime_seconds", "Seconds since the daemon started.", float64(s.UptimeSeconds)),
		gauge("gxtorrent_torrents_total", "Torrents in the session.", float64(s.Torrents)),
		states,
		gauge("gxtorrent_download_rate_bytes", "Current download rate in bytes per second.", float64(s.DownloadRate)),
		gauge("gxtorrent_upload_rate_bytes", "Current upload rate in bytes per second.", float64(s.UploadRate)),
		gauge("gxtorrent_peers", "Connected peers.", float64(s.Peers)),
		gauge("gxtorrent_active_downloads", "Downloads holding a queue slot.", float64(s.ActiveDownloads)),
		gauge("gxtorrent_active_seeds", "Seeds holding a queue slot.", float64(s.ActiveSeeds)),
		gauge("gxtorrent_active_limit", "Active torrents allowed by the queue.", float64(s.ActiveLimit)),
		gauge("gxtorrent_ratio", "Share ratio of this session.", s.Ratio),
		counter("gxtorrent_lifetime_downloaded_bytes_total", "Bytes downloaded across every session.", float64(s.Lifetime.Downloaded)),
		counter("gxtorrent_lifetime_uploaded_bytes_total", "Bytes uploaded across every session.", float64(s.Lifetime.Uploaded)),
		gauge("gxtorrent_port_mapped", "1 when UPnP, NAT-PMP or PCP opened the peer port.", boolValue(s.PortMapping.Method != "")),
		gauge("gxtorrent_ip_filter_rules", "Rules loaded in the IP filter.", float64(s.IPFilterRules)),
		gauge("gxtorrent_cache_read_bytes", "Read cache size in bytes.", float64(s.CacheReadMB<<20)),
		gauge("gxtorrent_cache_write_bytes", "Write cache size in bytes.", float64(s.CacheWriteMB<<20)),
		gauge("gxtorrent_disk_free_bytes", "Free space of the download folder.", float64(s.DiskFreeBytes)),
		gauge("gxtorrent_disk_total_bytes", "Size of the download folder's filesystem.", float64(s.DiskTotalBytes)),
		gauge("gxtorrent_lsd_peers_found", "Peers found by local service discovery.", float64(s.LSD.PeersFound)),
		gauge("process_resident_memory_bytes", "Resident memory of the daemon.", float64(processMemoryBytes())),
		counter("process_cpu_seconds_total", "CPU time used by the daemon.", processCPUTime().Seconds()),
	}
	// The engine's session counters, one family each, in a stable order.
	keys := make([]string, 0, len(s.Session))
	for key := range s.Session {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := "gxtorrent_session_" + promName(key)
		metrics = append(metrics, gauge(name, "Engine session counter "+key+".", float64(s.Session[key])))
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	var b strings.Builder
	for _, m := range metrics {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", m.name, m.help, m.name, m.kind)
		for _, sample := range m.samples {
			value := strconv.FormatFloat(sample.value, 'g', -1, 64)
			if sample.labels != "" {
				fmt.Fprintf(&b, "%s{%s} %s\n", m.name, sample.labels, value)
			} else {
				fmt.Fprintf(&b, "%s %s\n", m.name, value)
			}
		}
	}
	_, _ = w.Write([]byte(b.String()))
}

// promName turns a counter key into a valid Prometheus name part.
func promName(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// promLabel escapes a label value.
func promLabel(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
}

// handleQbitTorrentsCount answers torrents/count: the number of torrents, as
// plain text.
func (d *Daemon) handleQbitTorrentsCount(w http.ResponseWriter, _ *http.Request) {
	qbitText(w, http.StatusOK, strconv.Itoa(len(d.snapshotViews())))
}

// handleQbitSetSpeedLimitsMode sets the alternative speed limits on (mode=1)
// or off (mode=0), the explicit form of toggleSpeedLimitsMode.
func (d *Daemon) handleQbitSetSpeedLimitsMode(w http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(r.FormValue("mode"))
	if mode != "0" && mode != "1" {
		http.Error(w, "mode must be 0 or 1", http.StatusBadRequest)
		return
	}
	if d.opts.Settings == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	d.opts.Settings.Set(altSpeedEnabledKey, strconv.FormatBool(mode == "1"))
	_ = d.opts.Settings.Save()
	d.applyScheduleNow()
	w.WriteHeader(http.StatusOK)
}

// webSeedURLs parses a qBittorrent "urls" value (separated by "|", or one per
// line) and refuses anything that is not an http(s) URL.
func webSeedURLs(raw string) ([]string, error) {
	var out []string
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == '|' || r == '\n' || r == '\r' }) {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if err := validWebSeedURL(field); err != nil {
			return nil, err
		}
		out = append(out, field)
	}
	return out, nil
}

func validWebSeedURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid web seed URL: %s", raw)
	}
	return nil
}

// qbitWebSeedTorrent checks the "hash" of a web seed call: 404 when the
// torrent is not in the session.
func (d *Daemon) qbitWebSeedTorrent(w http.ResponseWriter, r *http.Request) (string, bool) {
	hash := strings.TrimSpace(r.FormValue("hash"))
	d.mu.Lock()
	t, _ := d.findLocked(hash)
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "Torrent hash was not found", http.StatusNotFound)
		return "", false
	}
	return hash, true
}

// handleQbitAddWebSeeds adds web seeds (BEP 19) to a torrent, live.
func (d *Daemon) handleQbitAddWebSeeds(w http.ResponseWriter, r *http.Request) {
	hash, ok := d.qbitWebSeedTorrent(w, r)
	if !ok {
		return
	}
	urls, err := webSeedURLs(r.FormValue("urls"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := d.addWebseeds(hash, urls); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRemoveWebSeeds removes web seeds from a torrent, live.
func (d *Daemon) handleQbitRemoveWebSeeds(w http.ResponseWriter, r *http.Request) {
	hash, ok := d.qbitWebSeedTorrent(w, r)
	if !ok {
		return
	}
	urls, err := webSeedURLs(r.FormValue("urls"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := d.removeWebseeds(hash, urls); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitEditWebSeed replaces one web seed of a torrent: 409 when origUrl
// is not one of its web seeds, 400 when newUrl is not a valid URL.
func (d *Daemon) handleQbitEditWebSeed(w http.ResponseWriter, r *http.Request) {
	hash, ok := d.qbitWebSeedTorrent(w, r)
	if !ok {
		return
	}
	original := strings.TrimSpace(r.FormValue("origUrl"))
	replacement := strings.TrimSpace(r.FormValue("newUrl"))
	if err := validWebSeedURL(replacement); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	d.mu.Lock()
	t, _ := d.findLocked(hash)
	d.mu.Unlock()
	found := false
	if t != nil {
		for _, ws := range t.Webseeds() {
			if ws.URL == original {
				found = true
				break
			}
		}
	}
	if !found {
		http.Error(w, "origUrl is not a web seed of this torrent", http.StatusConflict)
		return
	}
	if original == replacement {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := d.removeWebseeds(hash, []string{original}); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err := d.addWebseeds(hash, []string{replacement}); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}
