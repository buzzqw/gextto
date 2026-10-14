package main

// dlna_ssdp.go is the SSDP part of the DLNA server: it joins the multicast
// group on the LAN interface, answers M-SEARCH probes (which is how a TV finds
// the media server) and announces/withdraws itself with ssdp:alive/byebye.

import (
	"net"
	"strings"
	"time"
)

// ssdpSTs are the search targets a media server answers to, in the order a
// ssdp:all probe gets its replies.
func (s *dlnaServer) ssdpSTs() []string {
	return []string{
		"upnp:rootdevice",
		s.uuid,
		dlnaDeviceType,
		dlnaContentDir,
		dlnaConnMgr,
	}
}

// ssdpMatches reports whether the server answers a search target. An empty ST
// (a malformed probe) matches nothing.
func (s *dlnaServer) ssdpMatches(st string) bool {
	st = strings.TrimSpace(st)
	if st == "" {
		return false
	}
	if st == "ssdp:all" {
		return true
	}
	for _, t := range s.ssdpSTs() {
		if strings.EqualFold(st, t) {
			return true
		}
	}
	return false
}

// ssdpResponses builds the unicast 200 OK replies to a probe with the given ST.
// A ssdp:all probe gets one reply per target; a specific probe one reply.
func (s *dlnaServer) ssdpResponses(st string) []string {
	targets := []string{strings.TrimSpace(st)}
	if strings.EqualFold(strings.TrimSpace(st), "ssdp:all") {
		targets = s.ssdpSTs()
	}
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		usn := s.uuid + "::" + t
		if t == s.uuid {
			usn = s.uuid
		}
		out = append(out, "HTTP/1.1 200 OK\r\n"+
			"CACHE-CONTROL: max-age=1800\r\n"+
			"DATE: "+time.Now().UTC().Format(http1Date)+"\r\n"+
			"EXT:\r\n"+
			"LOCATION: "+s.base+"/rootDesc.xml\r\n"+
			"SERVER: Linux/1.0 UPnP/1.0 gx-torrent/1.0\r\n"+
			"ST: "+t+"\r\n"+
			"USN: "+usn+"\r\n\r\n")
	}
	return out
}

// parseMSearch reads an M-SEARCH datagram's headers (case-insensitively) into a
// map. It returns false when the datagram is not an M-SEARCH.
func parseMSearch(data []byte) (map[string]string, bool) {
	text := string(data)
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(text)), "M-SEARCH") {
		return nil, false
	}
	headers := map[string]string{}
	lines := strings.Split(text, "\r\n")
	if len(lines) < 2 {
		lines = strings.Split(text, "\n")
	}
	for _, line := range lines[1:] {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		headers[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return headers, true
}

// startSSDP joins the multicast group on the interface that owns ip and serves
// probes until the server closes. It also sends the initial ssdp:alive.
func (s *dlnaServer) startSSDP(ip net.IP) error {
	iface := interfaceForIP(ip)
	group := &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 1900}
	conn, err := net.ListenMulticastUDP("udp4", iface, group)
	if err != nil {
		return err
	}
	_ = conn.SetReadBuffer(1 << 16)
	s.ssdp = conn
	if dst, err := net.ResolveUDPAddr("udp4", dlnaSSDPGroup); err == nil {
		s.ssdpDst = dst
	}
	go s.ssdpLoop(conn)
	go s.aliveLoop()
	return nil
}

// aliveLoop announces the server at start (twice: UDP multicast is lossy) and
// again every dlnaAliveEvery, before the advertised max-age runs out, until the
// server closes.
func (s *dlnaServer) aliveLoop() {
	s.sendAlive()
	select {
	case <-s.closeC:
		return
	case <-time.After(time.Second):
	}
	s.sendAlive()
	ticker := time.NewTicker(dlnaAliveEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.closeC:
			return
		case <-ticker.C:
			s.sendAlive()
		}
	}
}

// ssdpLoop reads probes and answers them from a fresh unicast socket, so the
// reply goes back to the sender rather than to the group.
func (s *dlnaServer) ssdpLoop(conn net.PacketConn) {
	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return // closed with the server, or a fatal socket error
		}
		headers, ok := parseMSearch(buf[:n])
		if !ok {
			continue
		}
		st := headers["st"]
		if !s.ssdpMatches(st) {
			continue
		}
		fromAddr, ok := from.(*net.UDPAddr)
		if !ok || !dlnaLocalAddr(fromAddr.IP) {
			continue
		}
		s.logSearch(fromAddr.IP.String(), st)
		out, err := net.DialUDP("udp4", nil, fromAddr)
		if err != nil {
			continue
		}
		for _, resp := range s.ssdpResponses(st) {
			_, _ = out.Write([]byte(resp))
		}
		_ = out.Close()
	}
}

// sendAlive announces the server (three NOTIFYs, the usual practice).
func (s *dlnaServer) sendAlive() {
	s.notify("ssdp:alive")
}

// sendByebye withdraws the server when it stops.
func (s *dlnaServer) sendByebye() {
	s.notify("ssdp:byebye")
}

// notify sends one NOTIFY per search target to the multicast group.
func (s *dlnaServer) notify(nt string) {
	if s.ssdpDst == nil {
		return
	}
	out, err := net.DialUDP("udp4", nil, s.ssdpDst)
	if err != nil {
		return
	}
	defer out.Close()
	for _, t := range s.ssdpSTs() {
		usn := s.uuid + "::" + t
		if t == s.uuid {
			usn = s.uuid
		}
		msg := "NOTIFY * HTTP/1.1\r\n" +
			"HOST: 239.255.255.250:1900\r\n" +
			"CACHE-CONTROL: max-age=1800\r\n" +
			"NT: " + t + "\r\n" +
			"NTS: " + nt + "\r\n" +
			"LOCATION: " + s.base + "/rootDesc.xml\r\n" +
			"SERVER: Linux/1.0 UPnP/1.0 gx-torrent/1.0\r\n" +
			"USN: " + usn + "\r\n\r\n"
		if nt == "ssdp:byebye" {
			msg = "NOTIFY * HTTP/1.1\r\n" +
				"HOST: 239.255.255.250:1900\r\n" +
				"NT: " + t + "\r\n" +
				"NTS: ssdp:byebye\r\n" +
				"USN: " + usn + "\r\n\r\n"
		}
		_, _ = out.Write([]byte(msg))
	}
}

// http1Date is the RFC 1123 date format of HTTP headers.
const http1Date = "Mon, 02 Jan 2006 15:04:05 GMT"

// interfaceForIP returns the network interface that owns ip, or nil (the
// default) when it is not found or ip is loopback.
func interfaceForIP(ip net.IP) *net.Interface {
	if ip == nil || ip.IsLoopback() {
		return nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for i := range ifaces {
		addrs, err := ifaces[i].Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(ip) {
				return &ifaces[i]
			}
		}
	}
	return nil
}
