package partfile

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/md4"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// PartFile is a .part plus its .part.met metadata. The part bitmap is not
// persisted by eMule; it is rebuilt by hashing the .part on Open, which is the
// same verification a resumed download needs.
type PartFile struct {
	PartPath string
	Meta     *Meta
	file     *os.File
	have     []bool
}

// Create writes the .part.met and creates a sparse .part of the declared size.
func Create(partPath string, meta *Meta) (*PartFile, error) {
	if meta == nil {
		return nil, errors.New("partfile: nil metadata")
	}
	if meta.Size < 0 {
		return nil, errors.New("partfile: negative size")
	}
	raw, err := meta.Bytes()
	if err != nil {
		return nil, err
	}
	tmp := partPath + ".met.tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, partPath+".met"); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	if err := file.Truncate(meta.Size); err != nil {
		file.Close()
		return nil, err
	}
	return &PartFile{PartPath: partPath, Meta: meta, file: file, have: make([]bool, len(meta.Parts))}, nil
}

// Open loads an existing .part/.part.met pair and rebuilds the part bitmap by
// hashing every part. A missing or shorter .part is extended with zeroes; a
// part whose hash does not match stays missing.
func Open(partPath string) (*PartFile, error) {
	raw, err := os.ReadFile(partPath + ".met")
	if err != nil {
		return nil, err
	}
	meta, err := ParseMeta(raw)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if info.Size() < meta.Size {
		if err := file.Truncate(meta.Size); err != nil {
			file.Close()
			return nil, err
		}
	}
	p := &PartFile{PartPath: partPath, Meta: meta, file: file, have: make([]bool, len(meta.Parts))}
	for index := range meta.Parts {
		length := PartLength(meta.Size, index)
		if length == 0 {
			continue
		}
		data := make([]byte, length)
		if _, err := file.ReadAt(data, int64(index)*PartSize); err != nil && err != io.EOF {
			file.Close()
			return nil, err
		}
		var hash ed2k.ID
		copy(hash[:], md4Sum(data))
		p.have[index] = hash == meta.Parts[index]
	}
	return p, nil
}

// WritePart verifies the MD4 of data against the metadata and writes it at the
// part offset. A mismatching part is rejected and nothing is written.
func (p *PartFile) WritePart(index int, data []byte) error {
	if index < 0 || index >= len(p.Meta.Parts) {
		return fmt.Errorf("partfile: part %d out of range", index)
	}
	if int64(len(data)) != PartLength(p.Meta.Size, index) {
		return fmt.Errorf("partfile: part %d has %d bytes, want %d", index, len(data), PartLength(p.Meta.Size, index))
	}
	var hash ed2k.ID
	copy(hash[:], md4Sum(data))
	if hash != p.Meta.Parts[index] {
		return fmt.Errorf("partfile: part %d hash mismatch", index)
	}
	if _, err := p.file.WriteAt(data, int64(index)*PartSize); err != nil {
		return err
	}
	p.have[index] = true
	return nil
}

// ReadPart returns the bytes of one part, padded to its declared length.
func (p *PartFile) ReadPart(index int) ([]byte, error) {
	if index < 0 || index >= len(p.Meta.Parts) {
		return nil, fmt.Errorf("partfile: part %d out of range", index)
	}
	length := PartLength(p.Meta.Size, index)
	data := make([]byte, length)
	if _, err := p.file.ReadAt(data, int64(index)*PartSize); err != nil && err != io.EOF {
		return nil, err
	}
	return data, nil
}

func (p *PartFile) Have(index int) bool {
	return index >= 0 && index < len(p.have) && p.have[index]
}

func (p *PartFile) Complete() bool {
	for _, have := range p.have {
		if !have {
			return false
		}
	}
	return true
}

// Verify re-hashes the .part and compares the whole-file and part hashes with
// the metadata. It is the gate before a file can leave .part and be imported.
func (p *PartFile) Verify() (bool, error) {
	hashes, err := HashFile(p.PartPath)
	if err != nil {
		return false, err
	}
	if hashes.File != p.Meta.File || len(hashes.Parts) != len(p.Meta.Parts) {
		return false, nil
	}
	for index := range hashes.Parts {
		if hashes.Parts[index] != p.Meta.Parts[index] {
			return false, nil
		}
	}
	return true, nil
}

func (p *PartFile) Close() error {
	if p.file == nil {
		return nil
	}
	return p.file.Close()
}

func md4Sum(data []byte) []byte {
	hash := md4.New()
	_, _ = hash.Write(data)
	return hash.Sum(nil)
}
