package main

// dlna.go implements an opt-in DLNA/UPnP AV media server for the daemon: a TV
// or console on the LAN can browse the completed torrents' media files and
// play them. It speaks SSDP (discovery, dlna_ssdp.go), the UPnP AV
// ContentDirectory:1 (Browse with BrowseMetadata/BrowseDirectChildren and
// paging) and ConnectionManager:1, accepts GENA subscriptions, and serves the
// media with HTTP Range by the same core as the UI player (stream.go) plus the
// DLNA headers (contentFeatures/transferMode) the renderers expect. Subtitles
// next to a video (same name) are attached to it, not listed as items. It is
// LAN-only (every request from a non-local address is refused), exposes only
// the media of completed torrents, and is off by default.

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	dlnaEnabledKey    = "dlna-enabled"
	dlnaListenKey     = "dlna-listen"
	dlnaNameKey       = "dlna-name"
	dlnaDefaultListen = ":8200"
	dlnaDefaultName   = "gx-torrent"
	dlnaSSDPGroup     = "239.255.255.250:1900"
	dlnaContentDir    = "urn:schemas-upnp-org:service:ContentDirectory:1"
	dlnaConnMgr       = "urn:schemas-upnp-org:service:ConnectionManager:1"
	dlnaDeviceType    = "urn:schemas-upnp-org:device:MediaServer:1"
	// dlnaRootID is the ContentDirectory root; its parent is "-1".
	dlnaRootID = "0"
	// dlnaFeatures are the DLNA content features of every resource: Range
	// seeking (OP=01), not transcoded (CI=0), streaming + background transfer
	// and DLNA 1.5 (FLAGS).
	dlnaFeatures = "DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000"
	// dlnaLibraryTTL bounds how stale the media listing may be: every media
	// request checks it, and players issue many Range requests.
	dlnaLibraryTTL = 5 * time.Second
	// dlnaAliveEvery re-announces the server before the 1800 s SSDP max-age
	// expires, so a TV that missed the first NOTIFY still finds it.
	dlnaAliveEvery = 15 * time.Minute
	dlnaCheckEvery = 10 * time.Second
)

// dlnaEnabled reports whether the media server is on: settings.json wins when
// the daemon runs standalone (and can change it live), else the flag/env value
// Gextto set in managed mode.
func (d *Daemon) dlnaEnabled() bool {
	if d.opts.Settings != nil {
		if raw := strings.TrimSpace(d.opts.Settings.Get(dlnaEnabledKey, "")); raw != "" {
			return strings.EqualFold(raw, "true")
		}
	}
	return d.opts.DLNAEnabled
}

// dlnaListen is the HTTP bind address of the media server.
func (d *Daemon) dlnaListen() string {
	if d.opts.Settings != nil {
		if v := strings.TrimSpace(d.opts.Settings.Get(dlnaListenKey, "")); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(d.opts.DLNAListen); v != "" {
		return v
	}
	return dlnaDefaultListen
}

// dlnaServerName is the friendly name advertised over SSDP.
func (d *Daemon) dlnaServerName() string {
	if d.opts.Settings != nil {
		if v := strings.TrimSpace(d.opts.Settings.Get(dlnaNameKey, "")); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(d.opts.DLNAName); v != "" {
		return v
	}
	return dlnaDefaultName
}

// dlnaStatusURL is the base URL of the running media server, "" when it is
// off (shown on the settings page).
func (d *Daemon) dlnaStatusURL() string {
	d.dlnaStatusMu.Lock()
	defer d.dlnaStatusMu.Unlock()
	return d.dlnaStatus
}

func (d *Daemon) setDLNAStatus(base string) {
	d.dlnaStatusMu.Lock()
	d.dlnaStatus = base
	d.dlnaStatusMu.Unlock()
}

// runDLNA keeps the media server in step with the settings: it starts it when
// enabled, stops it when disabled and restarts it when the bind address or the
// name change, without the settings handler having to know about it.
func (d *Daemon) runDLNA(stop <-chan struct{}) {
	var srv *dlnaServer
	var running string // listen+name+folders of srv
	shutdown := func() {
		if srv != nil {
			srv.Close()
			srv = nil
			running = ""
			d.setDLNAStatus("")
		}
	}
	apply := func() {
		if !d.dlnaEnabled() {
			if srv != nil {
				shutdown()
				logf("DLNA: media server stopped")
			}
			return
		}
		want := d.dlnaListen() + "\x00" + d.dlnaServerName() + "\x00" + strings.Join(d.dlnaFolders(), "\x00")
		if srv != nil && want == running {
			return
		}
		if srv != nil {
			shutdown()
			logf("DLNA: settings changed, restarting the media server")
		}
		started, err := d.startDLNA()
		if err != nil {
			logf("DLNA: cannot start the media server: %v", err)
			return
		}
		srv, running = started, want
		d.setDLNAStatus(srv.base)
		logf("DLNA: media server «%s» listening on %s (%s)", srv.name, srv.httpAddr, srv.base)
	}
	apply()
	ticker := time.NewTicker(dlnaCheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			shutdown()
			return
		case <-ticker.C:
			apply()
		}
	}
}

// dlnaServer is one running media server instance.
type dlnaServer struct {
	d        *Daemon
	name     string
	uuid     string
	httpSrv  *http.Server
	httpAddr string
	// base is the http://ip:port SSDP advertises in LOCATION. HTTP replies
	// use the Host the client reached instead (dlnaBaseFor), so a multi-homed
	// host hands each client a URL on the interface it used.
	base string
	// browse returns the media listing; nil uses the daemon's torrents. It is
	// a field so tests can drive the server without a live session.
	browse  func() []dlnaFolder
	ssdp    net.PacketConn
	ssdpDst *net.UDPAddr
	closeC  chan struct{}
	once    sync.Once
	// lib caches the listing for dlnaLibraryTTL.
	libMu sync.Mutex
	lib   *dlnaLibrary
	libAt time.Time
	// seen records the last SSDP search logged per source address, so a chatty
	// control point does not fill the log.
	seenMu sync.Mutex
	seen   map[string]time.Time
	// served throttles the media log to one line per file per minute: a
	// player issues a Range request per seek or buffer refill.
	servedMu sync.Mutex
	served   map[string]time.Time
	// libRoots are the library folders shown beside the torrents
	// (dlna_library.go); exclude are absolute paths never listed or served.
	libRoots []dlnaLibRoot
	exclude  []string
	dlnaDirCacheState
}

// logSearch logs an M-SEARCH, at most once per source address per minute.
func (s *dlnaServer) logSearch(ip, st string) {
	if s.throttle(&s.seenMu, &s.seen, ip) {
		logf("DLNA: search from %s for %s", ip, st)
	}
}

// throttle reports whether key was not seen in the last minute (and records
// it). The map is reset when it grows, so it stays bounded.
func (s *dlnaServer) throttle(mu *sync.Mutex, seen *map[string]time.Time, key string) bool {
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	if *seen == nil || len(*seen) > 256 {
		*seen = map[string]time.Time{}
	}
	if last, ok := (*seen)[key]; ok && now.Sub(last) < time.Minute {
		return false
	}
	(*seen)[key] = now
	return true
}

// startDLNA binds the HTTP listener and the SSDP socket and starts both.
func (d *Daemon) startDLNA() (*dlnaServer, error) {
	ln, err := net.Listen("tcp", d.dlnaListen())
	if err != nil {
		return nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	ip := dlnaLocalIP(addr.IP)
	name := d.dlnaServerName()
	srv := &dlnaServer{
		d:        d,
		name:     name,
		uuid:     dlnaUUID(addr.Port),
		httpAddr: ln.Addr().String(),
		base:     "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(addr.Port)),
		closeC:   make(chan struct{}),
		libRoots: dlnaLibRoots(d.dlnaFolders()),
	}
	for _, ex := range d.opts.DLNAExclude {
		if abs, err := filepath.Abs(ex); err == nil {
			srv.exclude = append(srv.exclude, abs)
		}
	}
	for _, root := range srv.libRoots {
		logf("DLNA: library folder %s", root.path)
	}
	srv.httpSrv = &http.Server{Handler: srv.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.httpSrv.Serve(ln) }()

	// SSDP: join the multicast group on the interface that owns the LAN IP and
	// answer M-SEARCH. A failure here is not fatal: the server still works for
	// clients that find it by URL, so it is logged only.
	if err := srv.startSSDP(ip); err != nil {
		logf("DLNA: SSDP unavailable (%v); the server still answers on %s", err, srv.base)
	}
	return srv, nil
}

// handler builds the HTTP mux: the device/service descriptions, the SOAP
// control endpoints, the GENA event endpoints and the media route, all behind
// the LAN-only guard.
func (s *dlnaServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rootDesc.xml", s.handleRootDesc)
	mux.HandleFunc("GET /ContentDirectory.xml", s.handleContentDirSCPD)
	mux.HandleFunc("GET /ConnectionManager.xml", s.handleConnMgrSCPD)
	mux.HandleFunc("POST /control/ContentDirectory", s.handleContentDirControl)
	mux.HandleFunc("POST /control/ConnectionManager", s.handleConnMgrControl)
	mux.HandleFunc("SUBSCRIBE /event/{service}", s.handleSubscribe)
	mux.HandleFunc("UNSUBSCRIBE /event/{service}", s.handleUnsubscribe)
	mux.HandleFunc("GET /res/{hash}/{file}", s.handleMedia) // GET also matches HEAD
	mux.HandleFunc("GET /lib/{n}/{path...}", s.handleLibMedia)
	return dlnaLANOnly(mux)
}

// dlnaLANOnly refuses every request whose source is not on a local network:
// the server has no authentication, so a port forwarded or exposed by mistake
// must not hand the media to the internet.
func dlnaLANOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !dlnaLocalAddr(net.ParseIP(host)) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// dlnaLocalAddr reports whether ip is loopback, private (RFC 1918 / ULA) or
// link-local: the addresses a LAN media client can have.
func dlnaLocalAddr(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// Close shuts the HTTP server and the SSDP socket down.
func (s *dlnaServer) Close() {
	s.once.Do(func() {
		close(s.closeC)
		s.sendByebye()
		if s.ssdp != nil {
			_ = s.ssdp.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(ctx)
	})
}

// dlnaLocalIP returns the address to advertise: the listener's when it is a
// real address, else the host's preferred outbound IP (UDP "connect" picks it
// without sending anything), else loopback.
func dlnaLocalIP(bound net.IP) net.IP {
	if bound != nil && !bound.IsUnspecified() {
		return bound
	}
	if conn, err := net.Dial("udp4", "8.8.8.8:80"); err == nil {
		ip := conn.LocalAddr().(*net.UDPAddr).IP
		conn.Close()
		if ip != nil && !ip.IsUnspecified() {
			return ip
		}
	}
	return net.ParseIP("127.0.0.1")
}

// dlnaUUID renders a UUID-shaped device id from the host name and the port, so
// two instances on one host differ and it stays stable across restarts (a TV
// keeps its bookmarks).
func dlnaUUID(port int) string {
	host, _ := os.Hostname()
	h := fnv.New64a()
	_, _ = h.Write([]byte(host))
	_, _ = h.Write([]byte{byte(port >> 8), byte(port)})
	return fmt.Sprintf("uuid:6b7a9c2e-3f1d-4a5b-8c9d-%012x", h.Sum64()&0xffffffffffff)
}

// dlnaBaseFor is the base URL for the links in an HTTP reply: the Host the
// client used (the right interface on a multi-homed host), else the
// advertised base.
func (s *dlnaServer) dlnaBaseFor(r *http.Request) string {
	if r != nil {
		if host := strings.TrimSpace(r.Host); host != "" && !strings.ContainsAny(host, "/\\\"<> ") {
			return "http://" + host
		}
	}
	return s.base
}

// ---------------------------------------------------------------------------
// Media listing
// ---------------------------------------------------------------------------

// dlnaEntry is one playable media file offered to a renderer.
type dlnaEntry struct {
	hash      string
	fileIndex int
	name      string // path inside the torrent
	size      int64
	mime      string
	video     bool
	subs      []dlnaSub // subtitles next to a video
}

// dlnaSub is a subtitle file attached to a video.
type dlnaSub struct {
	fileIndex int
	mime      string
	kind      string // srt, vtt
}

// dlnaFolder is one completed torrent with at least one playable file.
type dlnaFolder struct {
	hash  string
	name  string
	items []dlnaEntry
}

// dlnaLibrary is the listing indexed for Browse and for the media guard.
type dlnaLibrary struct {
	folders []dlnaFolder // sorted by name
	byHash  map[string]int
	// files are the (hash, index) pairs /res may serve: items and subtitles.
	files map[dlnaFileKey]string // -> mime
	// updateID changes whenever the listing changes (SystemUpdateID).
	updateID uint32
}

type dlnaFileKey struct {
	hash  string
	index int
}

// dlnaPlayable reports whether a MIME type is something a renderer plays.
func dlnaPlayable(mime string) bool {
	return strings.HasPrefix(mime, "video/") || strings.HasPrefix(mime, "audio/")
}

// dlnaSubtitleKind returns srt/vtt for a subtitle MIME type, "" otherwise.
func dlnaSubtitleKind(mime string) string {
	switch mime {
	case "application/x-subrip":
		return "srt"
	case "text/vtt":
		return "vtt"
	}
	return ""
}

// dlnaFile is the minimal view of a torrent file the listing needs.
type dlnaFile struct {
	path   string
	length int64
}

// dlnaFolderFor builds the folder of one completed torrent: its playable files
// sorted by path, each video with the subtitles that share its name (ep1.mkv
// takes ep1.srt and ep1.it.srt from the same directory). ok is false when the
// torrent has no playable file.
func dlnaFolderFor(hash, name string, files []dlnaFile) (dlnaFolder, bool) {
	type subFile struct {
		index      int
		dir, stem  string
		mime, kind string
	}
	var subs []subFile
	folder := dlnaFolder{hash: hash, name: name}
	for i, f := range files {
		mime := mediaContentType(f.path)
		if kind := dlnaSubtitleKind(mime); kind != "" {
			dir, file := path.Split(f.path)
			subs = append(subs, subFile{index: i, dir: dir, stem: strings.ToLower(strings.TrimSuffix(file, path.Ext(file))), mime: mime, kind: kind})
			continue
		}
		if !dlnaPlayable(mime) {
			continue
		}
		folder.items = append(folder.items, dlnaEntry{
			hash: hash, fileIndex: i, name: f.path, size: f.length,
			mime: mime, video: strings.HasPrefix(mime, "video/"),
		})
	}
	if len(folder.items) == 0 {
		return folder, false
	}
	for i := range folder.items {
		e := &folder.items[i]
		if !e.video {
			continue
		}
		dir, file := path.Split(e.name)
		stem := strings.ToLower(strings.TrimSuffix(file, path.Ext(file)))
		for _, s := range subs {
			if s.dir == dir && (s.stem == stem || strings.HasPrefix(s.stem, stem+".")) {
				e.subs = append(e.subs, dlnaSub{fileIndex: s.index, mime: s.mime, kind: s.kind})
			}
		}
	}
	sort.SliceStable(folder.items, func(a, b int) bool {
		return strings.ToLower(folder.items[a].name) < strings.ToLower(folder.items[b].name)
	})
	return folder, true
}

// newDLNALibrary indexes a listing: folders sorted by name, the servable files
// and an update id derived from the content.
func newDLNALibrary(folders []dlnaFolder) *dlnaLibrary {
	sort.SliceStable(folders, func(a, b int) bool {
		na, nb := strings.ToLower(folders[a].name), strings.ToLower(folders[b].name)
		if na != nb {
			return na < nb
		}
		return folders[a].hash < folders[b].hash
	})
	lib := &dlnaLibrary{folders: folders, byHash: make(map[string]int, len(folders)), files: map[dlnaFileKey]string{}}
	h := fnv.New32a()
	for i, f := range folders {
		lib.byHash[f.hash] = i
		fmt.Fprintf(h, "%s|%s|", f.hash, f.name)
		for _, e := range f.items {
			lib.files[dlnaFileKey{e.hash, e.fileIndex}] = e.mime
			fmt.Fprintf(h, "%d:%d:%s|", e.fileIndex, e.size, e.name)
			for _, s := range e.subs {
				lib.files[dlnaFileKey{e.hash, s.fileIndex}] = s.mime
				fmt.Fprintf(h, "s%d|", s.fileIndex)
			}
		}
	}
	// 0 is avoided: some control points treat it as "never changed".
	lib.updateID = h.Sum32() | 1
	return lib
}

// daemonListing lists the completed torrents with playable media. Only fully
// downloaded torrents are exposed: a renderer probes the container metadata
// (moov/cue) at both ends and a partial file plays badly.
func (s *dlnaServer) daemonListing() []dlnaFolder {
	var out []dlnaFolder
	if s.d == nil || s.d.session == nil {
		return out
	}
	for _, t := range s.d.session.ListTorrents() {
		st := t.Stats()
		if st.Bytes.Incomplete > 0 || st.Bytes.Total == 0 {
			continue
		}
		files, err := t.Files()
		if err != nil {
			continue
		}
		list := make([]dlnaFile, len(files))
		for i, f := range files {
			list[i] = dlnaFile{path: f.Path(), length: f.Length()}
		}
		if folder, ok := dlnaFolderFor(t.InfoHash().String(), t.Name(), list); ok {
			out = append(out, folder)
		}
	}
	return out
}

// library returns the current listing, rebuilt at most every dlnaLibraryTTL.
func (s *dlnaServer) library() *dlnaLibrary {
	s.libMu.Lock()
	defer s.libMu.Unlock()
	if s.lib != nil && time.Since(s.libAt) < dlnaLibraryTTL {
		return s.lib
	}
	var folders []dlnaFolder
	if s.browse != nil {
		folders = s.browse()
	} else {
		folders = s.daemonListing()
	}
	s.lib, s.libAt = newDLNALibrary(folders), time.Now()
	return s.lib
}

// ---------------------------------------------------------------------------
// Media
// ---------------------------------------------------------------------------

// handleMedia serves one media (or subtitle) file of a completed torrent with
// Range. Only the files the listing offers are served: the route has no
// authentication, so it must not reach incomplete torrents or other files.
func (s *dlnaServer) handleMedia(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(strings.TrimSpace(r.PathValue("hash")))
	fileIndex, err := strconv.Atoi(strings.TrimSpace(r.PathValue("file")))
	if err != nil || fileIndex < 0 || len(hash) != 40 {
		http.NotFound(w, r)
		return
	}
	mime, ok := s.library().files[dlnaFileKey{hash, fileIndex}]
	if !ok || s.d == nil {
		http.NotFound(w, r)
		return
	}
	d := s.d
	d.mu.Lock()
	t, meta := d.findLocked(hash)
	d.mu.Unlock()
	if t == nil || meta == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && s.throttle(&s.servedMu, &s.served, r.RemoteAddr+hash+strconv.Itoa(fileIndex)) {
		logf("DLNA: %s plays %s file %d", r.RemoteAddr, hash, fileIndex)
	}
	// The DLNA headers are set with their exact spelling (not canonicalised):
	// some renderers match them case-sensitively.
	if dlnaSubtitleKind(mime) != "" {
		w.Header()["transferMode.dlna.org"] = []string{"Interactive"}
	} else {
		w.Header()["transferMode.dlna.org"] = []string{"Streaming"}
		w.Header()["contentFeatures.dlna.org"] = []string{dlnaFeatures}
	}
	d.serveTorrentFile(w, r, hash, t, meta, fileIndex)
}

// mediaURL is the absolute URL of one file of a torrent.
func mediaURL(base, hash string, index int) string {
	return fmt.Sprintf("%s/res/%s/%d", base, hash, index)
}

// ---------------------------------------------------------------------------
// ContentDirectory: DIDL-Lite
// ---------------------------------------------------------------------------

const didlOpen = `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:dlna="urn:schemas-dlna-org:metadata-1-0/" xmlns:sec="http://www.sec.co.kr/">`

// dlnaContainerID is the object id of a torrent container.
func dlnaContainerID(hash string) string { return "torrent:" + hash }

// dlnaEntryKey is the object id of one media file.
func dlnaEntryKey(hash string, index int) string {
	return fmt.Sprintf("file:%s:%d", hash, index)
}

// didlClass names the upnp class of an entry.
func (e dlnaEntry) didlClass() string {
	if e.video {
		return "object.item.videoItem"
	}
	return "object.item.audioItem.musicTrack"
}

func writeRootContainer(b *strings.Builder, name string, children int) {
	fmt.Fprintf(b, `<container id="0" parentID="-1" restricted="1" searchable="0" childCount="%d"><dc:title>%s</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>`,
		children, xmlEscape(name))
}

func writeFolder(b *strings.Builder, f dlnaFolder, parent string) {
	fmt.Fprintf(b, `<container id="%s" parentID="%s" restricted="1" searchable="0" childCount="%d"><dc:title>%s</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>`,
		xmlEscape(dlnaContainerID(f.hash)), xmlEscape(parent), len(f.items), xmlEscape(f.name))
}

// writeItem renders a torrent file as a DIDL item.
func writeItem(b *strings.Builder, e dlnaEntry, base string) {
	writeItemAt(b, e, dlnaEntryKey(e.hash, e.fileIndex), dlnaContainerID(e.hash),
		func(index int) string { return mediaURL(base, e.hash, index) })
}

// writeItemAt renders one playable file with its id, its parent and the URL
// of a file index (the media itself and its subtitles).
func writeItemAt(b *strings.Builder, e dlnaEntry, id, parent string, urlOf func(int) string) {
	fmt.Fprintf(b, `<item id="%s" parentID="%s" restricted="1"><dc:title>%s</dc:title><upnp:class>%s</upnp:class>`,
		xmlEscape(id), xmlEscape(parent),
		xmlEscape(strings.TrimSuffix(path.Base(e.name), path.Ext(e.name))), e.didlClass())
	fmt.Fprintf(b, `<res protocolInfo="http-get:*:%s:%s" size="%d">%s</res>`,
		e.mime, dlnaFeatures, e.size, xmlEscape(urlOf(e.fileIndex)))
	for _, s := range e.subs {
		u := xmlEscape(urlOf(s.fileIndex))
		// The res form is the DLNA one; CaptionInfoEx is what Samsung and LG
		// renderers read.
		fmt.Fprintf(b, `<res protocolInfo="http-get:*:%s:*">%s</res><sec:CaptionInfoEx sec:type="%s">%s</sec:CaptionInfoEx>`,
			s.mime, u, s.kind, u)
	}
	b.WriteString(`</item>`)
}

// dlnaFault is a UPnP error code and description for a SOAP fault.
type dlnaFault struct {
	code int
	desc string
}

var (
	dlnaNoSuchObject = &dlnaFault{701, "No such object"}
	dlnaInvalidArgs  = &dlnaFault{402, "Invalid Args"}
)

// browse answers a ContentDirectory Browse on the torrents: BrowseMetadata
// returns the object itself, BrowseDirectChildren its children from start
// (count 0 = all). top is the id holding the torrent containers: the root, or
// the "torrents" container when library folders are configured. It is pure,
// so it is tested without a session.
func (lib *dlnaLibrary) browse(objectID, flag string, start, count int, name, base string, top ...string) (didl string, returned, total int, fault *dlnaFault) {
	parent := dlnaRootID
	if len(top) > 0 {
		parent = top[0]
	}
	if start < 0 || count < 0 {
		return "", 0, 0, dlnaInvalidArgs
	}
	var b strings.Builder
	b.WriteString(didlOpen)
	page := func(n int, write func(i int)) {
		total = n
		end := n
		if count > 0 && start+count < n {
			end = start + count
		}
		for i := start; i < end; i++ {
			write(i)
			returned++
		}
	}
	switch flag {
	case "BrowseMetadata":
		switch {
		case objectID == dlnaRootID:
			writeRootContainer(&b, name, len(lib.folders))
		case strings.HasPrefix(objectID, "torrent:"):
			fi, ok := lib.byHash[strings.TrimPrefix(objectID, "torrent:")]
			if !ok {
				return "", 0, 0, dlnaNoSuchObject
			}
			writeFolder(&b, lib.folders[fi], parent)
		default:
			e, ok := lib.entry(objectID)
			if !ok {
				return "", 0, 0, dlnaNoSuchObject
			}
			writeItem(&b, e, base)
		}
		returned, total = 1, 1
	case "BrowseDirectChildren":
		switch {
		case objectID == dlnaRootID:
			page(len(lib.folders), func(i int) { writeFolder(&b, lib.folders[i], parent) })
		case strings.HasPrefix(objectID, "torrent:"):
			fi, ok := lib.byHash[strings.TrimPrefix(objectID, "torrent:")]
			if !ok {
				return "", 0, 0, dlnaNoSuchObject
			}
			items := lib.folders[fi].items
			page(len(items), func(i int) { writeItem(&b, items[i], base) })
		default:
			if _, ok := lib.entry(objectID); !ok {
				return "", 0, 0, dlnaNoSuchObject
			}
			// An item has no children.
		}
	default:
		return "", 0, 0, dlnaInvalidArgs
	}
	b.WriteString(`</DIDL-Lite>`)
	return b.String(), returned, total, nil
}

// browseObject routes a Browse: without library folders the torrents sit in
// the root (lib.browse); with folders the root holds one container per folder
// plus a "Torrent" container, and lib:<n>/... ids go to the folder tree.
func (s *dlnaServer) browseObject(lib *dlnaLibrary, objectID, flag string, start, count int, base string) (string, int, int, *dlnaFault) {
	if start < 0 || count < 0 || (flag != "BrowseMetadata" && flag != "BrowseDirectChildren") {
		return "", 0, 0, dlnaInvalidArgs
	}
	if len(s.libRoots) == 0 {
		return lib.browse(objectID, flag, start, count, s.name, base)
	}
	torrents := func(b *strings.Builder) {
		fmt.Fprintf(b, `<container id="%s" parentID="0" restricted="1" searchable="0" childCount="%d"><dc:title>Torrent</dc:title><upnp:class>object.container.storageFolder</upnp:class></container>`,
			dlnaTorrentsID, len(lib.folders))
	}
	switch {
	case strings.HasPrefix(objectID, dlnaLibPrefix):
		return s.browseLib(objectID, flag, start, count, base)
	case objectID == dlnaTorrentsID:
		if flag == "BrowseDirectChildren" {
			return lib.browse(dlnaRootID, flag, start, count, s.name, base, dlnaTorrentsID)
		}
		var b strings.Builder
		b.WriteString(didlOpen)
		torrents(&b)
		b.WriteString(`</DIDL-Lite>`)
		return b.String(), 1, 1, nil
	case objectID == dlnaRootID:
		children := len(s.libRoots) + 1
		var b strings.Builder
		b.WriteString(didlOpen)
		if flag == "BrowseMetadata" {
			writeRootContainer(&b, s.name, children)
			b.WriteString(`</DIDL-Lite>`)
			return b.String(), 1, 1, nil
		}
		end := children
		if count > 0 && start+count < children {
			end = start + count
		}
		returned := 0
		for i := start; i < end; i++ {
			if i < len(s.libRoots) {
				writeLibContainer(&b, dlnaLibID(i, ""), dlnaRootID, s.libRoots[i].name)
			} else {
				torrents(&b)
			}
			returned++
		}
		b.WriteString(`</DIDL-Lite>`)
		return b.String(), returned, children, nil
	}
	return lib.browse(objectID, flag, start, count, s.name, base, dlnaTorrentsID)
}

// entry resolves a file:<hash>:<index> object id.
func (lib *dlnaLibrary) entry(objectID string) (dlnaEntry, bool) {
	rest, ok := strings.CutPrefix(objectID, "file:")
	if !ok {
		return dlnaEntry{}, false
	}
	hash, idx, ok := strings.Cut(rest, ":")
	n, err := strconv.Atoi(idx)
	fi, found := lib.byHash[hash]
	if !ok || err != nil || !found {
		return dlnaEntry{}, false
	}
	for _, e := range lib.folders[fi].items {
		if e.fileIndex == n {
			return e, true
		}
	}
	return dlnaEntry{}, false
}

// xmlEscape escapes text for XML content and attributes.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ---------------------------------------------------------------------------
// HTTP: device and service descriptions
// ---------------------------------------------------------------------------

func (s *dlnaServer) handleRootDesc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	fmt.Fprintf(w, `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0">
<specVersion><major>1</major><minor>0</minor></specVersion>
<device>
<deviceType>%s</deviceType>
<friendlyName>%s</friendlyName>
<manufacturer>gx-torrent</manufacturer>
<modelName>gx-torrent media server</modelName>
<modelNumber>1</modelNumber>
<dlna:X_DLNADOC>DMS-1.50</dlna:X_DLNADOC>
<UDN>%s</UDN>
<serviceList>
<service><serviceType>%s</serviceType><serviceId>urn:upnp-org:serviceId:ContentDirectory</serviceId><SCPDURL>/ContentDirectory.xml</SCPDURL><controlURL>/control/ContentDirectory</controlURL><eventSubURL>/event/ContentDirectory</eventSubURL></service>
<service><serviceType>%s</serviceType><serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId><SCPDURL>/ConnectionManager.xml</SCPDURL><controlURL>/control/ConnectionManager</controlURL><eventSubURL>/event/ConnectionManager</eventSubURL></service>
</serviceList>
</device>
</root>`, dlnaDeviceType, xmlEscape(s.name), s.uuid, dlnaContentDir, dlnaConnMgr)
}

func (s *dlnaServer) handleContentDirSCPD(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	_, _ = w.Write([]byte(contentDirSCPD))
}

func (s *dlnaServer) handleConnMgrSCPD(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	_, _ = w.Write([]byte(connMgrSCPD))
}

// ---------------------------------------------------------------------------
// HTTP: SOAP control
// ---------------------------------------------------------------------------

// soapEnvelope is the minimal SOAP parsing the Browse action needs.
type soapEnvelope struct {
	Body struct {
		Browse struct {
			ObjectID       string `xml:"ObjectID"`
			BrowseFlag     string `xml:"BrowseFlag"`
			StartingIndex  string `xml:"StartingIndex"`
			RequestedCount string `xml:"RequestedCount"`
		} `xml:"Browse"`
	} `xml:"Body"`
}

// soapUint parses a ui4 argument; empty is 0.
func soapUint(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(v, 10, 31)
	return int(n), err == nil
}

func (s *dlnaServer) handleContentDirControl(w http.ResponseWriter, r *http.Request) {
	lib := s.library()
	switch soapAction(r) {
	case "Browse":
		var env soapEnvelope
		if err := xml.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&env); err != nil {
			soapFault(w, dlnaInvalidArgs)
			return
		}
		args := env.Body.Browse
		start, ok1 := soapUint(args.StartingIndex)
		count, ok2 := soapUint(args.RequestedCount)
		if !ok1 || !ok2 {
			soapFault(w, dlnaInvalidArgs)
			return
		}
		objectID := strings.TrimSpace(args.ObjectID)
		didl, returned, total, fault := s.browseObject(lib, objectID, strings.TrimSpace(args.BrowseFlag), start, count, s.dlnaBaseFor(r))
		if fault != nil {
			soapFault(w, fault)
			return
		}
		if s.throttle(&s.seenMu, &s.seen, "browse"+r.RemoteAddr+objectID) {
			logf("DLNA: %s browses %q: %d of %d item(s)", r.RemoteAddr, objectID, returned, total)
		}
		soapReply(w, dlnaContentDir, "Browse", fmt.Sprintf(
			"<Result>%s</Result><NumberReturned>%d</NumberReturned><TotalMatches>%d</TotalMatches><UpdateID>%d</UpdateID>",
			xmlEscape(didl), returned, total, lib.updateID))
	case "GetSortCapabilities":
		soapReply(w, dlnaContentDir, "GetSortCapabilities", "<SortCaps></SortCaps>")
	case "GetSearchCapabilities":
		soapReply(w, dlnaContentDir, "GetSearchCapabilities", "<SearchCaps></SearchCaps>")
	case "GetSystemUpdateID":
		soapReply(w, dlnaContentDir, "GetSystemUpdateID", fmt.Sprintf("<Id>%d</Id>", lib.updateID))
	default:
		soapFault(w, &dlnaFault{401, "Invalid Action"})
	}
}

// dlnaSourceProtocols lists the protocolInfo of every type the server can
// serve (ConnectionManager SourceProtocolInfo).
func dlnaSourceProtocols() string {
	seen := map[string]bool{}
	var out []string
	for _, mime := range mediaContentTypes {
		if seen[mime] {
			continue
		}
		seen[mime] = true
		if dlnaPlayable(mime) {
			out = append(out, "http-get:*:"+mime+":"+dlnaFeatures)
		} else {
			out = append(out, "http-get:*:"+mime+":*")
		}
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func (s *dlnaServer) handleConnMgrControl(w http.ResponseWriter, r *http.Request) {
	switch soapAction(r) {
	case "GetProtocolInfo":
		soapReply(w, dlnaConnMgr, "GetProtocolInfo", "<Source>"+xmlEscape(dlnaSourceProtocols())+"</Source><Sink></Sink>")
	case "GetCurrentConnectionIDs":
		soapReply(w, dlnaConnMgr, "GetCurrentConnectionIDs", "<ConnectionIDs>0</ConnectionIDs>")
	case "GetCurrentConnectionInfo":
		soapReply(w, dlnaConnMgr, "GetCurrentConnectionInfo",
			"<RcsID>-1</RcsID><AVTransportID>-1</AVTransportID><ProtocolInfo></ProtocolInfo>"+
				"<PeerConnectionManager></PeerConnectionManager><PeerConnectionID>-1</PeerConnectionID>"+
				"<Direction>Output</Direction><Status>OK</Status>")
	default:
		soapFault(w, &dlnaFault{401, "Invalid Action"})
	}
}

// soapAction extracts the action name from the SOAPAction header.
func soapAction(r *http.Request) string {
	raw := strings.Trim(strings.TrimSpace(r.Header.Get("SOAPAction")), `"`)
	if i := strings.LastIndex(raw, "#"); i >= 0 {
		return raw[i+1:]
	}
	return raw
}

// soapReply writes a SOAP response with the given out arguments.
func soapReply(w http.ResponseWriter, service, action, inner string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Header().Set("EXT", "")
	fmt.Fprintf(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body>
</s:Envelope>`, action, service, inner, action)
}

// soapFault writes a UPnP error.
func soapFault(w http.ResponseWriter, f *dlnaFault) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail>
<UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError>
</detail></s:Fault></s:Body></s:Envelope>`, f.code, html.EscapeString(f.desc))
}

// ---------------------------------------------------------------------------
// HTTP: GENA eventing
// ---------------------------------------------------------------------------

// dlnaSubTimeout is the subscription lifetime granted to a control point.
const dlnaSubTimeout = 1800

// handleSubscribe accepts a GENA subscription (several TVs refuse a server
// whose eventSubURL fails) and sends the initial event with the current state
// variables. Renewals (SID, no CALLBACK) are accepted as they come. The
// callback must point back at the subscriber's own address, so the server
// cannot be used to post to a third host.
func (s *dlnaServer) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	service := r.PathValue("service")
	if service != "ContentDirectory" && service != "ConnectionManager" {
		http.NotFound(w, r)
		return
	}
	sid := strings.TrimSpace(r.Header.Get("SID"))
	callback := strings.TrimSpace(r.Header.Get("CALLBACK"))
	if sid != "" {
		if callback != "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("SID", sid)
		w.Header().Set("TIMEOUT", "Second-"+strconv.Itoa(dlnaSubTimeout))
		return
	}
	target, ok := dlnaCallback(callback, r.RemoteAddr)
	if !ok || !strings.EqualFold(strings.TrimSpace(r.Header.Get("NT")), "upnp:event") {
		http.Error(w, "precondition failed", http.StatusPreconditionFailed)
		return
	}
	sid = fmt.Sprintf("uuid:%x", time.Now().UnixNano())
	w.Header().Set("SID", sid)
	w.Header().Set("TIMEOUT", "Second-"+strconv.Itoa(dlnaSubTimeout))
	w.WriteHeader(http.StatusOK)
	var props string
	if service == "ContentDirectory" {
		props = fmt.Sprintf("<e:property><SystemUpdateID>%d</SystemUpdateID></e:property>", s.library().updateID)
	} else {
		props = "<e:property><SourceProtocolInfo>" + xmlEscape(dlnaSourceProtocols()) + "</SourceProtocolInfo></e:property>" +
			"<e:property><SinkProtocolInfo></SinkProtocolInfo></e:property>" +
			"<e:property><CurrentConnectionIDs>0</CurrentConnectionIDs></e:property>"
	}
	go dlnaSendEvent(target, sid, props)
}

func (s *dlnaServer) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("SID")) == "" {
		http.Error(w, "precondition failed", http.StatusPreconditionFailed)
	}
}

// dlnaCallback picks the first http callback URL of a CALLBACK header
// ("<url1><url2>") whose host is the subscriber's own address.
func dlnaCallback(header, remoteAddr string) (string, bool) {
	remote, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return "", false
	}
	for _, part := range strings.Split(header, ">") {
		raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "<"))
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "http" || u.Hostname() == "" {
			continue
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.Equal(net.ParseIP(remote)) {
			return u.String(), true
		}
	}
	return "", false
}

// dlnaSendEvent posts the initial GENA event (SEQ 0).
func dlnaSendEvent(target, sid, props string) {
	body := `<?xml version="1.0"?><e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0">` + props + `</e:propertyset>`
	req, err := http.NewRequest("NOTIFY", target, bytes.NewBufferString(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("NT", "upnp:event")
	req.Header.Set("NTS", "upnp:propchange")
	req.Header.Set("SID", sid)
	req.Header.Set("SEQ", "0")
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if resp, err := client.Do(req); err == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
	}
}

const contentDirSCPD = `<?xml version="1.0"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
<actionList>
<action><name>Browse</name><argumentList>
<argument><name>ObjectID</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_ObjectID</relatedStateVariable></argument>
<argument><name>BrowseFlag</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_BrowseFlag</relatedStateVariable></argument>
<argument><name>Filter</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Filter</relatedStateVariable></argument>
<argument><name>StartingIndex</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Index</relatedStateVariable></argument>
<argument><name>RequestedCount</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>SortCriteria</name><direction>in</direction><relatedStateVariable>A_ARG_TYPE_SortCriteria</relatedStateVariable></argument>
<argument><name>Result</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Result</relatedStateVariable></argument>
<argument><name>NumberReturned</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>TotalMatches</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_Count</relatedStateVariable></argument>
<argument><name>UpdateID</name><direction>out</direction><relatedStateVariable>A_ARG_TYPE_UpdateID</relatedStateVariable></argument>
</argumentList></action>
<action><name>GetSortCapabilities</name><argumentList><argument><name>SortCaps</name><direction>out</direction><relatedStateVariable>SortCapabilities</relatedStateVariable></argument></argumentList></action>
<action><name>GetSearchCapabilities</name><argumentList><argument><name>SearchCaps</name><direction>out</direction><relatedStateVariable>SearchCapabilities</relatedStateVariable></argument></argumentList></action>
<action><name>GetSystemUpdateID</name><argumentList><argument><name>Id</name><direction>out</direction><relatedStateVariable>SystemUpdateID</relatedStateVariable></argument></argumentList></action>
</actionList>
<serviceStateTable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_ObjectID</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_BrowseFlag</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Filter</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Index</name><dataType>ui4</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Count</name><dataType>ui4</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_SortCriteria</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_Result</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>A_ARG_TYPE_UpdateID</name><dataType>ui4</dataType></stateVariable>
<stateVariable sendEvents="no"><name>SortCapabilities</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>SearchCapabilities</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="yes"><name>SystemUpdateID</name><dataType>ui4</dataType></stateVariable>
</serviceStateTable>
</scpd>`

const connMgrSCPD = `<?xml version="1.0"?>
<scpd xmlns="urn:schemas-upnp-org:service-1-0">
<actionList>
<action><name>GetProtocolInfo</name><argumentList>
<argument><name>Source</name><direction>out</direction><relatedStateVariable>SourceProtocolInfo</relatedStateVariable></argument>
<argument><name>Sink</name><direction>out</direction><relatedStateVariable>SinkProtocolInfo</relatedStateVariable></argument>
</argumentList></action>
<action><name>GetCurrentConnectionIDs</name><argumentList><argument><name>ConnectionIDs</name><direction>out</direction><relatedStateVariable>CurrentConnectionIDs</relatedStateVariable></argument></argumentList></action>
</actionList>
<serviceStateTable>
<stateVariable sendEvents="yes"><name>SourceProtocolInfo</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="yes"><name>SinkProtocolInfo</name><dataType>string</dataType></stateVariable>
<stateVariable sendEvents="no"><name>CurrentConnectionIDs</name><dataType>string</dataType></stateVariable>
</serviceStateTable>
</scpd>`
