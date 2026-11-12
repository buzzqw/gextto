package main

import (
	"bytes"
	"compress/gzip"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestPeerCountryUsesTheConfiguredFile loads the operator's file in the
// background on the first lookup and answers once it is ready.
func TestPeerCountryUsesTheConfiguredFile(t *testing.T) {
	d := newTestDaemon(t)
	path := filepath.Join(t.TempDir(), "geo.csv")
	if err := os.WriteFile(path, []byte("1.0.0.0,1.0.0.255,AU\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := d.peerCountry("1.0.0.9:6881"); got != "" {
		t.Fatalf("with the column off: %q", got)
	}
	d.opts.Network.PeerCountry = true
	d.opts.Network.GeoIPFile = path
	waitFor(t, "country loaded", func() bool { return d.peerCountry("1.0.0.9:6881") == "AU" })
	if d.geoAutomatic() {
		t.Fatal("an operator file is not the automatic database")
	}
	// Unused for long enough, the database leaves memory.
	d.geo.mu.Lock()
	d.geo.lastUsed = time.Now().Add(-2 * geoIdleUnload)
	d.geo.mu.Unlock()
	stop := make(chan struct{})
	go d.runGeoIP(stop)
	waitFor(t, "database dropped", func() bool {
		d.geo.mu.Lock()
		defer d.geo.mu.Unlock()
		return d.geo.db == nil
	})
	close(stop)
}

// TestGeoAutomaticDownload fetches the monthly DB-IP file (the previous
// month when this one is not out) into the data folder and reads it.
func TestGeoAutomaticDownload(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if strings.Contains(r.URL.Path, "2026-11") {
			http.NotFound(w, r)
			return
		}
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write([]byte("0.0.0.0,0.255.255.255,ZZ\n1.0.0.0,1.0.0.255,AU\n"))
		_ = gz.Close()
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()
	old := geoAutoURL
	geoAutoURL = func(month time.Time) string { return srv.URL + "/" + month.Format("2006-01") + ".csv.gz" }
	defer func() { geoAutoURL = old }()
	// A daemon without a run loop: only this test asks for the file.
	d := &Daemon{opts: Options{DataDir: t.TempDir(), Network: NetworkOptions{PeerCountry: true}}}
	path := filepath.Join(d.opts.DataDir, geoAutoFile)
	d.fetchGeo(path, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
	if len(asked) != 2 || asked[0] != "/2026-11.csv.gz" || asked[1] != "/2026-10.csv.gz" {
		t.Fatalf("asked %v, want this month then the previous one", asked)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no database saved: %v", err)
	}
	waitFor(t, "automatic country", func() bool { return d.peerCountry("1.0.0.1") == "AU" })
	if st := d.geoStatusSnapshot(); !st.Automatic || st.FetchedAt == nil {
		t.Fatalf("status = %+v", st)
	}
}

// TestGeoDBCompactsRanges: gaps read as no country, adjacent ranges of one
// country merge, and IPv6 ranges in the same /64 keep the first country.
func TestGeoDBCompactsRanges(t *testing.T) {
	csv := "1.0.0.0,1.0.0.255,AU\n1.0.1.0,1.0.1.255,AU\n1.0.4.0,1.0.4.255,CN\n" +
		"2001:db8::,2001:db8::ffff,IT\n2001:db8::1:0,2001:db8::ffff:ffff,FR\n2001:db9::,2001:db9:ffff:ffff:ffff:ffff:ffff:ffff,DE\n"
	db, err := loadGeoDB(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	for addr, want := range map[string]string{"1.0.1.9": "AU", "1.0.2.1": "", "1.0.4.4": "CN", "1.0.5.0": "", "2001:db8::5": "IT", "2001:db9::1": "DE", "2001:dba::1": ""} {
		if got := db.country(net.ParseIP(addr)); got != want {
			t.Errorf("%s = %q, want %q", addr, got, want)
		}
	}
	if len(db.v4Start) != 4 { // AU (merged), gap, CN, gap
		t.Fatalf("IPv4 table = %v %v", db.v4Start, db.v4CC)
	}
}

func parseIP(s string) net.IP { return net.ParseIP(s) }

// TestGeoDBReadsAFileOutOfOrder: a file not in address order (sorted only
// after part of it was compacted) gives the same answers as a sorted one.
func TestGeoDBReadsAFileOutOfOrder(t *testing.T) {
	csv := "1.0.4.0,1.0.4.255,CN\n1.0.0.0,1.0.0.255,AU\n2001:db9::,2001:db9:ffff:ffff:ffff:ffff:ffff:ffff,DE\n2001:db8::,2001:db8::ffff,IT\n5.0.0.0,5.0.0.255,FR\n"
	db, err := loadGeoDB(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	for addr, want := range map[string]string{"1.0.0.9": "AU", "1.0.4.4": "CN", "1.0.2.1": "", "5.0.0.1": "FR", "2001:db8::5": "IT", "2001:db9::1": "DE"} {
		if got := db.country(net.ParseIP(addr)); got != want {
			t.Errorf("%s = %q, want %q", addr, got, want)
		}
	}
}

// TestDaemonExternalMergeCompletesATorrent: a torrent with no peers is
// completed from a copy of its file elsewhere (another program's download),
// every piece checked against its hashes; a wrong-size file is refused.
func TestDaemonExternalMergeCompletesATorrent(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "episode.mkv", 3<<20)
	dest := filepath.Join(t.TempDir(), "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: dest, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "torrent downloading", func() bool {
		info, _ := findInfo(d, hash)
		return info.State == "downloading" || info.State == "stalled"
	})
	external, err := filepath.Glob(filepath.Join(src, "*"))
	if err != nil || len(external) == 0 {
		t.Fatalf("no source file: %v", err)
	}
	short := filepath.Join(t.TempDir(), "short.mkv")
	_ = os.WriteFile(short, []byte("short"), 0o644)
	if err := d.mergeExternal(hash, 0, short); err == nil {
		t.Fatal("a file of another size was accepted")
	}
	if err := d.mergeExternal(hash, 0, "relative.mkv"); err == nil {
		t.Fatal("a relative path was accepted")
	}
	if err := d.mergeExternal(hash, 0, external[0]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "torrent completed from the external copy", func() bool {
		info, _ := findInfo(d, hash)
		return info.Progress >= 100
	})
}
