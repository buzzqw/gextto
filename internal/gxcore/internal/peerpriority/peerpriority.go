// Package peerpriority orders connection candidates per BEP 40: peers are
// ranked by a CRC32C of the address pair, masked so that close addresses
// (same /16, /24) spread across the hash space instead of clustering.
package peerpriority

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"net"
)

// Priority ranks dial candidates; higher pops first.
type Priority = uint32

var crcTable = crc32.MakeTable(crc32.Castagnoli)

// Calculate ranks candidate a against our own address b.
func Calculate(a, b *net.TCPAddr) Priority {
	first, second := priorityInputs(a, b)
	digest := crc32.New(crcTable)
	if bytes.Compare(first, second) < 0 {
		_, _ = digest.Write(first)
		_, _ = digest.Write(second)
	} else {
		_, _ = digest.Write(second)
		_, _ = digest.Write(first)
	}
	return digest.Sum32()
}

// priorityInputs derives the two hashed blobs: ports for identical IPs,
// masked network parts for IPv4 pairs, full address plus port otherwise
// (BEP 40 only defines IPv4; the fallback keeps distinct peers distinct).
func priorityInputs(a, b *net.TCPAddr) (first, second []byte) {
	if a.IP.Equal(b.IP) {
		var ports [4]byte
		binary.BigEndian.PutUint16(ports[0:2], port16(a.Port))
		binary.BigEndian.PutUint16(ports[2:4], port16(b.Port))
		return ports[0:2], ports[2:4]
	}
	if a4, b4 := a.IP.To4(), b.IP.To4(); a4 != nil && b4 != nil {
		mask := subnetMask(a4, b4)
		return a4.Mask(mask), b4.Mask(mask)
	}
	fullA := append(append([]byte{}, a.IP...), byte(a.Port>>8), byte(a.Port))
	fullB := append(append([]byte{}, b.IP...), byte(b.Port>>8), byte(b.Port))
	return fullA, fullB
}

// port16 converts a TCP port to its 16-bit wire value. A net.TCPAddr.Port is
// always in range, but the value can originate from a parsed string; clamping
// keeps the conversion total and explicit instead of truncating silently.
func port16(p int) uint16 {
	switch {
	case p < 0:
		return 0
	case p > 0xffff:
		return 0xffff
	default:
		return uint16(p)
	}
}

// subnetMask picks the BEP 40 mask by closeness: /16 strangers share only
// half the third byte, /24 neighbors the full prefix.
func subnetMask(a, b net.IP) net.IPMask {
	if !sameSubnet(16, 32, a, b) {
		return net.IPv4Mask(0xff, 0xff, 0x55, 0x55)
	}
	if !sameSubnet(24, 32, a, b) {
		return net.IPv4Mask(0xff, 0xff, 0xff, 0x55)
	}
	return net.IPv4Mask(0xff, 0xff, 0xff, 0xff)
}

func sameSubnet(ones, bits int, a, b net.IP) bool {
	mask := net.CIDRMask(ones, bits)
	return a.Mask(mask).Equal(b.Mask(mask))
}
