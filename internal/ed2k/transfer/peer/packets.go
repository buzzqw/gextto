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

// EncodeFileRequestExtended adds the extended part status and complete-source
// count that eMule expects when the hello advertised extended requests.
func EncodeFileRequestExtended(request FileRequest) []byte {
	out := make([]byte, 0, 20)
	out = append(out, request.Hash[:]...)
	return append(out, 0, 0, 0, 0) // no parts, no complete sources
}

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
	out := make([]byte, 0, 18+len(answer.Name))
	out = append(out, answer.Hash[:]...)
	var size [2]byte
	binary.LittleEndian.PutUint16(size[:], uint16(len(answer.Name)))
	out = append(out, size[:]...)
	return append(out, answer.Name...)
}

func DecodeFileNameAnswer(payload []byte) (FileNameAnswer, error) {
	var answer FileNameAnswer
	if len(payload) < 18 {
		return answer, errors.New("peer: short file-name answer")
	}
	copy(answer.Hash[:], payload[:16])
	length := int(binary.LittleEndian.Uint16(payload[16:]))
	if 18+length > len(payload) {
		return answer, errors.New("peer: truncated file name")
	}
	answer.Name = string(payload[18 : 18+length])
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

// HashsetAnswer is OP_HASHSETANSWER: the file hash, the part count and the
// part hashes. eMule writes the file hash before the count.
type HashsetAnswer struct {
	Hash   ed2k.ID
	Hashes []ed2k.ID
}

func EncodeHashsetAnswer(answer HashsetAnswer) ([]byte, error) {
	if len(answer.Hashes) > 0xFFFF {
		return nil, errors.New("peer: too many part hashes")
	}
	out := make([]byte, 0, 18+len(answer.Hashes)*16)
	out = append(out, answer.Hash[:]...)
	var count [2]byte
	binary.LittleEndian.PutUint16(count[:], uint16(len(answer.Hashes)))
	out = append(out, count[:]...)
	for _, hash := range answer.Hashes {
		out = append(out, hash[:]...)
	}
	return out, nil
}

func DecodeHashsetAnswer(payload []byte) (HashsetAnswer, error) {
	var answer HashsetAnswer
	if len(payload) < 18 {
		return answer, errors.New("peer: short hashset answer")
	}
	copy(answer.Hash[:], payload[:16])
	count := int(binary.LittleEndian.Uint16(payload[16:]))
	if 18+count*16 > len(payload) {
		return answer, errors.New("peer: truncated hashset answer")
	}
	answer.Hashes = make([]ed2k.ID, 0, count)
	for i := 0; i < count; i++ {
		var hash ed2k.ID
		copy(hash[:], payload[18+i*16:])
		answer.Hashes = append(answer.Hashes, hash)
	}
	return answer, nil
}

// FileStatus is OP_FILESTATUS: the file hash, the eD2k part count and the
// bitmap of completed parts. The eD2k count is size/PARTSIZE+1, which includes
// the extra slot for a file whose size is an exact multiple of PARTSIZE.
type FileStatus struct {
	Hash  ed2k.ID
	Parts int
	Bits  []byte
}

func EncodeFileStatus(status FileStatus) ([]byte, error) {
	if status.Parts <= 0 || status.Parts > 0xFFFF {
		return nil, errors.New("peer: invalid file-status part count")
	}
	want := (status.Parts + 7) / 8
	if len(status.Bits) != want {
		return nil, fmt.Errorf("peer: file-status bitmap has %d bytes, want %d", len(status.Bits), want)
	}
	out := make([]byte, 0, 18+len(status.Bits))
	out = append(out, status.Hash[:]...)
	var count [2]byte
	binary.LittleEndian.PutUint16(count[:], uint16(status.Parts))
	out = append(out, count[:]...)
	return append(out, status.Bits...), nil
}

func DecodeFileStatus(payload []byte) (FileStatus, error) {
	var status FileStatus
	if len(payload) < 18 {
		return status, errors.New("peer: short file status")
	}
	copy(status.Hash[:], payload[:16])
	status.Parts = int(binary.LittleEndian.Uint16(payload[16:]))
	if status.Parts <= 0 || 18+(status.Parts+7)/8 > len(payload) {
		return status, errors.New("peer: invalid file-status bitmap")
	}
	size := (status.Parts + 7) / 8
	status.Bits = append([]byte(nil), payload[18:18+size]...)
	return status, nil
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
	// The size field describes the whole compressed stream; a large stream
	// may be split across packets, so the payload length is authoritative.
	part.Data = append([]byte(nil), payload[24:]...)
	return part, nil
}

// Download control opcodes on the client TCP protocol.
const (
	OpStartUploadReq  = 0x54
	OpAcceptUploadReq = 0x55
	OpCancelTransfer  = 0x56
	OpOutOfPartReqs   = 0x57
	OpQueueRanking    = 0x60
)

// Informational opcodes. OP_EMULEINFO uses the same numeric value as OP_HELLO
// but travels on the extended (eMule) protocol, so the session treats it as a
// benign packet after the handshake.
const (
	OpEmuleInfo       = 0x01
	OpEmuleInfoAnswer = 0x02
	OpEndOfDownload   = 0x49
	OpMessage         = 0x4E
	OpSetReqFileID    = 0x4F
	OpFileStatus      = 0x50
)

// QueueRanking is OP_QUEUERANKING: the position in the peer's upload queue.
type QueueRanking struct {
	Rank uint16
}

func EncodeQueueRanking(ranking QueueRanking) []byte {
	var out [2]byte
	binary.LittleEndian.PutUint16(out[:], ranking.Rank)
	return out[:]
}

func DecodeQueueRanking(payload []byte) (QueueRanking, error) {
	if len(payload) < 2 {
		return QueueRanking{}, errors.New("peer: short queue ranking")
	}
	return QueueRanking{Rank: binary.LittleEndian.Uint16(payload)}, nil
}

// Source-exchange opcodes (eMule extended protocol).
const (
	OpRequestsSources  = 0x81
	OpAnswersSources   = 0x82
	OpRequestsSources2 = 0x83
	OpAnswersSources2  = 0x84
)

// Queue and callback opcodes. These belong to the client UDP protocol: they
// are not valid on the TCP session and are kept apart from the TCP opcodes.
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

// 64-bit part opcodes for files above 4 GiB. They travel on the eMule 0xC5
// protocol.
const (
	OpCompressedPartI64 = 0xA1
	OpSendingPartI64    = 0xA2
	OpRequestPartsI64   = 0xA3
)

// PartRequest64 is OP_REQUESTPARTS_I64: three 64-bit ranges.
type PartRequest64 struct {
	Hash   ed2k.ID
	Starts [3]uint64
	Ends   [3]uint64
}

func EncodePartRequest64(request PartRequest64) []byte {
	out := make([]byte, 0, 16+48)
	out = append(out, request.Hash[:]...)
	var value [8]byte
	for _, start := range request.Starts {
		binary.LittleEndian.PutUint64(value[:], start)
		out = append(out, value[:]...)
	}
	for _, end := range request.Ends {
		binary.LittleEndian.PutUint64(value[:], end)
		out = append(out, value[:]...)
	}
	return out
}

func DecodePartRequest64(payload []byte) (PartRequest64, error) {
	if len(payload) < 64 {
		return PartRequest64{}, errors.New("peer: short 64-bit part request")
	}
	var request PartRequest64
	copy(request.Hash[:], payload[:16])
	for i := 0; i < 3; i++ {
		request.Starts[i] = binary.LittleEndian.Uint64(payload[16+i*8:])
		request.Ends[i] = binary.LittleEndian.Uint64(payload[40+i*8:])
	}
	return request, nil
}

// SendingPart64 is OP_SENDINGPART_I64.
type SendingPart64 struct {
	Hash  ed2k.ID
	Start uint64
	End   uint64
	Data  []byte
}

func EncodeSendingPart64(part SendingPart64) ([]byte, error) {
	if part.End < part.Start || uint64(len(part.Data)) != part.End-part.Start {
		return nil, errors.New("peer: sending part64 length mismatch")
	}
	out := make([]byte, 0, 32+len(part.Data))
	out = append(out, part.Hash[:]...)
	var value [8]byte
	binary.LittleEndian.PutUint64(value[:], part.Start)
	out = append(out, value[:]...)
	binary.LittleEndian.PutUint64(value[:], part.End)
	out = append(out, value[:]...)
	return append(out, part.Data...), nil
}

func DecodeSendingPart64(payload []byte) (SendingPart64, error) {
	if len(payload) < 32 {
		return SendingPart64{}, errors.New("peer: short sending part64")
	}
	var part SendingPart64
	copy(part.Hash[:], payload[:16])
	part.Start = binary.LittleEndian.Uint64(payload[16:])
	part.End = binary.LittleEndian.Uint64(payload[24:])
	if part.End < part.Start || 32+int(part.End-part.Start) > len(payload) {
		return SendingPart64{}, errors.New("peer: invalid sending part64 range")
	}
	part.Data = append([]byte(nil), payload[32:32+int(part.End-part.Start)]...)
	return part, nil
}

// CompressedPart64 is OP_COMPRESSEDPART_I64.
type CompressedPart64 struct {
	Hash  ed2k.ID
	Start uint64
	Size  uint32
	Data  []byte
}

func EncodeCompressedPart64(part CompressedPart64) ([]byte, error) {
	if part.Size != uint32(len(part.Data)) {
		return nil, errors.New("peer: compressed part64 size mismatch")
	}
	out := make([]byte, 0, 28+len(part.Data))
	out = append(out, part.Hash[:]...)
	var start [8]byte
	binary.LittleEndian.PutUint64(start[:], part.Start)
	out = append(out, start[:]...)
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], part.Size)
	out = append(out, size[:]...)
	return append(out, part.Data...), nil
}

func DecodeCompressedPart64(payload []byte) (CompressedPart64, error) {
	if len(payload) < 28 {
		return CompressedPart64{}, errors.New("peer: short compressed part64")
	}
	var part CompressedPart64
	copy(part.Hash[:], payload[:16])
	part.Start = binary.LittleEndian.Uint64(payload[16:])
	part.Size = binary.LittleEndian.Uint32(payload[24:])
	part.Data = append([]byte(nil), payload[28:]...)
	return part, nil
}
