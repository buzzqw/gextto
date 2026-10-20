// Package amule is Gextto's client for aMule's External Connections (EC)
// protocol, the binary TCP protocol amuled exposes to amulecmd, amuleweb and
// the remote GUI. It is our own code: aMule's sources were read only as a
// protocol reference (packet framing, tag layout, opcodes and the login
// handshake), nothing is copied or linked.
//
// Wire format (protocol version 0x0204, aMule 2.3.x):
//
//	packet  = flags:uint32 BE | length:uint32 BE | payload[length]
//	payload = opcode:uint8 | tagCount:uint16 | tag*
//	tag     = name:uint16 (name<<1 | hasChildren) | type:uint8 | length:uint32
//	          [ childCount:uint16 | tag* ] data
//
// Numbers in the payload are big-endian unless the packet flags carry
// EC_FLAG_UTF8_NUMBERS, in which case each one is a UTF-8 style varint. A
// tag's length counts its data plus every child with a fixed-size header
// (2+1+4, plus 2 when the child has children of its own), independent of the
// number encoding. EC_FLAG_ZLIB means the payload is one zlib stream.
//
// This client never advertises zlib or UTF-8 numbers, so amuled answers with
// plain packets; the decoder still accepts both, which keeps it working if a
// future core sends them anyway.
package amule

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

// Packet flags.
const (
	flagBase        = 0x20
	flagZlib        = 0x01
	flagUTF8Numbers = 0x02
	flagUnknownMask = 0xff7f7f08
)

// maxPacket bounds what we accept from the core. A full download queue or a
// large search result list is a few MB at most.
const maxPacket = 64 << 20

// Tag data types.
const (
	TypeUnknown = 0
	TypeCustom  = 1
	TypeUint8   = 2
	TypeUint16  = 3
	TypeUint32  = 4
	TypeUint64  = 5
	TypeString  = 6
	TypeDouble  = 7
	TypeIPv4    = 8
	TypeHash16  = 9
	TypeUint128 = 10
)

// Tag is one EC tag: a name, a typed value and optional children.
type Tag struct {
	Name     uint16
	Type     uint8
	Data     []byte
	Children []Tag
}

// Packet is one EC request or reply.
type Packet struct {
	Op   uint8
	Tags []Tag
}

// IntTag builds an integer tag with the smallest width that holds value, as
// the core does.
func IntTag(name uint16, value uint64, children ...Tag) Tag {
	var data []byte
	var kind uint8
	switch {
	case value <= math.MaxUint8:
		kind, data = TypeUint8, []byte{byte(value)}
	case value <= math.MaxUint16:
		kind, data = TypeUint16, binary.BigEndian.AppendUint16(nil, uint16(value))
	case value <= math.MaxUint32:
		kind, data = TypeUint32, binary.BigEndian.AppendUint32(nil, uint32(value))
	default:
		kind, data = TypeUint64, binary.BigEndian.AppendUint64(nil, value)
	}
	return Tag{Name: name, Type: kind, Data: data, Children: children}
}

// StringTag builds a NUL-terminated UTF-8 string tag.
func StringTag(name uint16, value string, children ...Tag) Tag {
	data := append([]byte(value), 0)
	return Tag{Name: name, Type: TypeString, Data: data, Children: children}
}

// HashTag builds a 16-byte hash tag (an MD4 file hash or an MD5 digest).
func HashTag(name uint16, hash [16]byte, children ...Tag) Tag {
	return Tag{Name: name, Type: TypeHash16, Data: append([]byte(nil), hash[:]...), Children: children}
}

// EmptyTag builds a tag with no data, used for flags and containers.
func EmptyTag(name uint16, children ...Tag) Tag {
	return Tag{Name: name, Type: TypeCustom, Children: children}
}

// Child returns the first child with the given name.
func (t Tag) Child(name uint16) (Tag, bool) {
	for _, child := range t.Children {
		if child.Name == name {
			return child, true
		}
	}
	return Tag{}, false
}

// Int decodes an integer tag. Any width is accepted.
func (t Tag) Int() uint64 {
	switch len(t.Data) {
	case 1:
		return uint64(t.Data[0])
	case 2:
		return uint64(binary.BigEndian.Uint16(t.Data))
	case 4:
		return uint64(binary.BigEndian.Uint32(t.Data))
	case 8:
		return binary.BigEndian.Uint64(t.Data)
	}
	return 0
}

// String decodes a string tag, dropping the NUL terminator.
func (t Tag) String() string {
	return string(bytes.TrimRight(t.Data, "\x00"))
}

// Hash decodes a 16-byte hash tag.
func (t Tag) Hash() ([16]byte, bool) {
	var out [16]byte
	if len(t.Data) != 16 {
		return out, false
	}
	copy(out[:], t.Data)
	return out, true
}

// ChildInt is Child(name).Int() with a found flag.
func (t Tag) ChildInt(name uint16) (uint64, bool) {
	child, ok := t.Child(name)
	if !ok {
		return 0, false
	}
	return child.Int(), true
}

// ChildString is Child(name).String().
func (t Tag) ChildString(name uint16) string {
	child, _ := t.Child(name)
	return child.String()
}

// Tag returns the first top-level tag with the given name.
func (p Packet) Tag(name uint16) (Tag, bool) {
	for _, tag := range p.Tags {
		if tag.Name == name {
			return tag, true
		}
	}
	return Tag{}, false
}

// Message returns the EC_TAG_STRING text the core attaches to failures.
func (p Packet) Message() string {
	tag, _ := p.Tag(TagString)
	return tag.String()
}

// wireLen is the tag length the core puts on the wire (CECTag::GetTagLen).
func (t Tag) wireLen() uint32 {
	length := uint32(len(t.Data))
	for _, child := range t.Children {
		length += child.wireLen() + 7
		if len(child.Children) > 0 {
			length += 2
		}
	}
	return length
}

// Encode serialises a packet with plain (big-endian) numbers and no
// compression, header included.
func (p Packet) Encode() ([]byte, error) {
	if len(p.Tags) > math.MaxUint16 {
		return nil, errors.New("amule: too many tags")
	}
	payload := []byte{p.Op}
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(p.Tags)))
	for _, tag := range p.Tags {
		var err error
		if payload, err = appendTag(payload, tag); err != nil {
			return nil, err
		}
	}
	out := binary.BigEndian.AppendUint32(nil, flagBase)
	out = binary.BigEndian.AppendUint32(out, uint32(len(payload)))
	return append(out, payload...), nil
}

func appendTag(out []byte, tag Tag) ([]byte, error) {
	if tag.Name > math.MaxUint16>>1 {
		return nil, fmt.Errorf("amule: tag name %#x out of range", tag.Name)
	}
	if len(tag.Children) > math.MaxUint16 {
		return nil, errors.New("amule: too many child tags")
	}
	name := tag.Name << 1
	if len(tag.Children) > 0 {
		name |= 1
	}
	out = binary.BigEndian.AppendUint16(out, name)
	out = append(out, tag.Type)
	out = binary.BigEndian.AppendUint32(out, tag.wireLen())
	if len(tag.Children) > 0 {
		out = binary.BigEndian.AppendUint16(out, uint16(len(tag.Children)))
		for _, child := range tag.Children {
			var err error
			if out, err = appendTag(out, child); err != nil {
				return nil, err
			}
		}
	}
	return append(out, tag.Data...), nil
}

// ReadPacket reads one packet from r.
func ReadPacket(r io.Reader) (Packet, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Packet{}, err
	}
	flags := binary.BigEndian.Uint32(header[:4])
	length := binary.BigEndian.Uint32(header[4:])
	if flags&0x60 != flagBase || flags&flagUnknownMask != 0 {
		return Packet{}, fmt.Errorf("amule: unsupported packet flags %#x", flags)
	}
	if length > maxPacket {
		return Packet{}, fmt.Errorf("amule: packet of %d bytes is too large", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Packet{}, err
	}
	return DecodePayload(flags, payload)
}

// DecodePayload decodes a packet body read with the given flags.
func DecodePayload(flags uint32, payload []byte) (Packet, error) {
	if flags&flagZlib != 0 {
		reader, err := zlib.NewReader(bytes.NewReader(payload))
		if err != nil {
			return Packet{}, fmt.Errorf("amule: zlib: %w", err)
		}
		inflated, err := io.ReadAll(io.LimitReader(reader, maxPacket+1))
		_ = reader.Close()
		if err != nil {
			return Packet{}, fmt.Errorf("amule: zlib: %w", err)
		}
		if len(inflated) > maxPacket {
			return Packet{}, errors.New("amule: inflated packet is too large")
		}
		payload = inflated
	}
	d := &decoder{data: payload, utf8: flags&flagUTF8Numbers != 0}
	op, err := d.number(1)
	if err != nil {
		return Packet{}, err
	}
	tags, err := d.children(0)
	if err != nil {
		return Packet{}, err
	}
	return Packet{Op: uint8(op), Tags: tags}, nil
}

type decoder struct {
	data []byte
	pos  int
	utf8 bool
}

// maxDepth bounds tag nesting; the core never goes beyond a handful levels.
const maxDepth = 32

func (d *decoder) bytes(n int) ([]byte, error) {
	if n < 0 || d.pos+n > len(d.data) {
		return nil, io.ErrUnexpectedEOF
	}
	out := d.data[d.pos : d.pos+n]
	d.pos += n
	return out, nil
}

func (d *decoder) number(size int) (uint64, error) {
	if d.utf8 {
		value, n := decodeVarint(d.data[d.pos:])
		if n == 0 {
			return 0, errors.New("amule: invalid UTF-8 number")
		}
		d.pos += n
		return value, nil
	}
	raw, err := d.bytes(size)
	if err != nil {
		return 0, err
	}
	switch size {
	case 1:
		return uint64(raw[0]), nil
	case 2:
		return uint64(binary.BigEndian.Uint16(raw)), nil
	default:
		return uint64(binary.BigEndian.Uint32(raw)), nil
	}
}

func (d *decoder) children(depth int) ([]Tag, error) {
	if depth > maxDepth {
		return nil, errors.New("amule: tags nested too deeply")
	}
	count, err := d.number(2)
	if err != nil {
		return nil, err
	}
	tags := make([]Tag, 0, min(int(count), 1024))
	for i := uint64(0); i < count; i++ {
		tag, err := d.tag(depth)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (d *decoder) tag(depth int) (Tag, error) {
	rawName, err := d.number(2)
	if err != nil {
		return Tag{}, err
	}
	kind, err := d.number(1)
	if err != nil {
		return Tag{}, err
	}
	length, err := d.number(4)
	if err != nil {
		return Tag{}, err
	}
	tag := Tag{Name: uint16(rawName >> 1), Type: uint8(kind)}
	if rawName&1 != 0 {
		if tag.Children, err = d.children(depth + 1); err != nil {
			return Tag{}, err
		}
	}
	childLen := uint64(tag.wireLen())
	if childLen > length {
		return Tag{}, errors.New("amule: tag length shorter than its children")
	}
	data, err := d.bytes(int(length - childLen))
	if err != nil {
		return Tag{}, err
	}
	if len(data) > 0 {
		tag.Data = append([]byte(nil), data...)
	}
	return tag, nil
}

// decodeVarint reads the core's "UTF-8 number": the original (up to 6 byte)
// UTF-8 scheme applied to a 32-bit value. It returns the byte count, 0 on
// error.
func decodeVarint(data []byte) (uint64, int) {
	if len(data) == 0 {
		return 0, 0
	}
	first := data[0]
	var size int
	var value uint64
	switch {
	case first < 0x80:
		return uint64(first), 1
	case first&0xE0 == 0xC0:
		size, value = 2, uint64(first&0x1F)
	case first&0xF0 == 0xE0:
		size, value = 3, uint64(first&0x0F)
	case first&0xF8 == 0xF0:
		size, value = 4, uint64(first&0x07)
	case first&0xFC == 0xF8:
		size, value = 5, uint64(first&0x03)
	case first&0xFE == 0xFC:
		size, value = 6, uint64(first&0x01)
	default:
		return 0, 0
	}
	if len(data) < size {
		return 0, 0
	}
	for _, b := range data[1:size] {
		if b&0xC0 != 0x80 {
			return 0, 0
		}
		value = value<<6 | uint64(b&0x3F)
	}
	return value, size
}

// encodeVarint is the inverse of decodeVarint, used by the tests to build
// UTF-8-number packets like the core does.
func encodeVarint(value uint32) []byte {
	if value < utf8.RuneSelf {
		return []byte{byte(value)}
	}
	limits := []struct {
		max    uint32
		prefix byte
	}{{0x7FF, 0xC0}, {0xFFFF, 0xE0}, {0x1FFFFF, 0xF0}, {0x3FFFFFF, 0xF8}, {0x7FFFFFFF, 0xFC}}
	for index, limit := range limits {
		if value > limit.max {
			continue
		}
		size := index + 2
		out := make([]byte, size)
		for i := size - 1; i > 0; i-- {
			out[i] = 0x80 | byte(value&0x3F)
			value >>= 6
		}
		out[0] = limit.prefix | byte(value)
		return out
	}
	return nil
}
