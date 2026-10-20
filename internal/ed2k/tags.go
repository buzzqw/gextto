package ed2k

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Tag types shared by the eD2k and Kad protocols.
const (
	TagHash16  = 0x01
	TagString  = 0x02
	TagUint32  = 0x03
	TagFloat32 = 0x04
	TagBool    = 0x05
	TagBoolArr = 0x06
	TagBlob    = 0x07
	TagUint16  = 0x08
	TagUint8   = 0x09
	TagBsob    = 0x0A
	TagUint64  = 0x0B
	TagStr1    = 0x11
	TagStr16   = 0x20
)

// Numeric tag names used by Kad and the eD2k servers.
const (
	TagFileName       = 0x01
	TagFileSize       = 0x02
	TagFileType       = 0x03
	TagFileFormat     = 0x04
	TagSources        = 0x15
	TagPublishInfo    = 0x33
	TagSourceIP       = 0xFE
	TagSourcePort     = 0xFD
	TagSourceUDPPort  = 0xFC
	TagSourceType     = 0xFF
	TagEncryption     = 0xF3
	TagFileSizeHigh   = 0x3A
	TagCompleteSource = 0x30
	TagLastShared     = 0x34
	TagBuddyHash      = 0xF8
	TagServerPort     = 0xFA
	TagServerIP       = 0xFB
)

// Tag is one decoded key/value pair.
type Tag struct {
	Name  uint8
	Type  uint8
	Str   string
	Int   uint64
	Bytes []byte
}

// ErrTruncatedPacket is returned when a packet ends before its declared
// structure does. Network input is untrusted: every reader returns this
// instead of panicking.
var ErrTruncatedPacket = errors.New("ed2k: truncated packet")

type reader struct {
	data []byte
	pos  int
	bo   binary.ByteOrder
}

func newReader(data []byte, bo binary.ByteOrder) *reader {
	return &reader{data: data, bo: bo}
}

func (r *reader) remaining() int { return len(r.data) - r.pos }

func (r *reader) need(n int) error {
	if n < 0 || r.remaining() < n {
		return ErrTruncatedPacket
	}
	return nil
}

func (r *reader) u8() (uint8, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	value := r.data[r.pos]
	r.pos++
	return value, nil
}

func (r *reader) u16() (uint16, error) {
	if err := r.need(2); err != nil {
		return 0, err
	}
	value := r.bo.Uint16(r.data[r.pos:])
	r.pos += 2
	return value, nil
}

func (r *reader) u32() (uint32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	value := r.bo.Uint32(r.data[r.pos:])
	r.pos += 4
	return value, nil
}

func (r *reader) u64() (uint64, error) {
	if err := r.need(8); err != nil {
		return 0, err
	}
	value := r.bo.Uint64(r.data[r.pos:])
	r.pos += 8
	return value, nil
}

func (r *reader) n(count int) ([]byte, error) {
	if err := r.need(count); err != nil {
		return nil, err
	}
	value := r.data[r.pos : r.pos+count]
	r.pos += count
	return value, nil
}

func (r *reader) string16() (string, error) {
	size, err := r.u16()
	if err != nil {
		return "", err
	}
	raw, err := r.n(int(size))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (r *reader) id() (ID, error) {
	var id ID
	raw, err := r.n(16)
	if err != nil {
		return id, err
	}
	copy(id[:], raw)
	return id, nil
}

type writer struct {
	buf []byte
	bo  binary.ByteOrder
}

func newWriter(bo binary.ByteOrder) *writer { return &writer{bo: bo} }

func (w *writer) u8(v uint8) { w.buf = append(w.buf, v) }
func (w *writer) u16(v uint16) {
	var raw [2]byte
	w.bo.PutUint16(raw[:], v)
	w.buf = append(w.buf, raw[:]...)
}
func (w *writer) u32(v uint32) {
	var raw [4]byte
	w.bo.PutUint32(raw[:], v)
	w.buf = append(w.buf, raw[:]...)
}
func (w *writer) u64(v uint64) {
	var raw [8]byte
	w.bo.PutUint64(raw[:], v)
	w.buf = append(w.buf, raw[:]...)
}
func (w *writer) raw(v []byte) { w.buf = append(w.buf, v...) }
func (w *writer) id(v ID)      { w.buf = append(w.buf, v[:]...) }

// kadID reads a 128-bit Kad value (node ID, keyword target or file hash).
// eMule and aMule keep Kad values as four native-order 32-bit chunks, so the
// wire bytes of each chunk are reversed relative to the canonical big-endian
// hex form (the one used by ed2k links and by ID.Hex).
func (r *reader) kadID() (ID, error) {
	var id ID
	raw, err := r.n(16)
	if err != nil {
		return id, err
	}
	for i := 0; i < 16; i += 4 {
		id[i], id[i+1], id[i+2], id[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	return id, nil
}

// kadID writes a Kad 128-bit value in the chunk order used on the wire.
func (w *writer) kadID(v ID) {
	for i := 0; i < 16; i += 4 {
		w.buf = append(w.buf, v[i+3], v[i+2], v[i+1], v[i])
	}
}
func (w *writer) string16(v string) {
	w.u16(uint16(len(v)))
	w.buf = append(w.buf, v...)
}

// readTag decodes one tag. Both the Kad (big endian) and the eD2k server
// (little endian) layouts use the same tag encoding, so the byte order is the
// only difference.
func (r *reader) tag() (Tag, error) {
	rawType, err := r.u8()
	if err != nil {
		return Tag{}, err
	}
	tag := Tag{Type: rawType & 0x7F}
	if rawType&0x80 != 0 {
		name, err := r.u8()
		if err != nil {
			return Tag{}, err
		}
		tag.Name = name
	} else {
		nameLength, err := r.u16()
		if err != nil {
			return Tag{}, err
		}
		if nameLength == 1 {
			name, err := r.u8()
			if err != nil {
				return Tag{}, err
			}
			tag.Name = name
		} else {
			name, err := r.n(int(nameLength))
			if err != nil {
				return Tag{}, err
			}
			if len(name) > 0 {
				tag.Name = name[0]
			}
		}
	}

	switch tag.Type {
	case TagHash16:
		raw, err := r.n(16)
		if err != nil {
			return Tag{}, err
		}
		tag.Bytes = append([]byte(nil), raw...)
	case TagString:
		value, err := r.string16()
		if err != nil {
			return Tag{}, err
		}
		tag.Str = value
	case TagUint32:
		value, err := r.u32()
		if err != nil {
			return Tag{}, err
		}
		tag.Int = uint64(value)
	case TagUint64:
		value, err := r.u64()
		if err != nil {
			return Tag{}, err
		}
		tag.Int = value
	case TagUint16:
		value, err := r.u16()
		if err != nil {
			return Tag{}, err
		}
		tag.Int = uint64(value)
		tag.Type = TagUint32
	case TagUint8:
		value, err := r.u8()
		if err != nil {
			return Tag{}, err
		}
		tag.Int = uint64(value)
		tag.Type = TagUint32
	case TagFloat32:
		if _, err := r.n(4); err != nil {
			return Tag{}, err
		}
	case TagBool:
		if _, err := r.u8(); err != nil {
			return Tag{}, err
		}
	case TagBoolArr:
		size, err := r.u16()
		if err != nil {
			return Tag{}, err
		}
		if _, err := r.n(int(size/8) + 1); err != nil {
			return Tag{}, err
		}
	case TagBlob:
		size, err := r.u32()
		if err != nil {
			return Tag{}, err
		}
		if size > uint32(r.remaining()) {
			return Tag{}, ErrTruncatedPacket
		}
		raw, err := r.n(int(size))
		if err != nil {
			return Tag{}, err
		}
		tag.Bytes = append([]byte(nil), raw...)
	case TagBsob:
		size, err := r.u8()
		if err != nil {
			return Tag{}, err
		}
		raw, err := r.n(int(size))
		if err != nil {
			return Tag{}, err
		}
		tag.Bytes = append([]byte(nil), raw...)
	default:
		if tag.Type >= TagStr1 && tag.Type <= 0x22 {
			size := int(tag.Type-TagStr1) + 1
			raw, err := r.n(size)
			if err != nil {
				return Tag{}, err
			}
			tag.Str = string(raw)
			tag.Type = TagString
		} else {
			return Tag{}, fmt.Errorf("ed2k: unknown tag type 0x%02x", rawType)
		}
	}
	return tag, nil
}

// writeTag writes a numeric-name tag. Integer values use the smallest
// representation accepted by every client.
func (w *writer) tag(tag Tag) {
	kind := tag.Type
	if kind == 0 {
		kind = TagUint32
	}
	switch kind {
	case TagString:
		length := len(tag.Str)
		if length >= 1 && length <= 16 {
			kind = TagStr1 + uint8(length-1)
		}
	}
	w.u8(kind)
	w.u16(1)
	w.u8(tag.Name)
	switch kind {
	case TagString:
		w.string16(tag.Str)
	case TagUint64:
		w.u64(tag.Int)
	case TagUint32:
		w.u32(uint32(tag.Int))
	case TagUint16:
		w.u16(uint16(tag.Int))
	case TagUint8:
		w.u8(uint8(tag.Int))
	case TagHash16:
		w.raw(tag.Bytes)
	case TagBlob:
		w.u32(uint32(len(tag.Bytes)))
		w.raw(tag.Bytes)
	default:
		if kind >= TagStr1 && kind <= TagStr16 {
			w.raw([]byte(tag.Str))
		}
	}
}

// tagsByID indexes a decoded tag list.
func tagsByID(tags []Tag) map[uint8]Tag {
	out := make(map[uint8]Tag, len(tags))
	for _, tag := range tags {
		out[tag.Name] = tag
	}
	return out
}

func tagUint(tags map[uint8]Tag, name uint8) (uint64, bool) {
	tag, ok := tags[name]
	if !ok || tag.Type != TagUint32 && tag.Type != TagUint64 {
		return 0, false
	}
	return tag.Int, true
}

func tagString(tags map[uint8]Tag, name uint8) (string, bool) {
	tag, ok := tags[name]
	if !ok || tag.Type != TagString {
		return "", false
	}
	return tag.Str, true
}

// fileSizeFromTags reads TAG_FILESIZE (+ TAG_FILESIZE_HI for files above
// 4 GiB) as a 64-bit size.
func fileSizeFromTags(tags map[uint8]Tag) (int64, bool) {
	low, ok := tagUint(tags, TagFileSize)
	if !ok {
		return 0, false
	}
	high, hasHigh := tagUint(tags, TagFileSizeHigh)
	size := low
	if hasHigh {
		if high > math.MaxUint32 {
			return 0, false
		}
		size = (high << 32) | (low & 0xFFFFFFFF)
	}
	if size == 0 || size > math.MaxInt64 {
		return 0, false
	}
	return int64(size), true
}

// ReadTagList decodes count tags with the given byte order and returns the
// bytes consumed. It is the generic tag list used by search packets, server
// responses and .part.met files.
func ReadTagList(data []byte, count int, bo binary.ByteOrder) ([]Tag, int, error) {
	r := newReader(data, bo)
	tags := make([]Tag, 0, count)
	for i := 0; i < count; i++ {
		tag, err := r.tag()
		if err != nil {
			return nil, 0, err
		}
		tags = append(tags, tag)
	}
	return tags, r.pos, nil
}

// WriteTagList encodes tags with the given byte order.
func WriteTagList(tags []Tag, bo binary.ByteOrder) []byte {
	w := newWriter(bo)
	for _, tag := range tags {
		w.tag(tag)
	}
	return w.buf
}
