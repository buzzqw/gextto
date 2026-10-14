package main

// api_qbit_extra.go implements the read-side qBittorrent API endpoints added on
// top of the base surface in api_qbit.go: piece diagnostics, web seeds, the
// per-torrent limits in reading, the alternative-limits state, the network
// interfaces and the logs. They are registered only in standalone mode, like
// the rest of /api/v2.

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// handleQbitPieceStates answers /api/v2/torrents/pieceStates: an array of
// integers, one per piece, 0 = not downloaded, 1 = downloading, 2 = downloaded.
// An unknown hash (or no metadata yet) yields an empty array, like qBittorrent.
func (d *Daemon) handleQbitPieceStates(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	t, _ := d.findLocked(r.URL.Query().Get("hash"))
	d.mu.Unlock()
	out := []int{}
	if t != nil {
		if states, ok := t.PieceStates(); ok {
			out = make([]int, len(states))
			for i, state := range states {
				switch state {
				case "have":
					out[i] = 2
				case "downloading":
					out[i] = 1
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleQbitPieceHashes answers /api/v2/torrents/pieceHashes: the hex hashes of
// every piece, in order. Empty before the metadata arrives.
func (d *Daemon) handleQbitPieceHashes(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	t, _ := d.findLocked(r.URL.Query().Get("hash"))
	d.mu.Unlock()
	out := []string{}
	if t != nil {
		if hashes := t.PieceHashes(); hashes != nil {
			out = hashes
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// qbitWebseed is the qBittorrent web seed object (url, plus the error the
// daemon knows about, an extra field clients ignore).
type qbitWebseed struct {
	URL   string `json:"url"`
	Error string `json:"error,omitempty"`
}

func (d *Daemon) handleQbitWebseeds(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	t, _ := d.findLocked(r.URL.Query().Get("hash"))
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	out := []qbitWebseed{}
	for _, ws := range t.Webseeds() {
		seed := qbitWebseed{URL: ws.URL}
		if ws.Error != nil {
			seed.Error = ws.Error.Error()
		}
		out = append(out, seed)
	}
	writeJSON(w, http.StatusOK, out)
}

// qbitWantedHashes is the /torrents/info hash filter for handlers that accept
// the list in a form field (POST) as well as in the query string (GET).
func (d *Daemon) qbitWantedHashes(r *http.Request) map[string]struct{} {
	raw := strings.TrimSpace(r.FormValue("hashes"))
	if raw == "" {
		raw = strings.TrimSpace(r.FormValue("hash"))
	}
	if raw == "" || strings.EqualFold(raw, "all") {
		return nil
	}
	set := make(map[string]struct{})
	for _, h := range d.qbitHashes(raw) {
		set[strings.ToLower(h)] = struct{}{}
	}
	return set
}

func (d *Daemon) handleQbitTorrentDownloadLimits(w http.ResponseWriter, r *http.Request) {
	d.qbitTorrentLimits(w, r, true)
}

func (d *Daemon) handleQbitTorrentUploadLimits(w http.ResponseWriter, r *http.Request) {
	d.qbitTorrentLimits(w, r, false)
}

// qbitTorrentLimits answers torrents/downloadLimit and torrents/uploadLimit: a
// map hash -> limit in bytes per second, 0 when no limit is applied.
func (d *Daemon) qbitTorrentLimits(w http.ResponseWriter, r *http.Request, download bool) {
	wanted := d.qbitWantedHashes(r)
	out := map[string]int64{}
	for _, v := range d.snapshotViews() {
		hash := strings.ToLower(v.Hash)
		if wanted != nil {
			if _, ok := wanted[hash]; !ok {
				continue
			}
		}
		kib := v.UploadLimitKib
		if download {
			kib = v.DownloadLimitKib
		}
		if kib > 0 {
			out[hash] = kib * 1024
		} else {
			out[hash] = 0
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleQbitSpeedLimitsMode reports whether the alternative (scheduled) speed
// limits are currently in force: 1 or 0, like qBittorrent.
func (d *Daemon) handleQbitSpeedLimitsMode(w http.ResponseWriter, _ *http.Request) {
	if _, _, active := d.scheduledSpeedLimits(time.Now()); active {
		qbitText(w, http.StatusOK, "1")
		return
	}
	qbitText(w, http.StatusOK, "0")
}

func (d *Daemon) handleQbitNetworkInterfaceList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, interfaceNames())
}

// interfaceNames returns the machine's interface names, sorted by name, the
// list qBittorrent's networkInterfaceList exposes.
func interfaceNames() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		out = append(out, iface.Name)
	}
	return out
}

// handleQbitNetworkInterfaceAddressList returns the addresses of one interface
// (?iface=NAME). An unknown name yields an empty array.
func (d *Daemon) handleQbitNetworkInterfaceAddressList(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("iface"))
	out := []string{}
	if name != "" {
		if iface, err := net.InterfaceByName(name); err == nil {
			if addrs, err := iface.Addrs(); err == nil {
				for _, addr := range addrs {
					out = append(out, addr.String())
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// qbitLogEntry is one entry of the qBittorrent log API. Types: 1 normal, 2
// info, 4 warning, 8 critical.
type qbitLogEntry struct {
	ID        int64  `json:"id"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
	Type      int    `json:"type"`
}

// handleQbitLogMain answers /api/v2/log/main from the daemon's in-memory ring.
func (d *Daemon) handleQbitLogMain(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lastID := int64(-1)
	if raw := strings.TrimSpace(q.Get("last_known_id")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			lastID = v
		}
	}
	includeNormal := qbitQueryBool(q, "normal")
	includeInfo := qbitQueryBool(q, "info")
	includeWarn := qbitQueryBool(q, "warning")
	includeCrit := qbitQueryBool(q, "critical")
	out := []qbitLogEntry{}
	now := time.Now().Unix()
	for _, entry := range logRing.tailEntries(2000) {
		if entry.ID <= lastID {
			continue
		}
		typ := qbitLogType(entry.Text)
		switch typ {
		case 1:
			if !includeNormal {
				continue
			}
		case 2:
			if !includeInfo {
				continue
			}
		case 4:
			if !includeWarn {
				continue
			}
		case 8:
			if !includeCrit {
				continue
			}
		}
		out = append(out, qbitLogEntry{ID: entry.ID, Message: entry.Text, Timestamp: now, Type: typ})
	}
	writeJSON(w, http.StatusOK, out)
}

// qbitQueryBool reads a log filter flag, defaulting to true when absent (the
// qBittorrent default) and to false only on an explicit false/0.
func qbitQueryBool(q map[string][]string, key string) bool {
	values, ok := q[key]
	if !ok || len(values) == 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(values[0])) {
	case "false", "0":
		return false
	default:
		return true
	}
}

// qbitLogType classifies a log line for the qBittorrent log API.
func qbitLogType(line string) int {
	upper := strings.ToUpper(line)
	switch {
	case strings.Contains(upper, "CRITICAL") || strings.Contains(upper, "FATAL"):
		return 8
	case strings.Contains(upper, "ERROR") || strings.Contains(upper, "WARN"):
		return 4
	case strings.Contains(upper, "DEBUG") || strings.Contains(upper, "INFO"):
		return 2
	default:
		return 1
	}
}

// handleQbitLogPeers answers /api/v2/log/peers. The engine does not keep a peer
// log, so the array is empty (the endpoint exists for the clients that call it).
func (d *Daemon) handleQbitLogPeers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []qbitLogEntry{})
}
