package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// The country of a peer, from an offline database: by default the free
// DB-IP "IP to Country Lite" file, downloaded once a month into the data
// folder (CC BY 4.0, credited on the pages that show it); or a CSV file the
// operator names (DB-IP lite form, first,last,CC with addresses, or the
// IP2Location LITE form with integers). Lookups never leave the machine.
//
// The database is loaded in the background the first time a peer list asks
// for a country and dropped again after geoIdleUnload without lookups, so a
// daemon nobody looks at keeps none of it in memory.

const (
	// peerCountryKey turns the country column on (default on).
	peerCountryKey = "peer-country"
	// geoIPFileKey names an operator file; empty = the automatic DB-IP one.
	geoIPFileKey = "geoip-file"
	// geoAutoFile is the automatic database in the data folder.
	geoAutoFile = "dbip-country-lite.csv.gz"
	// geoRefreshAge is how old the automatic file may get before a new
	// month's edition is fetched.
	geoRefreshAge = 32 * 24 * time.Hour
	// geoRetry is the wait after a failed download.
	geoRetry = 6 * time.Hour
	// geoIdleUnload drops the loaded database after this long unused.
	geoIdleUnload = 10 * time.Minute
)

// geoAutoURL is the DB-IP free country file of a month (a variable for tests).
var geoAutoURL = func(month time.Time) string {
	return "https://download.db-ip.com/free/dbip-country-lite-" + month.Format("2006-01") + ".csv.gz"
}

// geoDB is a loaded database: for each family the sorted start addresses of
// contiguous ranges and their country (0 = none). Every range is closed by the
// next start, so a gap is a start with country 0. IPv6 keeps the upper 64
// bits, the granularity country allocations use. About 6 bytes per IPv4 range
// and 10 per IPv6 range: the DB-IP file (~730,000 ranges) takes ~6 MiB.
type geoDB struct {
	v4Start []uint32
	v4CC    []uint16
	v6Start []uint64
	v6CC    []uint16
}

// geoState holds the database of the configured source.
type geoState struct {
	mu       sync.Mutex
	source   string // path of the file the database was read from
	db       *geoDB
	loading  bool
	err      string
	lastUsed time.Time
	ranges   int
	// fetchedAt/fetchErr describe the automatic download.
	fetchedAt time.Time
	fetchErr  string
}

func ccCode(cc string) uint16 {
	return uint16(cc[0])<<8 | uint16(cc[1])
}

func ccString(code uint16) string {
	if code == 0 {
		return ""
	}
	return string([]byte{byte(code >> 8), byte(code)})
}

// parseGeoAddr reads an address, or a decimal integer (IP2Location), as
// its upper and lower 64 bits; v4 reports an IPv4 one. wide marks an
// integer of an IPv6 file, whose small values are still IPv6.
func parseGeoAddr(field string, wide bool) (hi, lo uint64, v4, ok bool) {
	field = strings.Trim(strings.TrimSpace(field), `"`)
	if addr, err := netip.ParseAddr(field); err == nil {
		if addr.Is4() || addr.Is4In6() {
			b := addr.Unmap().As4()
			return 0, uint64(binary.BigEndian.Uint32(b[:])), true, true
		}
		b := addr.As16()
		return binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:]), false, true
	}
	if field == "" || len(field) > 39 {
		return 0, 0, false, false
	}
	if !wide {
		n, err := strconv.ParseUint(field, 10, 32)
		if err != nil {
			return 0, 0, false, false
		}
		return 0, n, true, true
	}
	n, okBig := new(big.Int).SetString(field, 10)
	if !okBig || n.Sign() < 0 || n.BitLen() > 128 {
		return 0, 0, false, false
	}
	var b [16]byte
	n.FillBytes(b[:])
	return binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:]), false, true
}

// geoRow4 and geoRow6 hold a parsed range when the file is not in address
// order and has to be sorted before it is compacted.
type geoRow4 struct {
	first, last uint32
	cc          uint16
}

type geoRow6 struct {
	first, last uint64
	cc          uint16
}

// loadGeoDB reads a CSV database (plain or gzip); malformed lines are
// skipped. A file in address order (DB-IP's is) is compacted while it is
// read; otherwise its ranges are sorted first.
func loadGeoDB(r io.Reader) (*geoDB, error) {
	buffered := bufio.NewReaderSize(r, 1<<16)
	if magic, _ := buffered.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(buffered)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		buffered = bufio.NewReaderSize(gz, 1<<16)
	}
	db := &geoDB{}
	var rows4 []geoRow4
	var rows6 []geoRow6
	sorted4, sorted6 := true, true
	var last4 uint32
	var last6 uint64
	seen4, seen6 := false, false
	scanner := bufio.NewScanner(buffered)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f0, rest, ok := strings.Cut(line, ",")
		if !ok {
			continue
		}
		f1, rest, ok := strings.Cut(rest, ",")
		if !ok {
			continue
		}
		f2, _, _ := strings.Cut(rest, ",")
		cc := strings.ToUpper(strings.Trim(strings.TrimSpace(f2), `"`))
		if len(cc) != 2 || cc == "ZZ" || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' {
			continue
		}
		// IP2Location ships separate IPv4 and IPv6 files: a v6 file's integers
		// are 128-bit even for small values, recognised by their width.
		wide := len(strings.Trim(f1, `" `)) > 10
		firstHi, firstLo, v4a, ok1 := parseGeoAddr(f0, wide)
		lastHi, lastLo, v4b, ok2 := parseGeoAddr(f1, wide)
		if !ok1 || !ok2 || v4a != v4b || firstHi > lastHi || (firstHi == lastHi && firstLo > lastLo) {
			continue
		}
		code := ccCode(cc)
		if v4a {
			row := geoRow4{uint32(firstLo), uint32(lastLo), code}
			if sorted4 && seen4 && row.first < last4 {
				sorted4 = false
				firsts, lasts, codes := expandRanges(db.v4Start, db.v4CC, ^uint32(0))
				for k := range firsts {
					rows4 = append(rows4, geoRow4{firsts[k], lasts[k], codes[k]})
				}
			}
			seen4, last4 = true, row.first
			if sorted4 {
				db.v4Start, db.v4CC = appendRange(db.v4Start, db.v4CC, row.first, row.last, row.cc, ^uint32(0))
			} else {
				rows4 = append(rows4, row)
			}
			continue
		}
		row := geoRow6{firstHi, lastHi, code}
		if sorted6 && seen6 && row.first < last6 {
			sorted6 = false
			firsts, lasts, codes := expandRanges(db.v6Start, db.v6CC, ^uint64(0))
			for k := range firsts {
				rows6 = append(rows6, geoRow6{firsts[k], lasts[k], codes[k]})
			}
		}
		seen6, last6 = true, row.first
		if sorted6 {
			db.v6Start, db.v6CC = appendRange(db.v6Start, db.v6CC, row.first, row.last, row.cc, ^uint64(0))
		} else {
			rows6 = append(rows6, row)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !seen4 && !seen6 {
		return nil, fmt.Errorf("no address range with a country code was found")
	}
	if !sorted4 {
		sort.Slice(rows4, func(i, j int) bool { return rows4[i].first < rows4[j].first })
		db.v4Start, db.v4CC = nil, nil
		for _, row := range rows4 {
			db.v4Start, db.v4CC = appendRange(db.v4Start, db.v4CC, row.first, row.last, row.cc, ^uint32(0))
		}
	}
	if !sorted6 {
		sort.Slice(rows6, func(i, j int) bool { return rows6[i].first < rows6[j].first })
		db.v6Start, db.v6CC = nil, nil
		for _, row := range rows6 {
			db.v6Start, db.v6CC = appendRange(db.v6Start, db.v6CC, row.first, row.last, row.cc, ^uint64(0))
		}
	}
	// Trim the slack of append so the table holds only what it uses.
	db.v4Start, db.v4CC = slicesClip(db.v4Start), slicesClip(db.v4CC)
	db.v6Start, db.v6CC = slicesClip(db.v6Start), slicesClip(db.v6CC)
	return db, nil
}

// expandRanges turns a compacted table back into ranges, for a file found
// out of order after part of it was compacted: the ranges are the same.
func expandRanges[T uint32 | uint64](starts []T, ccs []uint16, maxAddr T) (firsts, lasts []T, codes []uint16) {
	for i, start := range starts {
		if ccs[i] == 0 {
			continue
		}
		last := maxAddr
		if i+1 < len(starts) {
			last = starts[i+1] - 1
		}
		firsts, lasts, codes = append(firsts, start), append(lasts, last), append(codes, ccs[i])
	}
	return firsts, lasts, codes
}

func slicesClip[T any](s []T) []T {
	if cap(s) == len(s) {
		return s
	}
	out := make([]T, len(s))
	copy(out, s)
	return out
}

// appendRange adds [first,last] with country cc to a start-only table,
// closing it with a "no country" start when the next address is not the
// start of another range; adjacent ranges of one country are merged.
func appendRange[T uint32 | uint64](starts []T, ccs []uint16, first, last T, cc uint16, maxAddr T) ([]T, []uint16) {
	n := len(starts)
	if n > 0 && starts[n-1] > first {
		// Overlaps the previous range (or, for IPv6, shares its /64): the
		// first country seen keeps the overlap, this one takes the rest.
		if last < starts[n-1] {
			return starts, ccs
		}
		first = starts[n-1]
	}
	if n > 0 && starts[n-1] == first {
		// The terminator of the previous range is where this one starts.
		ccs[n-1] = cc
	} else {
		starts, ccs = append(starts, first), append(ccs, cc)
	}
	n = len(starts)
	if n >= 2 && ccs[n-1] == ccs[n-2] {
		starts, ccs = starts[:n-1], ccs[:n-1]
	}
	if last != maxAddr {
		starts, ccs = append(starts, last+1), append(ccs, 0)
	}
	return starts, ccs
}

// country returns the two-letter code of an address, or "".
func (db *geoDB) country(ip net.IP) string {
	if db == nil || ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		n := binary.BigEndian.Uint32(v4)
		i := sort.Search(len(db.v4Start), func(i int) bool { return db.v4Start[i] > n }) - 1
		if i < 0 {
			return ""
		}
		return ccString(db.v4CC[i])
	}
	ip = ip.To16()
	if ip == nil {
		return ""
	}
	hi := binary.BigEndian.Uint64(ip[:8])
	i := sort.Search(len(db.v6Start), func(i int) bool { return db.v6Start[i] > hi }) - 1
	if i < 0 {
		return ""
	}
	return ccString(db.v6CC[i])
}

// size counts the ranges that carry a country.
func (db *geoDB) size() int {
	n := 0
	for _, cc := range db.v4CC {
		if cc != 0 {
			n++
		}
	}
	for _, cc := range db.v6CC {
		if cc != 0 {
			n++
		}
	}
	return n
}

// peerCountryEnabled reports whether the country column is on.
func (d *Daemon) peerCountryEnabled() bool {
	return d.settingBool(peerCountryKey, d.opts.Network.PeerCountry)
}

// geoIPFile returns the operator's database path (empty = automatic).
func (d *Daemon) geoIPFile() string {
	if d.opts.Settings != nil {
		if v := strings.TrimSpace(d.opts.Settings.Get(geoIPFileKey, "")); v != "" {
			return v
		}
	}
	return strings.TrimSpace(d.opts.Network.GeoIPFile)
}

// geoSource is the file the countries are read from, or "" when off.
func (d *Daemon) geoSource() string {
	if !d.peerCountryEnabled() {
		return ""
	}
	if path := d.geoIPFile(); path != "" {
		return path
	}
	return filepath.Join(d.opts.DataDir, geoAutoFile)
}

// geoAutomatic reports whether the countries come from the DB-IP download.
func (d *Daemon) geoAutomatic() bool {
	return d.peerCountryEnabled() && d.geoIPFile() == ""
}

// peerCountry returns the country code of a peer address ("host:port" or a
// bare address). The first call starts loading the database in the
// background and answers "" until it is ready.
func (d *Daemon) peerCountry(addr string) string {
	source := d.geoSource()
	if source == "" {
		return ""
	}
	d.geo.mu.Lock()
	if d.geo.source != source {
		d.geo.source, d.geo.db, d.geo.err, d.geo.ranges = source, nil, "", 0
	}
	d.geo.lastUsed = time.Now()
	db := d.geo.db
	if db == nil && !d.geo.loading && d.geo.err == "" {
		d.geo.loading = true
		go d.loadGeo(source)
	}
	d.geo.mu.Unlock()
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	return db.country(net.ParseIP(host))
}

// loadGeo reads the database of source into memory.
func (d *Daemon) loadGeo(source string) {
	var db *geoDB
	f, err := os.Open(source)
	if err == nil {
		db, err = loadGeoDB(f)
		f.Close()
	}
	d.geo.mu.Lock()
	defer d.geo.mu.Unlock()
	d.geo.loading = false
	if d.geo.source != source {
		return
	}
	if err != nil {
		// A missing automatic file is not an error yet: it is being fetched.
		if !(os.IsNotExist(err) && source == filepath.Join(d.opts.DataDir, geoAutoFile)) {
			d.geo.err = err.Error()
			logf("The country database %s could not be read: %v", source, err)
		}
		return
	}
	d.geo.db, d.geo.err, d.geo.ranges = db, "", db.size()
}

// runGeoIP keeps the automatic database current and drops the loaded one
// when nobody has looked at a peer list for a while.
func (d *Daemon) runGeoIP(stop <-chan struct{}) {
	var lastTry time.Time
	for {
		now := time.Now()
		if d.geoAutomatic() {
			path := filepath.Join(d.opts.DataDir, geoAutoFile)
			info, err := os.Stat(path)
			due := err != nil || now.Sub(info.ModTime()) >= geoRefreshAge
			if due && (lastTry.IsZero() || now.Sub(lastTry) >= geoRetry) {
				lastTry = now
				d.fetchGeo(path, now)
			}
		}
		d.geo.mu.Lock()
		if d.geo.db != nil && now.Sub(d.geo.lastUsed) >= geoIdleUnload {
			d.geo.db = nil
		}
		d.geo.mu.Unlock()
		select {
		case <-stop:
			return
		case <-time.After(time.Minute):
		}
	}
}

// fetchGeo downloads this month's DB-IP file (the previous month's while
// the new one is not out yet) through the engine transport.
func (d *Daemon) fetchGeo(path string, now time.Time) {
	var lastErr error
	for _, month := range []time.Time{now, now.AddDate(0, -1, 0)} {
		data, err := fetchGeoFile(geoAutoURL(month))
		if err != nil {
			lastErr = err
			continue
		}
		if _, err := loadGeoDB(bytes.NewReader(data)); err != nil {
			lastErr = err
			continue
		}
		tmp := path + ".tmp"
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = os.WriteFile(tmp, data, 0o644)
			if err == nil {
				err = os.Rename(tmp, path)
			}
			if err != nil {
				_ = os.Remove(tmp)
				lastErr = err
				break
			}
		}
		d.geo.mu.Lock()
		d.geo.fetchedAt, d.geo.fetchErr = now, ""
		// The next lookup reads the new edition.
		if d.geo.source == path {
			d.geo.db, d.geo.err = nil, ""
		}
		d.geo.mu.Unlock()
		logf("Country database downloaded: DB-IP edition of %s", month.Format("January 2006"))
		return
	}
	d.geo.mu.Lock()
	d.geo.fetchErr = lastErr.Error()
	d.geo.mu.Unlock()
	logf("The country database could not be downloaded: %v", lastErr)
}

func fetchGeoFile(rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gx-torrent")
	resp, err := (&http.Client{Transport: torrent.HTTPTransport()}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

// geoStatus describes the database for the settings page and the API.
type geoStatus struct {
	Enabled   bool       `json:"enabled"`
	Automatic bool       `json:"automatic"`
	Loaded    bool       `json:"loaded"`
	Ranges    int        `json:"ranges,omitempty"`
	FetchedAt *time.Time `json:"fetched_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

func (d *Daemon) geoStatusSnapshot() geoStatus {
	out := geoStatus{Enabled: d.peerCountryEnabled(), Automatic: d.geoAutomatic()}
	d.geo.mu.Lock()
	defer d.geo.mu.Unlock()
	out.Loaded = d.geo.db != nil
	out.Ranges = d.geo.ranges
	out.Error = d.geo.err
	if out.Automatic {
		if d.geo.fetchErr != "" && out.Error == "" {
			out.Error = d.geo.fetchErr
		}
		if info, err := os.Stat(filepath.Join(d.opts.DataDir, geoAutoFile)); err == nil {
			at := info.ModTime()
			out.FetchedAt = &at
		}
	}
	return out
}
