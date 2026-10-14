package main

// dlna.go implements an opt-in DLNA/UPnP AV media server for the daemon: a TV
// or console on the LAN can browse the completed torrents' media files and
// play them. It speaks SSDP (discovery) and the UPnP AV ContentDirectory
// (browse); the media themselves are served with HTTP Range by the same core
// as the UI player (stream.go), so a file still being downloaded streams as
// soon as the wanted pieces are there. It is LAN-only and off by default.

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"path"
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
	// dlnaClassUnknownContainer/title sizing are constants so the DIDL is
	// stable and testable.
	dlnaRootParent = "-1"
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

// runDLNA keeps the media server in step with the settings: it starts it when
// enabled and stops it when disabled, without needing the settings handler to
// know about it. Runs beside the queue loop.
func (d *Daemon) runDLNA(stop <-chan struct{}) {
	var srv *dlnaServer
	apply := func() {
		if d.dlnaEnabled() {
			if srv == nil {
				started, err := d.startDLNA()
				if err != nil {
					logf("DLNA: cannot start the media server: %v", err)
					return
				}
				srv = started
				logf("DLNA: media server «%s» listening on %s (HTTP %s)", srv.name, srv.httpAddr, srv.baseURL())
			}
		} else if srv != nil {
			srv.Close()
			srv = nil
			logf("DLNA: media server stopped")
		}
	}
	apply()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			if srv != nil {
				srv.Close()
			}
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
	listener net.Listener
	httpAddr string
	// base is the http://ip:port the LOCATION and the media URLs use.
	base string
	// browse returns the media listing; nil uses the daemon's torrents. It is
	// a field so tests can drive the DIDL without a live session.
	browse  func() map[string][]dlnaEntry
	ssdp    net.PacketConn
	ssdpDst *net.UDPAddr
	closeC  chan struct{}
	once    sync.Once
}

// startDLNA binds the HTTP listener and the SSDP socket and starts both.
func (d *Daemon) startDLNA() (*dlnaServer, error) {
	ln, err := net.Listen("tcp", d.dlnaListen())
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ip := dlnaLocalIP(ln.Addr().(*net.TCPAddr).IP)
	srv := &dlnaServer{
		d:        d,
		name:     d.dlnaServerName(),
		uuid:     dlnaUUID(),
		listener: ln,
		httpAddr: ln.Addr().String(),
		base:     fmt.Sprintf("http://%s", net.JoinHostPort(ip.String(), strconv.Itoa(port))),
		closeC:   make(chan struct{}),
	}
	srv.httpSrv = &http.Server{Handler: srv.handler(), ReadHeaderTimeout: 10 * time.Second}
	go srv.httpSrv.Serve(ln)

	// SSDP: join the multicast group on the interface that owns the LAN IP and
	// answer M-SEARCH. A failure here is not fatal: the server still works for
	// clients that find it by URL, so it is logged only.
	if err := srv.startSSDP(ip); err != nil {
		logf("DLNA: SSDP unavailable (%v); the server still answers on %s", err, srv.base)
	}
	return srv, nil
}

// baseURL is the root the LOCATION and the media URLs use.
func (s *dlnaServer) baseURL() string { return s.base }

// handler builds the HTTP mux: the device/service descriptions, the SOAP
// control endpoints and the media route.
func (s *dlnaServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rootDesc.xml", s.handleRootDesc)
	mux.HandleFunc("GET /ContentDirectory.xml", s.handleContentDirSCPD)
	mux.HandleFunc("GET /ConnectionManager.xml", s.handleConnMgrSCPD)
	mux.HandleFunc("POST /control/ContentDirectory", s.handleContentDirControl)
	mux.HandleFunc("POST /control/ConnectionManager", s.handleConnMgrControl)
	mux.HandleFunc("GET /res/{hash}/{file}", s.handleMedia)
	return mux
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
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		ip := conn.LocalAddr().(*net.UDPAddr).IP
		conn.Close()
		if ip != nil && !ip.IsUnspecified() {
			return ip
		}
	}
	return net.ParseIP("127.0.0.1")
}

// dlnaQuery renders a UUID-shaped device serial from the host and the port, so
// two instances on one host differ and it stays stable across restarts.
func dlnaUUID() string {
	host, _ := os.Hostname()
	var sum uint32 = 2166136261
	for i := 0; i < len(host); i++ {
		sum = (sum ^ uint32(host[i])) * 16777619
	}
	return fmt.Sprintf("uuid:6b7a9c2e-3f1d-4a5b-8c9d-%012x", sum)
}

// handleMedia serves one media file of a torrent with Range, LAN, no auth.
func (s *dlnaServer) handleMedia(w http.ResponseWriter, r *http.Request) {
	hash := strings.TrimSpace(r.PathValue("hash"))
	fileIndex, err := strconv.Atoi(strings.TrimSpace(r.PathValue("file")))
	if err != nil || fileIndex < 0 || len(hash) != 40 {
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
	d.serveTorrentFile(w, r, hash, t, meta, fileIndex)
}

// dlnaEntry is one media file offered to a renderer.
type dlnaEntry struct {
	hash      string
	fileIndex int
	name      string
	size      int64
	mime      string
	video     bool
}

// dlnaBrowse lists the media files of the completed torrents, grouped by
// torrent. Only fully downloaded torrents are exposed: a renderer probes the
// container metadata (moov/cue) and a partial file plays badly, so the safe
// default is completed material.
func (d *dlnaServer) dlnaBrowse() map[string][]dlnaEntry {
	out := map[string][]dlnaEntry{}
	if d.d.session == nil {
		return out
	}
	for _, t := range d.d.session.ListTorrents() {
		st := t.Stats()
		if st.Bytes.Incomplete > 0 || st.Bytes.Total == 0 {
			continue
		}
		files, err := t.Files()
		if err != nil {
			continue
		}
		name := t.Name()
		hash := t.InfoHash().String()
		var entries []dlnaEntry
		for i, f := range files {
			if !isMediaFile(f.Path()) {
				continue
			}
			mime := mediaContentType(f.Path())
			entries = append(entries, dlnaEntry{
				hash:      hash,
				fileIndex: i,
				name:      f.Path(),
				size:      f.Length(),
				mime:      mime,
				video:     strings.HasPrefix(mime, "video/"),
			})
		}
		if len(entries) > 0 {
			out[name+"\x00"+hash] = entries
		}
	}
	return out
}

// didlClass names the upnp class of an entry.
func (e dlnaEntry) didlClass() string {
	if e.video {
		return "object.item.videoItem"
	}
	return "object.item.audioItem"
}

// dlnaEntryKey is the object id of one media file.
func dlnaEntryKey(hash string, index int) string {
	return fmt.Sprintf("file:%s:%d", hash, index)
}

// dlnaContainerID is the object id of a torrent container.
func dlnaContainerID(hash string) string { return "torrent:" + hash }

// browseData returns the current media listing.
func (s *dlnaServer) browseData() map[string][]dlnaEntry {
	if s.browse != nil {
		return s.browse()
	}
	return s.dlnaBrowse()
}

// browseDIDL renders the DIDL-Lite for one object: the root lists the torrent
// containers, a container lists its media files.
func (s *dlnaServer) browseDIDL(objectID string) (didl string, total int, updateID int) {
	return didlForObject(s.browseData(), objectID, s.base)
}

// didlForObject renders the DIDL-Lite of one object id from a media listing. It
// is pure, so it can be tested without a session.
func didlForObject(browse map[string][]dlnaEntry, objectID, base string) (didl string, total int, updateID int) {
	if objectID == "" || objectID == "0" {
		var b strings.Builder
		b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`)
		names := make([]string, 0, len(browse))
		for k := range browse {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			name, hash, _ := strings.Cut(k, "\x00")
			b.WriteString(`<container id="` + xmlEscape(dlnaContainerID(hash)) + `" parentID="0" restricted="1" searchable="0">` +
				`<dc:title>` + xmlEscape(name) + `</dc:title>` +
				`<upnp:class>object.container.storageFolder</upnp:class></container>`)
		}
		b.WriteString(`</DIDL-Lite>`)
		return b.String(), len(names), 0
	}
	hash := strings.TrimPrefix(objectID, "torrent:")
	if hash == objectID { // a file id browsed directly: nothing below it
		return `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"></DIDL-Lite>`, 0, 0
	}
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/">`)
	count := 0
	for _, entries := range browse {
		for _, e := range entries {
			if e.hash != hash {
				continue
			}
			title := path.Base(e.name)
			b.WriteString(`<item id="` + xmlEscape(dlnaEntryKey(e.hash, e.fileIndex)) + `" parentID="` + xmlEscape(dlnaContainerID(e.hash)) + `" restricted="1">` +
				`<dc:title>` + xmlEscape(title) + `</dc:title>` +
				`<upnp:class>` + e.didlClass() + `</upnp:class>` +
				`<res protocolInfo="http-get:*:` + e.mime + `:*" size="` + strconv.FormatInt(e.size, 10) + `">` +
				xmlEscape(mediaURL(base, e)) + `</res></item>`)
			count++
		}
	}
	b.WriteString(`</DIDL-Lite>`)
	return b.String(), count, 0
}

// mediaURL is the absolute URL of one media file.
func mediaURL(base string, e dlnaEntry) string {
	return fmt.Sprintf("%s/res/%s/%d", base, e.hash, e.fileIndex)
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
<root xmlns="urn:schemas-upnp-org:device-1-0">
<specVersion><major>1</major><minor>0</minor></specVersion>
<device>
<deviceType>%s</deviceType>
<friendlyName>%s</friendlyName>
<manufacturer>gx-torrent</manufacturer>
<modelName>gx-torrent media server</modelName>
<UDN>%s</UDN>
<serviceList>
<service><serviceType>%s</serviceType><serviceId>urn:upnp-org:serviceId:ContentDirectory</serviceId><SCPDURL>/ContentDirectory.xml</SCPDURL><controlURL>/control/ContentDirectory</controlURL><eventSubURL>/event/ContentDirectory</eventSubURL></service>
<service><serviceType>%s</serviceType><serviceId>urn:upnp-org:serviceId:ConnectionManager</serviceId><SCPDURL>/ConnectionManager.xml</SCPDURL><controlURL>/control/ConnectionManager</controlURL><eventSubURL>/event/ConnectionManager</eventSubURL></service>
</serviceList>
</device>
</root>`, dlnaDeviceType, html.EscapeString(s.name), s.uuid, dlnaContentDir, dlnaConnMgr)
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
			StartingIndex  int    `xml:"StartingIndex"`
			RequestedCount int    `xml:"RequestedCount"`
		} `xml:"Browse"`
	} `xml:"Body"`
}

func (s *dlnaServer) handleContentDirControl(w http.ResponseWriter, r *http.Request) {
	action := soapAction(r)
	switch action {
	case "Browse":
		var env soapEnvelope
		body := io.LimitReader(r.Body, 1<<20)
		dec := xml.NewDecoder(body)
		if err := dec.Decode(&env); err != nil {
			soapFault(w, 402, "Invalid Args")
			return
		}
		didl, count, updateID := s.browseDIDL(strings.TrimSpace(env.Body.Browse.ObjectID))
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		fmt.Fprintf(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body>
<u:BrowseResponse xmlns:u="%s">
<Result>%s</Result>
<NumberReturned>%d</NumberReturned>
<TotalMatches>%d</TotalMatches>
<UpdateID>%d</UpdateID>
</u:BrowseResponse>
</s:Body>
</s:Envelope>`, dlnaContentDir, xmlEscape(didl), count, count, updateID)
	case "GetSortCapabilities":
		soapReply(w, dlnaContentDir, "GetSortCapabilities", "<SortCaps>dc:title</SortCaps>")
	case "GetSearchCapabilities":
		soapReply(w, dlnaContentDir, "GetSearchCapabilities", "<SearchCaps></SearchCaps>")
	case "GetSystemUpdateID":
		soapReply(w, dlnaContentDir, "GetSystemUpdateID", "<Id>0</Id>")
	default:
		soapFault(w, 401, "Invalid Action")
	}
}

func (s *dlnaServer) handleConnMgrControl(w http.ResponseWriter, r *http.Request) {
	switch soapAction(r) {
	case "GetProtocolInfo":
		soapReply(w, dlnaConnMgr, "GetProtocolInfo", "<Source></Source><Sink></Sink>")
	case "GetCurrentConnectionIDs":
		soapReply(w, dlnaConnMgr, "GetCurrentConnectionIDs", "<ConnectionIDs>0</ConnectionIDs>")
	default:
		soapFault(w, 401, "Invalid Action")
	}
}

// soapAction extracts the action name from the SOAPAction header or the body.
func soapAction(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get("SOAPAction"))
	raw = strings.Trim(raw, `"`)
	if raw == "" {
		return ""
	}
	if i := strings.LastIndex(raw, "#"); i >= 0 {
		return raw[i+1:]
	}
	return raw
}

// soapReply writes a one-element SOAP response.
func soapReply(w http.ResponseWriter, service, action, inner string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	fmt.Fprintf(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body><u:%sResponse xmlns:u="%s">%s</u:%sResponse></s:Body>
</s:Envelope>`, action, service, inner, action)
}

// soapFault writes a UPnP error.
func soapFault(w http.ResponseWriter, code int, desc string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">
<s:Body><s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail>
<UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode><errorDescription>%s</errorDescription></UPnPError>
</detail></s:Fault></s:Body></s:Envelope>`, code, html.EscapeString(desc))
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
