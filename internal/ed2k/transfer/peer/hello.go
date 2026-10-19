// Package peer contains the eMule client-to-client TCP framing and the hello
// exchange. It is an isolated spike for the gated transfer plane: nothing here
// is imported by cmd/gx-ed2k yet.
package peer

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// ClientProtocol is the eMule client TCP protocol byte; the server protocol is
// 0xE3 and is handled by internal/ed2k.
const ClientProtocol = 0xC5

// Opcodes used by the hello exchange.
const (
	OpHello       = 0x01
	OpHelloAnswer = 0x4C
)

// Hello tags (eMule CTag ids).
const (
	TagName        = 0x01
	TagVersion     = 0x11
	TagUDPPorts    = 0xF9
	TagMiscOptions = 0xFA
	TagEmodVersion = 0xFB
)

// Hello is the payload of OP_HELLO / OP_HELLOANSWER.
type Hello struct {
	UserHash   ed2k.ID
	ClientID   uint32
	Port       uint16
	Name       string
	Version    uint32
	UDPPort    uint16
	KadPort    uint16
	MiscOption uint32
}

// EncodeHello builds the hello payload (without the 0xC5 framing).
func EncodeHello(hello Hello) []byte {
	out := make([]byte, 0, 16+4+2+4+64)
	out = append(out, hello.UserHash[:]...)
	var fixed [4]byte
	binary.LittleEndian.PutUint32(fixed[:], hello.ClientID)
	out = append(out, fixed[:]...)
	binary.LittleEndian.PutUint16(fixed[:2], hello.Port)
	out = append(out, fixed[:2]...)

	tags := []ed2k.Tag{
		{Name: TagName, Type: ed2k.TagString, Str: hello.Name},
		{Name: TagVersion, Type: ed2k.TagUint32, Int: uint64(hello.Version)},
		{Name: TagUDPPorts, Type: ed2k.TagUint32, Int: uint64(hello.KadPort)<<16 | uint64(hello.UDPPort)},
	}
	if hello.MiscOption != 0 {
		tags = append(tags, ed2k.Tag{Name: TagMiscOptions, Type: ed2k.TagUint32, Int: uint64(hello.MiscOption)})
	}
	binary.LittleEndian.PutUint32(fixed[:], uint32(len(tags)))
	out = append(out, fixed[:]...)
	out = append(out, ed2k.WriteTagList(tags, binary.LittleEndian)...)
	return out
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
	tags, _, err := ed2k.ReadTagList(payload[26:], count, binary.LittleEndian)
	if err != nil {
		return hello, fmt.Errorf("peer: hello tags: %w", err)
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

// Frame adds the eMule client TCP header: protocol 0xC5, opcode, payload size.
func Frame(opcode byte, payload []byte) []byte {
	out := make([]byte, 6+len(payload))
	out[0] = ClientProtocol
	out[1] = opcode
	binary.LittleEndian.PutUint32(out[2:], uint32(len(payload)))
	copy(out[6:], payload)
	return out
}

// Unframe validates one complete client packet and returns its opcode and
// payload. It does not read from a stream; the caller buffers six bytes first.
func Unframe(packet []byte) (byte, []byte, error) {
	if len(packet) < 6 {
		return 0, nil, errors.New("peer: short packet")
	}
	if packet[0] != ClientProtocol {
		return 0, nil, errors.New("peer: not an eMule client packet")
	}
	size := int(binary.LittleEndian.Uint32(packet[2:]))
	if size < 0 || 6+size > len(packet) {
		return 0, nil, errors.New("peer: truncated packet")
	}
	return packet[1], packet[6 : 6+size], nil
}
