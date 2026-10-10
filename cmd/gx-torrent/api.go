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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

const maxTorrentFile = 32 << 20

func (d *Daemon) routes() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/health", d.handleHealth)
	api.HandleFunc("GET /api/v1/stats", d.handleStats)
	api.HandleFunc("GET /api/v1/portcheck", d.handlePortCheck)
	api.HandleFunc("GET /api/v1/torrents", d.handleList)
	api.HandleFunc("POST /api/v1/add", d.handleAdd)
	api.HandleFunc("POST /api/v1/add-file", d.handleAddFile)
	api.HandleFunc("DELETE /api/v1/torrents/{hash}", d.handleRemove)
	api.HandleFunc("GET /api/v1/torrents/{hash}/{what}", d.handleInspect)
	api.HandleFunc("POST /api/v1/torrents/{hash}/{action}", d.handleAction)
	api.HandleFunc("POST /api/v1/pins/clear", d.handleClearPins)
	api.HandleFunc("POST /api/v1/ipfilter", d.handleIPFilter)
	api.HandleFunc("GET /api/v1/config", d.handleGetConfig)
	api.HandleFunc("POST /api/v1/config", d.handleSetConfig)

	root := http.NewServeMux()
	// The API keeps the token middleware; the read-only page below is served
	// outside it (handleUI checks the token itself) so a browser can open
	// http://127.0.0.1:8890/ without sending a custom header.
	root.Handle("/api/", d.authenticate(api))
	root.HandleFunc("GET /{$}", d.handleUI)
	root.HandleFunc("GET /favicon.ico", handleFavicon)
	root.HandleFunc("GET /ui", d.handleUI)
	root.HandleFunc("GET /ui/live", d.handleUILive)
	root.HandleFunc("GET /ui/detail", d.handleUIDetail)
	root.HandleFunc("GET /ui/gextto-log", d.handleUIGexttoLog)
	root.HandleFunc("GET /ui/search", d.handleUISearch)
	root.HandleFunc("GET /ui/feeds", d.handleUIFeeds)
	root.HandleFunc("POST /ui/feeds/poll", d.handleUIFeedPoll)
	root.HandleFunc("GET /ui/feed-items", d.handleUIFeedItems)
	root.HandleFunc("GET /ui/rss", d.handleUIRss)
	root.HandleFunc("POST /ui/rss-config", d.handleUIRssConfig)
	root.HandleFunc("GET /ui/portcheck", d.handleUIPortCheck)
	root.HandleFunc("GET /ui/stream", d.handleUIStream)
	root.HandleFunc("GET /ui/login", d.handleUILogin)
	root.HandleFunc("POST /ui/login", d.handleUILogin)
	root.HandleFunc("GET /ui/setup", d.handleUISetup)
	root.HandleFunc("POST /ui/setup", d.handleUISetup)
	root.HandleFunc("GET /ui/torrent-file", d.handleUITorrentFile)
	root.HandleFunc("POST /ui/action", d.handleUIAction)
	root.HandleFunc("POST /ui/bulk", d.handleUIBulk)
	root.HandleFunc("POST /ui/remove", d.handleUIRemove)
	root.HandleFunc("POST /ui/add", d.handleUIAdd)
	root.HandleFunc("POST /ui/add-file", d.handleUIAddFile)
	root.HandleFunc("POST /ui/ipfilter", d.handleUIIPFilter)
	root.HandleFunc("POST /ui/file-priority", d.handleUIFilePriority)
	root.HandleFunc("POST /ui/trackers", d.handleUITrackers)
	root.HandleFunc("POST /ui/webseeds", d.handleUIWebSeeds)
	root.HandleFunc("POST /ui/seed-limits", d.handleUISeedLimits)
	root.HandleFunc("POST /ui/super-seeding", d.handleUISuperSeeding)
	root.HandleFunc("POST /ui/move", d.handleUIMove)
	root.HandleFunc("POST /ui/pin", d.handleUIPin)
	root.HandleFunc("POST /ui/category", d.handleUICategory)
	if d.opts.Mode == ModeStandalone {
		// The qBittorrent-compatible API lets *arr and the mobile apps drive
		// the standalone daemon; managed mode is driven by Gextto only.
		root.Handle("/api/v2/", d.routesQbit())
	}
	return d.standaloneGuard(root)
}

// authenticate requires the shared token on every request when one is set.
func (d *Daemon) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		markAPISeen()
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

// addRequestFromForm builds the options shared by the add endpoints (API and
// UI, magnet/URL and file): one place for the parameter names so the forms
// cannot drift apart. Callers set Magnet or TorrentData on top.
func addRequestFromForm(r *http.Request) addRequest {
	return addRequest{
		Destination:    r.FormValue("destination"),
		Paused:         formBool(r, "paused"),
		QueueTop:       formBool(r, "top"),
		StopAtMetadata: formBool(r, "stop_at_metadata"),
		Sequential:     formBool(r, "sequential"),
		FirstLast:      formBool(r, "first_last"),
		SuperSeeding:   formBool(r, "super_seeding"),
		SeedRatio:      formFloat(r, "seed_ratio", -1),
		SeedDays:       formInt(r, "seed_days", -1),
	}
}

func (d *Daemon) handleHealth(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	ok := d.session != nil
	count := len(d.state.Torrents)
	d.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "version": runtimeVersion(), "torrents": count,
		"pid": os.Getpid(), "mode": string(d.opts.Mode), "fingerprint": d.opts.Fingerprint, "data_dir": d.opts.DataDir})
}

func (d *Daemon) handleStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.stats())
}

// handlePortCheck is the eMule-style "test ports": is the daemon listening and
// does the router actually forward the peer port?
func (d *Daemon) handlePortCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.mapper.check())
}

func (d *Daemon) handleList(w http.ResponseWriter, _ *http.Request) {
	// Serve the published snapshot: the read path never touches a torrent run
	// loop, so a torrent blocked on a slow network mount cannot make the API
	// time out and look like a dead daemon to Gextto.
	writeJSON(w, http.StatusOK, d.snapshotViews())
}

func (d *Daemon) handleAdd(w http.ResponseWriter, r *http.Request) {
	magnet := strings.TrimSpace(r.FormValue("magnet"))
	if magnet == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing magnet"))
		return
	}
	req := addRequestFromForm(r)
	req.Magnet = magnet
	hash, existing, err := d.add(req)
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
	req := addRequestFromForm(r)
	req.TorrentData = data
	hash, existing, err := d.add(req)
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
		var download, upload *int64
		if r.FormValue("download_limit") != "" {
			value := formInt(r, "download_limit", -1)
			download = &value
		}
		if r.FormValue("upload_limit") != "" {
			value := formInt(r, "upload_limit", -1)
			upload = &value
		}
		err = d.setLimits(hash, download, upload, ratio, days)
	case "conn-limits":
		var maxConnections, maxUploads *int64
		if r.FormValue("max_connections") != "" {
			value := formInt(r, "max_connections", -1)
			maxConnections = &value
		}
		if r.FormValue("max_uploads") != "" {
			value := formInt(r, "max_uploads", -1)
			maxUploads = &value
		}
		err = d.setConnLimits(hash, maxConnections, maxUploads)
	case "super-seeding":
		err = d.setSuperSeeding(hash, formBool(r, "enabled"))
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
	case "set-trackers":
		var urls []string
		for _, line := range strings.Split(r.FormValue("urls"), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				urls = append(urls, line)
			}
		}
		err = d.setTrackers(hash, urls)
	case "webseeds":
		var urls []string
		for _, line := range strings.Split(r.FormValue("urls"), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				urls = append(urls, line)
			}
		}
		if r.FormValue("remove") == "1" {
			err = d.removeWebseeds(hash, urls)
		} else {
			err = d.addWebseeds(hash, urls)
		}
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
	Source       string  `json:"source"`
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

// pieceRun is a compact run of consecutive pieces sharing a state. End is
// inclusive, matching the Gextto piece-diagnostics contract.
type pieceRun struct {
	Begin int    `json:"begin"`
	End   int    `json:"end"`
	State string `json:"state"`
}

// compressPieceRuns groups consecutive pieces with the same state, so a large
// torrent is a few runs instead of one entry per piece.
func compressPieceRuns(states []string) []pieceRun {
	runs := make([]pieceRun, 0, 8)
	for index, state := range states {
		if last := len(runs) - 1; last >= 0 && runs[last].State == state {
			runs[last].End = index
			continue
		}
		runs = append(runs, pieceRun{Begin: index, End: index, State: state})
	}
	return runs
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
				Source:       sourceName(peer.Source),
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
	case "pieces":
		states, ready := t.PieceStates()
		if !ready {
			writeJSON(w, http.StatusOK, map[string]any{"piece_count": 0, "runs": []pieceRun{}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"piece_count": len(states), "runs": compressPieceRuns(states)})
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
	var v2 string
	for _, xt := range parsed.Query()["xt"] {
		lower := strings.ToLower(xt)
		if value, found := strings.CutPrefix(lower, "urn:btih:"); found {
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
			continue
		}
		// A v2-only magnet is identified by the first 20 bytes of its btmh
		// digest, the same truncated hash the engine uses on the wire.
		if value, found := strings.CutPrefix(lower, "urn:btmh:1220"); found {
			if raw, err := hex.DecodeString(value); err == nil && len(raw) >= 20 {
				v2 = hex.EncodeToString(raw[:20])
			}
		}
	}
	return v2, v2 != ""
}

// handleIPFilter reloads the IP filter. Form fields: `url` (downloaded and
// decoded) or `path` (local file); with neither it reloads the configured file.
func (d *Daemon) handleIPFilter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.FormValue("path"))
	if rawURL := strings.TrimSpace(r.FormValue("url")); rawURL != "" {
		fetched, err := d.fetchIPFilterURL(rawURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, err)
			return
		}
		path = fetched
	}
	rules, err := d.loadIPFilter(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// magnetIsV2Only reports a magnet that carries only a v2 (btmh) hash.
func magnetIsV2Only(magnet string) bool {
	lower := strings.ToLower(magnet)
	return strings.Contains(lower, "urn:btmh:") && !strings.Contains(lower, "urn:btih:")
}

// normalizeMagnet makes the v1 (btih) hash the first xt and drops any
// btmh. The engine's magnet parser reads only the first xt and rejects a v2
// multihash, so a hybrid magnet that lists btmh before btih would otherwise be
// refused even though its v1 side is downloadable.
func normalizeMagnet(magnet string) string {
	parsed, err := url.Parse(strings.TrimSpace(magnet))
	if err != nil || parsed.Scheme != "magnet" {
		return magnet
	}
	query := parsed.Query()
	v1 := ""
	for _, xt := range query["xt"] {
		if strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
			v1 = xt
			break
		}
	}
	if v1 == "" {
		// No v1 hash: leave it untouched (a v2-only magnet is rejected earlier).
		return magnet
	}
	ordered := url.Values{}
	ordered.Set("xt", v1)
	for key, values := range query {
		if key == "xt" {
			continue
		}
		for _, value := range values {
			ordered.Add(key, value)
		}
	}
	parsed.RawQuery = ordered.Encode()
	return parsed.String()
}

// torrentIsV2Only reports a .torrent whose info has "meta version" 2 and no
// v1 "pieces" (hybrids carry both).
func torrentIsV2Only(data []byte) bool {
	return bytes.Contains(data, []byte("12:meta versioni2e")) && !bytes.Contains(data, []byte("6:pieces"))
}
