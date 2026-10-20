package partfile

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// AICHNode is one node of the SHA-1 tree eMule uses for AICH. A node is
// either a data block (Size <= Base) or the hash of its two children.
type AICHNode struct {
	Hash       AICHRoot
	Valid      bool
	Size       int64
	Base       int64
	LeftBranch bool
	Left       *AICHNode
	Right      *AICHNode
}

// AICHTree is an AICH hash tree with a known master hash at the root.
type AICHTree struct {
	Root *AICHNode
	Size int64
}

// NewAICHTree creates a tree shell whose root hash is already trusted. The
// lower hashes are filled by recovery data.
func NewAICHTree(size int64, root AICHRoot) (*AICHTree, error) {
	if size <= 0 {
		return nil, errors.New("partfile: invalid AICH tree size")
	}
	base := AICHPartSize
	if size <= AICHPartSize {
		base = AICHBlockSize
	}
	node := &AICHNode{Size: size, Base: base, LeftBranch: true, Hash: root, Valid: true}
	return &AICHTree{Root: node, Size: size}, nil
}

// BuildAICHTreeFile hashes a file into a complete AICH tree.
func BuildAICHTreeFile(path string) (*AICHTree, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return BuildAICHTreeReader(file, info.Size())
}

// BuildAICHTreeReader hashes a reader into a complete AICH tree. It reads
// each block once and keeps only the hashes in memory.
func BuildAICHTreeReader(reader io.ReaderAt, size int64) (*AICHTree, error) {
	if size <= 0 {
		return nil, errors.New("partfile: invalid AICH tree size")
	}
	base := AICHPartSize
	if size <= AICHPartSize {
		base = AICHBlockSize
	}
	node, err := buildAICHNode(reader, 0, size, base, true)
	if err != nil {
		return nil, err
	}
	return &AICHTree{Root: node, Size: size}, nil
}

func buildAICHNode(reader io.ReaderAt, offset, size, base int64, leftBranch bool) (*AICHNode, error) {
	node := &AICHNode{Size: size, Base: base, LeftBranch: leftBranch}
	if size <= base {
		data := make([]byte, size)
		if _, err := reader.ReadAt(data, offset); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		node.Hash = sha1.Sum(data)
		node.Valid = true
		return node, nil
	}
	leftSize, _, _ := aichSplit(size, base, leftBranch)
	left, err := buildAICHNode(reader, offset, leftSize, aichChildBase(leftSize), true)
	if err != nil {
		return nil, err
	}
	right, err := buildAICHNode(reader, offset+leftSize, size-leftSize, aichChildBase(size-leftSize), false)
	if err != nil {
		return nil, err
	}
	node.Left, node.Right = left, right
	node.Hash = aichCombine(left.Hash, right.Hash)
	node.Valid = true
	return node, nil
}

func aichChildBase(size int64) int64 {
	if size <= AICHPartSize {
		return AICHBlockSize
	}
	return AICHPartSize
}

func aichCombine(left, right AICHRoot) AICHRoot {
	var raw [40]byte
	copy(raw[:20], left[:])
	copy(raw[20:], right[:])
	return sha1.Sum(raw[:])
}

func aichSplit(size, base int64, leftBranch bool) (left, right, leftBlocks int64) {
	blocks := (size + base - 1) / base
	leftBlocks = blocks / 2
	if leftBranch {
		leftBlocks = (blocks + 1) / 2
	}
	left = leftBlocks * base
	right = size - left
	return
}

func (n *AICHNode) ensureChildren() {
	if n.Left != nil || n.Size <= n.Base {
		return
	}
	leftSize, rightSize, _ := aichSplit(n.Size, n.Base, n.LeftBranch)
	n.Left = &AICHNode{Size: leftSize, Base: aichChildBase(leftSize), LeftBranch: true}
	n.Right = &AICHNode{Size: rightSize, Base: aichChildBase(rightSize), LeftBranch: false}
}

// find returns the node covering [start,start+size) relative to n.
func (n *AICHNode) find(start, size int64) (*AICHNode, error) {
	if start < 0 || size <= 0 || start+size > n.Size {
		return nil, fmt.Errorf("partfile: AICH range %d+%d outside %d", start, size, n.Size)
	}
	if start == 0 && size == n.Size {
		return n, nil
	}
	if n.Size <= n.Base {
		return nil, errors.New("partfile: AICH range below a leaf")
	}
	n.ensureChildren()
	leftSize, _, _ := aichSplit(n.Size, n.Base, n.LeftBranch)
	if start < leftSize {
		return n.Left.find(start, size)
	}
	return n.Right.find(start-leftSize, size)
}

// setHash inserts a hash identified by the eMule path identifier. The highest
// set bit is the sentinel; the bits below it walk the tree (1 left, 0 right).
func (n *AICHNode) setHash(ident uint32, hash AICHRoot) bool {
	high := -1
	for bit := 31; bit >= 0; bit-- {
		if ident&(uint32(1)<<uint(bit)) != 0 {
			high = bit
			break
		}
	}
	if high <= 0 {
		return false
	}
	node := n
	for bit := high - 1; bit >= 0; bit-- {
		if node.Size <= node.Base {
			return false
		}
		node.ensureChildren()
		if ident&(uint32(1)<<uint(bit)) != 0 {
			node = node.Left
		} else {
			node = node.Right
		}
	}
	if node.Valid && node.Hash != hash {
		return false
	}
	node.Hash = hash
	node.Valid = true
	return true
}

// verify recalculates every internal hash from its children and checks the
// known master hashes on the way up.
func (n *AICHNode) verify() bool {
	if n.Size <= n.Base {
		return n.Valid
	}
	if n.Left == nil || n.Right == nil {
		// A sibling hash from recovery data can be a complete trusted
		// subtree; its children are not needed outside the target part.
		return n.Valid && n.Left == nil && n.Right == nil
	}
	if !n.Left.verify() || !n.Right.verify() {
		return false
	}
	hash := aichCombine(n.Left.Hash, n.Right.Hash)
	if n.Valid && n.Hash != hash {
		return false
	}
	n.Hash = hash
	n.Valid = true
	return true
}

// PartHashes returns the AICH hash of every 180 KiB block in a part.
func (t *AICHTree) PartHashes(part int) ([][20]byte, error) {
	if part < 0 || int64(part)*AICHPartSize >= t.Size {
		return nil, fmt.Errorf("partfile: AICH part %d out of range", part)
	}
	start := int64(part) * AICHPartSize
	length := AICHPartSize
	if remaining := t.Size - start; remaining < length {
		length = remaining
	}
	count := (length + AICHBlockSize - 1) / AICHBlockSize
	out := make([][20]byte, 0, count)
	for offset := int64(0); offset < length; offset += AICHBlockSize {
		size := AICHBlockSize
		if remaining := length - offset; remaining < size {
			size = remaining
		}
		node, err := t.Root.find(start+offset, size)
		if err != nil || !node.Valid {
			return nil, errors.New("partfile: AICH part hashes are incomplete")
		}
		out = append(out, [20]byte(node.Hash))
	}
	return out, nil
}

// RecoveryData writes the eMule V2 recovery packet for one part. It contains
// the sibling hashes from the root down to the part and all part block hashes.
func (t *AICHTree) RecoveryData(part int) ([]byte, error) {
	if part < 0 || int64(part)*AICHPartSize >= t.Size {
		return nil, fmt.Errorf("partfile: AICH part %d out of range", part)
	}
	start := int64(part) * AICHPartSize
	length := AICHPartSize
	if remaining := t.Size - start; remaining < length {
		length = remaining
	}
	entries, err := t.recoveryEntries(start, length)
	if err != nil {
		return nil, err
	}
	large := t.Size > 0xFFFFFFFF
	var out []byte
	if large {
		out = append(out, 0, 0) // no 16-bit entries
		var count [2]byte
		binary.LittleEndian.PutUint16(count[:], uint16(len(entries)))
		out = append(out, count[:]...)
		for _, entry := range entries {
			var ident [4]byte
			binary.LittleEndian.PutUint32(ident[:], entry.ident)
			out = append(out, ident[:]...)
			out = append(out, entry.hash[:]...)
		}
		return out, nil
	}
	var count [2]byte
	binary.LittleEndian.PutUint16(count[:], uint16(len(entries)))
	out = append(out, count[:]...)
	for _, entry := range entries {
		if entry.ident > 0xFFFF {
			return nil, errors.New("partfile: AICH identifier overflows uint16")
		}
		var ident [2]byte
		binary.LittleEndian.PutUint16(ident[:], uint16(entry.ident))
		out = append(out, ident[:]...)
		out = append(out, entry.hash[:]...)
	}
	out = append(out, 0, 0) // no 32-bit entries
	return out, nil
}

type aichEntry struct {
	ident uint32
	hash  AICHRoot
}

func (t *AICHTree) recoveryEntries(start, size int64) ([]aichEntry, error) {
	var entries []aichEntry
	if err := t.Root.recoveryEntries(start, size, 0, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (n *AICHNode) recoveryEntries(start, size int64, ident uint32, out *[]aichEntry) error {
	if start == 0 && size == n.Size {
		return n.writeLowestLevel(ident, out)
	}
	if n.Size <= n.Base || n.Left == nil || n.Right == nil {
		return errors.New("partfile: AICH recovery tree is incomplete")
	}
	ident <<= 1
	if n.LeftBranch {
		ident |= 1
	}
	leftSize, _, _ := aichSplit(n.Size, n.Base, n.LeftBranch)
	if start < leftSize {
		if !n.Right.Valid {
			return errors.New("partfile: AICH right sibling hash is missing")
		}
		*out = append(*out, aichEntry{ident: siblingIdent(ident, false), hash: n.Right.Hash})
		return n.Left.recoveryEntries(start, size, ident, out)
	}
	if !n.Left.Valid {
		return errors.New("partfile: AICH left sibling hash is missing")
	}
	*out = append(*out, aichEntry{ident: siblingIdent(ident, true), hash: n.Left.Hash})
	return n.Right.recoveryEntries(start-leftSize, size, ident, out)
}

func siblingIdent(parent uint32, left bool) uint32 {
	ident := parent << 1
	if left {
		ident |= 1
	}
	return ident
}

func (n *AICHNode) writeLowestLevel(ident uint32, out *[]aichEntry) error {
	ident = siblingIdent(ident, n.LeftBranch)
	if n.Size <= n.Base {
		if !n.Valid {
			return errors.New("partfile: AICH leaf hash is missing")
		}
		*out = append(*out, aichEntry{ident: ident, hash: n.Hash})
		return nil
	}
	if n.Left == nil || n.Right == nil {
		return errors.New("partfile: AICH recovery subtree is incomplete")
	}
	if err := n.Left.writeLowestLevel(ident, out); err != nil {
		return err
	}
	return n.Right.writeLowestLevel(ident, out)
}

// ReadRecoveryData reads a V2 AICH recovery packet for a part and verifies it
// against the trusted master hash.
func (t *AICHTree) ReadRecoveryData(part int, payload []byte) error {
	if part < 0 || int64(part)*AICHPartSize >= t.Size {
		return fmt.Errorf("partfile: AICH part %d out of range", part)
	}
	reader := &aichPayloadReader{data: payload}
	count16, err := reader.u16()
	if err != nil {
		return err
	}
	count := int(count16)
	wide := false
	if count == 0 {
		count32, err := reader.u16()
		if err != nil {
			return err
		}
		count = int(count32)
		wide = true
	}
	if count <= 0 || count > 1<<16 {
		return errors.New("partfile: invalid AICH recovery count")
	}
	for i := 0; i < count; i++ {
		var ident uint32
		if wide {
			value, err := reader.u32()
			if err != nil {
				return err
			}
			ident = value
		} else {
			value, err := reader.u16()
			if err != nil {
				return err
			}
			ident = uint32(value)
		}
		hash, err := reader.hash()
		if err != nil {
			return err
		}
		if !t.Root.setHash(ident, hash) {
			return errors.New("partfile: invalid AICH recovery identifier")
		}
	}
	if !wide {
		// A trailing zero count is the 32-bit section marker. It is allowed
		// to be absent in old packets; an all-zero 16-bit count above already
		// selected the 32-bit form.
		if reader.remaining() >= 2 {
			if _, err := reader.u16(); err != nil {
				return err
			}
		}
	}
	if !t.Root.verify() {
		return errors.New("partfile: AICH recovery tree does not verify")
	}
	start := int64(part) * AICHPartSize
	length := AICHPartSize
	if remaining := t.Size - start; remaining < length {
		length = remaining
	}
	for offset := int64(0); offset < length; offset += AICHBlockSize {
		size := AICHBlockSize
		if remaining := length - offset; remaining < size {
			size = remaining
		}
		node, err := t.Root.find(start+offset, size)
		if err != nil || !node.Valid {
			return errors.New("partfile: AICH recovery data is incomplete")
		}
	}
	return nil
}

func (t *AICHTree) RootHash() AICHRoot {
	if t == nil || t.Root == nil {
		return AICHRoot{}
	}
	return t.Root.Hash
}

type aichPayloadReader struct {
	data []byte
	pos  int
}

func (r *aichPayloadReader) remaining() int { return len(r.data) - r.pos }

func (r *aichPayloadReader) u16() (uint16, error) {
	if r.remaining() < 2 {
		return 0, io.ErrUnexpectedEOF
	}
	value := binary.LittleEndian.Uint16(r.data[r.pos:])
	r.pos += 2
	return value, nil
}

func (r *aichPayloadReader) u32() (uint32, error) {
	if r.remaining() < 4 {
		return 0, io.ErrUnexpectedEOF
	}
	value := binary.LittleEndian.Uint32(r.data[r.pos:])
	r.pos += 4
	return value, nil
}

func (r *aichPayloadReader) hash() (AICHRoot, error) {
	var hash AICHRoot
	if r.remaining() < 20 {
		return hash, io.ErrUnexpectedEOF
	}
	copy(hash[:], r.data[r.pos:r.pos+20])
	r.pos += 20
	return hash, nil
}
