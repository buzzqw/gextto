package torrent

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// uuidEpochOffset bridges the RFC 4122 epoch (1582-10-15 UTC) and the Unix
// epoch in seconds. V1 timestamps count 100ns ticks since the former; a
// time.Duration cannot span 444 years (int64 nanoseconds overflow), so the
// minting below works in uint64 seconds instead.
const uuidEpochOffset = uint64(12219292800)

// newTorrentID mints a version-1 (timestamp + random) UUID for the internal
// torrent id. Uniqueness — not secrecy or ordering — is the requirement:
// ids key the session map and name the storage directory. The layout follows
// RFC 4122 (version nibble 1, variant bits 10) so UUID tooling keeps parsing
// them.
func newTorrentID() ([16]byte, error) {
	var id [16]byte
	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return id, err
	}
	now := time.Now()
	ticks := (uint64(now.Unix())+uuidEpochOffset)*10000000 + uint64(now.Nanosecond())/100
	binary.BigEndian.PutUint32(id[0:4], uint32(ticks))
	binary.BigEndian.PutUint16(id[4:6], uint16(ticks>>32))
	binary.BigEndian.PutUint16(id[6:8], uint16(ticks>>48)&0x0fff|0x1000) // version 1
	// 14-bit clock sequence, random per id; variant 10xx.
	binary.BigEndian.PutUint16(id[8:10], binary.BigEndian.Uint16(rnd[0:2])&0x3fff|0x8000)
	// 48-bit node, random with the multicast bit set (no MAC address used).
	copy(id[10:], rnd[2:])
	id[10] |= 0x01
	return id, nil
}
