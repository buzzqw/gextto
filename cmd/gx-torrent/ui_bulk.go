package main

// ui_bulk.go holds the page actions that apply to a selection of torrents or
// to the whole session, like qBittorrent's context menu and status bar:
// per-torrent speed limits and save path for many torrents at once, the
// global speed limits, and adding or banning peers from the Peers tab.

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// uiSelectedHashes is the selection a bulk form sends (hashes=…, repeated).
func uiSelectedHashes(r *http.Request) []string {
	var out []string
	for _, h := range r.Form["hashes"] {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// handleUIBulkLimits sets the per-torrent speed limits (KiB/s, -1 global,
// 0 unlimited) of every selected torrent. A blank field leaves that limit as
// it is, so the dialog can change only one of the two.
func (d *Daemon) handleUIBulkLimits(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hashes := uiSelectedHashes(r)
	if len(hashes) == 0 {
		d.uiDone(w, r, "", fmt.Errorf("no torrents selected"))
		return
	}
	var download, upload *int64
	if strings.TrimSpace(r.FormValue("download_limit")) != "" {
		v := max(formInt(r, "download_limit", -1), -1)
		download = &v
	}
	if strings.TrimSpace(r.FormValue("upload_limit")) != "" {
		v := max(formInt(r, "upload_limit", -1), -1)
		upload = &v
	}
	var firstErr error
	for _, hash := range hashes {
		if err := d.setLimits(hash, download, upload, nil, nil); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	d.uiDone(w, r, "Speed limits saved", firstErr)
}

// handleUIBulkMove moves the data of every selected torrent to one folder
// (qBittorrent's "Set location").
func (d *Daemon) handleUIBulkMove(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hashes := uiSelectedHashes(r)
	destination := strings.TrimSpace(r.FormValue("destination"))
	if len(hashes) == 0 || destination == "" {
		d.uiDone(w, r, "", fmt.Errorf("a folder and at least one torrent are required"))
		return
	}
	var firstErr error
	for _, hash := range hashes {
		if err := d.move(hash, destination, false); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	d.uiDone(w, r, "Move started", firstErr)
}

// handleUIGlobalLimits sets the global speed limits from the status bar
// (KiB/s, 0 = unlimited). A blank field keeps the current value.
func (d *Daemon) handleUIGlobalLimits(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	patch := map[string]json.RawMessage{}
	for field, key := range map[string]string{"download_limit": "speed_limit_download", "upload_limit": "speed_limit_upload"} {
		if v, ok := parseFormKib(r.FormValue(field)); ok {
			raw, _ := json.Marshal(v)
			patch[key] = raw
		}
	}
	var err error
	if len(patch) > 0 {
		_, err = d.setConfig(patch)
		d.applyScheduleNow()
	}
	d.uiDone(w, r, "Global speed limits saved", err)
}

// handleUIPeers adds peers to a torrent (op=add, peers = ip:port per line or
// comma separated) or bans a peer's IP in the whole session (op=ban).
func (d *Daemon) handleUIPeers(w http.ResponseWriter, r *http.Request) {
	if !d.uiAuthorized(w, r) || !d.uiSameOrigin(w, r) {
		return
	}
	_ = r.ParseForm()
	hash := strings.TrimSpace(r.FormValue("hash"))
	peers := strings.FieldsFunc(r.FormValue("peers"), func(c rune) bool { return c == '\n' || c == ',' || c == '|' || c == ' ' || c == '\r' })
	if len(peers) == 0 {
		d.uiDoneDetail(w, r, hash, "peers", "", fmt.Errorf("no peer given"))
		return
	}
	switch r.FormValue("op") {
	case "add":
		d.mu.Lock()
		t, _ := d.findLocked(hash)
		d.mu.Unlock()
		if t == nil {
			d.uiDoneDetail(w, r, hash, "peers", "", errNotFound)
			return
		}
		var firstErr error
		for _, peer := range peers {
			if _, _, err := net.SplitHostPort(peer); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: an address is ip:port", peer)
				}
				continue
			}
			if err := t.AddPeer(peer); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		d.uiDoneDetail(w, r, hash, "peers", "Peers added", firstErr)
	case "ban":
		if d.session == nil {
			d.uiDoneDetail(w, r, hash, "peers", "", fmt.Errorf("not available"))
			return
		}
		for _, peer := range peers {
			ip := peer
			if host, _, err := net.SplitHostPort(peer); err == nil {
				ip = host
			}
			if net.ParseIP(ip) != nil {
				d.session.BanIP(ip, qbitBanTTL)
			}
		}
		d.uiDoneDetail(w, r, hash, "peers", "Peer banned", nil)
	default:
		d.uiDoneDetail(w, r, hash, "peers", "", fmt.Errorf("unknown action"))
	}
}
