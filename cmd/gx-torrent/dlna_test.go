package main

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDLNADidlForObject(t *testing.T) {
	hash := strings.Repeat("ab", 20)
	browse := map[string][]dlnaEntry{
		"Show\x00" + hash: {
			{hash: hash, fileIndex: 1, name: "Show/ep1.mkv", size: 1234, mime: "video/x-matroska", video: true},
			{hash: hash, fileIndex: 2, name: "Show/ep2.mp3", size: 10, mime: "audio/mpeg"},
		},
	}
	// Root: one container per torrent, titled with the torrent name.
	didl, n, _ := didlForObject(browse, "0", "http://h:8200")
	if n != 1 || !strings.Contains(didl, "<dc:title>Show</dc:title>") || !strings.Contains(didl, "id=\"torrent:"+hash+"\"") {
		t.Fatalf("root DIDL wrong: %s", didl)
	}
	// Container: the media files, each with a Range-capable res URL and class.
	didl, n, _ = didlForObject(browse, "torrent:"+hash, "http://h:8200")
	if n != 2 {
		t.Fatalf("container items = %d, want 2: %s", n, didl)
	}
	if !strings.Contains(didl, "http://h:8200/res/"+hash+"/1") {
		t.Fatalf("media URL missing: %s", didl)
	}
	if !strings.Contains(didl, "<upnp:class>object.item.videoItem</upnp:class>") ||
		!strings.Contains(didl, "<upnp:class>object.item.audioItem</upnp:class>") {
		t.Fatalf("item classes wrong: %s", didl)
	}
	if !strings.Contains(didl, `protocolInfo="http-get:*:video/x-matroska:*"`) {
		t.Fatalf("protocolInfo missing: %s", didl)
	}
	// An unknown object yields an empty (but valid) DIDL.
	if _, n, _ := didlForObject(browse, "torrent:nope", "http://h"); n != 0 {
		t.Fatal("an unknown container must be empty")
	}
}

func TestDLNARootDesc(t *testing.T) {
	srv := &dlnaServer{name: "Casa", uuid: "uuid:test", base: "http://1.2.3.4:8200"}
	rec := httptest.NewRecorder()
	srv.handleRootDesc(rec, httptest.NewRequest("GET", "/rootDesc.xml", nil))
	body := rec.Body.String()
	for _, want := range []string{"<friendlyName>Casa</friendlyName>", dlnaDeviceType, dlnaContentDir, dlnaConnMgr, "<UDN>uuid:test</UDN>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("root desc missing %q:\n%s", want, body)
		}
	}
}

func TestDLNAContentDirBrowse(t *testing.T) {
	hash := strings.Repeat("cd", 20)
	srv := &dlnaServer{
		base: "http://h:8200",
		browse: func() map[string][]dlnaEntry {
			return map[string][]dlnaEntry{"Film\x00" + hash: {{hash: hash, fileIndex: 3, name: "Film/movie.mp4", size: 999, mime: "video/mp4", video: true}}}
		},
	}
	soap := func(objectID string) string {
		return `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>` +
			`<u:Browse xmlns:u="` + dlnaContentDir + `"><ObjectID>` + objectID + `</ObjectID>` +
			`<BrowseFlag>BrowseDirectChildren</BrowseFlag><Filter>*</Filter><StartingIndex>0</StartingIndex>` +
			`<RequestedCount>100</RequestedCount><SortCriteria></SortCriteria></u:Browse></s:Body></s:Envelope>`
	}
	post := func(objectID string) string {
		req := httptest.NewRequest("POST", "/control/ContentDirectory", strings.NewReader(soap(objectID)))
		req.Header.Set("SOAPAction", `"`+dlnaContentDir+`#Browse"`)
		rec := httptest.NewRecorder()
		srv.handleContentDirControl(rec, req)
		return rec.Body.String()
	}
	if body := post("0"); !strings.Contains(html.UnescapeString(body), "<dc:title>Film</dc:title>") || !strings.Contains(body, "<NumberReturned>1</NumberReturned>") {
		t.Fatalf("root browse wrong:\n%s", body)
	}
	if body := post("torrent:" + hash); !strings.Contains(html.UnescapeString(body), "http://h:8200/res/"+hash+"/3") {
		t.Fatalf("container browse wrong:\n%s", body)
	}
}

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
		!strings.Contains(resp[0], "ST: "+dlnaDeviceType) || !strings.Contains(resp[0], "USN: uuid:abc::"+dlnaDeviceType) {
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

func TestDLNAMediaRejectsBadRequest(t *testing.T) {
	srv := &dlnaServer{d: &Daemon{}}
	// A short hash is refused before touching the session.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/res/short/0", nil)
	req.SetPathValue("hash", "short")
	req.SetPathValue("file", "0")
	srv.handleMedia(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestDLNARoutes drives the real HTTP mux end to end, as a client reaches it.
func TestDLNARoutes(t *testing.T) {
	srv := &dlnaServer{name: "n", uuid: "uuid:r", base: "http://h:8200", d: &Daemon{}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	for _, path := range []string{"/rootDesc.xml", "/ContentDirectory.xml", "/ConnectionManager.xml"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("%s -> %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
	resp, err := http.Get(ts.URL + "/res/short/0")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("bad media path -> %d, want 404", resp.StatusCode)
	}
}
