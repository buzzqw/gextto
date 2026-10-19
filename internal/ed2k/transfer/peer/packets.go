package peer

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/buzzqw/gextto/internal/ed2k"
)

// Client opcodes used by the file and part exchange (eMule 0xC5 protocol).
const (
	OpRequestFileName = 0x58
	OpFileNameAnswer  = 0x59
	OpFileNoSuchFile  = 0x48
	OpHashsetRequest  = 0x51
	OpHashsetAnswer   = 0x52
	OpRequestParts    = 0x47
	OpSendingPart     = 0x46
	OpCompressedPart  = 0x40
)

// FileRequest asks a peer for a file by MD4. The extended multipacket variant
// is deliberately not implemented here.
type FileRequest struct {
	Hash ed2k.ID
}

func EncodeFileRequest(request FileRequest) []byte { return append([]byte(nil), request.Hash[:]...) }

func DecodeFileRequest(payload []byte) (FileRequest, error) {
	if len(payload) < 16 {
		return FileRequest{}, errors.New("peer: short file request")
	}
	var request FileRequest
	copy(request.Hash[:], payload[:16])
	return request, nil
}

// FileNameAnswer is OP_REQFILENAMEANSWER: the identity plus the claimed name.
// The name is not trusted until the part hashes match.
type FileNameAnswer struct {
	Hash ed2k.ID
	Name string
}

func EncodeFileNameAnswer(answer FileNameAnswer) []byte {
	out := make([]byte, 0, 16+4+len(answer.Name))
	out = append(out, answer.Hash[:]...)
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(answer.Name)))
	out = append(out, size[:]...)
	return append(out, answer.Name...)
}

func DecodeFileNameAnswer(payload []byte) (FileNameAnswer, error) {
	var answer FileNameAnswer
	if len(payload) < 20 {
		return answer, errors.New("peer: short file-name answer")
	}
	copy(answer.Hash[:], payload[:16])
	length := int(binary.LittleEndian.Uint32(payload[16:]))
	if length < 0 || 20+length > len(payload) {
		return answer, errors.New("peer: truncated file name")
	}
	answer.Name = string(payload[20 : 20+length])
	return answer, nil
}

// HashsetRequest is the deprecated OP_HASHSETREQUEST: just the file hash.
type HashsetRequest struct {
	Hash ed2k.ID
}

func EncodeHashsetRequest(request HashsetRequest) []byte {
	return append([]byte(nil), request.Hash[:]...)
}

func DecodeHashsetRequest(payload []byte) (HashsetRequest, error) {
	if len(payload) < 16 {
		return HashsetRequest{}, errors.New("peer: short hashset request")
	}
	var request HashsetRequest
	copy(request.Hash[:], payload[:16])
	return request, nil
}

// HashsetAnswer is the deprecated OP_HASHSETANSWER: a part hash list. The
// requester knows which file it belongs to.
type HashsetAnswer struct {
	Hashes []ed2k.ID
}

func EncodeHashsetAnswer(answer HashsetAnswer) ([]byte, error) {
	if len(answer.Hashes) > 0xFFFF {
		return nil, errors.New("peer: too many part hashes")
	}
	out := make([]byte, 2, 2+len(answer.Hashes)*16)
	binary.LittleEndian.PutUint16(out, uint16(len(answer.Hashes)))
	for _, hash := range answer.Hashes {
		out = append(out, hash[:]...)
	}
	return out, nil
}

func DecodeHashsetAnswer(payload []byte) (HashsetAnswer, error) {
	if len(payload) < 2 {
		return HashsetAnswer{}, errors.New("peer: short hashset answer")
	}
	count := int(binary.LittleEndian.Uint16(payload))
	if 2+count*16 > len(payload) {
		return HashsetAnswer{}, errors.New("peer: truncated hashset answer")
	}
	answer := HashsetAnswer{Hashes: make([]ed2k.ID, 0, count)}
	for i := 0; i < count; i++ {
		var hash ed2k.ID
		copy(hash[:], payload[2+i*16:])
		answer.Hashes = append(answer.Hashes, hash)
	}
	return answer, nil
}

// PartRequest asks for up to three byte ranges of one file.
type PartRequest struct {
	Hash   ed2k.ID
	Starts [3]uint32
	Ends   [3]uint32
}

func EncodePartRequest(request PartRequest) []byte {
	out := make([]byte, 0, 16+24)
	out = append(out, request.Hash[:]...)
	var value [4]byte
	for _, start := range request.Starts {
		binary.LittleEndian.PutUint32(value[:], start)
		out = append(out, value[:]...)
	}
	for _, end := range request.Ends {
		binary.LittleEndian.PutUint32(value[:], end)
		out = append(out, value[:]...)
	}
	return out
}

func DecodePartRequest(payload []byte) (PartRequest, error) {
	if len(payload) < 40 {
		return PartRequest{}, errors.New("peer: short part request")
	}
	var request PartRequest
	copy(request.Hash[:], payload[:16])
	for i := 0; i < 3; i++ {
		request.Starts[i] = binary.LittleEndian.Uint32(payload[16+i*4:])
		request.Ends[i] = binary.LittleEndian.Uint32(payload[28+i*4:])
	}
	return request, nil
}

// SendingPart carries one uncompressed byte range. End is exclusive, as in
// eMule: len(Data) == End-Start.
type SendingPart struct {
	Hash  ed2k.ID
	Start uint32
	End   uint32
	Data  []byte
}

func EncodeSendingPart(part SendingPart) ([]byte, error) {
	if part.End < part.Start || int(part.End-part.Start) != len(part.Data) {
		return nil, errors.New("peer: sending part length mismatch")
	}
	out := make([]byte, 0, 24+len(part.Data))
	out = append(out, part.Hash[:]...)
	var value [4]byte
	binary.LittleEndian.PutUint32(value[:], part.Start)
	out = append(out, value[:]...)
	binary.LittleEndian.PutUint32(value[:], part.End)
	out = append(out, value[:]...)
	return append(out, part.Data...), nil
}

func DecodeSendingPart(payload []byte) (SendingPart, error) {
	if len(payload) < 24 {
		return SendingPart{}, errors.New("peer: short sending part")
	}
	var part SendingPart
	copy(part.Hash[:], payload[:16])
	part.Start = binary.LittleEndian.Uint32(payload[16:])
	part.End = binary.LittleEndian.Uint32(payload[20:])
	if part.End < part.Start || 24+int(part.End-part.Start) > len(payload) {
		return SendingPart{}, fmt.Errorf("peer: invalid sending part range %d-%d", part.Start, part.End)
	}
	part.Data = append([]byte(nil), payload[24:24+int(part.End-part.Start)]...)
	return part, nil
}

// CompressedPart carries one DEFLATE-compressed range of a part.
type CompressedPart struct {
	Hash  ed2k.ID
	Start uint32
	Size  uint32
	Data  []byte
}

func EncodeCompressedPart(part CompressedPart) ([]byte, error) {
	if part.Size != uint32(len(part.Data)) {
		return nil, errors.New("peer: compressed part size mismatch")
	}
	out := make([]byte, 0, 24+len(part.Data))
	out = append(out, part.Hash[:]...)
	var value [4]byte
	binary.LittleEndian.PutUint32(value[:], part.Start)
	out = append(out, value[:]...)
	binary.LittleEndian.PutUint32(value[:], part.Size)
	out = append(out, value[:]...)
	return append(out, part.Data...), nil
}

func DecodeCompressedPart(payload []byte) (CompressedPart, error) {
	if len(payload) < 24 {
		return CompressedPart{}, errors.New("peer: short compressed part")
	}
	var part CompressedPart
	copy(part.Hash[:], payload[:16])
	part.Start = binary.LittleEndian.Uint32(payload[16:])
	part.Size = binary.LittleEndian.Uint32(payload[20:])
	if 24+int(part.Size) > len(payload) {
		return CompressedPart{}, errors.New("peer: truncated compressed part")
	}
	part.Data = append([]byte(nil), payload[24:24+int(part.Size)]...)
	return part, nil
}

// Queue and callback opcodes.
const (
	OpReaskFilePing     = 0x90
	OpReaskAck          = 0x91
	OpFileNotFound      = 0x92
	OpQueueFull         = 0x93
	OpDirectCallbackReq = 0x95
)

// ReaskFilePing asks a queued peer whether the file is still available.
type ReaskFilePing struct {
	Hash ed2k.ID
}

func EncodeReaskFilePing(ping ReaskFilePing) []byte { return append([]byte(nil), ping.Hash[:]...) }

func DecodeReaskFilePing(payload []byte) (ReaskFilePing, error) {
	if len(payload) < 16 {
		return ReaskFilePing{}, errors.New("peer: short reask ping")
	}
	var ping ReaskFilePing
	copy(ping.Hash[:], payload[:16])
	return ping, nil
}

// ReaskAck is the queue position answer.
type ReaskAck struct {
	Rank uint16
}

func EncodeReaskAck(ack ReaskAck) []byte {
	var out [2]byte
	binary.LittleEndian.PutUint16(out[:], ack.Rank)
	return out[:]
}

func DecodeReaskAck(payload []byte) (ReaskAck, error) {
	if len(payload) < 2 {
		return ReaskAck{}, errors.New("peer: short reask ack")
	}
	return ReaskAck{Rank: binary.LittleEndian.Uint16(payload)}, nil
}

// DirectCallbackRequest is OP_DIRECTCALLBACKREQ: the TCP port, identity and
// connection options a LowID client asks a HighID peer to call back.
type DirectCallbackRequest struct {
	Port     uint16
	UserHash ed2k.ID
	Options  byte
}

func EncodeDirectCallbackRequest(request DirectCallbackRequest) []byte {
	out := make([]byte, 0, 19)
	var port [2]byte
	binary.LittleEndian.PutUint16(port[:], request.Port)
	out = append(out, port[:]...)
	out = append(out, request.UserHash[:]...)
	return append(out, request.Options)
}

func DecodeDirectCallbackRequest(payload []byte) (DirectCallbackRequest, error) {
	if len(payload) < 19 {
		return DirectCallbackRequest{}, errors.New("peer: short direct callback request")
	}
	var request DirectCallbackRequest
	request.Port = binary.LittleEndian.Uint16(payload)
	copy(request.UserHash[:], payload[2:18])
	request.Options = payload[18]
	return request, nil
}
