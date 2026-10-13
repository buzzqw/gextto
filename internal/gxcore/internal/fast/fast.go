// Package fast implements the BEP 6 fast extension peer set.
// See https://www.bittorrent.org/beps/bep_0006.html
package fast

import (
	"crypto/sha1"
	"encoding/binary"
	"net"
	"slices"
)

// GenerateFastSet picks the k allowed-fast piece indexes for a peer.
// The set is deterministic per (masked IP, info hash) so both sides agree
// without exchanging it. The /24 mask keeps it stable across reconnects
// from the same network while the SHA-1 chain spreads picks over the piece
// space. IPv6 and non-IPv4 peers get no fast set.
func GenerateFastSet(k int, numPieces uint32, infoHash [20]byte, ip net.IP) []uint32 {
	v4 := ip.To4()
	if v4 == nil {
		return nil
	}
	// Hash input: masked IP followed by the info hash. Note the input
	// buffer is reused across rounds: Sum(input[:0]) shortens it to the
	// 20-byte digest, so round 0 hashes 24 bytes and later rounds hash 20.
	// This byte-exact behavior is part of the BEP 6 interop contract.
	input := make([]byte, 24)
	copy(input[:4], v4.Mask(net.CIDRMask(24, 32)))
	copy(input[4:], infoHash[:])

	picks := make([]uint32, 0, k)
	digest := sha1.New()
	// At most k hash rounds: with tiny torrents the set cannot fill up
	// (every candidate collides), and the loop must still terminate.
	for round := 0; round < k && len(picks) < k; round++ {
		_, _ = digest.Write(input)
		block := digest.Sum(input[:0])
		input = block
		digest.Reset()
		for i := 0; i < len(block) && len(picks) < k; i += 4 {
			if index := binary.BigEndian.Uint32(block[i:i+4]) % numPieces; !slices.Contains(picks, index) {
				picks = append(picks, index)
			}
		}
	}
	return picks
}
