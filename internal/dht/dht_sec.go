package dht

import (
	"crypto/rand"
	"hash/crc32"
	"net"
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
	return crc32.Checksum(buf[:len(mask)], crc32.MakeTable(crc32.Castagnoli))
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

// noteExternalIP adopts the external IP a remote node reports for us (BEP 42
// "ip" field, 4/16 bytes plus a 2-byte port) and, once, regenerates a node ID
// secure for it. Only used when the external IP was not configured at startup.
// It runs in the DHT run loop.
func (d *DHT) noteExternalIP(raw string) {
	if d.config.PublicIP != nil || len(raw) < 6 {
		return
	}
	var ip net.IP
	switch len(raw) {
	case 6: // IPv4 + port
		ip = net.IPv4(raw[0], raw[1], raw[2], raw[3])
	case 18: // IPv6 + port
		ip = net.IP(append([]byte(nil), raw[:16]...))
	default:
		return
	}
	if isLocalNetworkIP(ip) {
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
