// Package partfile contains the storage primitives of the eD2k transfer plane:
// the 9,728,000-byte part layout and the MD4 hashes the network identifies a
// file by. It is deliberately not wired into the daemon while the Phase 0 gate
// is open; the design is in docs/ed2k-transfer.md.
package partfile

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/md4"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// PartSize is the eMule PARTSIZE: the unit of the part hash list.
const PartSize int64 = 9_728_000

// Hashes is the identity of a file: the whole-file MD4 (the ed2k link hash)
// and the MD4 of every part.
type Hashes struct {
	File  ed2k.ID
	Parts []ed2k.ID
}

// PartCount is the number of parts a file of size bytes has.
func PartCount(size int64) int {
	if size <= 0 {
		return 0
	}
	return int((size + PartSize - 1) / PartSize)
}

// PartLength is the byte length of one part; the last part may be shorter.
func PartLength(size int64, index int) int64 {
	if index < 0 || int64(index) >= int64(PartCount(size)) {
		return 0
	}
	start := int64(index) * PartSize
	if remaining := size - start; remaining < PartSize {
		return remaining
	}
	return PartSize
}

// HashReader consumes exactly size bytes and returns the whole-file and
// per-part MD4 hashes. It reads in bounded chunks, so a multi-gigabyte file
// does not need to fit in memory.
func HashReader(reader io.Reader, size int64) (Hashes, error) {
	return hashReader(reader, size, PartSize)
}

func hashReader(reader io.Reader, size, partSize int64) (Hashes, error) {
	if size < 0 || partSize <= 0 {
		return Hashes{}, errors.New("partfile: invalid size")
	}
	whole := md4.New()
	hashes := Hashes{Parts: make([]ed2k.ID, 0, partCount(size, partSize))}
	if size == 0 {
		copy(hashes.File[:], whole.Sum(nil))
		return hashes, nil
	}
	part := md4.New()
	buffer := make([]byte, 1<<20)
	var read, inPart int64
	for read < size {
		limit := int64(len(buffer))
		if remaining := size - read; remaining < limit {
			limit = remaining
		}
		if remaining := partSize - inPart; remaining < limit {
			limit = remaining
		}
		if _, err := io.ReadFull(reader, buffer[:limit]); err != nil {
			return Hashes{}, fmt.Errorf("partfile: read at %d: %w", read, err)
		}
		chunk := buffer[:limit]
		_, _ = whole.Write(chunk)
		_, _ = part.Write(chunk)
		read += limit
		inPart += limit
		if inPart == partSize {
			var hash ed2k.ID
			copy(hash[:], part.Sum(nil))
			hashes.Parts = append(hashes.Parts, hash)
			part.Reset()
			inPart = 0
		}
	}
	if inPart > 0 {
		var hash ed2k.ID
		copy(hash[:], part.Sum(nil))
		hashes.Parts = append(hashes.Parts, hash)
	}
	copy(hashes.File[:], whole.Sum(nil))
	return hashes, nil
}

func partCount(size, partSize int64) int {
	if size <= 0 {
		return 0
	}
	return int((size + partSize - 1) / partSize)
}

// HashFile hashes a file from disk without loading it into memory.
func HashFile(path string) (Hashes, error) {
	file, err := os.Open(path)
	if err != nil {
		return Hashes{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Hashes{}, err
	}
	return HashReader(file, info.Size())
}
