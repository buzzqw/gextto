package peer

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/buzzqw/gextto/internal/ed2k/secident"
)

// eMule secure identification opcodes. They travel on the eMule 0xC5
// protocol, like EMULEINFO and the other client extensions.
const (
	OpPublicKey     = 0x85 // <keylen 1><DER public key>
	OpSignature     = 0x86 // <siglen 1><signature>
	OpSecIdentState = 0x87 // <state 1><challenge 4>
)

// Secure identification states, first byte of OP_SECIDENTSTATE.
const (
	IdentUnavailable     = 0
	IdentSignatureNeeded = 1
	IdentKeyAndSignature = 2
)

// maxPublicKey bounds the one-byte length prefix used by the packets.
const maxPublicKey = 250

// Secure identification v2 challenge IP kinds (CRYPT_CIP_* in eMule/aMule).
const (
	challengeRemoteClient = 10 // the signature covers the verifier's address
	challengeLocalClient  = 20 // the signature covers the signer's address
	challengeNone         = 30 // the signature covers no address
)

// Packet is a ready-to-send extended-protocol packet.
type Packet struct {
	Opcode  byte
	Payload []byte
}

// SecIdent runs one secure identification exchange. The state is per
// connection: peer keys and challenges do not survive it.
type SecIdent struct {
	key           *secident.Key
	challengeFor  uint32
	challengeFrom uint32
	peerKey       []byte
	signatureSent bool
	verified      bool
	unavailable   bool
	localIP       net.IP
	peerIP        net.IP
}

// NewSecIdent returns a handshake bound to the local identity.
func NewSecIdent(key *secident.Key) *SecIdent { return &SecIdent{key: key} }

// SetPeerAddresses records the endpoints of the TCP session. Version 2
// signatures bind the challenge to one of them; without them a v2 signature
// is left unverified.
func (s *SecIdent) SetPeerAddresses(local, remote net.IP) {
	s.localIP = local
	s.peerIP = remote
}

// Verified reports whether the peer proved ownership of its key.
func (s *SecIdent) Verified() bool { return s.verified }

// Unavailable reports whether the peer declined secure identification.
func (s *SecIdent) Unavailable() bool { return s.unavailable }

// Start builds the opening OP_SECIDENTSTATE. The state asks for the peer's
// key and signature, and carries the challenge we will verify.
func (s *SecIdent) Start() (Packet, error) {
	if s.challengeFor == 0 {
		challenge, err := randomChallenge()
		if err != nil {
			return Packet{}, err
		}
		s.challengeFor = challenge
	}
	state := byte(IdentKeyAndSignature)
	if len(s.peerKey) > 0 {
		state = IdentSignatureNeeded
	}
	payload := make([]byte, 5)
	payload[0] = state
	binary.LittleEndian.PutUint32(payload[1:], s.challengeFor)
	return Packet{Opcode: OpSecIdentState, Payload: payload}, nil
}

// HandleState stores the peer challenge and returns the packets the peer is
// waiting for: our public key and, when both keys are known, our signature.
func (s *SecIdent) HandleState(payload []byte) ([]Packet, error) {
	if len(payload) != 5 {
		return nil, fmt.Errorf("peer: secure ident state has %d bytes", len(payload))
	}
	switch payload[0] {
	case IdentUnavailable:
		s.unavailable = true
		return nil, nil
	case IdentSignatureNeeded, IdentKeyAndSignature:
	default:
		return nil, fmt.Errorf("peer: unknown secure ident state %d", payload[0])
	}
	challenge := binary.LittleEndian.Uint32(payload[1:])
	if challenge == 0 {
		return nil, errors.New("peer: secure ident with an empty challenge")
	}
	if challenge != s.challengeFrom {
		s.challengeFrom = challenge
		s.signatureSent = false
	}

	var packets []Packet
	if payload[0] == IdentKeyAndSignature || len(s.peerKey) == 0 {
		packets = append(packets, Packet{Opcode: OpPublicKey, Payload: encodePublicKey(s.key.PublicKey())})
	}
	if sig, ok, err := s.signature(); err != nil {
		return nil, err
	} else if ok {
		packets = append(packets, Packet{Opcode: OpSignature, Payload: sig})
	}
	return packets, nil
}

// HandlePublicKey stores the peer key and answers with our signature when
// the peer has already asked for it.
func (s *SecIdent) HandlePublicKey(payload []byte) ([]Packet, error) {
	if len(payload) < 1 {
		return nil, errors.New("peer: empty public key packet")
	}
	size := int(payload[0])
	if size == 0 || size != len(payload)-1 || size > maxPublicKey {
		return nil, fmt.Errorf("peer: inconsistent public key size %d", size)
	}
	key := append([]byte(nil), payload[1:]...)
	if err := secident.ValidPublicKey(key); err != nil {
		return nil, err
	}
	s.peerKey = key
	sig, ok, err := s.signature()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return []Packet{{Opcode: OpSignature, Payload: sig}}, nil
}

// HandleSignature verifies the peer signature against our key and challenge.
// A bad signature leaves the peer unverified: it is not a protocol error.
func (s *SecIdent) HandleSignature(payload []byte) (bool, error) {
	if len(payload) < 1 {
		return false, errors.New("peer: empty signature packet")
	}
	size := int(payload[0])
	var kind byte
	switch size {
	case len(payload) - 1:
		// Version 1: key and challenge only.
	case len(payload) - 2:
		// Version 2 appends the kind of IP the challenge is bound to.
		kind = payload[len(payload)-1]
	default:
		return false, fmt.Errorf("peer: inconsistent signature size %d", size)
	}
	if size == 0 || size > maxPublicKey {
		return false, fmt.Errorf("peer: inconsistent signature size %d", size)
	}
	if len(s.peerKey) == 0 {
		return false, errors.New("peer: signature without a public key")
	}
	if s.challengeFor == 0 {
		return false, errors.New("peer: signature without an outgoing challenge")
	}
	data := make([]byte, 0, len(s.key.PublicKey())+4)
	data = append(data, s.key.PublicKey()...)
	var challenge [4]byte
	// The challenge is signed exactly as it travelled on the wire; eMule
	// parses it and writes it back with the same byte order.
	binary.LittleEndian.PutUint32(challenge[:], s.challengeFor)
	data = append(data, challenge[:]...)
	if kind != 0 {
		ip, ok := s.challengeIP(kind)
		if !ok {
			// The caller could not supply the endpoint the peer signed, so
			// the signature cannot be checked. Keep the peer unverified
			// instead of failing the transfer.
			return false, nil
		}
		data = append(data, ip[:4]...)
		data = append(data, kind)
	}

	if err := secident.Verify(s.peerKey, data, payload[1:1+size]); err != nil {
		s.verified = false
		return false, nil
	}
	s.verified = true
	return true, nil
}

// challengeIP returns the IPv4 address a v2 signature binds, following the
// signer's point of view: the verifier for kind 10, the signer for kind 20.
func (s *SecIdent) challengeIP(kind byte) (net.IP, bool) {
	switch kind {
	case challengeRemoteClient:
		return s.ipv4(s.localIP)
	case challengeLocalClient:
		return s.ipv4(s.peerIP)
	case challengeNone:
		return net.IPv4zero.To4(), true
	default:
		return nil, false
	}
}

func (s *SecIdent) ipv4(ip net.IP) (net.IP, bool) {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, false
	}
	return ip4, true
}

// signature signs the peer key and the challenge the peer sent. Both sides
// know the same two values, so the signature cannot be replayed elsewhere.
func (s *SecIdent) signature() ([]byte, bool, error) {
	if s.signatureSent || len(s.peerKey) == 0 || s.challengeFrom == 0 {
		return nil, false, nil
	}
	data := make([]byte, 0, len(s.peerKey)+4)
	data = append(data, s.peerKey...)
	var challenge [4]byte
	binary.LittleEndian.PutUint32(challenge[:], s.challengeFrom)
	data = append(data, challenge[:]...)
	sig, err := s.key.Sign(data)
	if err != nil {
		return nil, false, err
	}
	s.signatureSent = true
	return encodeSignature(sig), true, nil
}

func encodePublicKey(der []byte) []byte {
	out := make([]byte, 0, len(der)+1)
	out = append(out, byte(len(der)))
	return append(out, der...)
}

func encodeSignature(sig []byte) []byte {
	out := make([]byte, 0, len(sig)+1)
	out = append(out, byte(len(sig)))
	return append(out, sig...)
}

func randomChallenge() (uint32, error) {
	var raw [4]byte
	for {
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, fmt.Errorf("peer: secure ident challenge: %w", err)
		}
		if value := binary.LittleEndian.Uint32(raw[:]); value != 0 {
			return value, nil
		}
	}
}
