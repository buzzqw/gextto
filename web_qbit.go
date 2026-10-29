package gextto

// web_qbit.go answers a subset of the qBittorrent Web API (/api/v2) on
// Gextto's own address, so the browser extensions written for qBittorrent
// (Torrent Control and similar) can send a magnet link or a .torrent file
// straight to Gextto: the item enters the queue and the active engine (gx-core
// by default) downloads it, exactly as if it had been added from the interface.
//
// It is a compatibility subset, not a qBittorrent replacement: only the calls a
// browser extension needs are answered (login, add, list, pause, resume,
// delete, recheck and a few informational endpoints). Categories, tags and the
// content layout are refused with an explicit error instead of being ignored:
// in Gextto a category is a download tag of the queue and the content layout
// changes the folders on disk, so a silent no-op would put the torrent
// somewhere else than the client asked. Gextto's own API (/api/...) stays the
// complete one.

import (
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/models"
)

const (
	// qbitSIDCookie is the session cookie qBittorrent clients expect.
	qbitSIDCookie = "SID"
	// qbitWebAPIVersion is the Web API version this subset declares. Clients
	// use it only to pick the request flavour, so it names a qBittorrent
	// release whose calls are answered here.
	qbitWebAPIVersion = "2.9.3"
	// qbitOKBody and qbitFailBody are the plain-text answers qBittorrent sends:
	// clients compare them literally.
	qbitOKBody   = "Ok."
	qbitFailBody = "Fails."
	// qbitInfiniteETA is the ETA qBittorrent reports when it has no estimate.
	qbitInfiniteETA = int64(8640000)
	// qbitMaxTorrentBytes is the largest .torrent accepted from a client.
	qbitMaxTorrentBytes = 5 << 20
)

// registerQbitCompatRoutes installs the qBittorrent-compatible subset.
func registerQbitCompatRoutes(s *AppState, mux *http.ServeMux) {
	handle(s, mux, "POST /api/v2/auth/login", QbitCompatLogin)
	handle(s, mux, "POST /api/v2/auth/logout", QbitCompatLogout)
	handle(s, mux, "GET /api/v2/app/version", QbitCompatVersion)
	handle(s, mux, "GET /api/v2/app/webapiVersion", QbitCompatWebAPIVersion)
	handle(s, mux, "GET /api/v2/app/defaultSavePath", QbitCompatDefaultSavePath)
	handle(s, mux, "GET /api/v2/transfer/info", QbitCompatTransferInfo)
	handle(s, mux, "GET /api/v2/torrents/info", QbitCompatTorrentsInfo)
	handle(s, mux, "POST /api/v2/torrents/add", QbitCompatAdd)
	handle(s, mux, "POST /api/v2/torrents/delete", QbitCompatDelete)
	handle(s, mux, "POST /api/v2/torrents/pause", QbitCompatPause)
	handle(s, mux, "POST /api/v2/torrents/resume", QbitCompatResume)
	handle(s, mux, "POST /api/v2/torrents/stop", QbitCompatPause)
	handle(s, mux, "POST /api/v2/torrents/start", QbitCompatResume)
	handle(s, mux, "POST /api/v2/torrents/recheck", QbitCompatRecheck)
}

// qbitText writes the plain-text body of the qBittorrent API.
func qbitText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// QbitCompatLogin handles POST /api/v2/auth/login. The credentials are the ones
// of Gextto's access control; when that is off (or the client is on the local
// network with the local exemption on) any credentials are accepted, as a
// qBittorrent without a password does. A successful login leaves the SID cookie
// the client echoes on the following calls.
func QbitCompatLogin(w http.ResponseWriter, r *http.Request, s *AppState) {
	auth := authSettingsFrom(latestConfig(s))
	client := authClientAddress(r)
	now := time.Now()
	if authLoginBlocked(client, now) {
		qbitText(w, http.StatusTooManyRequests, qbitFailBody)
		return
	}
	credentialsOK := auth.usernameMatches(r.FormValue("username")) && auth.passwordMatches(r.FormValue("password"))
	if auth.active() && !credentialsOK && !(auth.localBypass && requestIsLocal(r)) {
		authLoginFailed(client, now)
		logging.Warn(fmt.Sprintf("Failed qBittorrent API login from %s", client))
		qbitText(w, http.StatusOK, qbitFailBody)
		return
	}
	if auth.active() && credentialsOK {
		expires := now.Add(authSessionLifetime)
		http.SetCookie(w, &http.Cookie{
			Name:     qbitSIDCookie,
			Value:    auth.sessionToken(expires),
			Path:     "/",
			Expires:  expires,
			HttpOnly: true,
			Secure:   requestIsHTTPS(r),
			SameSite: http.SameSiteLaxMode,
		})
	}
	qbitText(w, http.StatusOK, qbitOKBody)
}

// QbitCompatLogout handles POST /api/v2/auth/logout.
func QbitCompatLogout(w http.ResponseWriter, r *http.Request, s *AppState) {
	http.SetCookie(w, &http.Cookie{
		Name:     qbitSIDCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	qbitText(w, http.StatusOK, qbitOKBody)
}

// QbitCompatVersion handles GET /api/v2/app/version.
func QbitCompatVersion(w http.ResponseWriter, r *http.Request, s *AppState) {
	qbitText(w, http.StatusOK, "v"+constants.VersionLabel())
}

// QbitCompatWebAPIVersion handles GET /api/v2/app/webapiVersion.
func QbitCompatWebAPIVersion(w http.ResponseWriter, r *http.Request, s *AppState) {
	qbitText(w, http.StatusOK, qbitWebAPIVersion)
}

// QbitCompatDefaultSavePath handles GET /api/v2/app/defaultSavePath.
func QbitCompatDefaultSavePath(w http.ResponseWriter, r *http.Request, s *AppState) {
	qbitText(w, http.StatusOK, latestConfig(s).DownloadDir)
}

// QbitCompatTransferInfo handles GET /api/v2/transfer/info: the global speeds a
// client shows in its header.
func QbitCompatTransferInfo(w http.ResponseWriter, r *http.Request, s *AppState) {
	var download, upload uint64
	for _, torrent := range s.activeEngine().List() {
		download += torrent.DownloadRate
		upload += torrent.UploadRate
	}
	jsonResponse(w, map[string]any{
		"dl_info_speed":     download,
		"up_info_speed":     upload,
		"dl_info_data":      0,
		"up_info_data":      0,
		"dl_rate_limit":     0,
		"up_rate_limit":     0,
		"dht_nodes":         0,
		"connection_status": "connected",
	})
}

// QbitCompatTorrentsInfo handles GET /api/v2/torrents/info. Only `hashes` and
// the common values of `filter` are honoured; sorting, paging and the
// category/tag filters are not part of the subset.
func QbitCompatTorrentsInfo(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := r.URL.Query()
	wanted := qbitHashSet(query.Get("hashes"))
	filter := strings.ToLower(strings.TrimSpace(query.Get("filter")))
	torrents := []map[string]any{}
	for _, torrent := range s.activeEngine().List() {
		if len(wanted) > 0 && !wanted[strings.ToLower(strings.TrimSpace(torrent.Hash))] {
			continue
		}
		if !qbitFilterMatches(filter, torrent) {
			continue
		}
		torrents = append(torrents, qbitTorrentPayload(torrent))
	}
	jsonResponse(w, torrents)
}

// QbitCompatAdd handles POST /api/v2/torrents/add: `urls` (magnets and http(s)
// links) and the uploaded `.torrent` files, with the options a browser
// extension sends.
func QbitCompatAdd(w http.ResponseWriter, r *http.Request, s *AppState) {
	if !SetupComplete(s.cfg) {
		qbitText(w, http.StatusConflict, qbitFailBody)
		return
	}
	if err := qbitParseAddRequest(r); err != nil {
		qbitText(w, http.StatusBadRequest, qbitFailBody)
		return
	}
	if unsupported := qbitUnsupportedAddOption(r); unsupported != "" {
		logging.Warn("a qBittorrent client asked for a torrent option this API does not support; the torrent was not added", "option", unsupported)
		qbitText(w, http.StatusBadRequest, qbitFailBody)
		return
	}
	cfg := latestConfig(s)
	options := AddOptions{
		Paused:      qbitFormBool(r, "stopped") || qbitFormBool(r, "paused"),
		Sequential:  qbitFormBool(r, "sequentialDownload"),
		FirstLast:   qbitFormBool(r, "firstLastPiecePrio"),
		SeedMode:    qbitFormBool(r, "skip_checking"),
		Preallocate: cfg.TorrentPreallocate(),
	}
	preferred := qbitPreferredPath(r)

	added, duplicates := 0, 0
	var firstErr error
	record := func(addedTorrent bool, err error) {
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
		case addedTorrent:
			added++
		default:
			duplicates++
		}
	}
	for _, link := range qbitLinks(r) {
		record(qbitAddLink(r.Context(), s, cfg, link, preferred, options))
	}
	for _, header := range qbitUploadedFiles(r) {
		record(qbitAddUploadedFile(s, cfg, header, preferred, options))
	}
	switch {
	case added > 0:
		qbitText(w, http.StatusOK, qbitOKBody)
	case duplicates > 0:
		// Torrent Control reads 409 as "this torrent is already there".
		qbitText(w, http.StatusConflict, qbitFailBody)
	case firstErr != nil:
		logging.Warn("a torrent sent by a qBittorrent client could not be added", "error", firstErr.Error())
		qbitText(w, http.StatusBadRequest, qbitFailBody)
	default:
		qbitText(w, http.StatusBadRequest, qbitFailBody)
	}
}

// QbitCompatDelete handles POST /api/v2/torrents/delete.
func QbitCompatDelete(w http.ResponseWriter, r *http.Request, s *AppState) {
	if err := r.ParseForm(); err != nil {
		qbitText(w, http.StatusBadRequest, qbitFailBody)
		return
	}
	cfg := latestConfig(s)
	deleteFiles := qbitFormBool(r, "deleteFiles")
	removed, firstErr := 0, error(nil)
	for _, hash := range qbitRequestedHashes(r, s) {
		ok, err := SafeRemoveTorrent(s, cfg, hash, deleteFiles)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if ok {
			removed++
		}
	}
	if removed == 0 && firstErr != nil {
		logging.Warn("a qBittorrent client asked to remove a torrent that could not be removed", "error", firstErr.Error())
		qbitText(w, http.StatusBadRequest, qbitFailBody)
		return
	}
	qbitText(w, http.StatusOK, qbitOKBody)
}

// QbitCompatPause handles POST /api/v2/torrents/pause and its 5.x alias stop.
func QbitCompatPause(w http.ResponseWriter, r *http.Request, s *AppState) {
	qbitTorrentAction(w, r, s, "pause")
}

// QbitCompatResume handles POST /api/v2/torrents/resume and its 5.x alias start.
func QbitCompatResume(w http.ResponseWriter, r *http.Request, s *AppState) {
	qbitTorrentAction(w, r, s, "resume")
}

// QbitCompatRecheck handles POST /api/v2/torrents/recheck.
func QbitCompatRecheck(w http.ResponseWriter, r *http.Request, s *AppState) {
	qbitTorrentAction(w, r, s, "recheck")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// qbitTorrentAction applies pause, resume or recheck to the hashes of the
// request (all of them when the client sends none).
func qbitTorrentAction(w http.ResponseWriter, r *http.Request, s *AppState, action string) {
	if err := r.ParseForm(); err != nil {
		qbitText(w, http.StatusBadRequest, qbitFailBody)
		return
	}
	hashes := qbitRequestedHashes(r, s)
	if len(hashes) == 0 {
		// qBittorrent answers Ok even for an unknown hash: the client is only
		// showing that the torrent is no longer there.
		qbitText(w, http.StatusOK, qbitOKBody)
		return
	}
	failed := error(nil)
	for _, hash := range hashes {
		var err error
		switch action {
		case "pause":
			_, err = s.activeEngine().Pause(hash)
		case "resume":
			_, err = s.activeEngine().Resume(hash)
		default:
			_, err = s.activeEngine().ForceRecheck(hash)
		}
		if err != nil && failed == nil {
			failed = err
		}
	}
	if failed != nil {
		logging.Warn("a torrent action asked by a qBittorrent client failed", "action", action, "error", failed.Error())
		qbitText(w, http.StatusBadRequest, qbitFailBody)
		return
	}
	qbitText(w, http.StatusOK, qbitOKBody)
}

// qbitParseAddRequest reads the form, multipart or not.
func qbitParseAddRequest(r *http.Request) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && mediaType == "multipart/form-data" {
		return r.ParseMultipartForm(maxRequestBodyBytes)
	}
	return r.ParseForm()
}

// qbitUnsupportedAddOption names the first option of the request that this
// subset cannot honour, or "" when there is none.
func qbitUnsupportedAddOption(r *http.Request) string {
	if strings.TrimSpace(r.FormValue("category")) != "" {
		return "category"
	}
	if strings.TrimSpace(r.FormValue("tags")) != "" {
		return "tags"
	}
	if layout := strings.TrimSpace(r.FormValue("contentLayout")); layout != "" && !strings.EqualFold(layout, "Original") {
		return "contentLayout"
	}
	return ""
}

// qbitPreferredPath is the save path the client asked for; nil means "the one
// the engine and the configuration decide", exactly like Gextto's own add.
func qbitPreferredPath(r *http.Request) *string {
	if requested := strings.TrimSpace(r.FormValue("savepath")); requested != "" {
		return &requested
	}
	return nil
}

// qbitLinks returns the magnet and http(s) links of the `urls` field (one per
// line, as qBittorrent sends them).
func qbitLinks(r *http.Request) []string {
	links := []string{}
	for _, line := range strings.Split(r.FormValue("urls"), "\n") {
		if link := strings.TrimSpace(line); link != "" {
			links = append(links, link)
		}
	}
	return links
}

// qbitUploadedFiles returns the .torrent files of the request, whichever field
// name the client used.
func qbitUploadedFiles(r *http.Request) []*multipart.FileHeader {
	if r.MultipartForm == nil {
		return nil
	}
	files := []*multipart.FileHeader{}
	for _, field := range []string{"fileselect", "torrents", "files"} {
		files = append(files, r.MultipartForm.File[field]...)
	}
	return files
}

// qbitAddLink adds one link: a magnet goes straight to the engine, an http(s)
// address is a .torrent to fetch first (as the browser handlers do).
func qbitAddLink(ctx context.Context, s *AppState, cfg *Config, link string, preferred *string, options AddOptions) (bool, error) {
	link = strings.TrimSpace(link)
	if strings.HasPrefix(link, "magnet:") {
		return s.activeEngine().AddWithOptions(link, cfg, preferred, options)
	}
	if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
		return false, fmt.Errorf("a magnet link or an http(s) address is required")
	}
	if cfg.DryRun {
		return false, fmt.Errorf("dry-run does not download torrent files")
	}
	payload, status, err := HTTPGetBytes(ctx, link, nil)
	if err != nil {
		return false, fmt.Errorf("the .torrent source is unreachable: %w", err)
	}
	if status < 200 || status >= 300 {
		return false, fmt.Errorf("the .torrent source answered %d", status)
	}
	if len(payload) == 0 || len(payload) > qbitMaxTorrentBytes {
		return false, fmt.Errorf("the .torrent source returned an empty or too large file")
	}
	return qbitAddTorrentBytes(s, cfg, payload, preferred, options)
}

// qbitAddUploadedFile adds one uploaded .torrent.
func qbitAddUploadedFile(s *AppState, cfg *Config, header *multipart.FileHeader, preferred *string, options AddOptions) (bool, error) {
	if header.Size <= 0 || header.Size > qbitMaxTorrentBytes {
		return false, fmt.Errorf("the .torrent file is empty or too large")
	}
	source, err := header.Open()
	if err != nil {
		return false, fmt.Errorf("the .torrent file is not readable: %w", err)
	}
	defer source.Close()
	payload, err := io.ReadAll(io.LimitReader(source, qbitMaxTorrentBytes+1))
	if err != nil {
		return false, fmt.Errorf("the .torrent file is not readable: %w", err)
	}
	if len(payload) == 0 || len(payload) > qbitMaxTorrentBytes {
		return false, fmt.Errorf("the .torrent file is empty or too large")
	}
	return qbitAddTorrentBytes(s, cfg, payload, preferred, options)
}

// qbitAddTorrentBytes stages a .torrent in the temporary folder (the engine
// reads it from disk) and hands it to the engine.
func qbitAddTorrentBytes(s *AppState, cfg *Config, payload []byte, preferred *string, options AddOptions) (bool, error) {
	dir := filepath.Join(cfg.DataDir, "incomplete")
	if cfg.DownloadTempDir != nil {
		dir = *cfg.DownloadTempDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	path := filepath.Join(dir, fmt.Sprintf("qbit-%s.torrent", backupUUID()))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return false, err
	}
	defer os.Remove(path)
	hash, err := s.activeEngine().AddTorrentFileWithOptions(path, cfg, preferred, options)
	if err != nil {
		return false, err
	}
	return hash != nil, nil
}

// qbitRequestedHashes returns the `hashes` field (pipe- or newline-separated);
// "all" and an empty value mean every torrent of the session.
func qbitRequestedHashes(r *http.Request, s *AppState) []string {
	raw := strings.TrimSpace(r.FormValue("hashes"))
	if raw == "" || strings.EqualFold(raw, "all") {
		hashes := []string{}
		for _, torrent := range s.activeEngine().List() {
			hashes = append(hashes, strings.ToLower(torrent.Hash))
		}
		return hashes
	}
	set := qbitHashSet(raw)
	hashes := make([]string, 0, len(set))
	for hash := range set {
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	return hashes
}

// qbitHashSet splits the `hashes` field into lowercase hashes.
func qbitHashSet(raw string) map[string]bool {
	set := map[string]bool{}
	for _, hash := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '|' || r == '\n' || r == ',' || r == ' '
	}) {
		if trimmed := strings.ToLower(strings.TrimSpace(hash)); trimmed != "" {
			set[trimmed] = true
		}
	}
	return set
}

// qbitFormBool reads a boolean form field the way qBittorrent clients send it.
func qbitFormBool(r *http.Request, key string) bool {
	switch strings.ToLower(strings.TrimSpace(r.FormValue(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// qbitProgress is the completion of a torrent in qBittorrent's 0..1 scale.
// Gextto reports it in percent (0..100, as both engines do), so the value is
// normalised here, once, for every answer of this API.
func qbitProgress(torrent models.TorrentView) float64 {
	progress := torrent.Progress
	if progress > 1 {
		progress /= 100
	}
	if progress < 0 {
		return 0
	}
	if progress > 1 {
		return 1
	}
	return progress
}

// qbitFilterMatches applies the `filter` values a client uses on the list.
func qbitFilterMatches(filter string, torrent models.TorrentView) bool {
	complete := qbitProgress(torrent) >= 0.9999 || torrent.IsSeeding
	switch filter {
	case "", "all":
		return true
	case "downloading":
		return !complete && torrent.State != "paused" && torrent.State != "error"
	case "seeding":
		return complete && torrent.State != "paused"
	case "completed":
		return complete
	case "paused", "stopped":
		return torrent.State == "paused"
	case "active":
		return torrent.DownloadRate > 0 || torrent.UploadRate > 0
	case "inactive":
		return torrent.DownloadRate == 0 && torrent.UploadRate == 0
	case "errored":
		return torrent.State == "error"
	case "stalled", "stalled_downloading", "stalled_uploading":
		if filter == "stalled_uploading" && !complete {
			return false
		}
		if filter == "stalled_downloading" && complete {
			return false
		}
		return torrent.Stalled || torrent.State == "stalled"
	}
	// An unknown filter lists everything rather than hiding torrents the
	// interface shows.
	return true
}

// qbitTorrentPayload renders one torrent the way qBittorrent clients read it.
func qbitTorrentPayload(torrent models.TorrentView) map[string]any {
	left := torrent.TotalSize - torrent.TotalDone
	if left < 0 {
		left = 0
	}
	eta := qbitInfiniteETA
	if torrent.EtaSeconds > 0 {
		eta = torrent.EtaSeconds
	}
	ratio := 0.0
	if torrent.AllTimeDownload > 0 {
		ratio = float64(torrent.AllTimeUpload) / float64(torrent.AllTimeDownload)
	}
	progress := qbitProgress(torrent)
	return map[string]any{
		"hash":           strings.ToLower(torrent.Hash),
		"name":           torrent.Name,
		"size":           torrent.TotalSize,
		"total_size":     torrent.TotalSize,
		"progress":       progress,
		"dlspeed":        torrent.DownloadRate,
		"upspeed":        torrent.UploadRate,
		"state":          qbitState(torrent),
		"save_path":      torrent.SavePath,
		"amount_left":    left,
		"downloaded":     torrent.TotalDone,
		"uploaded":       torrent.AllTimeUpload,
		"ratio":          ratio,
		"eta":            eta,
		"num_seeds":      torrent.NumSeeds,
		"num_leechs":     torrent.NumPeers,
		"num_complete":   torrent.NumComplete,
		"num_incomplete": torrent.NumIncomplete,
		"tracker":        torrent.CurrentTracker,
		"added_on":       torrent.AddedAt,
		"completion_on":  torrent.CompletedAt,
		"seeding_time":   torrent.SeedingSeconds,
		"time_active":    torrent.ActiveSeconds,
		"category":       "",
		"tags":           "",
		"auto_tmm":       torrent.AutoManaged,
		"force_start":    torrent.Pinned,
		"seq_dl":         torrent.Sequential,
		"dl_limit":       torrent.DownloadLimit,
		"up_limit":       torrent.UploadLimit,
		"error":          torrent.Error,
	}
}

// qbitState translates Gextto's state into the qBittorrent names clients know.
func qbitState(torrent models.TorrentView) string {
	complete := qbitProgress(torrent) >= 0.9999 || torrent.IsSeeding
	switch torrent.State {
	case "seeding":
		return "seeding"
	case "downloading":
		return "downloading"
	case "downloading_metadata":
		return "metaDL"
	case "checking_files":
		if complete {
			return "checkingUP"
		}
		return "checkingDL"
	case "moving":
		return "moving"
	case "error":
		return "error"
	case "stalled":
		if complete {
			return "stalledUP"
		}
		return "stalledDL"
	case "paused":
		if complete {
			return "pausedUP"
		}
		return "pausedDL"
	}
	return torrent.State
}
