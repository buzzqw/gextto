package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDaemonPeerOptionsActionAndPage sets the bandwidth share and the peer
// sources of a torrent through the API action and the daemon page, and
// reads them back from the list.
func TestDaemonPeerOptionsActionAndPage(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 50_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := findInfo(d, hash); info.BandwidthWeight != 100 || !info.DHT || !info.PEX || !info.LSD {
		t.Fatalf("defaults: %+v", info)
	}
	weight := int64(300)
	off := false
	if err := d.setPeerOptions(hash, &weight, &off, nil, &off); err != nil {
		t.Fatal(err)
	}
	if info, _ := findInfo(d, hash); info.BandwidthWeight != 300 || info.DHT || !info.PEX || info.LSD {
		t.Fatalf("after the action: %+v", info)
	}
	bad := int64(5)
	if err := d.setPeerOptions(hash, &bad, nil, nil, nil); err == nil {
		t.Fatal("a 5% weight was accepted")
	}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	form := url.Values{"hash": {hash}, "bandwidth_weight": {"50"}, "dht": {"1"}, "pex": {"1"}, "lsd": {"1"}}
	resp, err := http.Post(srv.URL+"/ui/peer-options", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if info, _ := findInfo(d, hash); info.BandwidthWeight != 50 || !info.DHT || !info.PEX || !info.LSD {
		t.Fatalf("after the page: %+v", info)
	}
}

func TestParseTrackerListKeepsValidAddresses(t *testing.T) {
	list := parseTrackerList(strings.NewReader("# comment\nudp://tracker.example:1337/announce\n\nhttps://t.example/announce\nudp://tracker.example:1337/announce\nnot a url\nftp://x.example/\nwss://w.example/\n"))
	want := []string{"udp://tracker.example:1337/announce", "https://t.example/announce", "wss://w.example/"}
	if strings.Join(list, ",") != strings.Join(want, ",") {
		t.Fatalf("list = %v", list)
	}
}

// TestGeoDBReadsBothFormats reads a DB-IP style file (addresses) and an
// IP2Location style one (integers), IPv4 and IPv6.
func TestGeoDBReadsBothFormats(t *testing.T) {
	dbip := "1.0.0.0,1.0.0.255,AU\n2.16.0.0,2.16.255.255,FR\n2001:db8::,2001:db8:ffff:ffff:ffff:ffff:ffff:ffff,IT\n10.0.0.0,10.255.255.255,ZZ\n"
	db, err := loadGeoDB(strings.NewReader(dbip))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"1.0.0.7": "AU", "2.16.4.4": "FR", "2001:db8::42": "IT", "10.1.1.1": "", "8.8.8.8": ""}
	for addr, want := range cases {
		if got := db.country(parseIP(addr)); got != want {
			t.Errorf("DB-IP %s = %q, want %q", addr, got, want)
		}
	}
	ip2l := `"16777216","16777471","AU","Australia"` + "\n" + `"34603008","34668543","FR","France"` + "\n"
	db, err = loadGeoDB(strings.NewReader(ip2l))
	if err != nil {
		t.Fatal(err)
	}
	if got := db.country(parseIP("1.0.0.7")); got != "AU" {
		t.Fatalf("IP2Location = %q", got)
	}
	if _, err := loadGeoDB(strings.NewReader("garbage\n")); err == nil {
		t.Fatal("a file without ranges was accepted")
	}
}

// TestPeerCountryUsesTheConfiguredFile loads the database from the flag.
func TestPeerCountryUsesTheConfiguredFile(t *testing.T) {
	d := newTestDaemon(t)
	path := filepath.Join(t.TempDir(), "geo.csv")
	if err := os.WriteFile(path, []byte("1.0.0.0,1.0.0.255,AU\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := d.peerCountry("1.0.0.9:6881"); got != "" {
		t.Fatalf("without a file: %q", got)
	}
	d.opts.Network.GeoIPFile = path
	if got := d.peerCountry("1.0.0.9:6881"); got != "AU" {
		t.Fatalf("with a file: %q", got)
	}
}

func parseIP(s string) net.IP { return net.ParseIP(s) }
