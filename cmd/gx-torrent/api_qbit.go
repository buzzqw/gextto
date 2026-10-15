package main

// api_qbit.go implements a qBittorrent-compatible Web API (the /api/v2 subset
// Sonarr, Radarr and the mobile apps speak), so those clients can drive the
// standalone daemon. It is registered only in standalone mode: in managed mode
// Gextto is the single master and the API is not exposed.

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
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
	mux.HandleFunc("GET /api/v2/app/defaultSavePath", d.handleQbitDefaultSavePath)
	mux.HandleFunc("GET /api/v2/app/preferences", d.handleQbitPreferences)
	mux.HandleFunc("GET /api/v2/app/buildInfo", d.handleQbitBuildInfo)
	mux.HandleFunc("GET /api/v2/app/cookies", d.handleQbitGetCookies)
	mux.HandleFunc("POST /api/v2/app/setCookies", d.handleQbitSetCookies)
	mux.HandleFunc("GET /api/v2/transfer/info", d.handleQbitTransferInfo)
	mux.HandleFunc("GET /api/v2/transfer/downloadLimit", d.handleQbitDownloadLimit)
	mux.HandleFunc("GET /api/v2/transfer/uploadLimit", d.handleQbitUploadLimit)
	mux.HandleFunc("POST /api/v2/transfer/setDownloadLimit", d.handleQbitSetDownloadLimit)
	mux.HandleFunc("POST /api/v2/transfer/setUploadLimit", d.handleQbitSetUploadLimit)
	mux.HandleFunc("GET /api/v2/torrents/info", d.handleQbitTorrentsInfo)
	mux.HandleFunc("GET /api/v2/sync/maindata", d.handleQbitSync)
	mux.HandleFunc("GET /api/v2/torrents/properties", d.handleQbitProperties)
	mux.HandleFunc("GET /api/v2/torrents/files", d.handleQbitFiles)
	mux.HandleFunc("POST /api/v2/torrents/add", d.handleQbitAdd)
	mux.HandleFunc("POST /api/v2/torrents/delete", d.handleQbitDelete)
	mux.HandleFunc("POST /api/v2/torrents/pause", d.handleQbitPause)
	mux.HandleFunc("POST /api/v2/torrents/resume", d.handleQbitResume)
	mux.HandleFunc("POST /api/v2/torrents/stop", d.handleQbitPause)
	mux.HandleFunc("POST /api/v2/torrents/start", d.handleQbitResume)
	mux.HandleFunc("POST /api/v2/torrents/recheck", d.handleQbitRecheck)
	mux.HandleFunc("POST /api/v2/torrents/reannounce", d.handleQbitReannounce)
	mux.HandleFunc("POST /api/v2/torrents/setLocation", d.handleQbitSetLocation)
	mux.HandleFunc("GET /api/v2/torrents/trackers", d.handleQbitTrackers)
	mux.HandleFunc("GET /api/v2/torrents/categories", d.handleQbitCategories)
	mux.HandleFunc("POST /api/v2/torrents/createCategory", d.handleQbitCreateCategory)
	mux.HandleFunc("POST /api/v2/torrents/removeCategories", d.handleQbitRemoveCategories)
	mux.HandleFunc("POST /api/v2/torrents/setCategory", d.handleQbitSetCategory)
	mux.HandleFunc("GET /api/v2/torrents/tags", d.handleQbitTags)
	mux.HandleFunc("POST /api/v2/torrents/createTags", d.handleQbitCreateTags)
	mux.HandleFunc("POST /api/v2/torrents/deleteTags", d.handleQbitDeleteTags)
	mux.HandleFunc("POST /api/v2/torrents/addTags", d.handleQbitAddTags)
	mux.HandleFunc("POST /api/v2/torrents/removeTags", d.handleQbitRemoveTags)
	mux.HandleFunc("POST /api/v2/torrents/toggleSequentialDownload", d.handleQbitToggleSequential)
	mux.HandleFunc("POST /api/v2/torrents/setSuperSeeding", d.handleQbitSetSuperSeeding)
	mux.HandleFunc("POST /api/v2/torrents/addTrackers", d.handleQbitAddTrackers)
	mux.HandleFunc("POST /api/v2/torrents/removeTrackers", d.handleQbitRemoveTrackers)
	mux.HandleFunc("POST /api/v2/torrents/editTracker", d.handleQbitEditTracker)
	mux.HandleFunc("POST /api/v2/torrents/filePrio", d.handleQbitFilePrio)
	mux.HandleFunc("GET /api/v2/torrents/export", d.handleQbitExport)
	mux.HandleFunc("POST /api/v2/torrents/setForceStart", d.handleQbitSetForceStart)
	mux.HandleFunc("POST /api/v2/torrents/setAutoManagement", d.handleQbitSetAutoManagement)
	mux.HandleFunc("POST /api/v2/torrents/setDownloadLimit", d.handleQbitSetTorrentDownloadLimit)
	mux.HandleFunc("POST /api/v2/torrents/setUploadLimit", d.handleQbitSetTorrentUploadLimit)
	mux.HandleFunc("POST /api/v2/app/setPreferences", d.handleQbitSetPreferences)
	mux.HandleFunc("POST /api/v2/torrents/setShareLimits", d.handleQbitSetShareLimits)
	mux.HandleFunc("GET /api/v2/sync/torrentPeers", d.handleQbitTorrentPeers)
	// Read-side endpoints added for the qBittorrent API surface: piece
	// diagnostics, web seeds, the per-torrent limits in reading, the
	// alternative-limits state, the network interfaces and the logs.
	mux.HandleFunc("GET /api/v2/torrents/pieceStates", d.handleQbitPieceStates)
	mux.HandleFunc("GET /api/v2/torrents/pieceHashes", d.handleQbitPieceHashes)
	mux.HandleFunc("GET /api/v2/torrents/webseeds", d.handleQbitWebseeds)
	mux.HandleFunc("GET /api/v2/torrents/downloadLimit", d.handleQbitTorrentDownloadLimits)
	mux.HandleFunc("POST /api/v2/torrents/downloadLimit", d.handleQbitTorrentDownloadLimits)
	mux.HandleFunc("GET /api/v2/torrents/uploadLimit", d.handleQbitTorrentUploadLimits)
	mux.HandleFunc("POST /api/v2/torrents/uploadLimit", d.handleQbitTorrentUploadLimits)
	mux.HandleFunc("GET /api/v2/transfer/speedLimitsMode", d.handleQbitSpeedLimitsMode)
	mux.HandleFunc("POST /api/v2/transfer/toggleSpeedLimitsMode", d.handleQbitToggleSpeedLimitsMode)
	mux.HandleFunc("GET /api/v2/app/networkInterfaceList", d.handleQbitNetworkInterfaceList)
	mux.HandleFunc("GET /api/v2/app/networkInterfaceAddressList", d.handleQbitNetworkInterfaceAddressList)
	mux.HandleFunc("GET /api/v2/log/main", d.handleQbitLogMain)
	mux.HandleFunc("GET /api/v2/log/peers", d.handleQbitLogPeers)
	// Write endpoints: peers, category editing, piece priority, queue priority
	// and a clean shutdown.
	mux.HandleFunc("POST /api/v2/torrents/addPeers", d.handleQbitAddPeers)
	mux.HandleFunc("POST /api/v2/torrents/rename", d.handleQbitRename)
	mux.HandleFunc("POST /api/v2/torrents/renameFile", d.handleQbitRenameFile)
	mux.HandleFunc("POST /api/v2/torrents/renameFolder", d.handleQbitRenameFolder)
	mux.HandleFunc("POST /api/v2/transfer/banPeers", d.handleQbitBanPeers)
	mux.HandleFunc("POST /api/v2/torrents/editCategory", d.handleQbitEditCategory)
	mux.HandleFunc("POST /api/v2/torrents/toggleFirstLastPiecePrio", d.handleQbitToggleFirstLast)
	mux.HandleFunc("POST /api/v2/torrents/increasePrio", d.handleQbitIncreasePrio)
	mux.HandleFunc("POST /api/v2/torrents/decreasePrio", d.handleQbitDecreasePrio)
	mux.HandleFunc("POST /api/v2/torrents/topPrio", d.handleQbitTopPrio)
	mux.HandleFunc("POST /api/v2/torrents/bottomPrio", d.handleQbitBottomPrio)
	mux.HandleFunc("POST /api/v2/app/shutdown", d.handleQbitShutdown)
	// Search API, mapped onto the configured Torznab indexers.
	mux.HandleFunc("GET /api/v2/search/plugins", d.handleQbitSearchPlugins)
	mux.HandleFunc("POST /api/v2/search/start", d.handleQbitSearchStart)
	mux.HandleFunc("GET /api/v2/search/status", d.handleQbitSearchStatus)
	mux.HandleFunc("GET /api/v2/search/results", d.handleQbitSearchResults)
	mux.HandleFunc("POST /api/v2/search/stop", d.handleQbitSearchStop)
	mux.HandleFunc("POST /api/v2/search/delete", d.handleQbitSearchDelete)
	mux.HandleFunc("POST /api/v2/search/installPlugin", d.handleQbitSearchPluginManage)
	mux.HandleFunc("POST /api/v2/search/uninstallPlugin", d.handleQbitSearchPluginManage)
	mux.HandleFunc("POST /api/v2/search/enablePlugin", d.handleQbitSearchPluginManage)
	mux.HandleFunc("POST /api/v2/search/updatePlugins", d.handleQbitSearchPluginManage)
	// RSS API, mapped onto the daemon's feeds and rules.
	mux.HandleFunc("GET /api/v2/rss/items", d.handleQbitRssItems)
	mux.HandleFunc("POST /api/v2/rss/addFolder", d.handleQbitRssAddFolder)
	mux.HandleFunc("POST /api/v2/rss/addFeed", d.handleQbitRssAddFeed)
	mux.HandleFunc("POST /api/v2/rss/setFeedURL", d.handleQbitRssSetFeedURL)
	mux.HandleFunc("POST /api/v2/rss/setFeedRefreshInterval", d.handleQbitRssSetFeedInterval)
	mux.HandleFunc("POST /api/v2/rss/removeItem", d.handleQbitRssRemoveItem)
	mux.HandleFunc("POST /api/v2/rss/moveItem", d.handleQbitRssMoveItem)
	mux.HandleFunc("POST /api/v2/rss/refreshItem", d.handleQbitRssRefreshItem)
	mux.HandleFunc("POST /api/v2/rss/markAsRead", d.handleQbitRssMarkAsRead)
	mux.HandleFunc("GET /api/v2/rss/rules", d.handleQbitRssRules)
	mux.HandleFunc("GET /api/v2/rss/matchingArticles", d.handleQbitRssMatchingArticles)
	mux.HandleFunc("POST /api/v2/rss/setRule", d.handleQbitRssSetRule)
	mux.HandleFunc("POST /api/v2/rss/renameRule", d.handleQbitRssRenameRule)
	mux.HandleFunc("POST /api/v2/rss/removeRule", d.handleQbitRssRemoveRule)
	// Torrent creator, mapped onto the engine's CreateTorrent.
	mux.HandleFunc("POST /api/v2/torrentcreator/addTask", d.handleQbitTorrentCreatorAdd)
	mux.HandleFunc("GET /api/v2/torrentcreator/status", d.handleQbitTorrentCreatorStatus)
	mux.HandleFunc("GET /api/v2/torrentcreator/torrentFile", d.handleQbitTorrentCreatorFile)
	mux.HandleFunc("POST /api/v2/torrentcreator/deleteTask", d.handleQbitTorrentCreatorDelete)
	return d.qbitAuth(mux)
}

// qbitAuth guards the qBittorrent API the same way the page and /api/v1 are
// guarded: no password configured, a LAN source with local-bypass on, or a valid
// session (the page's gx_session cookie or the qBittorrent SID). A cross-site
// POST is always rejected (no CSRF token is used); programmatic clients send no
// Origin/Referer and are allowed.
func (d *Daemon) qbitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !sameOrigin(r) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/api/v2/auth/login" || d.standaloneAPIAllowed(r) {
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
	ip := d.clientIPString(r)
	if !d.loginAllowed(ip) {
		// qBittorrent answers 200 with "Fails." when the login fails.
		qbitText(w, http.StatusOK, qbitInvalidCode)
		return
	}
	if !d.loginMatches(strings.TrimSpace(r.FormValue("username")), r.FormValue("password")) {
		d.recordLoginFailure(ip)
		// qBittorrent answers 200 with "Fails." and no cookie.
		qbitText(w, http.StatusOK, qbitInvalidCode)
		return
	}
	d.clearLoginFailures(ip)
	token := d.sessions.Create()
	http.SetCookie(w, &http.Cookie{
		Name: qbitSIDCookie, Value: token, Path: "/", HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteLaxMode, MaxAge: int(d.sessions.TTL().Seconds()),
	})
	qbitText(w, http.StatusOK, qbitOKCode)
}

func (d *Daemon) handleQbitLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(qbitSIDCookie); err == nil && d.sessions != nil {
		d.sessions.Drop(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: qbitSIDCookie, Value: "", Path: "/", Secure: secureRequest(r), MaxAge: -1})
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
	Hash          string  `json:"hash"`
	Name          string  `json:"name"`
	Size          int64   `json:"size"`
	TotalSize     int64   `json:"total_size"`
	Progress      float64 `json:"progress"`
	DLRate        int64   `json:"dlspeed"`
	ULRate        int64   `json:"upspeed"`
	State         string  `json:"state"`
	SavePath      string  `json:"save_path"`
	ContentPath   string  `json:"content_path"`
	Category      string  `json:"category"`
	Tags          string  `json:"tags"`
	NumSeeds      int     `json:"num_seeds"`
	NumLeechs     int     `json:"num_leechs"`
	NumComplete   int     `json:"num_complete"`
	NumIncomplete int     `json:"num_incomplete"`
	Ratio         float64 `json:"ratio"`
	ETA           int64   `json:"eta"`
	AddedOn       int64   `json:"added_on"`
	CompletionOn  int64   `json:"completion_on"`
	AmountLeft    int64   `json:"amount_left"`
	Downloaded    int64   `json:"downloaded"`
	Uploaded      int64   `json:"uploaded"`
	// DownloadedSession/UploadedSession: the daemon does not keep a separate
	// per-session counter, so they mirror the lifetime counters rather than
	// reporting a misleading zero.
	DownloadedSession int64   `json:"downloaded_session"`
	UploadedSession   int64   `json:"uploaded_session"`
	Completed         int64   `json:"completed"`
	Tracker           string  `json:"tracker"`
	DLimit            int64   `json:"dl_limit"`
	ULimit            int64   `json:"up_limit"`
	SeedingTime       int64   `json:"seeding_time"`
	TimeActive        int64   `json:"time_active"`
	TotalWasted       int64   `json:"total_wasted"`
	Availability      float64 `json:"availability"`
	Priority          int     `json:"priority"`
	SeqDL             bool    `json:"seq_dl"`
	FLPiecePrio       bool    `json:"f_l_piece_prio"`
	ForceStart        bool    `json:"force_start"`
	AutoTMM           bool    `json:"auto_tmm"`
	MagnetURI         string  `json:"magnet_uri"`
}

func (d *Daemon) handleQbitTorrentsInfo(w http.ResponseWriter, r *http.Request) {
	views := d.snapshotViews()
	wanted := d.qbitHashFilter(r)
	q := r.URL.Query()
	filter := strings.ToLower(strings.TrimSpace(q.Get("filter")))
	hasCategory := q.Has("category")
	category := strings.TrimSpace(q.Get("category"))
	hasTag := q.Has("tag")
	tag := strings.TrimSpace(q.Get("tag"))
	out := make([]qbitTorrent, 0, len(views))
	for _, v := range views {
		if wanted != nil {
			if _, ok := wanted[strings.ToLower(v.Hash)]; !ok {
				continue
			}
		}
		if filter != "" && filter != "all" && !qbitMatchFilter(filter, v) {
			continue
		}
		if hasCategory {
			// An empty "category" means "without category", like qBittorrent.
			if category == "" {
				if v.Category != "" {
					continue
				}
			} else if v.Category != category {
				continue
			}
		}
		if hasTag {
			if tag == "" {
				if len(v.Tags) > 0 {
					continue
				}
			} else if !qbitHasTag(v.Tags, tag) {
				continue
			}
		}
		out = append(out, qbitView(v))
	}
	qbitSortTorrents(out, q.Get("sort"), formBool(r, "reverse"))
	qbitPage(out, q.Get("limit"), q.Get("offset"))
	writeJSON(w, http.StatusOK, out)
}

// qbitMatchFilter implements the "filter" parameter of /torrents/info against a
// torrent's published view.
func qbitMatchFilter(filter string, v torrentInfo) bool {
	state := qbitStateFor(v)
	switch filter {
	case "downloading":
		return state == "downloading" || state == "metaDL" || state == "forcedDL" ||
			state == "stalledDL" || state == "allocating" || state == "checkingDL" || state == "queuedDL"
	case "seeding":
		return state == "uploading" || state == "forcedUP" || state == "stalledUP" ||
			state == "queuedUP" || state == "checkingUP"
	case "completed":
		return v.HasMetadata && v.TotalSize > 0 && v.TotalDone >= v.TotalSize
	case "stopped", "paused":
		return state == "pausedDL" || state == "pausedUP"
	case "running":
		return state != "pausedDL" && state != "pausedUP" && state != "error" && state != "unknown"
	case "active":
		return v.DownloadRate > 0 || v.UploadRate > 0
	case "inactive":
		return v.DownloadRate <= 0 && v.UploadRate <= 0
	case "stalled":
		return state == "stalledDL" || state == "stalledUP"
	case "stalled_uploading", "stalled_up":
		return state == "stalledUP"
	case "stalled_downloading", "stalled_dl":
		return state == "stalledDL"
	case "errored", "error":
		return state == "error"
	default:
		return true
	}
}

func qbitHasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// qbitSortTorrents orders the list by a qBittorrent sort key (any field name of
// the torrent object). Unknown keys leave the order unchanged.
func qbitSortTorrents(list []qbitTorrent, key string, reverse bool) {
	if key == "" {
		return
	}
	less := func(a, b qbitTorrent) bool {
		switch key {
		case "name":
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		case "size", "total_size":
			return a.TotalSize < b.TotalSize
		case "progress":
			return a.Progress < b.Progress
		case "dlspeed":
			return a.DLRate < b.DLRate
		case "upspeed":
			return a.ULRate < b.ULRate
		case "ratio":
			return a.Ratio < b.Ratio
		case "eta":
			return a.ETA < b.ETA
		case "added_on":
			return a.AddedOn < b.AddedOn
		case "completion_on":
			return a.CompletionOn < b.CompletionOn
		case "num_seeds":
			return a.NumSeeds < b.NumSeeds
		case "num_leechs":
			return a.NumLeechs < b.NumLeechs
		case "state":
			return a.State < b.State
		case "category":
			return a.Category < b.Category
		case "hash":
			return a.Hash < b.Hash
		case "time_active":
			return a.TimeActive < b.TimeActive
		case "seeding_time":
			return a.SeedingTime < b.SeedingTime
		case "downloaded":
			return a.Downloaded < b.Downloaded
		case "uploaded":
			return a.Uploaded < b.Uploaded
		case "amount_left":
			return a.AmountLeft < b.AmountLeft
		default:
			return false
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if reverse {
			return less(list[j], list[i])
		}
		return less(list[i], list[j])
	})
}

// qbitPage applies the optional "limit" and "offset" parameters (offset may be
// negative, counted from the end, like qBittorrent).
func qbitPage(list []qbitTorrent, limitRaw, offsetRaw string) []qbitTorrent {
	return pageSlice(list, limitRaw, offsetRaw)
}

// qbitHashFilter returns the set of requested info hashes (lowercase) from the
// "hashes" or "hash" field, or nil when the request asks for every torrent. An
// unknown hash yields an empty (non-nil) set, so /torrents/info?hashes=X is
// empty rather than returning an unrelated torrent.
func (d *Daemon) qbitHashFilter(r *http.Request) map[string]struct{} {
	raw := strings.TrimSpace(r.URL.Query().Get("hashes"))
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("hash"))
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

// qbitHashParam returns the torrents a POST action addresses: the singular
// "hash" field (addTrackers/removeTrackers) or the plural "hashes".
func (d *Daemon) qbitHashParam(r *http.Request) []string {
	if h := strings.TrimSpace(r.FormValue("hash")); h != "" {
		return d.qbitHashes(h)
	}
	return d.qbitHashes(r.FormValue("hashes"))
}

func qbitView(v torrentInfo) qbitTorrent {
	eta := v.ETASeconds
	if eta <= 0 || eta > qbitUnknownETA {
		eta = qbitUnknownETA
	}
	availability := -1.0
	if v.PiecesTotal > 0 {
		availability = float64(v.PiecesAvailable) / float64(v.PiecesTotal)
	}
	return qbitTorrent{
		Hash:              strings.ToLower(v.Hash),
		Name:              v.Name,
		Size:              v.TotalSize,
		TotalSize:         v.TotalSize,
		Progress:          v.Progress / 100,
		DLRate:            v.DownloadRate,
		ULRate:            v.UploadRate,
		State:             qbitStateFor(v),
		SavePath:          v.SavePath,
		ContentPath:       filepath.Join(v.SavePath, v.Name),
		Category:          v.Category,
		Tags:              strings.Join(v.Tags, ", "),
		NumSeeds:          v.NumSeeds,
		NumLeechs:         v.NumPeers,
		NumComplete:       v.NumComplete,
		NumIncomplete:     v.NumIncomplete,
		Ratio:             ratioOf(v),
		ETA:               eta,
		AddedOn:           v.AddedAt,
		CompletionOn:      v.CompletedAt,
		AmountLeft:        v.TotalSize - v.TotalDone,
		Downloaded:        v.Downloaded,
		Uploaded:          v.Uploaded,
		DownloadedSession: v.Downloaded,
		UploadedSession:   v.Uploaded,
		Completed:         v.TotalDone,
		Tracker:           v.CurrentTracker,
		DLimit:            qbitTorrentLimitOut(v.DownloadLimitKib),
		ULimit:            qbitTorrentLimitOut(v.UploadLimitKib),
		SeedingTime:       v.SeedingSeconds,
		TimeActive:        v.ActiveSeconds,
		TotalWasted:       v.Wasted,
		Availability:      availability,
		Priority:          0,
		SeqDL:             v.Sequential,
		FLPiecePrio:       v.FirstLast,
		ForceStart:        v.Pinned,
		AutoTMM:           v.AutoTMM,
	}
}

// qbitTorrentLimitOut renders a per-torrent limit for torrents/info: bytes per
// second when set, -1 when the torrent follows the global limit (qBittorrent
// reports -1 for "unlimited/global").
func qbitTorrentLimitOut(kib int64) int64 {
	if kib > 0 {
		return kib * 1024
	}
	return -1
}

// qbitStateFor refines qbitState with the view's progress and flags, so clients
// see the richer qBittorrent states (metaDL, forcedDL/UP, stalledUP, pausedUP,
// checkingUP), not just the base ones.
func qbitStateFor(v torrentInfo) string {
	done := v.HasMetadata && v.TotalSize > 0 && v.TotalDone >= v.TotalSize
	switch v.State {
	case "downloading":
		if v.Pinned {
			return "forcedDL"
		}
		return "downloading"
	case "downloading_metadata":
		if v.Pinned {
			return "forcedMetaDL"
		}
		return "metaDL"
	case "seeding":
		if v.Pinned {
			return "forcedUP"
		}
		return "uploading"
	case "paused":
		// The daemon reports "paused" both for a torrent the queue is holding
		// and for one stopped outside it; `auto_managed` tells them apart.
		// qBittorrent names the first queuedDL/queuedUP (and its "downloading"
		// and "seeding" filters include the queued states), the second
		// pausedDL/pausedUP.
		if v.AutoManaged {
			if done {
				return "queuedUP"
			}
			return "queuedDL"
		}
		if done {
			return "pausedUP"
		}
		return "pausedDL"
	case "stalled":
		if done {
			return "stalledUP"
		}
		return "stalledDL"
	case "checking_files":
		if done {
			return "checkingUP"
		}
		return "checkingDL"
	}
	return qbitState(v.State)
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

func (d *Daemon) handleQbitReannounce(w http.ResponseWriter, r *http.Request) {
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.reannounce(hash)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitSetLocation(w http.ResponseWriter, r *http.Request) {
	location := strings.TrimSpace(r.FormValue("location"))
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.move(hash, location, false)
	}
	w.WriteHeader(http.StatusOK)
}

// qbitTracker is the qBittorrent tracker object (status: 0 disabled, 1 not
// contacted, 2 working, 3 updating, 4 not working).
type qbitTracker struct {
	URL      string `json:"url"`
	Status   int    `json:"status"`
	NumPeers int    `json:"num_peers"`
	Message  string `json:"msg"`
}

func (d *Daemon) handleQbitTrackers(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	t, _ := d.findLocked(r.URL.Query().Get("hash"))
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	out := []qbitTracker{}
	for _, tracker := range t.Trackers() {
		status := 1
		switch tracker.Status {
		case torrent.Working:
			status = 2
		case torrent.Contacting:
			status = 3
		case torrent.NotWorking:
			status = 4
		}
		message := tracker.Warning
		if tracker.Error != nil {
			message = tracker.Error.Error()
		}
		out = append(out, qbitTracker{URL: tracker.URL, Status: status, NumPeers: tracker.Seeders + tracker.Leechers, Message: message})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleQbitToggleSequential flips sequential download for the given hashes.
func (d *Daemon) handleQbitToggleSequential(w http.ResponseWriter, r *http.Request) {
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.toggleSequential(hash)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitSetSuperSeeding toggles BEP 16 super-seeding (qBittorrent "value").
func (d *Daemon) handleQbitSetSuperSeeding(w http.ResponseWriter, r *http.Request) {
	enabled := strings.EqualFold(strings.TrimSpace(r.FormValue("value")), "true")
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.setSuperSeeding(hash, enabled)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitAddTrackers appends the "urls" (one per line) to the torrents.
func (d *Daemon) handleQbitAddTrackers(w http.ResponseWriter, r *http.Request) {
	urls := qbitLines(r.FormValue("urls"))
	if len(urls) > 0 {
		for _, hash := range d.qbitHashParam(r) {
			_ = d.addTrackers(hash, urls)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitRemoveTrackers drops the "urls" (one per line) from the torrents.
func (d *Daemon) handleQbitRemoveTrackers(w http.ResponseWriter, r *http.Request) {
	urls := qbitLines(r.FormValue("urls"))
	if len(urls) > 0 {
		for _, hash := range d.qbitHashParam(r) {
			_ = d.removeTrackers(hash, urls)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitEditTracker replaces origUrl with newUrl on one torrent.
func (d *Daemon) handleQbitEditTracker(w http.ResponseWriter, r *http.Request) {
	orig := strings.TrimSpace(r.FormValue("origUrl"))
	newURL := strings.TrimSpace(r.FormValue("newUrl"))
	if orig != "" && newURL != "" {
		_ = d.editTracker(r.FormValue("hash"), orig, newURL)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitTorrentPeers answers sync/torrentPeers: the connected peers keyed
// by "ip:port", the shape qBittorrent clients parse.
func (d *Daemon) handleQbitTorrentPeers(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	t, _ := d.findLocked(r.URL.Query().Get("hash"))
	d.mu.Unlock()
	peers := map[string]any{}
	if t != nil {
		for _, peer := range t.Peers() {
			connection := "BT"
			if peer.UTP {
				connection = "μTP"
			}
			var flags strings.Builder
			if peer.Source == torrent.SourceIncoming {
				flags.WriteByte('I')
			}
			if peer.EncryptedStream {
				flags.WriteByte('E')
			}
			ip, port := "", 0
			if addr, ok := peer.Addr.(*net.TCPAddr); ok {
				ip, port = addr.IP.String(), addr.Port
			}
			peers[peer.Addr.String()] = map[string]any{
				"ip":         ip,
				"port":       port,
				"client":     peer.Client,
				"progress":   peer.Progress,
				"dl_speed":   peer.DownloadSpeed,
				"up_speed":   peer.UploadSpeed,
				"connection": connection,
				"flags":      flags.String(),
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"peers": peers, "rid": 0})
}

// qbitLines splits a qBittorrent "urls" field: one URL per line, or pipe
// separated (removeTrackers sends them with "|").
func qbitLines(value string) []string {
	var out []string
	for _, field := range strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == '|' }) {
		if field = strings.TrimSpace(field); field != "" {
			out = append(out, field)
		}
	}
	return out
}

// handleQbitFilePrio sets the priority of the given file indices.
// qBittorrent file priorities are 0 (skip), 1 (normal), 6 (high) and 7
// (maximal): the engine uses the same 0..7 scale (default 4), so 1 maps to 4
// and 6/7 pass through as high priorities.
func (d *Daemon) handleQbitFilePrio(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.FormValue("hash"))
	priority, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("priority")))
	value := qbitFilePriorityIn(priority)
	var indices []int
	for _, id := range strings.FieldsFunc(r.FormValue("id"), func(r rune) bool {
		return r == '|' || r == ',' || r == ' '
	}) {
		if n, err := strconv.Atoi(id); err == nil {
			indices = append(indices, n)
		}
	}
	if hash == "" || len(indices) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}
	d.mu.Lock()
	t, meta := d.findLocked(hash)
	var previous []int
	if meta != nil {
		previous = append([]int(nil), meta.FilePriorities...)
	}
	d.mu.Unlock()
	if t == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	files, err := t.Files()
	if err != nil {
		http.Error(w, "metadata not available", http.StatusConflict)
		return
	}
	priorities := make([]int, len(files))
	for i := range priorities {
		priorities[i] = normalFilePriority
	}
	for i := 0; i < len(priorities) && i < len(previous); i++ {
		priorities[i] = previous[i]
	}
	for _, idx := range indices {
		if idx >= 0 && idx < len(priorities) {
			priorities[idx] = value
		}
	}
	if err := d.setFilePriorities(hash, priorities); err != nil {
		// qBittorrent returns 200 for these actions; the change is best effort.
		logf("qbit filePrio %s: %v", hash, err)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitExport returns the torrent's .torrent bytes.
func (d *Daemon) handleQbitExport(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	t, _ := d.findLocked(r.URL.Query().Get("hash"))
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	data, err := t.Torrent()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/x-bittorrent")
	_, _ = w.Write(data)
}

// handleQbitSetForceStart maps qBittorrent "force start" to the daemon's pin:
// a pinned torrent is not rotated out by the queue.
func (d *Daemon) handleQbitSetForceStart(w http.ResponseWriter, r *http.Request) {
	enabled := strings.EqualFold(strings.TrimSpace(r.FormValue("value")), "true")
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.setPin(hash, enabled)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitSetAutoManagement toggles qBittorrent automatic torrent management:
// when on, the category decides the save path and the torrent is relocated.
func (d *Daemon) handleQbitSetAutoManagement(w http.ResponseWriter, r *http.Request) {
	on := formBool(r, "value")
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.setAutoManagement(hash, on)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitSetTorrentDownloadLimit and handleQbitSetTorrentUploadLimit set a
// per-torrent limit. qBittorrent sends bytes per second; the daemon stores
// KiB/s. A non-positive limit removes the per-torrent limit (inherit global).
func (d *Daemon) handleQbitSetTorrentDownloadLimit(w http.ResponseWriter, r *http.Request) {
	d.applyQbitTorrentLimit(w, r, true)
}

func (d *Daemon) handleQbitSetTorrentUploadLimit(w http.ResponseWriter, r *http.Request) {
	d.applyQbitTorrentLimit(w, r, false)
}

func (d *Daemon) applyQbitTorrentLimit(w http.ResponseWriter, r *http.Request, download bool) {
	limit, _ := strconv.ParseInt(strings.TrimSpace(r.FormValue("limit")), 10, 64)
	kib := int64(-1) // no per-torrent limit: inherit the global one
	if limit > 0 {
		if kib = limit / 1024; kib < 1 {
			kib = 1
		}
	}
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		if download {
			value := kib
			_ = d.setLimits(hash, &value, nil, nil, nil)
		} else {
			value := kib
			_ = d.setLimits(hash, nil, &value, nil, nil)
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitDelete(w http.ResponseWriter, r *http.Request) {
	deleteFiles := formBool(r, "deleteFiles")
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.remove(hash, deleteFiles)
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitAdd accepts "urls" (magnet links and .torrent URLs, one per line)
// and an uploaded .torrent (multipart field "torrents"), plus savepath, paused,
// sequentialDownload and firstLastPiecePrio, the way Sonarr and Radarr send them.
func (d *Daemon) handleQbitAdd(w http.ResponseWriter, r *http.Request) {
	savePath := strings.TrimSpace(r.FormValue("savepath"))
	category := strings.TrimSpace(r.FormValue("category"))
	tags := splitFormList(r.FormValue("tags"))
	autoTMM := formBool(r, "autoTMM")
	switch {
	case autoTMM && category != "":
		// With automatic torrent management the category decides the path, even
		// when a savepath is also sent (qBittorrent behaviour).
		savePath = d.categorySavePath(category)
	case savePath == "" && category != "":
		// A category carries its own save path, like qBittorrent.
		savePath = d.categorySavePath(category)
	}
	paused := formBool(r, "paused")
	sequential := formBool(r, "sequentialDownload")
	firstLast := formBool(r, "firstLastPiecePrio")
	added := false

	// Uploaded .torrent files (multipart field "torrents").
	if err := r.ParseMultipartForm(maxTorrentFile); err == nil && r.MultipartForm != nil {
		for _, fhs := range r.MultipartForm.File["torrents"] {
			f, err := fhs.Open()
			if err != nil {
				continue
			}
			data, readErr := io.ReadAll(io.LimitReader(f, maxTorrentFile))
			f.Close()
			if readErr != nil || len(data) == 0 {
				continue
			}
			req := addRequest{TorrentData: data, Destination: savePath, Paused: paused, Category: category, Tags: tags, Sequential: sequential, FirstLast: firstLast, AutoTMM: autoTMM, AutoTMMSet: true}
			if _, _, err := d.add(req); err != nil {
				qbitText(w, http.StatusUnsupportedMediaType, qbitInvalidCode)
				return
			}
			added = true
		}
	}

	urls := r.FormValue("urls")
	for _, raw := range strings.FieldsFunc(urls, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		req := addRequest{Destination: savePath, Paused: paused, Category: category, Tags: tags, Sequential: sequential, FirstLast: firstLast, AutoTMM: autoTMM, AutoTMMSet: true}
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

// addRequestFromForm builds the options shared by the add endpoints (API and
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
	d.removeCategories(splitFormList(r.FormValue("categories")))
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
	d.createTags(splitFormList(r.FormValue("tags")))
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitDeleteTags(w http.ResponseWriter, r *http.Request) {
	d.deleteTags(splitFormList(r.FormValue("tags")))
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitAddTags(w http.ResponseWriter, r *http.Request) {
	tags := splitFormList(r.FormValue("tags"))
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.addTags(hash, tags)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitRemoveTags(w http.ResponseWriter, r *http.Request) {
	tags := splitFormList(r.FormValue("tags"))
	for _, hash := range d.qbitHashes(r.FormValue("hashes")) {
		_ = d.removeTags(hash, tags)
	}
	w.WriteHeader(http.StatusOK)
}

func (d *Daemon) handleQbitDefaultSavePath(w http.ResponseWriter, _ *http.Request) {
	qbitText(w, http.StatusOK, d.downloadDir())
}

// qbitPrefLimitOut renders a global limit in the qBittorrent preferences unit
// (KiB/s, -1 = no limit) from the daemon's own encoding (KiB/s, 0 = unlimited).
func qbitPrefLimitOut(kib int64) int64 {
	if kib <= 0 {
		return -1
	}
	return kib
}

// qbitEncryptionMode maps the daemon's encryption (0 off, 1 prefer, 2 forced)
// onto qBittorrent's (0 prefer, 1 force on, 2 force off).
func qbitEncryptionMode(gx int) int {
	switch gx {
	case 0:
		return 2
	case 2:
		return 1
	default:
		return 0
	}
}

// qbitProtocol maps the uTP switch onto qBittorrent's bittorrent_protocol
// (0 TCP+uTP, 1 TCP, 2 uTP).
func qbitProtocol(utp bool) int {
	if utp {
		return 0
	}
	return 1
}

// settingBool reads a standalone boolean setting (settings.json), with a
// fallback when it is unset or unparsable.
func (d *Daemon) settingBool(key string, fallback bool) bool {
	if d.opts.Settings == nil {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(d.opts.Settings.Get(key, ""))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// rssRefreshMinutes reports the feed refresh interval in minutes (qBittorrent's
// rss_refresh_interval unit); 0 when no feed interval is configured.
func (d *Daemon) rssRefreshMinutes() int {
	if d.opts.Settings == nil {
		return 0
	}
	raw := strings.TrimSpace(d.opts.Settings.Get(feedIntervalKey, ""))
	if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
		return (secs + 59) / 60
	}
	return 0
}

func (d *Daemon) handleQbitPreferences(w http.ResponseWriter, _ *http.Request) {
	stats := d.stats()
	cfg, _, _ := d.config()
	altDL, altUL, altManual := d.altSpeedSettings()
	proxyType := -1
	if stats.Proxy {
		// The clients only need to know a proxy is in use; the daemon keeps the
		// full proxy setting to itself.
		proxyType = 5
	}
	scanDirs := map[string]any{}
	for _, pair := range d.watchFolders() {
		folder, dest := pair[0], pair[1]
		switch {
		case dest == folder:
			scanDirs[folder] = 0
		case dest == "" || dest == d.downloadDir():
			scanDirs[folder] = 1
		default:
			scanDirs[folder] = dest
		}
	}
	exportDir, exportFinDir := d.exportDirs()
	writeJSON(w, http.StatusOK, map[string]any{
		"locale":                            d.opts.Lang,
		"save_path":                         d.downloadDir(),
		"temp_path":                         d.opts.PartsDir,
		"temp_path_enabled":                 d.opts.PartsDir != "",
		"create_subfolder_enabled":          true,
		"start_paused_enabled":              false,
		"preallocate_all":                   cfg.Preallocate,
		"incomplete_files_ext":              false,
		"listen_port":                       stats.PeerPort,
		"random_port":                       false,
		"dht":                               stats.DHT,
		"pex":                               !d.settingBool("no-pex", false),
		"lsd":                               !d.settingBool("no-lsd", false),
		"upnp":                              !d.settingBool("no-upnp", false),
		"encryption":                        qbitEncryptionMode(stats.Encryption),
		"bittorrent_protocol":               qbitProtocol(stats.UTP),
		"limit_utp_rate":                    true,
		"queueing_enabled":                  true,
		"max_active_downloads":              cfg.ActiveDownloads,
		"max_active_uploads":                cfg.ActiveSeeds,
		"max_active_torrents":               cfg.ActiveLimit,
		"dont_count_slow_torrents":          cfg.DontCountSlow,
		"slow_torrent_dl_rate_threshold":    int(cfg.SlowRate),
		"slow_torrent_ul_rate_threshold":    int(cfg.SlowRate),
		"slow_torrent_inactive_timer":       int(cfg.SlowAfterSecs),
		"dl_limit":                          qbitPrefLimitOut(cfg.SpeedLimitDownload),
		"up_limit":                          qbitPrefLimitOut(cfg.SpeedLimitUpload),
		"alt_dl_limit":                      altDL,
		"alt_up_limit":                      altUL,
		"use_alt_speed_limits":              altManual,
		"max_connec":                        cfg.MaxPeerDial + cfg.MaxPeerAccept,
		"max_connec_per_torrent":            qbitPrefLimitOut(int64(cfg.MaxConnectionsPerTorrent)),
		"max_uploads":                       0,
		"max_uploads_per_torrent":           0,
		"max_ratio_enabled":                 d.settingBool(maxRatioEnabledKey, false),
		"max_ratio":                         d.settingFloat(maxRatioKey, -1),
		"max_ratio_act":                     d.settingInt(maxRatioActKey, 0),
		"max_seeding_time_enabled":          d.settingBool(maxSeedingTimeEnabledKey, false),
		"max_seeding_time":                  d.settingInt(maxSeedingTimeKey, -1),
		"auto_tmm_enabled":                  d.settingBool(autoTMMEnabledKey, false),
		"torrent_changed_tmm_enabled":       true,
		"category_changed_tmm_enabled":      true,
		"save_path_changed_tmm_enabled":     true,
		"use_category_paths_in_manual_mode": false,
		"scheduler_enabled":                 d.settingBool(scheduleEnabledKey, false),
		"proxy_type":                        proxyType,
		"anonymous_mode":                    false,
		"ip_filter_enabled":                 stats.IPFilterRules > 0,
		"ip_filter_path":                    stats.IPFilterPath,
		"ip_filter_trackers":                d.settingBool("ipfilter-trackers", true),
		"rss_refresh_interval":              d.rssRefreshMinutes(),
		"export_dir":                        exportDir,
		"export_dir_fin":                    exportFinDir,
		"scan_dirs":                         scanDirs,
		"save_resume_data_interval":         60,
	})
}

func (d *Daemon) handleQbitBuildInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"application_name": "gx-torrent",
		"bitness":          64,
		"qt":               "n/a",
		"libtorrent":       "n/a",
		"boost":            "n/a",
		"openssl":          "n/a",
	})
}

// qbitFindView returns the published view of one torrent by hash.
func (d *Daemon) qbitFindView(hash string) (torrentInfo, bool) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	for _, v := range d.snapshotViews() {
		if strings.ToLower(v.Hash) == hash {
			return v, true
		}
	}
	return torrentInfo{}, false
}

func (d *Daemon) handleQbitProperties(w http.ResponseWriter, r *http.Request) {
	v, ok := d.qbitFindView(r.URL.Query().Get("hash"))
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	eta := v.ETASeconds
	if eta <= 0 || eta > qbitUnknownETA {
		eta = qbitUnknownETA
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"save_path":       v.SavePath,
		"piece_size":      v.PieceLength,
		"total_size":      v.TotalSize,
		"total_done":      v.TotalDone,
		"pieces_num":      v.PiecesTotal,
		"pieces_have":     v.PiecesHave,
		"dl_speed":        v.DownloadRate,
		"up_speed":        v.UploadRate,
		"eta":             eta,
		"seeds":           v.NumSeeds,
		"peers":           v.NumPeers,
		"seeds_total":     v.NumComplete,
		"peers_total":     v.NumIncomplete,
		"share_ratio":     ratioOf(v),
		"addition_date":   v.AddedAt,
		"completion_date": v.CompletedAt,
		"time_elapsed":    v.ActiveSeconds,
		"seeding_time":    v.SeedingSeconds,
		"dl_limit":        -1,
		"up_limit":        -1,
		"comment":         "",
		"created_by":      "",
	})
}

// qbitFile is the qBittorrent file object the clients may read.
type qbitFile struct {
	Name         string  `json:"name"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	Priority     int     `json:"priority"`
	IsSeed       bool    `json:"is_seed"`
	PieceRange   []int   `json:"piece_range"`
	Availability float64 `json:"availability"`
}

func (d *Daemon) handleQbitFiles(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	t, meta := d.findLocked(r.URL.Query().Get("hash"))
	d.mu.Unlock()
	if t == nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	files, err := t.Files()
	if err != nil {
		writeJSON(w, http.StatusOK, []qbitFile{})
		return
	}
	done := map[int]int64{}
	if stats, err := t.FileStats(); err == nil {
		for i, stat := range stats {
			done[i] = stat.BytesCompleted
		}
	}
	out := make([]qbitFile, 0, len(files))
	for i, file := range files {
		progress := 0.0
		if file.Length() > 0 {
			progress = float64(done[i]) / float64(file.Length())
		}
		priority := normalFilePriority
		if meta != nil {
			priority = filePriority(meta, i)
		}
		out = append(out, qbitFile{
			Name: file.Path(), Size: file.Length(), Progress: progress,
			Priority: qbitFilePriorityOut(priority), PieceRange: []int{0, 0}, Availability: -1,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// qbitFilePriorityIn maps a qBittorrent file priority to the engine scale:
// qBittorrent uses 0 (skip), 1 (normal), 6 (high) and 7 (maximal); the engine
// uses the same 0..7 scale with the libtorrent default 4, so 1 becomes 4.
func qbitFilePriorityIn(p int) int {
	switch {
	case p <= 0:
		return 0
	case p == 1:
		return normalFilePriority
	default:
		return p
	}
}

// qbitFilePriorityOut maps an engine priority (0..7, default 4) back to the
// qBittorrent scale so clients that read it see the values they expect.
func qbitFilePriorityOut(p int) int {
	switch {
	case p <= 0:
		return 0
	case p >= 7:
		return 7
	case p >= 5:
		return 6
	default:
		return 1
	}
}

// ratioOf is uploaded over downloaded (or over the data held when it came from
// disk), the same share ratio shown in the page.
func ratioOf(v torrentInfo) float64 {
	if v.TotalSize <= 0 {
		return 0
	}
	return float64(v.Uploaded) / float64(v.TotalSize)
}

func (d *Daemon) handleQbitDownloadLimit(w http.ResponseWriter, _ *http.Request) {
	cfg, _, _ := d.config()
	qbitText(w, http.StatusOK, strconv.FormatInt(cfg.SpeedLimitDownload*1024, 10))
}

func (d *Daemon) handleQbitUploadLimit(w http.ResponseWriter, _ *http.Request) {
	cfg, _, _ := d.config()
	qbitText(w, http.StatusOK, strconv.FormatInt(cfg.SpeedLimitUpload*1024, 10))
}

// qbitSetSpeedLimit stores a global limit in KiB/s (the API speaks bytes/s).
func (d *Daemon) qbitSetSpeedLimit(key, value string) error {
	bytesPerSecond, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || bytesPerSecond < 0 {
		return fmt.Errorf("invalid limit %q", value)
	}
	kib := bytesPerSecond / 1024
	if bytesPerSecond > 0 && kib < 1 {
		// A positive but tiny limit must not become 0 (unlimited).
		kib = 1
	}
	raw, err := json.Marshal(kib)
	if err != nil {
		return err
	}
	_, err = d.setConfig(map[string]json.RawMessage{key: raw})
	return err
}

func (d *Daemon) handleQbitSetDownloadLimit(w http.ResponseWriter, r *http.Request) {
	if err := d.qbitSetSpeedLimit("speed_limit_download", r.FormValue("limit")); err != nil {
		qbitText(w, http.StatusBadRequest, qbitInvalidCode)
		return
	}
	qbitText(w, http.StatusOK, qbitOKCode)
}

func (d *Daemon) handleQbitSetUploadLimit(w http.ResponseWriter, r *http.Request) {
	if err := d.qbitSetSpeedLimit("speed_limit_upload", r.FormValue("limit")); err != nil {
		qbitText(w, http.StatusBadRequest, qbitInvalidCode)
		return
	}
	qbitText(w, http.StatusOK, qbitOKCode)
}

// handleQbitSetPreferences applies the qBittorrent preferences that map onto the
// daemon's own settings and queue: the global download/upload limits (KiB/s,
// -1 = no limit) and the queue knobs. Everything else (disk_cache, scan_dirs,
// ...) is accepted and ignored: the daemon manages its cache and queue itself.
func (d *Daemon) handleQbitSetPreferences(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.FormValue("json"))
	if raw == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	var prefs map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &prefs); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	patch := map[string]json.RawMessage{}
	// qBittorrent sends the global limits (KiB/s) and the per-torrent
	// connection cap with -1 meaning "no limit"; the daemon stores 0 for it.
	for _, m := range []struct{ pref, config string }{
		{"dl_limit", "speed_limit_download"},
		{"up_limit", "speed_limit_upload"},
		{"max_connec_per_torrent", "max_connections_per_torrent"},
	} {
		v, ok := prefs[m.pref]
		if !ok {
			continue
		}
		var kib int64
		if json.Unmarshal(v, &kib) != nil {
			continue
		}
		if kib < 0 {
			kib = 0
		}
		encoded, err := json.Marshal(kib)
		if err != nil {
			continue
		}
		patch[m.config] = encoded
	}
	// Queue knobs the daemon exposes at runtime (same value, same unit).
	for _, m := range []struct{ pref, config string }{
		{"max_active_downloads", "active_downloads"},
		{"max_active_uploads", "active_seeds"},
		{"max_active_torrents", "active_limit"},
		{"dont_count_slow_torrents", "dont_count_slow"},
		{"slow_torrent_dl_rate_threshold", "slow_rate"},
		{"slow_torrent_ul_rate_threshold", "slow_rate"},
		{"slow_torrent_inactive_timer", "slow_after_secs"},
	} {
		if v, ok := prefs[m.pref]; ok {
			patch[m.config] = v
		}
	}
	if len(patch) > 0 {
		if _, err := d.setConfig(patch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	// Alternative limits and the manual toggle live in settings.json.
	if d.opts.Settings != nil {
		changed := false
		for _, m := range []struct{ pref, key string }{
			{"alt_dl_limit", altSpeedDLKey},
			{"alt_up_limit", altSpeedULKey},
		} {
			v, ok := prefs[m.pref]
			if !ok {
				continue
			}
			var kib int64
			if json.Unmarshal(v, &kib) != nil {
				continue
			}
			if kib < 0 {
				kib = 0
			}
			d.opts.Settings.Set(m.key, strconv.FormatInt(kib, 10))
			changed = true
		}
		if v, ok := prefs["use_alt_speed_limits"]; ok {
			var on bool
			if json.Unmarshal(v, &on) == nil {
				d.opts.Settings.Set(altSpeedEnabledKey, strconv.FormatBool(on))
				changed = true
			}
		}
		// scan_dirs (qBittorrent): folder -> 0 (the folder itself), 1 (default
		// save path) or a path; mapped onto the watch-folders list.
		if v, ok := prefs["scan_dirs"]; ok {
			var dirs map[string]json.RawMessage
			if json.Unmarshal(v, &dirs) == nil {
				lines := make([]string, 0, len(dirs))
				for folder, raw := range dirs {
					folder = strings.TrimSpace(folder)
					if folder == "" {
						continue
					}
					dest := ""
					var path string
					var mode int
					switch {
					case json.Unmarshal(raw, &path) == nil:
						dest = path
					case json.Unmarshal(raw, &mode) == nil && mode == 0:
						dest = folder
					}
					if dest == "" {
						lines = append(lines, folder)
					} else {
						lines = append(lines, folder+"="+dest)
					}
				}
				d.opts.Settings.Set(watchFoldersSettingKey, strings.Join(lines, "\n"))
				changed = true
			}
		}
		// export_dir / export_dir_fin (qBittorrent): where to copy the .torrent.
		for _, m := range []struct{ pref, key string }{
			{"export_dir", exportDirKey},
			{"export_dir_fin", exportFinDirKey},
		} {
			v, ok := prefs[m.pref]
			if !ok {
				continue
			}
			var path string
			if json.Unmarshal(v, &path) == nil {
				d.opts.Settings.Set(m.key, strings.TrimSpace(path))
				changed = true
			}
		}
		// Global seeding limits (qBittorrent max_ratio / max_seeding_time).
		for _, b := range []struct{ pref, key string }{
			{"max_ratio_enabled", maxRatioEnabledKey},
			{"max_seeding_time_enabled", maxSeedingTimeEnabledKey},
		} {
			v, ok := prefs[b.pref]
			if !ok {
				continue
			}
			var on bool
			if json.Unmarshal(v, &on) == nil {
				d.opts.Settings.Set(b.key, strconv.FormatBool(on))
				changed = true
			}
		}
		if v, ok := prefs["max_ratio"]; ok {
			var ratio float64
			if json.Unmarshal(v, &ratio) == nil {
				d.opts.Settings.Set(maxRatioKey, strconv.FormatFloat(ratio, 'f', -1, 64))
				changed = true
			}
		}
		if v, ok := prefs["max_ratio_act"]; ok {
			var act int
			if json.Unmarshal(v, &act) == nil {
				d.opts.Settings.Set(maxRatioActKey, strconv.Itoa(act))
				changed = true
			}
		}
		if v, ok := prefs["max_seeding_time"]; ok {
			var minutes int
			if json.Unmarshal(v, &minutes) == nil {
				d.opts.Settings.Set(maxSeedingTimeKey, strconv.Itoa(minutes))
				changed = true
			}
		}
		if v, ok := prefs["auto_tmm_enabled"]; ok {
			var on bool
			if json.Unmarshal(v, &on) == nil {
				d.opts.Settings.Set(autoTMMEnabledKey, strconv.FormatBool(on))
				changed = true
			}
		}
		if changed {
			_ = d.opts.Settings.Save()
			d.applyScheduleNow()
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleQbitSetShareLimits maps qBittorrent share limits onto the daemon's seed
// policy. ratioLimit: -2 keep the global, -1 infinite, >=0 explicit.
// seedingTimeLimit is in minutes (-2 global, -1 infinite, >=0 minutes); the
// daemon stores days, so a positive value is rounded up to the next day.
func (d *Daemon) handleQbitSetShareLimits(w http.ResponseWriter, r *http.Request) {
	ratio, rerr := strconv.ParseFloat(strings.TrimSpace(r.FormValue("ratioLimit")), 64)
	minutes, merr := strconv.ParseInt(strings.TrimSpace(r.FormValue("seedingTimeLimit")), 10, 64)
	for _, hash := range d.qbitHashParam(r) {
		var ratioPtr *float64
		if rerr == nil {
			switch {
			case ratio >= 0:
				ratioPtr = &ratio
			case ratio == -1: // infinite: never stop by ratio
				zero := 0.0
				ratioPtr = &zero
			}
		}
		var daysPtr *int64
		if merr == nil {
			switch {
			case minutes >= 0:
				days := (minutes + 1439) / 1440
				if days < 1 {
					days = 1
				}
				daysPtr = &days
			case minutes == -1:
				zero := int64(0)
				daysPtr = &zero
			}
		}
		if ratioPtr != nil || daysPtr != nil {
			_ = d.setLimits(hash, nil, nil, ratioPtr, daysPtr)
		}
	}
	w.WriteHeader(http.StatusOK)
}

// qbitSyncState is the last full snapshot sent through sync/maindata; the next
// poll builds its diff from it.
type qbitSyncState struct {
	torrents   map[string]qbitTorrent
	categories map[string]qbitCategory
	tags       map[string]struct{}
}

// qbitCurrentSyncState reads the current published state (the same data the
// full snapshot sends).
func (d *Daemon) qbitCurrentSyncState() *qbitSyncState {
	views := d.snapshotViews()
	torrents := make(map[string]qbitTorrent, len(views))
	for _, v := range views {
		torrents[strings.ToLower(v.Hash)] = qbitView(v)
	}
	categories := map[string]qbitCategory{}
	for name, savePath := range d.categoriesSnapshot() {
		categories[name] = qbitCategory{Name: name, SavePath: savePath}
	}
	tags := map[string]struct{}{}
	for _, tag := range d.tagsSnapshot() {
		tags[tag] = struct{}{}
	}
	return &qbitSyncState{torrents: torrents, categories: categories, tags: tags}
}

func qbitTagsList(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for tag := range set {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

// qbitServerState is the global transfer snapshot qBittorrent clients read from
// server_state (both of sync/maindata and transfer/info).
func qbitServerState(stats daemonStats) map[string]any {
	return map[string]any{
		"dl_info_speed":          stats.DownloadRate,
		"up_info_speed":          stats.UploadRate,
		"dl_info_data":           stats.Session["bytes_downloaded"],
		"up_info_data":           stats.Session["bytes_uploaded"],
		"dht_nodes":              stats.Session["dht_nodes"],
		"connection_status":      "connected",
		"free_space_on_disk":     stats.DiskFreeBytes,
		"queued_io_jobs":         0,
		"queueing":               true,
		"global_ratio":           strconv.FormatFloat(stats.Ratio, 'f', 2, 64),
		"total_peer_connections": stats.Peers,
	}
}

// handleQbitSync answers /api/v2/sync/maindata. When the client echoes the rid
// of the previous reply it receives only what changed (torrents, categories,
// tags and the removed entries); otherwise it receives a full snapshot. rid
// always advances.
func (d *Daemon) handleQbitSync(w http.ResponseWriter, r *http.Request) {
	clientRID := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("rid")); raw != "" {
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			clientRID = v
		}
	}
	current := d.qbitCurrentSyncState()
	stats := d.stats()

	d.qbitSyncMu.Lock()
	prev := d.qbitSnapshot
	full := prev == nil || clientRID != d.qbitLastRID
	rid := d.qbitRID.Add(1)
	d.qbitSnapshot = current
	d.qbitLastRID = rid
	d.qbitSyncMu.Unlock()

	payload := map[string]any{
		"rid":          rid,
		"full_update":  full,
		"server_state": qbitServerState(stats),
	}
	if full {
		payload["torrents"] = current.torrents
		payload["torrents_removed"] = []string{}
		payload["categories"] = current.categories
		payload["tags"] = qbitTagsList(current.tags)
		writeJSON(w, http.StatusOK, payload)
		return
	}

	changed := map[string]qbitTorrent{}
	for hash, t := range current.torrents {
		if old, ok := prev.torrents[hash]; !ok || old != t {
			changed[hash] = t
		}
	}
	removedTorrents := []string{}
	for hash := range prev.torrents {
		if _, ok := current.torrents[hash]; !ok {
			removedTorrents = append(removedTorrents, hash)
		}
	}
	changedCategories := map[string]qbitCategory{}
	for name, cat := range current.categories {
		if old, ok := prev.categories[name]; !ok || old != cat {
			changedCategories[name] = cat
		}
	}
	removedCategories := []string{}
	for name := range prev.categories {
		if _, ok := current.categories[name]; !ok {
			removedCategories = append(removedCategories, name)
		}
	}
	addedTags := []string{}
	for tag := range current.tags {
		if _, ok := prev.tags[tag]; !ok {
			addedTags = append(addedTags, tag)
		}
	}
	removedTags := []string{}
	for tag := range prev.tags {
		if _, ok := current.tags[tag]; !ok {
			removedTags = append(removedTags, tag)
		}
	}
	sort.Strings(removedTorrents)
	sort.Strings(removedCategories)
	sort.Strings(addedTags)
	sort.Strings(removedTags)
	payload["torrents"] = changed
	payload["torrents_removed"] = removedTorrents
	payload["categories"] = changedCategories
	payload["categories_removed"] = removedCategories
	payload["tags"] = addedTags
	payload["tags_removed"] = removedTags
	writeJSON(w, http.StatusOK, payload)
}
