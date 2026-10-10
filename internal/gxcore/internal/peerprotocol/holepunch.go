package peerprotocol

// gextto fork: BEP 55 "Holepunch extension". The message is a fixed-size binary
// payload (not bencoded) carried inside the BEP 10 extension protocol under the
// name "ut_holepunch":
//
//	msg_type (1 byte): 0 rendezvous, 1 connect, 2 error
//	addr_type (1 byte): 0 ipv4, 1 ipv6
//	addr (4 or 16 bytes)
//	port (2 bytes, big-endian)
//	err_code (4 bytes, big-endian)

import (
	"encoding/binary"
	"fmt"
	"net"
)

// HolepunchMessageType is the msg_type field of a ut_holepunch message.
type HolepunchMessageType byte

const (
	// HolepunchRendezvous asks a relaying peer to introduce us to a target.
	HolepunchRendezvous HolepunchMessageType = iota
	// HolepunchConnect tells a peer to start a uTP connection to an endpoint.
	HolepunchConnect
	// HolepunchError reports why a rendezvous could not be completed.
	HolepunchError
)

func (t HolepunchMessageType) String() string {
	switch t {
	case HolepunchRendezvous:
		return "rendezvous"
	case HolepunchConnect:
		return "connect"
	case HolepunchError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", byte(t))
	}
}

// HolepunchErrCode is the err_code field of a ut_holepunch error message.
type HolepunchErrCode uint32

const (
	HolepunchNoSuchPeer   HolepunchErrCode = iota + 1 // target endpoint is invalid
	HolepunchNotConnected                             // relay is not connected to the target
	HolepunchNoSupport                                // target does not support ut_holepunch
	HolepunchNoSelf                                   // endpoint belongs to the relay
)

func (c HolepunchErrCode) Error() string {
	switch c {
	case HolepunchNoSuchPeer:
		return "target endpoint is invalid"
	case HolepunchNotConnected:
		return "relaying peer is not connected to the target peer"
	case HolepunchNoSupport:
		return "target peer does not support ut_holepunch"
	case HolepunchNoSelf:
		return "target endpoint belongs to the relaying peer"
	default:
		return fmt.Sprintf("holepunch error code %d", uint32(c))
	}
}

// HolepunchMessage is a decoded ut_holepunch message.
type HolepunchMessage struct {
	Type    HolepunchMessageType
	Addr    net.IP
	Port    uint16
	ErrCode HolepunchErrCode
}

// AddrPort returns the endpoint as a *net.TCPAddr (the address type the engine uses
// for peers), or nil when the address is not a valid IP.
func (m HolepunchMessage) AddrPort() *net.TCPAddr {
	if m.Addr == nil {
		return nil
	}
	return &net.TCPAddr{IP: m.Addr, Port: int(m.Port)}
}

// MarshalBinary encodes the message; the wire format is fixed size.
func (m HolepunchMessage) MarshalBinary() ([]byte, error) {
	ip4 := m.Addr.To4()
	var addrType byte
	var addr []byte
	switch {
	case ip4 != nil:
		addrType = 0x00
		addr = ip4
	case m.Addr.To16() != nil:
		addrType = 0x01
		addr = m.Addr.To16()
	default:
		return nil, fmt.Errorf("invalid holepunch address: %v", m.Addr)
	}
	buf := make([]byte, 1+1+len(addr)+2+4)
	buf[0] = byte(m.Type)
	buf[1] = addrType
	copy(buf[2:], addr)
	off := 2 + len(addr)
	binary.BigEndian.PutUint16(buf[off:], m.Port)
	binary.BigEndian.PutUint32(buf[off+2:], uint32(m.ErrCode))
	return buf, nil
}

// UnmarshalBinary decodes a ut_holepunch payload.
func (m *HolepunchMessage) UnmarshalBinary(b []byte) error {
	if len(b) < 12 {
		return fmt.Errorf("holepunch message too short: %d bytes", len(b))
	}
	m.Type = HolepunchMessageType(b[0])
	addrType := b[1]
	rest := b[2:]
	var addr net.IP
	switch addrType {
	case 0x00:
		if len(rest) < 4+2+4 {
			return fmt.Errorf("holepunch ipv4 message too short")
		}
		addr = net.IP(append([]byte(nil), rest[:4]...))
		rest = rest[4:]
	case 0x01:
		if len(rest) < 16+2+4 {
			return fmt.Errorf("holepunch ipv6 message too short")
		}
		addr = net.IP(append([]byte(nil), rest[:16]...))
		rest = rest[16:]
	default:
		return fmt.Errorf("unhandled holepunch address type %d", addrType)
	}
	m.Addr = addr
	m.Port = binary.BigEndian.Uint16(rest[:2])
	m.ErrCode = HolepunchErrCode(binary.BigEndian.Uint32(rest[2:6]))
	if len(rest) != 6 {
		return fmt.Errorf("holepunch message has %d trailing bytes", len(rest)-6)
	}
	return nil
}
