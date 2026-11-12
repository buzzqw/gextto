package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The country of a peer, from an offline database the operator downloads:
// a CSV of address ranges with a two-letter country code, in the DB-IP
// "lite" form (first,last,CC with addresses) or the IP2Location LITE form
// (from,to,CC,name with integers). No network call is made: without the
// file the country column stays empty.

// geoRange4 is one IPv4 range; geoRange6 one IPv6 range as two halves.
type geoRange4 struct {
	first, last uint32
	cc          [2]byte
}

type geoRange6 struct {
	firstHi, firstLo, lastHi, lastLo uint64
	cc                               [2]byte
}

// geoDB is a loaded database, read-only after load.
type geoDB struct {
	v4 []geoRange4
	v6 []geoRange6
}

// geoState holds the database of the configured file, loaded once per path.
type geoState struct {
	mu   sync.Mutex
	path string
	db   *geoDB
	err  string
}

// parseGeoAddr reads an address or a decimal integer (IP2Location) as a
// 16-byte value; v4 reports an IPv4 (or IPv4-mapped) one.
func parseGeoAddr(field string, wide bool) (net.IP, bool) {
	field = strings.Trim(strings.TrimSpace(field), `"`)
	if ip := net.ParseIP(field); ip != nil {
		return ip.To16(), ip.To4() != nil
	}
	n, ok := new(big.Int).SetString(field, 10)
	if !ok || n.Sign() < 0 {
		return nil, false
	}
	raw := n.Bytes()
	if len(raw) > 16 {
		return nil, false
	}
	if !wide && len(raw) <= 4 {
		var b [4]byte
		copy(b[4-len(raw):], raw)
		return net.IPv4(b[0], b[1], b[2], b[3]).To16(), true
	}
	ip := make(net.IP, 16)
	copy(ip[16-len(raw):], raw)
	return ip, ip.To4() != nil
}

// loadGeoDB reads a CSV database; malformed lines are skipped.
func loadGeoDB(r io.Reader) (*geoDB, error) {
	db := &geoDB{}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 3 {
			continue
		}
		cc := strings.ToUpper(strings.Trim(strings.TrimSpace(fields[2]), `"`))
		if len(cc) != 2 || cc == "-" || cc == "ZZ" {
			continue
		}
		// IP2Location ships separate v4 and v6 files: a v6 file's integers
		// are 128-bit even for small values, recognised by their width.
		wide := len(strings.Trim(fields[1], `" `)) > 10
		first, ok1 := parseGeoAddr(fields[0], wide)
		last, ok2 := parseGeoAddr(fields[1], wide)
		if first == nil || last == nil {
			continue
		}
		code := [2]byte{cc[0], cc[1]}
		if ok1 && ok2 {
			db.v4 = append(db.v4, geoRange4{binary.BigEndian.Uint32(first.To4()), binary.BigEndian.Uint32(last.To4()), code})
			continue
		}
		db.v6 = append(db.v6, geoRange6{
			binary.BigEndian.Uint64(first[:8]), binary.BigEndian.Uint64(first[8:]),
			binary.BigEndian.Uint64(last[:8]), binary.BigEndian.Uint64(last[8:]), code,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(db.v4) == 0 && len(db.v6) == 0 {
		return nil, fmt.Errorf("no address range with a country code was found")
	}
	sort.Slice(db.v4, func(i, j int) bool { return db.v4[i].first < db.v4[j].first })
	sort.Slice(db.v6, func(i, j int) bool {
		a, b := db.v6[i], db.v6[j]
		return a.firstHi < b.firstHi || (a.firstHi == b.firstHi && a.firstLo < b.firstLo)
	})
	return db, nil
}

// country returns the two-letter code of an address, or "".
func (db *geoDB) country(ip net.IP) string {
	if db == nil || ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		n := binary.BigEndian.Uint32(v4)
		i := sort.Search(len(db.v4), func(i int) bool { return db.v4[i].first > n }) - 1
		if i >= 0 && n <= db.v4[i].last {
			return string(db.v4[i].cc[:])
		}
		return ""
	}
	ip = ip.To16()
	if ip == nil {
		return ""
	}
	hi, lo := binary.BigEndian.Uint64(ip[:8]), binary.BigEndian.Uint64(ip[8:])
	i := sort.Search(len(db.v6), func(i int) bool {
		r := db.v6[i]
		return r.firstHi > hi || (r.firstHi == hi && r.firstLo > lo)
	}) - 1
	if i < 0 {
		return ""
	}
	r := db.v6[i]
	if hi < r.lastHi || (hi == r.lastHi && lo <= r.lastLo) {
		return string(r.cc[:])
	}
	return ""
}

// geoIPFileKey is the settings.json key of the database path.
const geoIPFileKey = "geoip-file"

// geoIPFile returns the configured database path (empty = off).
func (d *Daemon) geoIPFile() string {
	if d.opts.Settings != nil {
		if v := strings.TrimSpace(d.opts.Settings.Get(geoIPFileKey, "")); v != "" {
			return v
		}
	}
	return strings.TrimSpace(d.opts.Network.GeoIPFile)
}

// peerCountry returns the country code of a peer address ("host:port" or a
// bare address), loading the database the first time a path is seen.
func (d *Daemon) peerCountry(addr string) string {
	path := d.geoIPFile()
	if path == "" {
		return ""
	}
	d.geo.mu.Lock()
	if d.geo.path != path {
		d.geo.path, d.geo.db, d.geo.err = path, nil, ""
		if f, err := os.Open(path); err != nil {
			d.geo.err = err.Error()
		} else {
			db, loadErr := loadGeoDB(f)
			f.Close()
			if loadErr != nil {
				d.geo.err = loadErr.Error()
				logf("The country database %s could not be read: %v", path, loadErr)
			} else {
				d.geo.db = db
				logf("Country database loaded: %s address ranges from %s", strconv.Itoa(len(db.v4)+len(db.v6)), path)
			}
		}
	}
	db := d.geo.db
	d.geo.mu.Unlock()
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	return db.country(net.ParseIP(host))
}
