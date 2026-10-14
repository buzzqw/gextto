package main

import (
	"encoding/xml"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	dlnaHashA = strings.Repeat("ab", 20)
	dlnaHashB = strings.Repeat("cd", 20)
)

// dlnaTestFolders is a listing of two completed torrents: a series with two
// episodes and their subtitles plus non-media files, and an album.
func dlnaTestFolders() []dlnaFolder {
	series, _ := dlnaFolderFor(dlnaHashA, "Show S01", []dlnaFile{
		{path: "Show/ep2.mkv", length: 2000},
		{path: "Show/ep1.mkv", length: 1000},
		{path: "Show/ep1.srt", length: 10},
		{path: "Show/ep1.it.srt", length: 11},
		{path: "Show/other/ep1.srt", length: 12}, // another directory: not attached
		{path: "Show/info.nfo", length: 5},
	})
	album, _ := dlnaFolderFor(dlnaHashB, "Album", []dlnaFile{{path: "Album/01 song.flac", length: 300}})
	return []dlnaFolder{series, album}
}

func TestDLNAFolderFor(t *testing.T) {
	f := dlnaTestFolders()[0]
	if len(f.items) != 2 || f.items[0].name != "Show/ep1.mkv" || f.items[1].name != "Show/ep2.mkv" {
		t.Fatalf("items = %+v, want the two episodes sorted (no subtitles, no nfo)", f.items)
	}
	ep1 := f.items[0]
	if ep1.fileIndex != 1 || !ep1.video || ep1.mime != "video/x-matroska" {
		t.Fatalf("ep1 = %+v", ep1)
	}
	if len(ep1.subs) != 2 || ep1.subs[0].fileIndex != 2 || ep1.subs[1].fileIndex != 3 || ep1.subs[0].kind != "srt" {
		t.Fatalf("ep1 subtitles = %+v, want ep1.srt and ep1.it.srt", ep1.subs)
	}
	if len(f.items[1].subs) != 0 {
		t.Fatal("ep2 has no subtitles")
	}
	if _, ok := dlnaFolderFor(dlnaHashA, "docs", []dlnaFile{{path: "a.pdf"}, {path: "a.srt"}}); ok {
		t.Fatal("a torrent without playable files must not be listed")
	}
}

func TestDLNALibraryBrowse(t *testing.T) {
	lib := newDLNALibrary(dlnaTestFolders())
	base := "http://h:8200"

	// The root itself (BrowseMetadata) as many TVs ask first.
	didl, n, total, fault := lib.browse("0", "BrowseMetadata", 0, 0, "Casa", base)
	if fault != nil || n != 1 || total != 1 || !strings.Contains(didl, `id="0" parentID="-1"`) ||
		!strings.Contains(didl, `childCount="2"`) || !strings.Contains(didl, "<dc:title>Casa</dc:title>") {
		t.Fatalf("root metadata wrong (%v): %s", fault, didl)
	}
	// Root children: the containers sorted by name.
	didl, n, total, _ = lib.browse("0", "BrowseDirectChildren", 0, 0, "Casa", base)
	if n != 2 || total != 2 || strings.Index(didl, "Album") > strings.Index(didl, "Show S01") {
		t.Fatalf("root children wrong: %s", didl)
	}
	// Paging: one at a time, TotalMatches stays the full count.
	didl, n, total, _ = lib.browse("0", "BrowseDirectChildren", 1, 1, "Casa", base)
	if n != 1 || total != 2 || !strings.Contains(didl, "Show S01") || strings.Contains(didl, "Album") {
		t.Fatalf("page 2 wrong (%d/%d): %s", n, total, didl)
	}
	if _, n, total, _ = lib.browse("0", "BrowseDirectChildren", 5, 10, "Casa", base); n != 0 || total != 2 {
		t.Fatalf("a page past the end = %d/%d, want 0/2", n, total)
	}
	// A container: items with DLNA protocolInfo, the media URL and subtitles.
	didl, n, _, _ = lib.browse("torrent:"+dlnaHashA, "BrowseDirectChildren", 0, 0, "Casa", base)
	if n != 2 {
		t.Fatalf("container items = %d, want 2: %s", n, didl)
	}
	for _, want := range []string{
		`<dc:title>ep1</dc:title>`,
		`protocolInfo="http-get:*:video/x-matroska:` + dlnaFeatures + `"`,
		`>http://h:8200/res/` + dlnaHashA + `/1</res>`,
		`<sec:CaptionInfoEx sec:type="srt">http://h:8200/res/` + dlnaHashA + `/2</sec:CaptionInfoEx>`,
		`<upnp:class>object.item.videoItem</upnp:class>`,
	} {
		if !strings.Contains(didl, want) {
			t.Fatalf("container DIDL misses %q:\n%s", want, didl)
		}
	}
	if err := xml.Unmarshal([]byte(didl), new(struct{})); err != nil {
		t.Fatalf("DIDL is not well-formed XML: %v", err)
	}
	// One item's metadata, and an item has no children.
	if didl, n, _, _ = lib.browse("file:"+dlnaHashB+":0", "BrowseMetadata", 0, 0, "Casa", base); n != 1 ||
		!strings.Contains(didl, "object.item.audioItem.musicTrack") || !strings.Contains(didl, `parentID="torrent:`+dlnaHashB+`"`) {
		t.Fatalf("item metadata wrong: %s", didl)
	}
	if _, n, _, fault = lib.browse("file:"+dlnaHashB+":0", "BrowseDirectChildren", 0, 0, "Casa", base); n != 0 || fault != nil {
		t.Fatal("an item must have no children")
	}
	// Unknown objects and bad flags are UPnP errors.
	for _, id := range []string{"torrent:nope", "file:" + dlnaHashA + ":2", "file:x", "junk"} {
		if _, _, _, fault := lib.browse(id, "BrowseMetadata", 0, 0, "Casa", base); fault != dlnaNoSuchObject {
			t.Fatalf("%q: fault = %v, want 701", id, fault)
		}
	}
	if _, _, _, fault := lib.browse("0", "Nope", 0, 0, "Casa", base); fault != dlnaInvalidArgs {
		t.Fatal("a bad BrowseFlag must be 402")
	}
}

func TestDLNALibraryServableFilesAndUpdateID(t *testing.T) {
	lib := newDLNALibrary(dlnaTestFolders())
	for _, k := range []dlnaFileKey{{dlnaHashA, 0}, {dlnaHashA, 1}, {dlnaHashA, 2}, {dlnaHashA, 3}, {dlnaHashB, 0}} {
		if _, ok := lib.files[k]; !ok {
			t.Fatalf("%v must be servable", k)
		}
	}
	for _, k := range []dlnaFileKey{{dlnaHashA, 4}, {dlnaHashA, 5}, {dlnaHashB, 1}} {
		if _, ok := lib.files[k]; ok {
			t.Fatalf("%v (not offered) must not be servable", k)
		}
	}
	same := newDLNALibrary(dlnaTestFolders())
	if lib.updateID == 0 || lib.updateID != same.updateID {
		t.Fatal("the update id must be stable for the same listing and never 0")
	}
	if fewer := newDLNALibrary(dlnaTestFolders()[:1]); fewer.updateID == lib.updateID {
		t.Fatal("the update id must change when the listing changes")
	}
}

func dlnaTestServer() *dlnaServer {
	return &dlnaServer{name: "Casa", uuid: "uuid:test", base: "http://1.2.3.4:8200", d: &Daemon{}, browse: dlnaTestFolders}
}

func TestDLNARootDesc(t *testing.T) {
	srv := dlnaTestServer()
	srv.name = "Casa & <co>"
	rec := httptest.NewRecorder()
	srv.handleRootDesc(rec, httptest.NewRequest("GET", "/rootDesc.xml", nil))
	body := rec.Body.String()
	for _, want := range []string{"<friendlyName>Casa &amp; &lt;co&gt;</friendlyName>", dlnaDeviceType, dlnaContentDir, dlnaConnMgr, "<UDN>uuid:test</UDN>", "DMS-1.50"} {
		if !strings.Contains(body, want) {
			t.Fatalf("root desc missing %q:\n%s", want, body)
		}
	}
}

// soapBrowse posts a Browse to the server's handler and returns the reply.
func soapBrowse(srv *dlnaServer, objectID, flag, start, count string) *httptest.ResponseRecorder {
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
		`<u:Browse xmlns:u="` + dlnaContentDir + `"><ObjectID>` + objectID + `</ObjectID>` +
		`<BrowseFlag>` + flag + `</BrowseFlag><Filter>*</Filter><StartingIndex>` + start + `</StartingIndex>` +
		`<RequestedCount>` + count + `</RequestedCount><SortCriteria></SortCriteria></u:Browse></s:Body></s:Envelope>`
	req := httptest.NewRequest("POST", "http://10.0.0.5:8200/control/ContentDirectory", strings.NewReader(body))
	req.Header.Set("SOAPAction", `"`+dlnaContentDir+`#Browse"`)
	rec := httptest.NewRecorder()
	srv.handleContentDirControl(rec, req)
	return rec
}

func TestDLNAContentDirBrowse(t *testing.T) {
	srv := dlnaTestServer()
	rec := soapBrowse(srv, "0", "BrowseDirectChildren", "0", "1")
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "<NumberReturned>1</NumberReturned>") || !strings.Contains(body, "<TotalMatches>2</TotalMatches>") {
		t.Fatalf("paged root browse wrong:\n%s", body)
	}
	if !strings.Contains(body, "<UpdateID>"+itoa(int(srv.library().updateID))+"</UpdateID>") {
		t.Fatalf("UpdateID missing:\n%s", body)
	}
	// The links use the Host the client reached.
	body = soapBrowse(srv, "torrent:"+dlnaHashB, "BrowseDirectChildren", "0", "0").Body.String()
	if !strings.Contains(html.UnescapeString(body), "http://10.0.0.5:8200/res/"+dlnaHashB+"/0") {
		t.Fatalf("container browse wrong:\n%s", body)
	}
	if rec := soapBrowse(srv, "torrent:nope", "BrowseDirectChildren", "0", "0"); rec.Code != 500 || !strings.Contains(rec.Body.String(), "<errorCode>701</errorCode>") {
		t.Fatalf("unknown object -> %d %s", rec.Code, rec.Body.String())
	}
	if rec := soapBrowse(srv, "0", "BrowseDirectChildren", "-1", "0"); !strings.Contains(rec.Body.String(), "<errorCode>402</errorCode>") {
		t.Fatalf("a negative index must be 402: %s", rec.Body.String())
	}
	// Other actions.
	req := httptest.NewRequest("POST", "/control/ContentDirectory", nil)
	req.Header.Set("SOAPAction", `"`+dlnaContentDir+`#GetSystemUpdateID"`)
	rec = httptest.NewRecorder()
	srv.handleContentDirControl(rec, req)
	if !strings.Contains(rec.Body.String(), "<Id>"+itoa(int(srv.library().updateID))+"</Id>") {
		t.Fatalf("GetSystemUpdateID wrong: %s", rec.Body.String())
	}
	req = httptest.NewRequest("POST", "/control/ConnectionManager", nil)
	req.Header.Set("SOAPAction", `"`+dlnaConnMgr+`#GetProtocolInfo"`)
	rec = httptest.NewRecorder()
	srv.handleConnMgrControl(rec, req)
	if !strings.Contains(rec.Body.String(), "http-get:*:video/mp4:DLNA.ORG_OP=01") {
		t.Fatalf("GetProtocolInfo lists no source: %s", rec.Body.String())
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestDLNASsdp(t *testing.T) {
	srv := &dlnaServer{name: "gx", uuid: "uuid:abc", base: "http://1.2.3.4:8200"}
	if !srv.ssdpMatches(dlnaDeviceType) || !srv.ssdpMatches("ssdp:all") {
		t.Fatal("SEARCH targets that must match")
	}
	if srv.ssdpMatches("urn:schemas-upnp-org:device:InternetGatewayDevice:1") || srv.ssdpMatches("") {
		t.Fatal("SEARCH targets that must not match")
	}
	resp := srv.ssdpResponses(dlnaDeviceType)
	if len(resp) != 1 || !strings.Contains(resp[0], "LOCATION: http://1.2.3.4:8200/rootDesc.xml") ||
		!strings.Contains(resp[0], "ST: "+dlnaDeviceType) || !strings.Contains(resp[0], "USN: uuid:abc::"+dlnaDeviceType) ||
		!strings.Contains(resp[0], "DATE: ") {
		t.Fatalf("SSDP reply wrong:\n%s", strings.Join(resp, "\n---\n"))
	}
	if all := srv.ssdpResponses("ssdp:all"); len(all) != len(srv.ssdpSTs()) {
		t.Fatalf("ssdp:all replies = %d, want %d", len(all), len(srv.ssdpSTs()))
	}
	headers, ok := parseMSearch([]byte("M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nST: ssdp:all\r\nMX: 2\r\n\r\n"))
	if !ok || headers["st"] != "ssdp:all" || headers["mx"] != "2" {
		t.Fatalf("parseMSearch = %v ok=%v", headers, ok)
	}
	if _, ok := parseMSearch([]byte("NOTIFY * HTTP/1.1\r\n")); ok {
		t.Fatal("a NOTIFY must not parse as M-SEARCH")
	}
}

func TestDLNALocalAddr(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "192.168.1.20", "10.1.2.3", "172.16.0.9", "169.254.1.1", "::1", "fd00::5", "fe80::1"} {
		if !dlnaLocalAddr(net.ParseIP(ip)) {
			t.Errorf("%s must be local", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "93.184.216.34", "2001:4860::8888", ""} {
		if dlnaLocalAddr(net.ParseIP(ip)) {
			t.Errorf("%q must not be local", ip)
		}
	}
}

func TestDLNACallback(t *testing.T) {
	if u, ok := dlnaCallback("<http://192.168.1.9:7000/ev>", "192.168.1.9:5555"); !ok || u != "http://192.168.1.9:7000/ev" {
		t.Fatalf("callback = %q %v", u, ok)
	}
	if u, ok := dlnaCallback("<http://10.0.0.1/a><http://192.168.1.9:7000/b>", "192.168.1.9:5555"); !ok || !strings.HasSuffix(u, "/b") {
		t.Fatalf("the subscriber's own URL must be picked: %q %v", u, ok)
	}
	for _, bad := range []string{"", "<http://10.0.0.1/a>", "<https://192.168.1.9/a>", "<file:///etc/passwd>", "garbage"} {
		if _, ok := dlnaCallback(bad, "192.168.1.9:5555"); ok {
			t.Errorf("callback %q must be refused", bad)
		}
	}
}

// TestDLNASubscribe: a subscription is granted with a SID and the initial
// event reaches the subscriber's callback; a renewal keeps the SID.
func TestDLNASubscribe(t *testing.T) {
	got := make(chan string, 1)
	cb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got <- r.Method + " " + r.Header.Get("NT") + " " + string(b)
	}))
	defer cb.Close()
	srv := dlnaTestServer()
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	req, _ := http.NewRequest("SUBSCRIBE", ts.URL+"/event/ContentDirectory", nil)
	req.Header.Set("CALLBACK", "<"+cb.URL+"/ev>")
	req.Header.Set("NT", "upnp:event")
	req.Header.Set("TIMEOUT", "Second-300")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	sid := resp.Header.Get("SID")
	if resp.StatusCode != 200 || !strings.HasPrefix(sid, "uuid:") || resp.Header.Get("TIMEOUT") == "" {
		t.Fatalf("SUBSCRIBE -> %d SID=%q", resp.StatusCode, sid)
	}
	select {
	case ev := <-got:
		if !strings.HasPrefix(ev, "NOTIFY upnp:event ") || !strings.Contains(ev, "<SystemUpdateID>") {
			t.Fatalf("initial event wrong: %s", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no initial event")
	}
	renew, _ := http.NewRequest("SUBSCRIBE", ts.URL+"/event/ContentDirectory", nil)
	renew.Header.Set("SID", sid)
	if resp, err = http.DefaultClient.Do(renew); err != nil || resp.StatusCode != 200 || resp.Header.Get("SID") != sid {
		t.Fatalf("renewal failed: %v", err)
	}
	resp.Body.Close()
	bad, _ := http.NewRequest("SUBSCRIBE", ts.URL+"/event/ContentDirectory", nil)
	bad.Header.Set("CALLBACK", "<http://10.255.0.1/ev>") // not the subscriber
	bad.Header.Set("NT", "upnp:event")
	if resp, err = http.DefaultClient.Do(bad); err != nil || resp.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("a foreign callback must be refused: %v %v", err, resp.StatusCode)
	}
	resp.Body.Close()
}

// TestDLNAMediaGuard: /res serves only what the listing offers.
func TestDLNAMediaGuard(t *testing.T) {
	srv := dlnaTestServer()
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	for _, p := range []string{
		"/res/short/0",
		"/res/" + dlnaHashA + "/5", // the .nfo: in the torrent, not offered
		"/res/" + strings.Repeat("ef", 20) + "/0",
		"/res/" + dlnaHashA + "/x",
	} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("%s -> %d, want 404", p, resp.StatusCode)
		}
	}
}

// TestDLNARoutesAndLANGuard drives the real mux: the descriptions answer a LAN
// client and every route refuses a public source address.
func TestDLNARoutesAndLANGuard(t *testing.T) {
	srv := dlnaTestServer()
	h := srv.handler()
	for _, path := range []string{"/rootDesc.xml", "/ContentDirectory.xml", "/ConnectionManager.xml"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "192.168.1.30:40000"
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s -> %d", path, rec.Code)
		}
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "8.8.8.8:40000"
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s from a public address -> %d, want 403", path, rec.Code)
		}
	}
}
