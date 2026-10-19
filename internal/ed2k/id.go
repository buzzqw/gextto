package ed2k

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/md4"
)

// ID is a 128-bit Kad node/contact/file identifier. Byte order is the wire
// order (big endian), so XOR and lexicographic comparison implement the Kad
// distance metric directly.
type ID [16]byte

// ParseID decodes a 32-character hexadecimal hash into an ID.
func ParseID(value string) (ID, error) {
	var id ID
	value = strings.TrimSpace(value)
	if !ValidHash(value) {
		return id, ErrBadEd2kHash
	}
	raw, err := hex.DecodeString(value)
	if err != nil {
		return id, ErrBadEd2kHash
	}
	copy(id[:], raw)
	return id, nil
}

// Hex renders the ID as lowercase hexadecimal.
func (id ID) Hex() string { return hex.EncodeToString(id[:]) }

// IsZero reports whether the ID is all zeroes.
func (id ID) IsZero() bool { return id == ID{} }

// Distance returns target XOR other.
func Distance(target, other ID) ID {
	var out ID
	for i := range target {
		out[i] = target[i] ^ other[i]
	}
	return out
}

// Less compares two distances as unsigned 128-bit big-endian integers.
func Less(a, b ID) bool { return bytes.Compare(a[:], b[:]) < 0 }

// KeywordID is the Kad lookup target of a keyword: MD4 of its UTF-8 bytes,
// lowercased. This is what eMule and aMule use (`KadGetKeywordHash`), so a
// keyword has the same ID on every client.
func KeywordID(keyword string) ID {
	hash := md4.New()
	_, _ = hash.Write([]byte(strings.ToLower(keyword)))
	var id ID
	copy(id[:], hash.Sum(nil))
	return id
}

// FileID is the file identity: MD4 of the content, exactly the hash in an
// ed2k link. It is also the Kad target for source and notes lookups.
func FileID(hash string) (ID, error) { return ParseID(hash) }

// RandomID returns a cryptographically random node ID.
func RandomID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return id, fmt.Errorf("ed2k: random node id: %w", err)
	}
	return id, nil
}
