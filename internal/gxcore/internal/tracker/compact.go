package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
)

// CompactPeer is one IPv4 peer in the 6-byte compact form (4-byte address +
// 2-byte port) trackers and PEX exchange. Pointer-free, so it works as a map
// key.
type CompactPeer struct {
	IP   [net.IPv4len]byte
	Port uint16
}

// NewCompactPeer folds addr into compact form, clamping out-of-range ports
// to zero instead of wrapping them.
func NewCompactPeer(addr *net.TCPAddr) CompactPeer {
	p := CompactPeer{Port: clampedPort(addr.Port)}
	copy(p.IP[:], addr.IP.To4())
	return p
}

// clampedPort keeps a port inside the uint16 range.
func clampedPort(port int) uint16 {
	if port < 0 || port > 65535 {
		return 0
	}
	return uint16(port)
}

// Addr expands the compact form back to a dialable address.
func (p CompactPeer) Addr() *net.TCPAddr {
	return &net.TCPAddr{IP: p.IP[:], Port: int(p.Port)}
}

// MarshalBinary encodes the 6-byte wire form.
func (p CompactPeer) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 6))
	err := binary.Write(buf, binary.BigEndian, p)
	return buf.Bytes(), err
}

// UnmarshalBinary decodes exactly 6 bytes into the peer.
func (p *CompactPeer) UnmarshalBinary(data []byte) error {
	if len(data) != 6 {
		return errors.New("invalid compact peer length")
	}
	return binary.Read(bytes.NewReader(data), binary.BigEndian, p)
}

// DecodePeersCompact parses a concatenated compact peer list.
func DecodePeersCompact(b []byte) ([]*net.TCPAddr, error) {
	if len(b)%6 != 0 {
		return nil, errors.New("invalid peer list length")
	}
	addrs := make([]*net.TCPAddr, 0, len(b)/6)
	for i := 0; i < len(b); i += 6 {
		var peer CompactPeer
		if err := peer.UnmarshalBinary(b[i : i+6]); err != nil {
			return nil, err
		}
		addrs = append(addrs, peer.Addr())
	}
	return addrs, nil
}

// DecodePeersCompact6 parses a BEP 7 compact IPv6 list: 16-byte address
// followed by a 2-byte port, 18 bytes per peer.
func DecodePeersCompact6(b []byte) ([]*net.TCPAddr, error) {
	if len(b)%18 != 0 {
		return nil, errors.New("invalid ipv6 peer list length")
	}
	addrs := make([]*net.TCPAddr, 0, len(b)/18)
	for i := 0; i < len(b); i += 18 {
		ip := make(net.IP, net.IPv6len)
		copy(ip, b[i:i+16])
		port := binary.BigEndian.Uint16(b[i+16 : i+18])
		addrs = append(addrs, &net.TCPAddr{IP: ip, Port: int(port)})
	}
	return addrs, nil
}

// CompactPeer6 is the 18-byte compact IPv6 form PEX trades in
// added6/dropped6 (BEP 11). Pointer-free, so it works as a map key.
type CompactPeer6 struct {
	IP   [net.IPv6len]byte
	Port uint16
}

// NewCompactPeer6 folds addr into compact IPv6 form, clamping wild ports.
func NewCompactPeer6(addr *net.TCPAddr) CompactPeer6 {
	p := CompactPeer6{Port: clampedPort(addr.Port)}
	copy(p.IP[:], addr.IP.To16())
	return p
}

// Addr expands the compact IPv6 form back to a dialable address.
func (p CompactPeer6) Addr() *net.TCPAddr {
	return &net.TCPAddr{IP: p.IP[:], Port: int(p.Port)}
}

// MarshalBinary encodes the 18-byte wire form.
func (p CompactPeer6) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 18))
	err := binary.Write(buf, binary.BigEndian, p)
	return buf.Bytes(), err
}

// UnmarshalBinary decodes exactly 18 bytes into the peer.
func (p *CompactPeer6) UnmarshalBinary(data []byte) error {
	if len(data) != 18 {
		return errors.New("invalid compact ipv6 peer length")
	}
	return binary.Read(bytes.NewReader(data), binary.BigEndian, p)
}

// IsIPv6 reports whether addr is a usable IPv6 peer address.
func IsIPv6(addr *net.TCPAddr) bool {
	return addr != nil && addr.IP.To4() == nil && addr.IP.To16() != nil
}
