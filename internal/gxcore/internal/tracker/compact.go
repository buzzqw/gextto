package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
)

// CompactPeer is a struct value which consist of a 4-bytes IP address and a 2-bytes port value.
// CompactPeer can be used as a key in maps because it does not contain any pointers.
type CompactPeer struct {
	IP   [net.IPv4len]byte
	Port uint16
}

// NewCompactPeer returns a new CompactPeer from a net.TCPAddr.
func NewCompactPeer(addr *net.TCPAddr) CompactPeer {
	port := addr.Port
	if port < 0 || port > 65535 {
		port = 0
	}
	p := CompactPeer{Port: uint16(port)}
	copy(p.IP[:], addr.IP.To4())
	return p
}

// Addr returns a net.TCPAddr from CompactPeer.
func (p CompactPeer) Addr() *net.TCPAddr {
	return &net.TCPAddr{IP: p.IP[:], Port: int(p.Port)}
}

// MarshalBinary returns the bytes.
func (p CompactPeer) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 6))
	err := binary.Write(buf, binary.BigEndian, p)
	return buf.Bytes(), err
}

// UnmarshalBinary reads bytes from a slice into the CompactPeer.
func (p *CompactPeer) UnmarshalBinary(data []byte) error {
	if len(data) != 6 {
		return errors.New("invalid compact peer length")
	}
	return binary.Read(bytes.NewReader(data), binary.BigEndian, p)
}

// DecodePeersCompact parses and returns addresses for list of CompactPeers.
func DecodePeersCompact(b []byte) ([]*net.TCPAddr, error) {
	if len(b)%6 != 0 {
		return nil, errors.New("invalid peer list length")
	}
	count := len(b) / 6
	addrs := make([]*net.TCPAddr, 0, count)
	for i := 0; i < len(b); i += 6 {
		var peer CompactPeer
		err := peer.UnmarshalBinary(b[i : i+6])
		if err != nil {
			return nil, err
		}
		addrs = append(addrs, peer.Addr())
	}
	return addrs, nil
}

// DecodePeersCompact6 parses a compact IPv6 peer list (BEP 7): each peer is a
// 16-byte address followed by a 2-byte port (18 bytes).
func DecodePeersCompact6(b []byte) ([]*net.TCPAddr, error) {
	if len(b)%18 != 0 {
		return nil, errors.New("invalid ipv6 peer list length")
	}
	count := len(b) / 18
	addrs := make([]*net.TCPAddr, 0, count)
	for i := 0; i < len(b); i += 18 {
		ip := make(net.IP, net.IPv6len)
		copy(ip, b[i:i+16])
		port := binary.BigEndian.Uint16(b[i+16 : i+18])
		addrs = append(addrs, &net.TCPAddr{IP: ip, Port: int(port)})
	}
	return addrs, nil
}

// CompactPeer6 is the compact IPv6 form of a peer (16-byte address + 2-byte
// port), used by PEX (BEP 11, added6/dropped6). It has no pointers, so it can
// be used as a map key.
type CompactPeer6 struct {
	IP   [net.IPv6len]byte
	Port uint16
}

// NewCompactPeer6 returns a new CompactPeer6 from a net.TCPAddr.
func NewCompactPeer6(addr *net.TCPAddr) CompactPeer6 {
	port := addr.Port
	if port < 0 || port > 65535 {
		port = 0
	}
	p := CompactPeer6{Port: uint16(port)}
	copy(p.IP[:], addr.IP.To16())
	return p
}

// Addr returns a net.TCPAddr from CompactPeer6.
func (p CompactPeer6) Addr() *net.TCPAddr {
	return &net.TCPAddr{IP: p.IP[:], Port: int(p.Port)}
}

// MarshalBinary returns the bytes (18).
func (p CompactPeer6) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, 18))
	err := binary.Write(buf, binary.BigEndian, p)
	return buf.Bytes(), err
}

// UnmarshalBinary reads 18 bytes into the CompactPeer6.
func (p *CompactPeer6) UnmarshalBinary(data []byte) error {
	if len(data) != 18 {
		return errors.New("invalid compact ipv6 peer length")
	}
	return binary.Read(bytes.NewReader(data), binary.BigEndian, p)
}

// IsIPv6 reports whether addr is an IPv6 peer address.
func IsIPv6(addr *net.TCPAddr) bool {
	return addr != nil && addr.IP.To4() == nil && addr.IP.To16() != nil
}
