package ed2k

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

// Kad wire protocol.
const (
	KadProtocol = 0xE4
	// KadProtocolPacked marks a zlib-compressed Kad datagram: byte 1 is still
	// the opcode and the compressed payload starts at byte 2. eMule uses it
	// for packets above 200 bytes, which includes most search result pages.
	KadProtocolPacked = 0xE5

	KadBootstrapReq   = 0x01
	KadBootstrapRes   = 0x09
	KadHelloReq       = 0x11
	KadHelloRes       = 0x19
	KadReq            = 0x21
	KadHelloResAck    = 0x22
	KadRes            = 0x29
	KadSearchKeyReq   = 0x33
	KadSearchSource   = 0x34
	KadSearchNotesReq = 0x35
	KadCallbackReq    = 0x52
	KadSearchRes      = 0x3B
	KadPublishKeyReq  = 0x43
	KadPublishSrcReq  = 0x44
	KadPublishNoteReq = 0x45
	KadPublishRes     = 0x4B
	KadFirewalled2Req = 0x53
	KadPing           = 0x60
	KadPong           = 0x61
	KadFirewallUDP    = 0x62

	// KadVersion is the protocol version we announce. Version 3 is the first
	// Kad2 generation with the 16-bit search start position; staying below 6
	// means "I do not support obfuscation", so peers answer in clear and we
	// never have to invent RC4 keys we cannot verify.
	KadVersion = 3

	// KadProtocolVersion is the eMule "client software" version advertised to
	// servers and peers. It is informational.
	KadProtocolVersion = 0x3C
)

// ED2K wire protocol (TCP with the server, UDP for server pings).
const (
	Ed2kProtocol = 0xE3

	OpLoginRequest   = 0x01
	OpReject         = 0x05
	OpGetServerList  = 0x14
	OpOfferFiles     = 0x15
	OpSearchRequest  = 0x16
	OpGetSources     = 0x19
	OpSearchUser     = 0x1A
	OpCallbackReq    = 0x1C
	OpServerList     = 0x32
	OpSearchResult   = 0x33
	OpServerStatus   = 0x34
	OpCallbackOK     = 0x35
	OpCallbackFail   = 0x36
	OpIDChange       = 0x40
	OpServerIdent    = 0x41
	OpFoundSources   = 0x42
	OpFoundSourcesOb = 0x44
)

// Contact is one Kad routing-table entry.
type Contact struct {
	ID      ID
	IP      net.IP
	UDPPort uint16
	TCPPort uint16
	Version uint8
}

// UDPAddr is the contact's Kad address.
func (c Contact) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: c.IP, Port: int(c.UDPPort)}
}

// valid reports whether a contact can receive Kad packets. Only public
// unicast addresses are kept: a private, loopback, link-local or multicast
// entry can never be a Kad peer and would otherwise crowd the closest set.
func (c Contact) valid() bool {
	return !c.ID.IsZero() && c.IP != nil && routableIP(c.IP) && c.UDPPort != 0 && c.Version > 1
}

func routableIP(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	// Loopback is allowed so the deterministic tests can run a fake peer on
	// 127.0.0.1; private, link-local and multicast addresses are not peers.
	if ip4.IsLoopback() {
		return true
	}
	if !ip4.IsGlobalUnicast() || ip4.IsPrivate() ||
		ip4.IsLinkLocalUnicast() || ip4.IsMulticast() || ip4.IsUnspecified() {
		return false
	}
	// Documentation and benchmark ranges are not peers either.
	switch {
	case ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2:
		return false
	case ip4[0] == 198 && (ip4[1] == 51 && ip4[2] == 100 || ip4[1] == 18 || ip4[1] == 19):
		return false
	case ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113:
		return false
	}
	return true
}

func kadPacket(opcode byte, payload []byte) []byte {
	out := make([]byte, 2+len(payload))
	out[0] = KadProtocol
	out[1] = opcode
	copy(out[2:], payload)
	return out
}

// splitKadPacket validates the header and returns the opcode and payload.
// Compressed datagrams are inflated first; network input is capped so a
// malicious peer cannot make us allocate without bound.
func splitKadPacket(data []byte) (byte, []byte, error) {
	if len(data) < 2 {
		return 0, nil, errors.New("ed2k: not a Kad packet")
	}
	switch data[0] {
	case KadProtocol:
		return data[1], data[2:], nil
	case KadProtocolPacked:
		zr, err := zlib.NewReader(bytes.NewReader(data[2:]))
		if err != nil {
			return 0, nil, fmt.Errorf("ed2k: packed Kad packet: %w", err)
		}
		defer zr.Close()
		payload, err := io.ReadAll(io.LimitReader(zr, 1<<20))
		if err != nil {
			return 0, nil, fmt.Errorf("ed2k: unpack Kad packet: %w", err)
		}
		return data[1], payload, nil
	default:
		return 0, nil, errors.New("ed2k: not a Kad packet")
	}
}

// contact writes the contact layout used by REQ/RES, bootstrap and nodes.dat.
// The ID is a Kad value and the IPv4 address is stored as a native-order
// 32-bit word, so its bytes are reversed relative to dotted notation.
func (w *writer) contact(c Contact) {
	w.kadID(c.ID)
	ip := c.IP.To4()
	if ip == nil {
		ip = net.IPv4zero.To4()
	}
	w.raw([]byte{ip[3], ip[2], ip[1], ip[0]})
	w.u16(c.UDPPort)
	w.u16(c.TCPPort)
	w.u8(c.Version)
}

func (r *reader) contact() (Contact, error) {
	var c Contact
	id, err := r.kadID()
	if err != nil {
		return c, err
	}
	c.ID = id
	ip, err := r.n(4)
	if err != nil {
		return c, err
	}
	c.IP = net.IPv4(ip[3], ip[2], ip[1], ip[0])
	udp, err := r.u16()
	if err != nil {
		return c, err
	}
	tcp, err := r.u16()
	if err != nil {
		return c, err
	}
	version, err := r.u8()
	if err != nil {
		return c, err
	}
	c.UDPPort, c.TCPPort, c.Version = udp, tcp, version
	return c, nil
}

// encodeHello builds the HELLO_REQ/HELLO_RES payload: our ID, TCP port,
// protocol version. No tags: we do not obfuscate, and peers take the UDP port
// from the packet source, which is the only address we can verify.
func encodeHello(id ID, tcpPort int) []byte {
	w := newWriter(binary.LittleEndian)
	w.kadID(id)
	w.u16(uint16(tcpPort))
	w.u8(KadVersion)
	w.u8(0)
	return w.buf
}

func decodeHello(payload []byte) (Contact, error) {
	r := newReader(payload, binary.LittleEndian)
	id, err := r.kadID()
	if err != nil {
		return Contact{}, err
	}
	tcpPort, err := r.u16()
	if err != nil {
		return Contact{}, err
	}
	version, err := r.u8()
	if err != nil {
		return Contact{}, err
	}
	if version < 2 {
		return Contact{}, fmt.Errorf("ed2k: unsupported Kad version %d", version)
	}
	if r.remaining() > 0 {
		count, err := r.u8()
		if err != nil {
			return Contact{}, err
		}
		for i := 0; i < int(count); i++ {
			if _, err := r.tag(); err != nil {
				return Contact{}, err
			}
		}
	}
	return Contact{ID: id, TCPPort: tcpPort, Version: version}, nil
}

// encodeBootstrapResponse returns our details and up to max contacts.
func encodeBootstrapResponse(id ID, tcpPort int, contacts []Contact, max int) []byte {
	if len(contacts) > max {
		contacts = contacts[:max]
	}
	w := newWriter(binary.LittleEndian)
	w.kadID(id)
	w.u16(uint16(tcpPort))
	w.u8(KadVersion)
	w.u16(uint16(len(contacts)))
	for _, contact := range contacts {
		w.contact(contact)
	}
	return w.buf
}

func decodeBootstrapResponse(payload []byte) (Contact, []Contact, error) {
	r := newReader(payload, binary.LittleEndian)
	id, err := r.kadID()
	if err != nil {
		return Contact{}, nil, err
	}
	tcpPort, err := r.u16()
	if err != nil {
		return Contact{}, nil, err
	}
	version, err := r.u8()
	if err != nil {
		return Contact{}, nil, err
	}
	count, err := r.u16()
	if err != nil {
		return Contact{}, nil, err
	}
	contacts := make([]Contact, 0, count)
	for i := 0; i < int(count); i++ {
		contact, err := r.contact()
		if err != nil {
			return Contact{}, nil, err
		}
		contacts = append(contacts, contact)
	}
	return Contact{ID: id, TCPPort: tcpPort, Version: version}, contacts, nil
}

// encodeNodeRequest asks a node for contacts close to target.
func encodeNodeRequest(target, receiver ID, count byte) []byte {
	w := newWriter(binary.LittleEndian)
	w.u8(count)
	w.kadID(target)
	w.kadID(receiver)
	return w.buf
}

func decodeNodeRequest(payload []byte) (byte, ID, ID, error) {
	r := newReader(payload, binary.LittleEndian)
	count, err := r.u8()
	if err != nil {
		return 0, ID{}, ID{}, err
	}
	target, err := r.kadID()
	if err != nil {
		return 0, ID{}, ID{}, err
	}
	receiver, err := r.kadID()
	if err != nil {
		return 0, ID{}, ID{}, err
	}
	return count, target, receiver, nil
}

func encodeNodeResponse(target ID, contacts []Contact) []byte {
	w := newWriter(binary.LittleEndian)
	w.kadID(target)
	w.u8(uint8(len(contacts)))
	for _, contact := range contacts {
		w.contact(contact)
	}
	return w.buf
}

func decodeNodeResponse(payload []byte) (ID, []Contact, error) {
	r := newReader(payload, binary.LittleEndian)
	target, err := r.kadID()
	if err != nil {
		return ID{}, nil, err
	}
	count, err := r.u8()
	if err != nil {
		return ID{}, nil, err
	}
	contacts := make([]Contact, 0, count)
	for i := 0; i < int(count); i++ {
		contact, err := r.contact()
		if err != nil {
			return ID{}, nil, err
		}
		contacts = append(contacts, contact)
	}
	return target, contacts, nil
}

// encodeSearchRequest asks for every file indexed under target. With terms it
// sends a restrictive search: bit 15 of the start position is set and an
// expression tree follows, so the peer filters the keyword bucket itself
// instead of returning everything indexed under the primary keyword.
func encodeSearchRequest(target ID, terms []string) []byte {
	w := newWriter(binary.LittleEndian)
	w.kadID(target)
	if len(terms) == 0 {
		w.u16(0)
		return w.buf
	}
	w.u16(0x8000)
	w.raw(encodeSearchTree(terms))
	return w.buf
}

// encodeSearchTree builds a left-deep AND of string terms, the layout read by
// eMule's CreateSearchExpressionTree: 0x01 + uint16 length + UTF-8 lowercased
// text for a leaf, 0x00 0x00 for AND, then the left and right subtrees.
func encodeSearchTree(terms []string) []byte {
	var tree []byte
	for index, term := range terms {
		leaf := newWriter(binary.LittleEndian)
		leaf.u8(0x01)
		leaf.string16(strings.ToLower(term))
		if index == 0 {
			tree = leaf.buf
			continue
		}
		node := make([]byte, 0, 2+len(tree)+len(leaf.buf))
		node = append(node, 0x00, 0x00)
		node = append(node, tree...)
		node = append(node, leaf.buf...)
		tree = node
	}
	return tree
}

func decodeSearchRequest(payload []byte) (ID, uint16, []byte, error) {
	r := newReader(payload, binary.LittleEndian)
	target, err := r.kadID()
	if err != nil {
		return ID{}, 0, nil, err
	}
	start, err := r.u16()
	if err != nil {
		return ID{}, 0, nil, err
	}
	return target, start, payload[r.pos:], nil
}

// SearchFile is one file returned by Kad or by an eD2k server.
type SearchFile struct {
	Name     string
	Size     int64
	Hash     string
	Sources  int64
	Type     string
	Format   string
	Artist   string
	Album    string
	Title    string
	Bitrate  int64
	Codec    string
	Publish  uint32
	ClientID uint32
	ClientIP net.IP
	Client   uint16
	Origin   string // "kad" or "server"
}

func searchFileFromTags(hash ID, tags map[uint8]Tag, origin string) (SearchFile, bool) {
	name, ok := tagString(tags, TagFileName)
	if !ok || name == "" {
		return SearchFile{}, false
	}
	size, ok := fileSizeFromTags(tags)
	if !ok {
		return SearchFile{}, false
	}
	file := SearchFile{
		Name: name, Size: size, Hash: hash.Hex(), Origin: origin,
	}
	if value, ok := tagUint(tags, TagSources); ok {
		file.Sources = int64(value)
	}
	if value, ok := tagString(tags, TagFileType); ok {
		file.Type = value
	} else if value, ok := tagUint(tags, TagFileType); ok {
		file.Type = fmt.Sprintf("%d", value)
	}
	if value, ok := tagString(tags, TagFileFormat); ok {
		file.Format = value
	}
	if value, ok := tagString(tags, 0xD0); ok {
		file.Artist = value
	}
	if value, ok := tagString(tags, 0xD1); ok {
		file.Album = value
	}
	if value, ok := tagString(tags, 0xD2); ok {
		file.Title = value
	}
	if value, ok := tagUint(tags, 0xD4); ok {
		file.Bitrate = int64(value)
	}
	if value, ok := tagString(tags, 0xD5); ok {
		file.Codec = value
	}
	if value, ok := tagUint(tags, TagPublishInfo); ok {
		file.Publish = uint32(value)
	}
	return file, true
}

// decodeSearchResponse parses the Kad result packet: sender ID, target,
// result count, then (file ID + tag list) entries.
func decodeSearchResponse(payload []byte) (ID, ID, []SearchFile, error) {
	r := newReader(payload, binary.LittleEndian)
	sender, err := r.kadID()
	if err != nil {
		return ID{}, ID{}, nil, err
	}
	target, err := r.kadID()
	if err != nil {
		return ID{}, ID{}, nil, err
	}
	count, err := r.u16()
	if err != nil {
		return ID{}, ID{}, nil, err
	}
	files := make([]SearchFile, 0, count)
	for i := 0; i < int(count); i++ {
		hash, err := r.kadID()
		if err != nil {
			return ID{}, ID{}, nil, err
		}
		tagCount, err := r.u8()
		if err != nil {
			return ID{}, ID{}, nil, err
		}
		tags := make([]Tag, 0, tagCount)
		for j := 0; j < int(tagCount); j++ {
			tag, err := r.tag()
			if err != nil {
				return ID{}, ID{}, nil, err
			}
			tags = append(tags, tag)
		}
		if file, ok := searchFileFromTags(hash, tagsByID(tags), "kad"); ok {
			files = append(files, file)
		}
		_ = sender
	}
	return sender, target, files, nil
}

// encodePong answers a PING.
func encodePong() []byte { return nil }

// encodeSearchExpression builds a single string search expression, the form
// used by both Kad and the eD2k servers. The byte layout is the network one:
// 0x01 (string parameter), uint16 length, UTF-8 text.
func encodeSearchExpression(bo binary.ByteOrder, keyword string) []byte {
	w := newWriter(bo)
	w.u8(0x01)
	w.string16(keyword)
	return w.buf
}
