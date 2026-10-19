// Package peer contains the eMule client-to-client TCP framing and the hello
// exchange. It is an isolated spike for the gated transfer plane: nothing here
// is imported by cmd/gx-ed2k yet.
package peer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// Client TCP protocol bytes. The standard eDonkey packets (hello, file
// request, hashset, parts, start-upload) use 0xE3; the eMule extensions
// (EMULEINFO, queue ranking, compressed parts, source exchange) use 0xC5.
// Using 0xC5 for a hello makes eMule read it as EMULEINFO and wait forever.
const (
	EdonkeyProtocol = 0xE3
	EmuleProtocol   = 0xC5
)

// Opcodes used by the hello exchange.
const (
	OpHello       = 0x01
	OpHelloAnswer = 0x4C
)

// Hello tags (eMule CTag ids).
const (
	TagName          = 0x01
	TagVersion       = 0x11
	TagUDPPorts      = 0xF9
	TagMiscOptions   = 0xFA
	TagEmodVersion   = 0xFB
	TagMiscOptions2  = 0xFE
	TagCompatOptions = 0xEF
)

// Hello is the payload of OP_HELLO / OP_HELLOANSWER.
type Hello struct {
	UserHash    ed2k.ID
	ClientID    uint32
	Port        uint16
	Name        string
	Version     uint32
	UDPPort     uint16
	KadPort     uint16
	MiscOption  uint32
	MiscOption2 uint32
	MuleVersion uint32
	Compat      uint32
	// ServerIP and ServerPort are the eD2k server this client is connected
	// to. eMule writes them after the tag list even when they are zero.
	ServerIP   net.IP
	ServerPort uint16
	// Legacy sends only name, version and UDP ports, without the eMule
	// feature tags. It is a compatibility fallback for old or strict peers.
	Legacy bool
}

// EncodeHello builds the OP_HELLOANSWER payload (without the 0xC5 framing).
// The request variant has a leading hash-size byte; see EncodeHelloRequest.
func EncodeHello(hello Hello) []byte {
	out := make([]byte, 0, 16+4+2+4+64)
	out = append(out, hello.UserHash[:]...)
	var fixed [4]byte
	binary.LittleEndian.PutUint32(fixed[:], hello.ClientID)
	out = append(out, fixed[:]...)
	binary.LittleEndian.PutUint16(fixed[:2], hello.Port)
	out = append(out, fixed[:2]...)

	if hello.Legacy {
		tags := []ed2k.Tag{
			{Name: TagName, Type: ed2k.TagString, Str: hello.Name},
			{Name: TagVersion, Type: ed2k.TagUint32, Int: uint64(hello.Version)},
			{Name: TagUDPPorts, Type: ed2k.TagUint32, Int: uint64(hello.KadPort)<<16 | uint64(hello.UDPPort)},
		}
		out = append(out, ed2k.WriteTagList(tags, binary.LittleEndian)...)
		ip := hello.ServerIP.To4()
		if ip == nil {
			ip = net.IPv4zero.To4()
		}
		out = append(out, ip...)
		binary.LittleEndian.PutUint16(fixed[:2], hello.ServerPort)
		return append(out, fixed[:2]...)
	}
	misc1 := hello.MiscOption
	if misc1 == 0 {
		// AICH 1, Unicode, UDP 4, no data compression (the reader does not
		// reassemble split zlib streams yet), no secure identification,
		// source exchange 3, extended requests 2, comments 1, multipacket 1.
		misc1 = 0x33F03212
	}
	misc2 := hello.MiscOption2
	if misc2 == 0 {
		// Kad 3, large files, extended multipacket, source exchange 2; no
		// crypt layer, because we cannot complete those handshakes yet.
		misc2 = 0x00000438
	}
	muleVersion := hello.MuleVersion
	if muleVersion == 0 {
		muleVersion = 0x03020303
	}
	compat := hello.Compat
	if compat == 0 {
		compat = 0
	}
	tags := []ed2k.Tag{
		{Name: TagName, Type: ed2k.TagString, Str: hello.Name},
		{Name: TagVersion, Type: ed2k.TagUint32, Int: uint64(hello.Version)},
		{Name: TagUDPPorts, Type: ed2k.TagUint32, Int: uint64(hello.KadPort)<<16 | uint64(hello.UDPPort)},
		{Name: TagEmodVersion, Type: ed2k.TagUint32, Int: uint64(muleVersion)},
		{Name: TagMiscOptions, Type: ed2k.TagUint32, Int: uint64(misc1)},
		{Name: TagMiscOptions2, Type: ed2k.TagUint32, Int: uint64(misc2)},
		{Name: TagCompatOptions, Type: ed2k.TagUint32, Int: uint64(compat)},
	}
	binary.LittleEndian.PutUint32(fixed[:], uint32(len(tags)))
	out = append(out, fixed[:]...)
	out = append(out, ed2k.WriteTagList(tags, binary.LittleEndian)...)
	ip := hello.ServerIP.To4()
	if ip == nil {
		ip = net.IPv4zero.To4()
	}
	out = append(out, ip...)
	binary.LittleEndian.PutUint16(fixed[:2], hello.ServerPort)
	return append(out, fixed[:2]...)
}

// EncodeHelloRequest builds the OP_HELLO payload. eDonkey writes the user-hash
// size (16) before the hash; eMule rejects a request without it.
func EncodeHelloRequest(hello Hello) []byte {
	out := make([]byte, 0, 1+16+4+2+4+64)
	out = append(out, 16)
	return append(out, EncodeHello(hello)...)
}

// DecodeHelloRequest parses OP_HELLO, which starts with the hash-size byte.
func DecodeHelloRequest(payload []byte) (Hello, error) {
	if len(payload) < 1 || payload[0] != 16 {
		return Hello{}, errors.New("peer: invalid hello hash size")
	}
	return DecodeHello(payload[1:])
}

// DecodeHello parses the hello payload. Unknown tags are kept in order but do
// not affect the decoded fields.
func DecodeHello(payload []byte) (Hello, error) {
	var hello Hello
	if len(payload) < 16+4+2+4 {
		return hello, errors.New("peer: hello too short")
	}
	copy(hello.UserHash[:], payload[:16])
	hello.ClientID = binary.LittleEndian.Uint32(payload[16:])
	hello.Port = binary.LittleEndian.Uint16(payload[20:])
	count := int(binary.LittleEndian.Uint32(payload[22:]))
	if count < 0 || count > 1_000_000 {
		return hello, errors.New("peer: invalid hello tag count")
	}
	tags, consumed, err := ed2k.ReadTagList(payload[26:], count, binary.LittleEndian)
	if err != nil {
		return hello, fmt.Errorf("peer: hello tags: %w", err)
	}
	if end := 26 + consumed; len(payload) >= end+6 {
		hello.ServerIP = net.IPv4(payload[end], payload[end+1], payload[end+2], payload[end+3])
		hello.ServerPort = binary.LittleEndian.Uint16(payload[end+4:])
	}
	for _, tag := range tags {
		switch tag.Name {
		case TagName:
			if tag.Type == ed2k.TagString {
				hello.Name = tag.Str
			}
		case TagVersion:
			if tag.Type == ed2k.TagUint32 || tag.Type == ed2k.TagUint64 {
				hello.Version = uint32(tag.Int)
			}
		case TagUDPPorts:
			if tag.Type == ed2k.TagUint32 || tag.Type == ed2k.TagUint64 {
				hello.KadPort = uint16(tag.Int >> 16)
				hello.UDPPort = uint16(tag.Int)
			}
		case TagMiscOptions:
			if tag.Type == ed2k.TagUint32 || tag.Type == ed2k.TagUint64 {
				hello.MiscOption = uint32(tag.Int)
			}
		}
	}
	return hello, nil
}

// Frame adds the client TCP header with the protocol byte that belongs to the
// opcode: 0xE3 for eDonkey packets and 0xC5 for eMule extensions. The header
// is protocol (1), length (4, little-endian, opcode included) and opcode (1),
// the same layout as the eD2k server protocol.
func Frame(opcode byte, payload []byte) []byte {
	return frameWithProtocol(protocolFor(opcode), opcode, payload)
}

func frameWithProtocol(protocol, opcode byte, payload []byte) []byte {
	out := make([]byte, 6+len(payload))
	out[0] = protocol
	binary.LittleEndian.PutUint32(out[1:], uint32(len(payload)+1))
	out[5] = opcode
	copy(out[6:], payload)
	return out
}

// EncodeMuleInfo builds OP_EMULEINFO: client version, eMule protocol version
// and the eMule feature tags. The packet is sent on the 0xC5 protocol.
func EncodeMuleInfo() []byte {
	tags := []ed2k.Tag{
		{Name: 0x20, Type: ed2k.TagUint32, Int: 1},         // compression
		{Name: 0x22, Type: ed2k.TagUint32, Int: 4},         // UDP version
		{Name: 0x21, Type: ed2k.TagUint32, Int: 4674},      // UDP port
		{Name: 0x23, Type: ed2k.TagUint32, Int: 3},         // source exchange
		{Name: 0x24, Type: ed2k.TagUint32, Int: 1},         // comments
		{Name: 0x25, Type: ed2k.TagUint32, Int: 2},         // extended requests
		{Name: 0x27, Type: ed2k.TagUint32, Int: 0},         // features
		{Name: 0x26, Type: ed2k.TagUint32, Int: 3},         // compatible client
		{Name: 0x55, Type: ed2k.TagString, Str: "gx-ed2k"}, // mod version
	}
	out := []byte{ed2k.KadProtocolVersion, 0x01}
	var count [4]byte
	binary.LittleEndian.PutUint32(count[:], uint32(len(tags)))
	out = append(out, count[:]...)
	return append(out, ed2k.WriteTagList(tags, binary.LittleEndian)...)
}

// protocolFor returns the header byte for one client opcode.
func protocolFor(opcode byte) byte {
	switch opcode {
	case OpQueueRanking, OpCompressedPart, OpCompressedPartI64, OpSendingPartI64, OpRequestPartsI64,
		OpRequestsSources, OpAnswersSources, OpRequestsSources2, OpAnswersSources2:
		return EmuleProtocol
	}
	return EdonkeyProtocol
}

// Unframe validates one complete client packet and returns its opcode and
// payload. It does not read from a stream; the caller buffers six bytes first.
func Unframe(packet []byte) (byte, []byte, error) {
	if len(packet) < 6 {
		return 0, nil, errors.New("peer: short packet")
	}
	if packet[0] != EdonkeyProtocol && packet[0] != EmuleProtocol {
		return 0, nil, errors.New("peer: not an eMule client packet")
	}
	size := int(binary.LittleEndian.Uint32(packet[1:]))
	if size < 1 || 6+size-1 > len(packet) {
		return 0, nil, errors.New("peer: truncated packet")
	}
	opcode := packet[5]
	return opcode, packet[6 : 6+size-1], nil
}
