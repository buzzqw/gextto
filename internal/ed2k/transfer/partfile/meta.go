package partfile

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// .part.met versions. Version 0xE1 is the splitted format and is not handled
// here; 0xE2 marks a file with 64-bit tags.
const (
	MetaVersion      = 0xE0
	MetaVersionSplit = 0xE1
	MetaVersionLarge = 0xE2
)

// Meta is the non-splitted .part.met payload: the ed2k identity, the part hash
// list and the eMule tags (at least name and size).
type Meta struct {
	Version  byte
	Modified uint32
	File     ed2k.ID
	Parts    []ed2k.ID
	Tags     []ed2k.Tag
	Name     string
	Size     int64
}

// NewMeta builds the metadata eMule expects for a new download.
func NewMeta(file ed2k.ID, parts []ed2k.ID, name string, size int64, modified uint32) *Meta {
	version := byte(MetaVersion)
	sizeTag := ed2k.Tag{Name: ed2k.TagFileSize, Type: ed2k.TagUint32, Int: uint64(uint32(size))}
	if size > math.MaxUint32 {
		version = MetaVersionLarge
		sizeTag = ed2k.Tag{Name: ed2k.TagFileSize, Type: ed2k.TagUint64, Int: uint64(size)}
	}
	return &Meta{
		Version: version, Modified: modified, File: file, Parts: parts, Name: name, Size: size,
		Tags: []ed2k.Tag{
			{Name: ed2k.TagFileName, Type: ed2k.TagString, Str: name},
			sizeTag,
		},
	}
}

// ParseMeta decodes a non-splitted .part.met. The layout follows eMule:
// version (1), last modified (4), file hash (16), part count (2), part hashes
// (16 each), tag count (4), tags.
func ParseMeta(data []byte) (*Meta, error) {
	if len(data) < 1+4+16+2+4 {
		return nil, errors.New("partfile: .part.met too short")
	}
	version := data[0]
	if version == MetaVersionSplit {
		return nil, errors.New("partfile: splitted .part.met is not supported")
	}
	if version != MetaVersion && version != MetaVersionLarge {
		return nil, fmt.Errorf("partfile: unsupported .part.met version 0x%02x", version)
	}
	meta := &Meta{Version: version, Modified: binary.LittleEndian.Uint32(data[1:])}
	offset := 5
	copy(meta.File[:], data[offset:offset+16])
	offset += 16
	count := int(binary.LittleEndian.Uint16(data[offset:]))
	offset += 2
	if count < 0 || count > 1_000_000 || offset+count*16+4 > len(data) {
		return nil, errors.New("partfile: invalid part count")
	}
	meta.Parts = make([]ed2k.ID, 0, count)
	for i := 0; i < count; i++ {
		var part ed2k.ID
		copy(part[:], data[offset:offset+16])
		offset += 16
		meta.Parts = append(meta.Parts, part)
	}
	tagCount := int(binary.LittleEndian.Uint32(data[offset:]))
	offset += 4
	if tagCount < 0 || tagCount > 1_000_000 {
		return nil, errors.New("partfile: invalid tag count")
	}
	tags, _, err := ed2k.ReadTagList(data[offset:], tagCount, binary.LittleEndian)
	if err != nil {
		return nil, fmt.Errorf("partfile: read tags: %w", err)
	}
	meta.Tags = tags
	for _, tag := range tags {
		switch tag.Name {
		case ed2k.TagFileName:
			if tag.Type == ed2k.TagString {
				meta.Name = tag.Str
			}
		case ed2k.TagFileSize:
			switch tag.Type {
			case ed2k.TagUint64:
				meta.Size = int64(tag.Int)
			case ed2k.TagUint32:
				meta.Size = int64(uint32(tag.Int))
			}
		case ed2k.TagFileSizeHigh:
			if tag.Type == ed2k.TagUint32 || tag.Type == ed2k.TagUint64 {
				meta.Size |= int64(uint32(tag.Int)) << 32
			}
		}
	}
	return meta, nil
}

// Bytes encodes the metadata in the non-splitted .part.met layout.
func (m *Meta) Bytes() ([]byte, error) {
	if m == nil {
		return nil, errors.New("partfile: nil metadata")
	}
	if m.Version != MetaVersion && m.Version != MetaVersionLarge {
		return nil, fmt.Errorf("partfile: unsupported .part.met version 0x%02x", m.Version)
	}
	if len(m.Parts) > math.MaxUint16 {
		return nil, errors.New("partfile: too many parts")
	}
	out := make([]byte, 0, 1+4+16+2+len(m.Parts)*16+4+64)
	out = append(out, m.Version)
	var fixed [4]byte
	binary.LittleEndian.PutUint32(fixed[:], m.Modified)
	out = append(out, fixed[:]...)
	out = append(out, m.File[:]...)
	binary.LittleEndian.PutUint16(fixed[:2], uint16(len(m.Parts)))
	out = append(out, fixed[:2]...)
	for _, part := range m.Parts {
		out = append(out, part[:]...)
	}
	binary.LittleEndian.PutUint32(fixed[:], uint32(len(m.Tags)))
	out = append(out, fixed[:]...)
	out = append(out, ed2k.WriteTagList(m.Tags, binary.LittleEndian)...)
	return out, nil
}
