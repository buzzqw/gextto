package main

// lsd.go: Local Service Discovery (BEP 14). Torrents are announced on the
// LAN multicast group and peers announcing the same torrents are added, as
// libtorrent does. Private torrents are never announced. LSD is off behind
// a proxy or an outgoing (VPN) interface: it would reveal the torrents
// outside the tunnel.
//
// Both families are used: IPv4 on 239.192.152.143:6771 and, best-effort,
// IPv6 on [ff15::efc0:988f]:6771.

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	lsdGroup         = "239.192.152.143:6771"
	lsdGroup6        = "[ff15::efc0:988f]:6771"
	lsdIntervalDef   = 5 * time.Minute
	lsdTickDefault   = 20 * time.Second
	lsdMaxPerMessage = 10
)

// lsdTick is how often due announcements are sent (tests shorten it).
var lsdTick = lsdTickDefault

// lsdInterval is how often the same torrent is re-announced (BEP 14). Tests
// shorten it: a peer that joins just after an announcement would otherwise wait
// the full interval before hearing the seeder again.
var lsdInterval = lsdIntervalDef

// lsdPeerHost maps the sender of an announcement to the address to dial.
// Tests on one machine remap it: the engine ignores the machine's own IPs.
var lsdPeerHost = func(ip net.IP) string { return ip.String() }

type lsdService struct {
	d      *Daemon
	port   int
	cookie string

	group4, group6   *net.UDPAddr
	listen4, listen6 *net.UDPConn
	send4, send6     *net.UDPConn
	ipv6             bool

	stop chan struct{}
	done sync.WaitGroup

	mu        sync.Mutex
	announced map[string]time.Time

	peersFound atomic.Int64
	lastErr    atomic.Value
}

func newLSD(d *Daemon, port int) (*lsdService, error) {
	group4, err := net.ResolveUDPAddr("udp4", lsdGroup)
	if err != nil {
		return nil, err
	}
	listen4, err := net.ListenMulticastUDP("udp4", nil, group4)
	if err != nil {
		return nil, fmt.Errorf("join %s: %w", lsdGroup, err)
	}
	send4, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		listen4.Close()
		return nil, err
	}
	cookie := make([]byte, 8)
	_, _ = rand.Read(cookie)
	s := &lsdService{
		d: d, port: port, cookie: hex.EncodeToString(cookie),
		group4: group4, listen4: listen4, send4: send4,
		stop: make(chan struct{}), announced: map[string]time.Time{},
	}

	// IPv6 LSD (BEP 14, group ff15::efc0:988f) is best-effort: a host without
	// IPv6 multicast keeps working over IPv4 only, so a failure is not fatal.
	if group6, err := net.ResolveUDPAddr("udp6", lsdGroup6); err == nil {
		if listen6, err := net.ListenMulticastUDP("udp6", nil, group6); err == nil {
			if send6, err := net.ListenUDP("udp6", &net.UDPAddr{}); err == nil {
				s.group6, s.listen6, s.send6, s.ipv6 = group6, listen6, send6, true
			} else {
				listen6.Close()
			}
		}
	}

	s.done.Add(2)
	go s.receiveLoop(listen4)
	go s.announceLoop()
	if s.ipv6 {
		s.done.Add(1)
		go s.receiveLoop(s.listen6)
	}
	return s, nil
}

func (s *lsdService) close() {
	if s == nil {
		return
	}
	close(s.stop)
	s.listen4.Close()
	s.send4.Close()
	if s.listen6 != nil {
		s.listen6.Close()
	}
	if s.send6 != nil {
		s.send6.Close()
	}
	s.done.Wait()
}

func (s *lsdService) message(host string, hashes []string) []byte {
	var b bytes.Buffer
	b.WriteString("BT-SEARCH * HTTP/1.1\r\n")
	b.WriteString("Host: " + host + "\r\n")
	b.WriteString("Port: " + strconv.Itoa(s.port) + "\r\n")
	for _, hash := range hashes {
		b.WriteString("Infohash: " + hash + "\r\n")
	}
	b.WriteString("cookie: " + s.cookie + "\r\n\r\n\r\n")
	return b.Bytes()
}

// dueHashes lists the running public torrents to announce now. It reads the
// lock-free snapshot: taking d.mu and calling t.Stats() here
// would block the whole daemon on a torrent run loop stuck on storage I/O.
func (s *lsdService) dueHashes(now time.Time) []string {
	var running []string
	for _, v := range s.d.snapshotViews() {
		if v.Private {
			continue
		}
		switch v.State {
		case "downloading", "downloading_metadata", "seeding", "checking_files", "moving":
			running = append(running, v.Hash)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	live := map[string]bool{}
	var due []string
	for _, hash := range running {
		live[hash] = true
		if now.Sub(s.announced[hash]) >= lsdInterval {
			due = append(due, hash)
			s.announced[hash] = now
		}
	}
	for hash := range s.announced {
		if !live[hash] {
			delete(s.announced, hash)
		}
	}
	return due
}

func (s *lsdService) announceLoop() {
	defer s.done.Done()
	ticker := time.NewTicker(lsdTick)
	defer ticker.Stop()
	for {
		due := s.dueHashes(time.Now())
		for start := 0; start < len(due); start += lsdMaxPerMessage {
			batch := due[start:min(start+lsdMaxPerMessage, len(due))]
			if _, err := s.send4.WriteToUDP(s.message(lsdGroup, batch), s.group4); err != nil {
				s.lastErr.Store(err.Error())
			}
			if s.send6 != nil {
				if _, err := s.send6.WriteToUDP(s.message(lsdGroup6, batch), s.group6); err != nil {
					s.lastErr.Store(err.Error())
				}
			}
		}
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}
	}
}

// parseLSD reads a BT-SEARCH message: port, info hashes and cookie.
func parseLSD(data []byte) (port int, hashes []string, cookie string, ok bool) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(data)))
	if err != nil || req.Method != "BT-SEARCH" {
		return 0, nil, "", false
	}
	port, err = strconv.Atoi(strings.TrimSpace(req.Header.Get("Port")))
	if err != nil || port <= 0 || port > 65535 {
		return 0, nil, "", false
	}
	for _, value := range req.Header.Values("Infohash") {
		hash := strings.ToLower(strings.TrimSpace(value))
		if len(hash) == 40 {
			if _, err := hex.DecodeString(hash); err == nil {
				hashes = append(hashes, hash)
			}
		}
	}
	return port, hashes, strings.TrimSpace(req.Header.Get("Cookie")), len(hashes) > 0
}

func (s *lsdService) receiveLoop(conn *net.UDPConn) {
	defer s.done.Done()
	// A larger buffer holds an IPv6 address comfortably.
	buf := make([]byte, 1500)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
			}
			time.Sleep(time.Second)
			continue
		}
		port, hashes, cookie, ok := parseLSD(buf[:n])
		if !ok || cookie == s.cookie {
			continue
		}
		peer := net.JoinHostPort(lsdPeerHost(from.IP), strconv.Itoa(port))
		for _, hash := range hashes {
			s.d.mu.Lock()
			t, _ := s.d.findLocked(hash)
			s.d.mu.Unlock()
			if t == nil || t.Stats().Private {
				continue
			}
			if err := t.AddPeer(peer); err == nil {
				s.peersFound.Add(1)
			}
		}
	}
}

type lsdStatus struct {
	Enabled    bool   `json:"enabled"`
	IPv6       bool   `json:"ipv6"`
	PeersFound int64  `json:"peers_found"`
	Error      string `json:"error,omitempty"`
}

func (s *lsdService) status() lsdStatus {
	if s == nil {
		return lsdStatus{}
	}
	out := lsdStatus{Enabled: true, IPv6: s.ipv6, PeersFound: s.peersFound.Load()}
	if err, ok := s.lastErr.Load().(string); ok {
		out.Error = err
	}
	return out
}
