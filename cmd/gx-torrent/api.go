package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cenkalti/rain/torrent"
)

const maxTorrentFile = 32 << 20

func (d *Daemon) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", d.handleHealth)
	mux.HandleFunc("GET /api/v1/stats", d.handleStats)
	mux.HandleFunc("GET /api/v1/torrents", d.handleList)
	mux.HandleFunc("POST /api/v1/add", d.handleAdd)
	mux.HandleFunc("POST /api/v1/add-file", d.handleAddFile)
	mux.HandleFunc("DELETE /api/v1/torrents/{hash}", d.handleRemove)
	mux.HandleFunc("GET /api/v1/torrents/{hash}/{what}", d.handleInspect)
	mux.HandleFunc("POST /api/v1/torrents/{hash}/{action}", d.handleAction)
	mux.HandleFunc("POST /api/v1/pins/clear", d.handleClearPins)
	mux.HandleFunc("POST /api/v1/ipfilter", d.handleIPFilter)
	mux.HandleFunc("GET /api/v1/config", d.handleGetConfig)
	mux.HandleFunc("POST /api/v1/config", d.handleSetConfig)
	return d.authenticate(mux)
}

// authenticate requires the shared token on every request when one is set.
func (d *Daemon) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.opts.Token != "" {
			given := r.Header.Get("X-Gx-Token")
			if given == "" {
				given = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			}
			if subtle.ConstantTimeCompare([]byte(given), []byte(d.opts.Token)) != 1 {
				writeError(w, http.StatusUnauthorized, errors.New("missing or wrong token"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeResult(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case errors.Is(err, errNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		writeError(w, http.StatusConflict, err)
	}
}

func formBool(r *http.Request, key string) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func formFloat(r *http.Request, key string, fallback float64) float64 {
	if value, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue(key)), 64); err == nil {
		return value
	}
	return fallback
}

func formInt(r *http.Request, key string, fallback int64) int64 {
	if value, err := strconv.ParseInt(strings.TrimSpace(r.FormValue(key)), 10, 64); err == nil {
		return value
	}
	return fallback
}

func (d *Daemon) handleHealth(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	ok := d.session != nil
	count := len(d.state.Torrents)
	d.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "version": version, "torrents": count})
}

func (d *Daemon) handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.stats())
}

func (d *Daemon) handleList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.list())
}

func (d *Daemon) handleAdd(w http.ResponseWriter, r *http.Request) {
	magnet := strings.TrimSpace(r.FormValue("magnet"))
	if magnet == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing magnet"))
		return
	}
	hash, existing, err := d.add(addRequest{
		Magnet:         magnet,
		Destination:    r.FormValue("destination"),
		Paused:         formBool(r, "paused"),
		QueueTop:       formBool(r, "top"),
		StopAtMetadata: formBool(r, "stop_at_metadata"),
		SeedRatio:      formFloat(r, "seed_ratio", -1),
		SeedDays:       formInt(r, "seed_days", -1),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "existing": existing})
}

func (d *Daemon) handleAddFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTorrentFile+1<<20)
	if err := r.ParseMultipartForm(maxTorrentFile); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	file, _, err := r.FormFile("torrent")
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxTorrentFile))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	hash, existing, err := d.add(addRequest{
		TorrentData:    data,
		Destination:    r.FormValue("destination"),
		Paused:         formBool(r, "paused"),
		QueueTop:       formBool(r, "top"),
		StopAtMetadata: formBool(r, "stop_at_metadata"),
		SeedRatio:      formFloat(r, "seed_ratio", -1),
		SeedDays:       formInt(r, "seed_days", -1),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "existing": existing})
}

func (d *Daemon) handleRemove(w http.ResponseWriter, r *http.Request) {
	writeResult(w, d.remove(r.PathValue("hash"), formBool(r, "delete_files")))
}

func (d *Daemon) handleClearPins(w http.ResponseWriter, _ *http.Request) {
	writeResult(w, d.setPin("", false))
}

func (d *Daemon) handleAction(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	var err error
	switch r.PathValue("action") {
	case "pause":
		err = d.pause(hash)
	case "resume":
		err = d.resume(hash)
	case "verify":
		err = d.verify(hash)
	case "reannounce":
		err = d.reannounce(hash)
	case "restart":
		err = d.restart(hash, time.Duration(formInt(r, "probe_secs", 0))*time.Second)
	case "park":
		err = d.park(hash)
	case "unpark":
		err = d.unpark(hash)
	case "pin":
		err = d.setPin(hash, formBool(r, "pinned"))
	case "top":
		err = d.moveToTop(hash)
	case "move":
		err = d.move(hash, r.FormValue("destination"), false)
	case "associate":
		err = d.move(hash, r.FormValue("destination"), true)
	case "seed-limits":
		var ratio *float64
		var days *int64
		if r.FormValue("seed_ratio") != "" {
			value := formFloat(r, "seed_ratio", -1)
			ratio = &value
		}
		if r.FormValue("seed_days") != "" {
			value := formInt(r, "seed_days", -1)
			days = &value
		}
		err = d.setSeedLimits(hash, ratio, days)
	case "file-priorities":
		var priorities []int
		for _, field := range strings.Split(r.FormValue("priorities"), ",") {
			value, convErr := strconv.Atoi(strings.TrimSpace(field))
			if convErr != nil {
				writeError(w, http.StatusBadRequest, fmt.Errorf("invalid priority %q", field))
				return
			}
			priorities = append(priorities, value)
		}
		err = d.setFilePriorities(hash, priorities)
	case "trackers":
		var urls []string
		for _, line := range strings.Split(r.FormValue("urls"), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				urls = append(urls, line)
			}
		}
		err = d.addTrackers(hash, urls)
	default:
		writeError(w, http.StatusBadRequest, errors.New("unknown action"))
		return
	}
	writeResult(w, err)
}

type fileInfo struct {
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Downloaded int64  `json:"downloaded"`
	Priority   int    `json:"priority"`
}

type peerInfo struct {
	Address      string  `json:"address"`
	Client       string  `json:"client"`
	DownloadRate int     `json:"download_rate"`
	UploadRate   int     `json:"upload_rate"`
	Incoming     bool    `json:"incoming"`
	Encrypted    bool    `json:"encrypted"`
	UTP          bool    `json:"utp"`
	Progress     float64 `json:"progress"`
	Seed         bool    `json:"seed"`
}

type trackerInfo struct {
	URL          string `json:"url"`
	Status       string `json:"status"`
	Seeders      int    `json:"seeders"`
	Leechers     int    `json:"leechers"`
	Message      string `json:"message,omitempty"`
	NextAnnounce int64  `json:"next_announce"`
}

func (d *Daemon) handleInspect(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	t, _ := d.findLocked(r.PathValue("hash"))
	d.mu.Unlock()
	if t == nil {
		writeError(w, http.StatusNotFound, errNotFound)
		return
	}
	switch r.PathValue("what") {
	case "files":
		d.mu.Lock()
		meta := d.metaLocked(t)
		d.mu.Unlock()
		files, err := t.Files()
		if err != nil {
			writeJSON(w, http.StatusOK, []fileInfo{})
			return
		}
		done := map[int]int64{}
		if stats, err := t.FileStats(); err == nil {
			for index, stat := range stats {
				done[index] = stat.BytesCompleted
			}
		}
		out := make([]fileInfo, 0, len(files))
		for index, file := range files {
			out = append(out, fileInfo{Path: file.Path(), Size: file.Length(), Downloaded: done[index], Priority: filePriority(meta, index)})
		}
		writeJSON(w, http.StatusOK, out)
	case "peers":
		out := []peerInfo{}
		for _, peer := range t.Peers() {
			out = append(out, peerInfo{
				Address:      peer.Addr.String(),
				Client:       peer.Client,
				DownloadRate: peer.DownloadSpeed,
				UploadRate:   peer.UploadSpeed,
				Incoming:     peer.Source == torrent.SourceIncoming,
				Encrypted:    peer.EncryptedStream,
				UTP:          peer.UTP,
				Progress:     peer.Progress * 100,
				Seed:         peer.Seed,
			})
		}
		writeJSON(w, http.StatusOK, out)
	case "trackers":
		out := []trackerInfo{}
		for _, tracker := range t.Trackers() {
			item := trackerInfo{URL: tracker.URL, Seeders: tracker.Seeders, Leechers: tracker.Leechers, Message: tracker.Warning}
			switch tracker.Status {
			case torrent.Working:
				item.Status = "working"
			case torrent.Contacting:
				item.Status = "contacting"
			case torrent.NotWorking:
				item.Status = "not_working"
			default:
				item.Status = "not_contacted"
			}
			if tracker.Error != nil {
				item.Message = tracker.Error.Error()
			}
			if !tracker.NextAnnounce.IsZero() {
				item.NextAnnounce = int64(time.Until(tracker.NextAnnounce).Seconds())
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, out)
	case "torrent-file":
		data, err := t.Torrent()
		if err != nil || len(data) == 0 {
			writeError(w, http.StatusNotFound, errors.New("metadata not available yet"))
			return
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(data)
	default:
		writeError(w, http.StatusNotFound, errors.New("unknown resource"))
	}
}

func (d *Daemon) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, limits, restart := d.config()
	writeJSON(w, http.StatusOK, map[string]any{
		"config":          cfg,
		"effective":       map[string]int{"active_downloads": limits.Downloads, "active_seeds": limits.Seeds, "active_limit": limits.Limit},
		"restart_pending": restart,
	})
}

func (d *Daemon) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	var patch map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg, err := d.setConfig(patch)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": cfg})
}

// magnetInfoHash extracts the v1 info hash (hex or base32) from a magnet.
func magnetInfoHash(magnet string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(magnet))
	if err != nil || parsed.Scheme != "magnet" {
		return "", false
	}
	for _, xt := range parsed.Query()["xt"] {
		value, found := strings.CutPrefix(strings.ToLower(xt), "urn:btih:")
		if !found {
			continue
		}
		switch len(value) {
		case 40:
			if _, err := hex.DecodeString(value); err == nil {
				return value, true
			}
		case 32:
			if raw, err := base32.StdEncoding.DecodeString(strings.ToUpper(value)); err == nil {
				return hex.EncodeToString(raw), true
			}
		}
	}
	return "", false
}

// handleIPFilter reloads the IP filter (form field path, default: the
// configured file).
func (d *Daemon) handleIPFilter(w http.ResponseWriter, r *http.Request) {
	rules, err := d.loadIPFilter(strings.TrimSpace(r.FormValue("path")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// errV2Only is returned for BitTorrent v2-only torrents: rain speaks v1 (and
// the v1 side of hybrid torrents) only.
var errV2Only = errors.New("v2_unsupported: BitTorrent v2-only torrent (no v1 info hash); hybrid and v1 torrents are supported")

// magnetIsV2Only reports a magnet that carries only a v2 (btmh) hash.
func magnetIsV2Only(magnet string) bool {
	lower := strings.ToLower(magnet)
	return strings.Contains(lower, "urn:btmh:") && !strings.Contains(lower, "urn:btih:")
}

// torrentIsV2Only reports a .torrent whose info has "meta version" 2 and no
// v1 "pieces" (hybrids carry both).
func torrentIsV2Only(data []byte) bool {
	return bytes.Contains(data, []byte("12:meta versioni2e")) && !bytes.Contains(data, []byte("6:pieces"))
}
