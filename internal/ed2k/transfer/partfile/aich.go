package partfile

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
)

// AICH hashes the file as a tree of SHA-1 hashes. eMule uses 180 KiB blocks
// inside a 9,728,000-byte part and combines part roots at the top.
const (
	AICHBlockSize int64 = 184_320
	AICHPartSize  int64 = PartSize
)

// AICHRoot is the AICH hash of a file.
type AICHRoot [20]byte

// AICHRootReader consumes exactly size bytes and returns the AICH root. It
// reads one part at a time, so memory stays bounded by AICHPartSize.
func AICHRootReader(reader io.Reader, size int64) (AICHRoot, error) {
	return aichRootReader(reader, size, AICHPartSize, AICHBlockSize)
}

func aichRootReader(reader io.Reader, size, partSize, blockSize int64) (AICHRoot, error) {
	if size < 0 || partSize <= 0 || blockSize <= 0 {
		return AICHRoot{}, errors.New("partfile: invalid AICH size")
	}
	if size == 0 {
		return AICHRoot{}, errors.New("partfile: AICH of an empty file")
	}
	parts := (size + partSize - 1) / partSize
	hashes := make([][20]byte, 0, parts)
	buffer := make([]byte, partSize)
	for index := int64(0); index < parts; index++ {
		length := partSize
		if remaining := size - index*partSize; remaining < length {
			length = remaining
		}
		part := buffer[:length]
		if _, err := io.ReadFull(reader, part); err != nil {
			return AICHRoot{}, fmt.Errorf("partfile: AICH read at part %d: %w", index, err)
		}
		hashes = append(hashes, aichPartHash(part, blockSize))
	}
	if len(hashes) == 1 {
		return AICHRoot(hashes[0]), nil
	}
	return AICHRoot(aichTreeHash(hashes, size, partSize, true)), nil
}

// AICHRootFile hashes a file from disk.
func AICHRootFile(path string) (AICHRoot, error) {
	file, err := os.Open(path)
	if err != nil {
		return AICHRoot{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return AICHRoot{}, err
	}
	return AICHRootReader(file, info.Size())
}

func aichPartHash(data []byte, blockSize int64) [20]byte {
	if blockSize <= 0 {
		blockSize = AICHBlockSize
	}
	count := (int64(len(data)) + blockSize - 1) / blockSize
	leaves := make([][20]byte, 0, count)
	for offset := int64(0); offset < int64(len(data)); offset += blockSize {
		end := offset + blockSize
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		leaves = append(leaves, sha1.Sum(data[offset:end]))
	}
	if len(leaves) == 1 {
		return leaves[0]
	}
	return aichTreeHash(leaves, int64(len(data)), blockSize, true)
}

// aichTreeHash builds the left-biased tree eMule uses: at each level the left
// subtree gets ceil(blocks/2) base-size units, the right subtree the rest.
func aichTreeHash(leaves [][20]byte, size, baseSize int64, leftBranch bool) [20]byte {
	if size <= baseSize {
		return leaves[0]
	}
	blocks := (size + baseSize - 1) / baseSize
	leftBlocks := blocks / 2
	if leftBranch {
		leftBlocks = (blocks + 1) / 2
	}
	leftSize := leftBlocks * baseSize
	rightSize := size - leftSize
	left := aichTreeHash(leaves[:leftBlocks], leftSize, baseSize, true)
	right := aichTreeHash(leaves[leftBlocks:], rightSize, baseSize, false)
	buffer := make([]byte, 0, 40)
	buffer = append(buffer, left[:]...)
	buffer = append(buffer, right[:]...)
	return sha1.Sum(buffer)
}
