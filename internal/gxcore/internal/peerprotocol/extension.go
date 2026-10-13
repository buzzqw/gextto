package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"github.com/buzzqw/gextto/internal/gxcore/internal/bencodecheck"
	"github.com/zeebo/bencode"
)

const (
	// ExtensionIDHandshake addresses the extension handshake itself.
	ExtensionIDHandshake = iota
	// ExtensionIDMetadata addresses ut_metadata (BEP 9).
	ExtensionIDMetadata
	// ExtensionIDPEX addresses ut_pex (BEP 11).
	ExtensionIDPEX
	// ExtensionIDHolepunch addresses ut_holepunch (BEP 55).
	ExtensionIDHolepunch
	// ExtensionIDDontHave addresses lt_donthave (BEP 54).
	ExtensionIDDontHave
)

const (
	// ExtensionKeyMetadata negotiates the metadata extension.
	ExtensionKeyMetadata = "ut_metadata"
	// ExtensionKeyPEX negotiates the PEX extension.
	ExtensionKeyPEX = "ut_pex"
	// ExtensionKeyHolepunch negotiates the BEP 55 holepunch extension.
	ExtensionKeyHolepunch = "ut_holepunch"
	// ExtensionKeyDontHave negotiates the BEP 54 lt_donthave extension.
	ExtensionKeyDontHave = "lt_donthave"
)

const (
	// ExtensionMetadataMessageTypeRequest asks for a metadata block.
	ExtensionMetadataMessageTypeRequest = iota
	// ExtensionMetadataMessageTypeData carries a metadata block.
	ExtensionMetadataMessageTypeData
	// ExtensionMetadataMessageTypeReject refuses a metadata block.
	ExtensionMetadataMessageTypeReject
)

// ExtensionMessage is one BEP 10 extended message: the negotiated sub-id
// plus a payload whose encoding depends on the extension — bencoded for the
// classic ones, fixed binary for holepunch and donthave.
type ExtensionMessage struct {
	ExtendedMessageID uint8
	Payload           any
}

// ID returns the peer protocol message type.
func (m ExtensionMessage) ID() MessageID { return Extension }

// Read is unsupported: extension messages serialize through WriteTo, which
// prefixes the payload length the peer writer needs up front.
func (m ExtensionMessage) Read([]byte) (int, error) {
	panic("Read must not be called, use WriteTo")
}

// WriteTo serializes the message: sub-id, then the payload in its own
// encoding, then the trailing metadata bytes for data messages.
func (m ExtensionMessage) WriteTo(w io.Writer) (n int64, err error) {
	var nn int
	if nn, err = w.Write([]byte{m.ExtendedMessageID}); err != nil {
		return int64(nn), err
	}
	n = int64(nn)
	if hp, ok := m.Payload.(HolepunchMessage); ok {
		data, merr := hp.MarshalBinary()
		if merr != nil {
			return n, merr
		}
		nn, err = w.Write(data)
		n += int64(nn)
		return n, err
	}
	if dh, ok := m.Payload.(DontHaveMessage); ok {
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], dh.Index)
		nn, err = w.Write(raw[:])
		return n + int64(nn), err
	}
	wc := newWriterCounter(w)
	err = bencode.NewEncoder(wc).Encode(m.Payload)
	n += wc.Count()
	if err != nil {
		return n, err
	}
	if mm, ok := m.Payload.(ExtensionMetadataMessage); ok {
		nn, err = w.Write(mm.Data)
		n += int64(nn)
	}
	return n, err
}

// UnmarshalBinary parses one received extended message by sub-id, clamping
// hostile negative sizes and rejecting short or unknown payloads.
func (m *ExtensionMessage) UnmarshalBinary(data []byte) error {
	var extID uint8
	if err := binary.Read(bytes.NewReader(data), binary.BigEndian, &extID); err != nil {
		return err
	}
	m.ExtendedMessageID = extID
	payload := data[1:]
	// The bencoded sub-messages are screened before decoding: zeebo/bencode
	// allocates a declared string length up front, so a tiny hostile message
	// could otherwise ask for gigabytes. (Dont-have and holepunch are binary,
	// so they are not screened here.)
	if m.ExtendedMessageID == ExtensionIDHandshake || m.ExtendedMessageID == ExtensionIDMetadata || m.ExtendedMessageID == ExtensionIDPEX {
		if err := bencodecheck.Validate(payload); err != nil {
			return err
		}
	}
	dec := bencode.NewDecoder(bytes.NewReader(payload))
	var err error
	switch m.ExtendedMessageID {
	case ExtensionIDHandshake:
		var extMsg ExtensionHandshakeMessage
		err = dec.Decode(&extMsg)
		if extMsg.MetadataSize < 0 {
			extMsg.MetadataSize = 0
		}
		if extMsg.RequestQueue < 0 {
			extMsg.RequestQueue = 0
		}
		m.Payload = extMsg
	case ExtensionIDMetadata:
		var extMsg ExtensionMetadataMessage
		err = dec.Decode(&extMsg)
		extMsg.Data = payload[dec.BytesParsed():]
		m.Payload = extMsg
	case ExtensionIDPEX:
		var extMsg ExtensionPEXMessage
		err = dec.Decode(&extMsg)
		m.Payload = extMsg
	case ExtensionIDHolepunch:
		var extMsg HolepunchMessage
		err = extMsg.UnmarshalBinary(payload)
		m.Payload = extMsg
	case ExtensionIDDontHave:
		if len(payload) < 4 {
			return fmt.Errorf("short dont-have payload: %d bytes", len(payload))
		}
		m.Payload = DontHaveMessage{Index: binary.BigEndian.Uint32(payload[:4])}
	default:
		return fmt.Errorf("peer sent invalid extension message id: %d", m.ExtendedMessageID)
	}
	return err
}

// ExtensionHandshakeMessage is the BEP 10 handshake dictionary.
type ExtensionHandshakeMessage struct {
	M            map[string]uint8 `bencode:"m"`
	V            string           `bencode:"v"`
	YourIP       string           `bencode:"yourip,omitempty"`
	MetadataSize int              `bencode:"metadata_size,omitempty"`
	RequestQueue int              `bencode:"reqq"`
	// UploadOnly is the BEP 21 "upload_only" header: set when the peer wants
	// nothing (a full or partial seed).
	UploadOnly int `bencode:"upload_only,omitempty"`
}

// NewExtensionHandshake advertises our extensions: metadata and PEX always,
// holepunch on request, donthave for receiving; the version, observed peer
// address, metadata size, request queue depth and seed state fill the rest.
func NewExtensionHandshake(metadataSize uint32, version string, yourip net.IP, requestQueueLength int, holepunch bool, uploadOnly bool) ExtensionHandshakeMessage {
	m := map[string]uint8{
		ExtensionKeyMetadata: ExtensionIDMetadata,
		ExtensionKeyPEX:      ExtensionIDPEX,
		ExtensionKeyDontHave: ExtensionIDDontHave,
	}
	if holepunch {
		m[ExtensionKeyHolepunch] = ExtensionIDHolepunch
	}
	msg := ExtensionHandshakeMessage{
		M:            m,
		V:            version,
		YourIP:       string(truncateIP(yourip)),
		MetadataSize: int(metadataSize),
		RequestQueue: requestQueueLength,
	}
	if uploadOnly {
		msg.UploadOnly = 1
	}
	return msg
}

// ExtensionMetadataMessage is one BEP 9 metadata exchange message; Data
// rides outside the bencoded dict.
type ExtensionMetadataMessage struct {
	Type      int    `bencode:"msg_type"`
	Piece     uint32 `bencode:"piece"`
	TotalSize int    `bencode:"total_size,omitempty"`
	Data      []byte `bencode:"-"`
}

// ExtensionPEXMessage is one BEP 11 gossip payload, compact-encoded per family.
type ExtensionPEXMessage struct {
	Added    string `bencode:"added"`
	Added6   string `bencode:"added6"`
	Dropped  string `bencode:"dropped"`
	Dropped6 string `bencode:"dropped6"`
}

// DontHaveMessage is the BEP 54 notice: the sender dropped a piece it had
// announced before.
type DontHaveMessage struct {
	Index uint32
}

// truncateIP compacts IPv4 addresses to 4 bytes for the yourip field.
func truncateIP(ip net.IP) net.IP {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return ip
}
