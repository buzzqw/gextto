package main

// api_qbit.go implements a qBittorrent-compatible Web API (the /api/v2 subset
// Sonarr, Radarr and the mobile apps speak), so those clients can drive the
// standalone daemon. It is registered only in standalone mode: in managed mode
// Gextto is the single master and the API is not exposed.

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	qbitSIDCookie   = "SID"
	qbitWebAPIVer   = "2.9.3"
	qbitUnknownETA  = int64(8640000)
	qbitInvalidCode = "Fails."
	qbitOKCode      = "Ok."
)

// routesQbit registers the qBittorrent-compatible API on its own mux. The
// caller decides whether to mount it (standalone only).
func (d *Daemon) routesQbit() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", d.handleQbitLogin)
	mux.HandleFunc("POST /api/v2/auth/logout", d.handleQbitLogout)
	mux.HandleFunc("GET /api/v2/app/version", d.handleQbitVersion)
	mux.HandleFunc("GET /api/v2/app/webapiVersion", d.handleQbitWebAPIVersion)
	mux.HandleFunc("GET /api/v2/transfer/info", d.handleQbitTransferInfo)
	mux.HandleFunc("GET /api/v2/torrents/info", d.handleQbitTorrentsInfo)
	mux.HandleFunc("POST /api/v2/torrents/add", d.handleQbitAdd)
	mux.HandleFunc("POST /api/v2/torrents/delete", d.handleQbitDelete)
	mux.HandleFunc("POST /api/v2/torrents/pause", d.handleQbitPause)
	mux.HandleFunc("POST /api/v2/torrents/resume", d.handleQbitResume)
	mux.HandleFunc("POST /api/v2/torrents/recheck", d.handleQbitRecheck)
	mux.HandleFunc("GET /api/v2/torrents/categories", d.handleQbitCategories)
	mux.HandleFunc("POST /api/v2/torrents/createCategory", d.handleQbitCreateCategory)
	mux.HandleFunc("POST /api/v2/torrents/removeCategories", d.handleQbitRemoveCategories)
	mux.HandleFunc("POST /api/v2/torrents/setCategory", d.handleQbitSetCategory)
	mux.HandleFunc("GET /api/v2/torrents/tags", d.handleQbitTags)
	mux.HandleFunc("POST /api/v2/torrents/createTags", d.handleQbitCreateTags)
	mux.HandleFunc("POST /api/v2/torrents/deleteTags", d.handleQbitDeleteTags)
	mux.HandleFunc("POST /api/v2/torrents/addTags", d.handleQbitAddTags)
	mux.HandleFunc("POST /api/v2/torrents/removeTags", d.handleQbitRemoveTags)
	return d.qbitAuth(mux)
}

// qbitAuth requires a valid SID session for every call but the login. With no
// password configured the API is open (fresh standalone install).
func (d *Daemon) qbitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/auth/login" || !d.standaloneAuthActive() {
			next.ServeHTTP(w, r)
			return
		}
		if cookie, err := r.Cookie(qbitSIDCookie); err == nil && d.sessions != nil && d.sessions.Valid(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
}

func qbitText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (d *Daemon) handleQbitLogin(w http.ResponseWriter, r *http.Request) {
	if !d.loginMatches(strings.TrimSpace(r.FormValue("username")), r.FormValue("password")) {
		// qBittorrent answers 200 with "Fails." and no cookie.
		qbitText(w, http.StatusOK, qbitInvalidCode)
		return
	}
	token := d.sessions.Create()
	http.SetCookie(w, &http.Cookie{
		Name: qbitSIDCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: int(d.sessions.TTL().Seconds()),
	})
	qbitText(w, http.StatusOK, qbitOKCode)
}

func (d *Daemon) handleQbitLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(qbitSIDCookie); err == nil && d.sessions != nil {
		d.sessions.Drop(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: qbitSIDCookie, Value: "", Path: "/", MaxAge: -1})
	qbitText(w, http.StatusOK, qbitOKCode)
}

func (d *Daemon) handleQbitVersion(w http.ResponseWriter, _ *http.Request) {
	qbitText(w, http.StatusOK, "v"+runtimeVersion())
}

func (d *Daemon) handleQbitWebAPIVersion(w http.ResponseWriter, _ *http.Request) {
	qbitText(w, http.StatusOK, qbitWebAPIVer)
}

func (d *Daemon) handleQbitTransferInfo(w http.ResponseWriter, _ *http.Request) {
	stats := d.stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"dl_info_speed":     stats.DownloadRate,
		"up_info_speed":     stats.UploadRate,
		"dl_info_data":      stats.Session["bytes_downloaded"],
		"up_info_data":      stats.Session["bytes_uploaded"],
		"connection_status": "connected",
	})
}

// qbitTorrent is the subset of the qBittorrent torrent object the clients read.
type qbitTorrent struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	TotalSize    int64   `json:"total_size"`
	Progress     float64 `json:"progress"`
	DLRate       int64   `json:"dlspeed"`
	ULRate       int64   `json:"upspeed"`
	State        string  `json:"state"`
	SavePath     string  `json:"save_path"`
	Category     string  `json:"category"`
	Tags         string  `json:"tags"`
	NumSeeds     int     `json:"num_seeds"`
	NumLeechs    int     `json:"num_leechs"`
	Ratio        float64 `json:"ratio"`
	ETA          int64   `json:"eta"`
	AddedOn      int64   `json:"added_on"`
	CompletionOn int64   `json:"completion_on"`
	AmountLeft   int64   `json:"amount_left"`
	Downloaded   int64   `json:"downloaded"`
	Uploaded     int64   `json:"uploaded"`
	Priority     int     `json:"priority"`
	SeqDL        bool    `json:"seq_dl"`
	FLPiecePrio  bool    `json:"f_l_piece_prio"`
	ForceStart   bool    `json:"force_start"`
	AutoTMM      bool    `json:"auto_tmm"`
	MagnetURI    string  `json:"magnet_uri"`
}

func (d *Daemon) handleQbitTorrentsInfo(w http.ResponseWriter, _ *http.Request) {
	views := d.snapshotViews()
	out := make([]qbitTorrent, 0, len(views))
	for _, v := range views {
		out = append(out, qbitView(v))
	}
	writeJSON(w, http.StatusOK, out)
}

func qbitView(v torrentInfo) qbitTorrent {
	ratio := 0.0
	if v.TotalSize > 0 {
		ratio = float64(v.Uploaded) / float64(v.TotalSize)
	}
	eta := v.ETASeconds
	if eta <= 0 || eta > qbitUnknownETA {
		eta = qbitUnknownETA
	}
	return qbitTorrent{
		Hash:         strings.ToLower(v.Hash),
		Name:         v.Name,
		Size:         v.TotalSize,
		TotalSize:    v.TotalSize,
		Progress:     v.Progress / 100,
		DLRate:       v.DownloadRate,
		ULRate:       v.UploadRate,
		State:        qbitState(v.State),
		SavePath:     v.SavePath,
		Category:     v.Category,
		Tags:         strings.Join(v.Tags, ", "),
		NumSeeds:     v.NumSeeds,
		NumLeechs:    v.NumPeers,
		Ratio:        ratio,
		ETA:          eta,
		AddedOn:      v.AddedAt,
		CompletionOn: v.CompletedAt,
		AmountLeft:   v.TotalSize - v.TotalDone,
		Downloaded:   v.Downloaded,
		Uploaded:     v.Uploaded,
		Priority:     0,
		SeqDL:        v.Sequential,
		FLPiecePrio:  v.FirstLast,
		ForceStart:   v.Pinned,
		AutoTMM:      false,
	}
}

// qbitState maps the daemon's state to the qBittorrent names the clients
// understand.
func qbitState(state string) string {
	switch state {
	case "downloading", "downloading_metadata":
		return "downloading"
	case "seeding":
		return "uploading"
	case "checking_files":
		return "checkingDL"
	case "paused":
		return "pausedDL"
	case "stalled":
		return "stalledDL"
	case "moving":
		return "moving"
	case "error":
		return "error"
	default:
		return "unknown"
	}
}

// qbitHashes parses the "hashes" parameter: "all", a pipe/comma separated list,
// or repeated values. Lower-cased.
func (d *Daemon) qbitHashes(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.EqualFold(value, "all") {
		views := d.snapshotViews()
		out := make([]string, 0, len(views))
		for _, v := range views {
			out = append(out, strings.ToLower(v.Hash))
		}
		return out
	}
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == '|' || r == ',' || r == ' ' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, strings.ToLower(field))
		}
	}
	return out
}

func (d *Daemon) handleQbitPause(w http.ResponseWriter, r *http.Request) {
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.pause(hash)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitResume(w http.ResponseWriter, r *http.Request) {
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.resume(hash)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRecheck(w http.ResponseWriter, r *http.Request) {
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.verify(hash)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitDelete(w http.ResponseWriter, r *http.Request) {
	deleteFiles := strings.EqualFold(strings.TrimSpace(r.FormValue("deleteFiles")), "true")
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.remove(hash, deleteFiles)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitAdd accepts the "urls" field (magnet links and .torrent URLs), one
// per line, plus savepath and paused, the way Sonarr and Radarr send them.
func (d *Daemon) handleQbitAdd(w http.ResponseWriter, r *http.Request) {
	savePath := strings.TrimSpace(r.FormValue("savepath"))
	category := strings.TrimSpace(r.FormValue("category"))
	tags := qbitList(r.FormValue("tags"))
	if savePath == "" && category != "" {
		// A category carries its own save path, like qBittorrent.
		savePath = d.categorySavePath(category)
	}
	paused := strings.EqualFold(strings.TrimSpace(r.FormValue("paused")), "true")
	urls := r.FormValue("urls")
	added := false
	for _, raw := range strings.FieldsFunc(urls, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		req := addRequest{Destination: savePath, Paused: paused, Category: category, Tags: tags}
		switch {
		case strings.HasPrefix(strings.ToLower(line), "magnet:"):
			req.Magnet = line
		case strings.HasPrefix(strings.ToLower(line), "http://"), strings.HasPrefix(strings.ToLower(line), "https://"):
			data, err := d.fetchTorrentURL(line)
			if err != nil {
				qbitText(w, http.StatusUnsupportedMediaType, qbitInvalidCode)
				return
			}
			req.TorrentData = data
		default:
			continue
		}
		if _, _, err := d.add(req); err != nil {
			qbitText(w, http.StatusUnsupportedMediaType, qbitInvalidCode)
			return
		}
		added = true
	}
	if !added {
		qbitText(w, http.StatusUnsupportedMediaType, qbitInvalidCode)
		return
	}
	qbitText(w, http.StatusOK, qbitOKCode)
}

// fetchTorrentURL downloads a .torrent from a URL, with the same size cap the
// API applies to an uploaded file.
func (d *Daemon) fetchTorrentURL(rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torrent URL returned %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxTorrentFile))
}

// qbitList splits a qBittorrent list parameter (comma, pipe or newline).
func qbitList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '|' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

// qbitCategory is the category object the clients read.
type qbitCategory struct {
	Name     string `json:"name"`
	SavePath string `json:"savePath"`
}

func (d *Daemon) handleQbitCategories(w http.ResponseWriter, _ *http.Request) {
	categories := d.categoriesSnapshot()
	out := make(map[string]qbitCategory, len(categories))
	for name, savePath := range categories {
		out[name] = qbitCategory{Name: name, SavePath: savePath}
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *Daemon) handleQbitCreateCategory(w http.ResponseWriter, r *http.Request) {
	d.createCategory(r.FormValue("category"), r.FormValue("savePath"))
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRemoveCategories(w http.ResponseWriter, r *http.Request) {
	d.removeCategories(qbitList(r.FormValue("categories")))
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitSetCategory(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.FormValue("category"))
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.setCategory(hash, category)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitTags(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, d.tagsSnapshot())
}

func (d *Daemon) handleQbitCreateTags(w http.ResponseWriter, r *http.Request) {
	d.createTags(qbitList(r.FormValue("tags")))
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitDeleteTags(w http.ResponseWriter, r *http.Request) {
	d.deleteTags(qbitList(r.FormValue("tags")))
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitAddTags(w http.ResponseWriter, r *http.Request) {
	tags := qbitList(r.FormValue("tags"))
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.addTags(hash, tags)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRemoveTags(w http.ResponseWriter, r *http.Request) {
	tags := qbitList(r.FormValue("tags"))
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.removeTags(hash, tags)
	}
	w.WriteHeader(http.StatusOK)
}
