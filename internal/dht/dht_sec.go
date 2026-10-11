package dht

import (
	"crypto/rand"
	"hash/crc32"
	"net"
	"sync"
	"sync/atomic"
)

// BEP 42 (DHT security extension): the first 21 bits of a node ID are derived
// from the node's external IP, so an attacker cannot freely choose node IDs
// close to a target info-hash to hijack its storage. CRC32C (Castagnoli) is
// used for speed and uniformity.
//
// https://www.bittorrent.org/beps/bep_0042.html

func bep42Mask(ip net.IP) []byte {
	if ip.To4() != nil {
		return []byte{0x03, 0x0f, 0x3f, 0xff}
	}
	return []byte{0x01, 0x03, 0x07, 0x0f, 0x1f, 0x3f, 0x7f, 0xff}
}

// castagnoliTable is built once: crc32.MakeTable allocates a 1 KiB lookup
// array, and bep42CRC runs for every secure node ID check.
var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

// bep42CRC returns the CRC32C of the masked IP with the random bits r mixed in.
func bep42CRC(ip net.IP, r byte) uint32 {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	buf := append(net.IP(nil), ip...)
	mask := bep42Mask(buf)
	for i := range mask {
		buf[i] &= mask[i]
	}
	buf[0] |= (r & 7) << 5
	return crc32.Checksum(buf[:len(mask)], castagnoliTable)
}

// SecureNodeID makes a 20-byte node ID secure for ip, in place (BEP 42). It sets
// the first 21 bits of the ID from the CRC and keeps the low 3 bits of id[2].
func SecureNodeID(id *[20]byte, ip net.IP) {
	crc := bep42CRC(ip, id[19])
	id[0] = byte(crc >> 24)
	id[1] = byte(crc >> 16)
	id[2] = byte(crc>>8&0xf8) | id[2]&0x07
}

// NodeIDSecure reports whether a 20-byte node ID is secure for ip (BEP 42).
// Nodes on a local network address are always accepted.
func NodeIDSecure(id [20]byte, ip net.IP) bool {
	if isLocalNetworkIP(ip) {
		return true
	}
	crc := bep42CRC(ip, id[19])
	return id[0] == byte(crc>>24) &&
		id[1] == byte(crc>>16) &&
		id[2]&0xf8 == byte(crc>>8&0xf8)
}

func isLocalNetworkIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate()
}

// generateSecureNodeID returns a random node ID made secure for ip.
func generateSecureNodeID(ip net.IP) []byte {
	var id [20]byte
	_, _ = rand.Read(id[:])
	SecureNodeID(&id, ip)
	return id[:]
}

// parseReflected decodes the BEP 42 "ip" field: an IPv4 or IPv6 address
// followed by a 2-byte big-endian port. Both are the sender's view of our
// external address.
func parseReflected(raw string) (net.IP, int, bool) {
	switch len(raw) {
	case 6: // IPv4 + port
		return net.IPv4(raw[0], raw[1], raw[2], raw[3]), int(raw[4])<<8 | int(raw[5]), true
	case 18: // IPv6 + port
		return net.IP(append([]byte(nil), raw[:16]...)), int(raw[16])<<8 | int(raw[17]), true
	default:
		return nil, 0, false
	}
}

// Reflected external address, learned from the BEP 42 "ip" field of responses
// to our own queries. A single node can lie, so the port is only published once
// at least two observations agree on the same ip:port; the consensus is the
// most-observed pair. The sample set is bounded so a flood of bogus values
// cannot grow it. Exposed by Stats() as "reflected_port" (0 while unknown) and
// used by the daemon's port check to tell a port-preserving NAT from one that
// rewrites the port.
const reflectedMaxSamples = 32

var (
	reflectedMu    sync.Mutex
	reflectedSeen  = make(map[string]*reflectedSample)
	reflectedOrder []string
	reflectedPort  atomic.Int64
)

type reflectedSample struct {
	port int
	n    int
}

// recordReflected notes the external address a remote node reported for us.
func recordReflected(raw string) {
	_, port, ok := parseReflected(raw)
	if !ok || port == 0 {
		return
	}
	reflectedMu.Lock()
	s := reflectedSeen[raw]
	if s == nil {
		if len(reflectedOrder) >= reflectedMaxSamples {
			delete(reflectedSeen, reflectedOrder[0])
			reflectedOrder = reflectedOrder[1:]
		}
		s = &reflectedSample{port: port}
		reflectedSeen[raw] = s
		reflectedOrder = append(reflectedOrder, raw)
	}
	s.n++
	// Deterministic consensus: the most-observed confirmed pair, ties broken by
	// the smaller port (map iteration order is random).
	bestN, bestPort := 0, 0
	for _, smp := range reflectedSeen {
		if smp.n < 2 {
			continue
		}
		if smp.n > bestN || (smp.n == bestN && smp.port < bestPort) {
			bestN, bestPort = smp.n, smp.port
		}
	}
	reflectedMu.Unlock()
	if bestPort > 0 {
		reflectedPort.Store(int64(bestPort))
	}
}

// noteExternalIP adopts the external IP a remote node reports for us (BEP 42
// "ip" field, 4/16 bytes plus a 2-byte port) and, once, regenerates a node ID
// secure for it. Only used when the external IP was not configured at startup.
// It runs in the DHT run loop.
func (d *DHT) noteExternalIP(raw string) {
	if d.config.PublicIP != nil {
		return
	}
	ip, _, ok := parseReflected(raw)
	if !ok || isLocalNetworkIP(ip) {
		return
	}
	d.publicIP = ip
	var cur [20]byte
	copy(cur[:], d.nodeId)
	if NodeIDSecure(cur, ip) {
		return
	}
	nid := generateSecureNodeID(ip)
	d.nodeId = string(nid)
	if d.routingTable != nil {
		d.routingTable.nodeId = d.nodeId
	}
	if d.store != nil {
		d.store.Id = nid
		saveStore(*d.store)
	}
	d.DebugLogger.Infof("DHT: adopted a BEP 42 secure node ID for external IP %s", ip)
}
