package ed2k

import (
	"encoding/binary"
	"math"
	"net"
)

// PeerSource is one eD2k peer that may hold a file. It is learned from a
// server (OP_FOUNDSOURCES), from Kad (SEARCH_SOURCE results) or from another
// peer (source exchange).
//
// The wire formats differ in one subtle point: eD2k servers send the IPv4
// address in network order, while Kad source tags send it as a host-order
// 32-bit integer. Both are normalised to net.IP here.
type PeerSource struct {
	IP         net.IP
	TCPPort    uint16
	UDPPort    uint16
	UserHash   ID
	Type       uint8
	Encryption uint8
	BuddyID    ID
	BuddyIP    net.IP
	BuddyPort  uint16
	Origin     string // "server", "server-lowid", "kad" or "exchange"

	// LowID sources are identified by the eD2k server client ID instead of
	// an IP/TCP endpoint. ServerIP/ServerPort identify the server that can
	// relay a callback to that ID.
	LowID      bool
	ClientID   uint32
	ServerIP   net.IP
	ServerPort uint16
}

// encodeGetSources builds OP_GETSOURCES. Modern servers want the file size
// after the hash; files above 4 GiB use the zero + uint64 large-file form.
func encodeGetSources(hash ID, size int64) []byte {
	out := make([]byte, 0, 24)
	out = append(out, hash[:]...)
	var raw [8]byte
	if size > math.MaxUint32 {
		out = append(out, 0, 0, 0, 0)
		binary.LittleEndian.PutUint64(raw[:], uint64(size))
		return append(out, raw[:]...)
	}
	if size < 0 {
		size = 0
	}
	binary.LittleEndian.PutUint32(raw[:4], uint32(size))
	return append(out, raw[:4]...)
}

// decodeFoundSources parses OP_FOUNDSOURCES: file hash, one-byte count, then
// count pairs of IPv4 (network order) and TCP port (little-endian).
func decodeFoundSources(payload []byte) (ID, []PeerSource, error) {
	return decodeFoundSourcesFromServer(payload, nil, 0)
}

// decodeFoundSourcesFromServer parses OP_FOUNDSOURCES. An entry whose numeric
// user ID is below 0x01000000 is a LowID: the server can call that client back
// but it has no reachable IP/TCP endpoint. HighID entries keep the usual
// network-order IPv4 representation.
func decodeFoundSourcesFromServer(payload []byte, serverIP net.IP, serverPort uint16) (ID, []PeerSource, error) {
	var hash ID
	if len(payload) < 17 {
		return hash, nil, ErrTruncatedPacket
	}
	copy(hash[:], payload[:16])
	count := int(payload[16])
	if 17+count*6 > len(payload) {
		return hash, nil, ErrTruncatedPacket
	}
	sources := make([]PeerSource, 0, count)
	offset := 17
	for i := 0; i < count; i++ {
		raw := payload[offset : offset+4]
		port := binary.LittleEndian.Uint16(payload[offset+4:])
		offset += 6
		userID := binary.LittleEndian.Uint32(raw)
		if userID != 0 && userID < 0x01000000 {
			sources = append(sources, PeerSource{
				LowID: true, ClientID: userID, TCPPort: 0,
				ServerIP: serverIP, ServerPort: serverPort,
				Origin: "server-lowid",
			})
			continue
		}
		if port == 0 {
			continue
		}
		ip := net.IPv4(raw[0], raw[1], raw[2], raw[3])
		sources = append(sources, PeerSource{
			IP: ip, TCPPort: port, ServerIP: serverIP, ServerPort: serverPort,
			Origin: "server",
		})
	}
	return hash, sources, nil
}

// encodeSourceSearch builds a Kad SEARCH_SOURCE request: the file hash as
// target, a zero start position and the file size.
func encodeSourceSearch(target ID, size int64) []byte {
	w := newWriter(binary.LittleEndian)
	w.kadID(target)
	w.u16(0)
	if size < 0 {
		size = 0
	}
	w.u64(uint64(size))
	return w.buf
}

// decodeSourceResponse parses a Kad SEARCH_RES whose entries are sources
// rather than files: sender ID, target, count, then (user hash + tags).
func decodeSourceResponse(payload []byte) (ID, []PeerSource, error) {
	r := newReader(payload, binary.LittleEndian)
	if _, err := r.kadID(); err != nil {
		return ID{}, nil, err
	}
	target, err := r.kadID()
	if err != nil {
		return ID{}, nil, err
	}
	count, err := r.u16()
	if err != nil {
		return ID{}, nil, err
	}
	sources := make([]PeerSource, 0, count)
	for i := 0; i < int(count); i++ {
		userHash, err := r.kadID()
		if err != nil {
			return ID{}, nil, err
		}
		tagCount, err := r.u8()
		if err != nil {
			return ID{}, nil, err
		}
		tags := make([]Tag, 0, tagCount)
		for j := 0; j < int(tagCount); j++ {
			tag, err := r.tag()
			if err != nil {
				return ID{}, nil, err
			}
			tags = append(tags, tag)
		}
		if source, ok := sourceFromTags(userHash, tags); ok {
			sources = append(sources, source)
		}
	}
	return target, sources, nil
}

func sourceFromTags(userHash ID, tags []Tag) (PeerSource, bool) {
	byID := tagsByID(tags)
	ipValue, ok := tagUint(byID, TagSourceIP)
	if !ok {
		return PeerSource{}, false
	}
	// A Kad source tag carries the address as a host-order integer, so the
	// most significant byte is the first octet.
	ip := net.IPv4(byte(ipValue>>24), byte(ipValue>>16), byte(ipValue>>8), byte(ipValue))
	port, hasPort := tagUint(byID, TagSourcePort)
	if hasPort && port > math.MaxUint16 {
		return PeerSource{}, false
	}
	source := PeerSource{
		IP:       ip,
		TCPPort:  uint16(port),
		UserHash: userHash,
		Origin:   "kad",
	}
	if udp, ok := tagUint(byID, TagSourceUDPPort); ok && udp <= math.MaxUint16 {
		source.UDPPort = uint16(udp)
	}
	if kind, ok := tagUint(byID, TagSourceType); ok && kind <= math.MaxUint8 {
		source.Type = uint8(kind)
	}
	if encryption, ok := tagUint(byID, TagEncryption); ok && encryption <= math.MaxUint8 {
		source.Encryption = uint8(encryption)
	}
	if buddy, ok := tagUint(byID, TagServerIP); ok {
		source.BuddyIP = net.IPv4(byte(buddy>>24), byte(buddy>>16), byte(buddy>>8), byte(buddy))
	}
	if buddyPort, ok := tagUint(byID, TagServerPort); ok && buddyPort <= math.MaxUint16 {
		source.BuddyPort = uint16(buddyPort)
	}
	if buddyHash, ok := tagString(byID, TagBuddyHash); ok {
		if id, err := ParseID(buddyHash); err == nil {
			source.BuddyID = id
		}
	}
	// Types 3, 5 and 6 are firewalled: they have no inbound TCP port but can
	// be reached with a direct UDP callback.
	lowID := source.Type == 3 || source.Type == 5 || source.Type == 6
	if source.TCPPort == 0 && !(lowID && source.UDPPort != 0) {
		return PeerSource{}, false
	}
	return source, true
}
