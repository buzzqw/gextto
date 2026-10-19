// Package download runs a single eD2k file transfer over the eMule client
// protocol. It is still an isolated building block of the gated transfer
// plane: cmd/gx-ed2k does not import it yet.
package download

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/partfile"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/peer"
)

// DefaultBlockSize is EMBLOCKSIZE from eMule: requests are made in 180 KiB
// blocks inside a 9,728,000-byte part.
const DefaultBlockSize int64 = 184_320

// ErrLargeFile is returned for files that need the 64-bit part opcodes.
var ErrLargeFile = errors.New("download: files above 4 GiB are not supported yet")

// Link is the immutable identity of the file to download.
type Link struct {
	Hash ed2k.ID
	Name string
	Size int64
}

// Config tunes one transfer.
type Config struct {
	TempDir          string
	BlockSize        int64
	MaxInFlight      int
	MaxPartAttempts  int
	ConnectTimeout   time.Duration
	HandshakeTimeout time.Duration
	QueueTimeout     time.Duration
	IdleTimeout      time.Duration
	ClientName       string
	TCPPort          uint16
	ClientID         uint32
	UserHash         ed2k.ID
	// OnProgress is called when a part is completed and when the transfer
	// starts, so a manager can expose a progress percentage.
	OnProgress func(done, total int64)
	// Logf receives protocol milestones and errors. Nil disables logging.
	Logf func(format string, args ...any)
}

func (c *Config) applyDefaults() {
	if c.TempDir == "" {
		c.TempDir = os.TempDir()
	}
	if c.BlockSize <= 0 || c.BlockSize > partfile.PartSize {
		c.BlockSize = DefaultBlockSize
	}
	if c.MaxInFlight <= 0 {
		c.MaxInFlight = 3
	}
	if c.MaxInFlight > 15 {
		c.MaxInFlight = 15
	}
	if c.MaxPartAttempts <= 0 {
		c.MaxPartAttempts = 3
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 20 * time.Second
	}
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = 15 * time.Second
	}
	if c.QueueTimeout <= 0 {
		c.QueueTimeout = 10 * time.Minute
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 60 * time.Second
	}
	if c.ClientName == "" {
		c.ClientName = "gx-ed2k"
	}
	if c.TCPPort == 0 {
		c.TCPPort = 4662
	}
	if c.UserHash == (ed2k.ID{}) {
		c.UserHash, _ = ed2k.RandomID()
	}
}

// Result is a completed and verified download.
type Result struct {
	Path     string
	Name     string
	Size     int64
	Hash     ed2k.ID
	Resumed  bool
	Verified bool
}

// queuedError reports that the peer put us in its upload queue. The caller
// can retry later; the partial file stays on disk and is resumed.
type queuedError struct {
	Rank uint16
}

func (e *queuedError) Error() string {
	return fmt.Sprintf("download: queued by peer (rank %d)", e.Rank)
}

// IsQueued reports whether err means "retry after the peer queue".
func IsQueued(err error) bool {
	var queued *queuedError
	return errors.As(err, &queued)
}

type blockKey struct {
	part  int
	block int
}

type pendingBlock struct {
	key   blockKey
	start int64
	end   int64
	data  []byte
	have  []bool
	got   int
}

type partDownload struct {
	data     []byte
	have     []bool
	got      int
	attempts int
}

type session struct {
	cfg      Config
	link     Link
	conn     *peer.Conn
	pf       *partfile.PartFile
	partPath string
	name     string

	accepted bool
	rank     uint16

	parts   map[int]*partDownload
	pending map[blockKey]pendingBlock
}

// Fetch downloads one file from one peer. If a matching .part/.part.met pair
// already exists in TempDir, it is resumed. The file is renamed to TempDir
// only after the whole-file MD4 matches the link hash.
func Fetch(ctx context.Context, address string, link Link, cfg Config) (*Result, error) {
	if link.Hash == (ed2k.ID{}) || link.Size <= 0 {
		return nil, errors.New("download: invalid link")
	}
	if link.Size > math.MaxUint32 {
		return nil, ErrLargeFile
	}
	cfg.applyDefaults()
	if err := os.MkdirAll(cfg.TempDir, 0o755); err != nil {
		return nil, err
	}

	partPath := filepath.Join(cfg.TempDir, link.Hash.Hex()+".part")
	var pf *partfile.PartFile
	var resumed bool
	if _, err := os.Stat(partPath + ".met"); err == nil {
		pf, err = partfile.Open(partPath)
		if err != nil {
			return nil, fmt.Errorf("download: resume: %w", err)
		}
		if pf.Meta.File != link.Hash || pf.Meta.Size != link.Size {
			pf.Close()
			return nil, errors.New("download: part file does not match the link")
		}
		resumed = true
	}

	cfg.applyDefaults()
	if cfg.Logf != nil {
		cfg.Logf("download: dialing %s for %q (%d bytes)", address, link.Name, link.Size)
	}
	dialer := net.Dialer{Timeout: cfg.ConnectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		if pf != nil {
			pf.Close()
		}
		return nil, fmt.Errorf("download: dial %s: %w", address, err)
	}
	s := &session{
		cfg:      cfg,
		link:     link,
		conn:     peer.NewConn(conn),
		pf:       pf,
		partPath: partPath,
		name:     link.Name,
		parts:    make(map[int]*partDownload),
		pending:  make(map[blockKey]pendingBlock),
	}
	defer s.conn.Close()
	// A paused/removed transfer must stop now, not after the next idle timeout.
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.conn.Close()
		case <-stopped:
		}
	}()
	defer close(stopped)

	if err := s.run(ctx); err != nil {
		if s.pf != nil {
			s.pf.Close()
		}
		return nil, err
	}
	return s.finish(resumed)
}

func (s *session) run(ctx context.Context) error {
	if s.pf != nil && s.pf.Complete() {
		return nil
	}
	if err := s.handshake(ctx); err != nil {
		return err
	}
	s.logf("download: handshake with %s complete", s.conn.RemoteAddr())
	if err := s.conn.SendExtended(peer.OpEmuleInfo, peer.EncodeMuleInfo()); err != nil {
		return fmt.Errorf("download: send eMule info: %w", err)
	}
	if err := s.requestFile(ctx); err != nil {
		return err
	}
	s.logf("download: peer shares %q", s.name)
	// A single-part file has no separate hashset: the part hash is the file
	// hash. Ask for the hashset after the upload slot for multi-part files;
	// eMule only answers it reliably once the upload file is set.
	if s.pf == nil && partfile.PartCount(s.link.Size) == 1 {
		if err := s.createPart([]ed2k.ID{s.link.Hash}); err != nil {
			return err
		}
	}
	if err := s.setRequestFile(ctx); err != nil {
		return err
	}
	if err := s.startUpload(ctx); err != nil {
		return err
	}
	s.logf("download: upload slot granted by %s", s.conn.RemoteAddr())
	if s.pf == nil {
		hashes, err := s.requestHashset(ctx)
		if err != nil {
			return err
		}
		s.logf("download: hashset with %d parts", len(hashes))
		if err := s.createPart(hashes); err != nil {
			return err
		}
	}
	return s.download(ctx)
}

func (s *session) setDeadline(ctx context.Context, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = s.conn.SetDeadline(deadline)
}

func (s *session) handshake(ctx context.Context) error {
	s.setDeadline(ctx, s.cfg.HandshakeTimeout)
	answer, err := s.conn.Handshake(ctx, peer.Hello{
		UserHash: s.cfg.UserHash,
		ClientID: s.cfg.ClientID,
		Port:     s.cfg.TCPPort,
		Name:     s.cfg.ClientName,
		Version:  ed2k.KadProtocolVersion,
	})
	if err != nil {
		return fmt.Errorf("download: handshake: %w", err)
	}
	_ = answer
	return nil
}

func (s *session) requestFile(ctx context.Context) error {
	s.setDeadline(ctx, s.cfg.HandshakeTimeout)
	if err := s.conn.Send(peer.OpRequestFileName, peer.EncodeFileRequestExtended(peer.FileRequest{Hash: s.link.Hash})); err != nil {
		return fmt.Errorf("download: file request: %w", err)
	}
	for {
		opcode, payload, err := s.conn.Receive()
		if err != nil {
			return fmt.Errorf("download: waiting for the file name: %w", err)
		}
		if handled, err := s.control(opcode, payload); handled {
			if err != nil {
				return err
			}
			continue
		}
		switch opcode {
		case peer.OpFileNameAnswer:
			answer, err := peer.DecodeFileNameAnswer(payload)
			if err != nil {
				return err
			}
			if answer.Hash != s.link.Hash {
				return errors.New("download: peer answered for a different file")
			}
			if s.name == "" {
				s.name = answer.Name
			}
			return nil
		case peer.OpFileNoSuchFile:
			return errors.New("download: peer does not have the file")
		}
	}
}

func (s *session) setRequestFile(ctx context.Context) error {
	s.setDeadline(ctx, s.cfg.HandshakeTimeout)
	if err := s.conn.Send(peer.OpSetReqFileID, peer.EncodeFileRequest(peer.FileRequest{Hash: s.link.Hash})); err != nil {
		return fmt.Errorf("download: set file id: %w", err)
	}
	return nil
}

func (s *session) requestHashset(ctx context.Context) ([]ed2k.ID, error) {
	s.setDeadline(ctx, s.cfg.HandshakeTimeout)
	if err := s.conn.Send(peer.OpHashsetRequest, peer.EncodeHashsetRequest(peer.HashsetRequest{Hash: s.link.Hash})); err != nil {
		return nil, fmt.Errorf("download: hashset request: %w", err)
	}
	for {
		opcode, payload, err := s.conn.Receive()
		if err != nil {
			return nil, fmt.Errorf("download: waiting for the hashset: %w", err)
		}
		if handled, err := s.control(opcode, payload); handled {
			if err != nil {
				return nil, err
			}
			continue
		}
		switch opcode {
		case peer.OpHashsetAnswer:
			answer, err := peer.DecodeHashsetAnswer(payload)
			if err != nil {
				return nil, err
			}
			if answer.Hash != s.link.Hash {
				return nil, errors.New("download: hashset for a different file")
			}
			return answer.Hashes, nil
		case peer.OpFileNoSuchFile:
			return nil, errors.New("download: peer does not have the file")
		}
	}
}

func (s *session) createPart(hashes []ed2k.ID) error {
	want := partfile.PartCount(s.link.Size)
	if len(hashes) != want {
		return fmt.Errorf("download: hashset has %d parts, want %d", len(hashes), want)
	}
	meta := partfile.NewMeta(s.link.Hash, hashes, s.name, s.link.Size, uint32(time.Now().Unix()))
	pf, err := partfile.Create(s.partPath, meta)
	if err != nil {
		return fmt.Errorf("download: create part file: %w", err)
	}
	s.pf = pf
	s.report()
	return nil
}

func (s *session) startUpload(ctx context.Context) error {
	if s.accepted {
		return nil
	}
	s.setDeadline(ctx, s.cfg.QueueTimeout)
	s.logf("download: asking for an upload slot (queue rank %d)", s.rank)
	if err := s.conn.Send(peer.OpStartUploadReq, peer.EncodeFileRequest(peer.FileRequest{Hash: s.link.Hash})); err != nil {
		return fmt.Errorf("download: start upload: %w", err)
	}
	for {
		opcode, payload, err := s.conn.Receive()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return &queuedError{Rank: s.rank}
			}
			return fmt.Errorf("download: waiting for an upload slot: %w", err)
		}
		if handled, err := s.control(opcode, payload); handled {
			if err != nil {
				return err
			}
			if s.accepted {
				return nil
			}
			continue
		}
		switch opcode {
		case peer.OpFileNoSuchFile:
			return errors.New("download: peer does not have the file")
		}
	}
}

func (s *session) control(opcode byte, payload []byte) (bool, error) {
	switch opcode {
	case peer.OpQueueRanking:
		ranking, err := peer.DecodeQueueRanking(payload)
		if err != nil {
			return true, err
		}
		s.rank = ranking.Rank
		return true, nil
	case peer.OpAcceptUploadReq:
		s.accepted = true
		return true, nil
	case peer.OpOutOfPartReqs:
		return true, nil
	case peer.OpCancelTransfer, peer.OpEndOfDownload:
		return true, errors.New("download: peer cancelled the transfer")
	case peer.OpMessage, peer.OpHello, peer.OpHelloAnswer, peer.OpFileStatus:
		return true, nil
	}
	if opcode == peer.OpEmuleInfo || opcode == peer.OpEmuleInfoAnswer {
		return true, nil
	}
	return false, nil
}

func (s *session) download(ctx context.Context) error {
	for {
		if s.pf.Complete() {
			return nil
		}
		if err := s.requestMore(); err != nil {
			return err
		}
		if len(s.pending) == 0 {
			return errors.New("download: no block available")
		}
		s.setDeadline(ctx, s.cfg.IdleTimeout)
		opcode, payload, err := s.conn.Receive()
		if err != nil {
			return fmt.Errorf("download: waiting for data: %w", err)
		}
		if handled, err := s.control(opcode, payload); handled {
			if err != nil {
				return err
			}
			continue
		}
		switch opcode {
		case peer.OpSendingPart:
			part, err := peer.DecodeSendingPart(payload)
			if err != nil {
				return err
			}
			if part.Hash != s.link.Hash {
				continue
			}
			if err := s.writeBlock(int64(part.Start), int64(part.End), part.Data); err != nil {
				return err
			}
		case peer.OpCompressedPart:
			part, err := peer.DecodeCompressedPart(payload)
			if err != nil {
				return err
			}
			if part.Hash != s.link.Hash {
				continue
			}
			start, end, err := s.blockBounds(int64(part.Start))
			if err != nil {
				return err
			}
			data, err := peer.DecompressData(part.Data, end-start)
			if err != nil {
				return err
			}
			if int64(len(data)) != end-start {
				return errors.New("download: compressed block has the wrong size")
			}
			if err := s.writeBlock(start, end, data); err != nil {
				return err
			}
		}
	}
}

func (s *session) requestMore() error {
	for len(s.pending) < s.cfg.MaxInFlight {
		ranges, err := s.nextRanges(s.cfg.MaxInFlight - len(s.pending))
		if err != nil {
			return err
		}
		if len(ranges) == 0 {
			return nil
		}
		request := peer.PartRequest{Hash: s.link.Hash}
		for i, r := range ranges {
			request.Starts[i] = uint32(r.start)
			request.Ends[i] = uint32(r.end)
			length := int(r.end - r.start)
			s.pending[r.key] = pendingBlock{
				key: r.key, start: r.start, end: r.end,
				data: make([]byte, length), have: make([]bool, length),
			}
		}
		s.logf("download: requesting %d ranges (%d-%d ...)", len(ranges), ranges[0].start, ranges[0].end)
		if err := s.conn.Send(peer.OpRequestParts, peer.EncodePartRequest(request)); err != nil {
			return fmt.Errorf("download: part request: %w", err)
		}
	}
	return nil
}

type blockRange struct {
	key   blockKey
	start int64
	end   int64
}

func (s *session) nextRanges(limit int) ([]blockRange, error) {
	out := make([]blockRange, 0, limit)
	for part := 0; part < len(s.pf.Meta.Parts); part++ {
		if s.pf.Have(part) {
			continue
		}
		state, err := s.partState(part)
		if err != nil {
			return nil, err
		}
		for block := range state.have {
			if len(out) == limit {
				return out, nil
			}
			key := blockKey{part: part, block: block}
			if state.have[block] {
				continue
			}
			if _, inflight := s.pending[key]; inflight {
				continue
			}
			start, end := s.blockOffsets(part, block)
			out = append(out, blockRange{key: key, start: start, end: end})
		}
	}
	return out, nil
}

func (s *session) partState(part int) (*partDownload, error) {
	if state, ok := s.parts[part]; ok {
		if state.attempts >= s.cfg.MaxPartAttempts {
			return nil, fmt.Errorf("download: part %d failed %d times", part, state.attempts)
		}
		return state, nil
	}
	length := partfile.PartLength(s.pf.Meta.Size, part)
	if length <= 0 {
		return nil, fmt.Errorf("download: invalid part %d", part)
	}
	blocks := int((length + s.cfg.BlockSize - 1) / s.cfg.BlockSize)
	state := &partDownload{data: make([]byte, length), have: make([]bool, blocks)}
	s.parts[part] = state
	return state, nil
}

func (s *session) blockOffsets(part, block int) (int64, int64) {
	start := int64(part)*partfile.PartSize + int64(block)*s.cfg.BlockSize
	end := start + s.cfg.BlockSize
	partEnd := int64(part)*partfile.PartSize + partfile.PartLength(s.pf.Meta.Size, part)
	if end > partEnd {
		end = partEnd
	}
	return start, end
}

func (s *session) blockBounds(start int64) (int64, int64, error) {
	if start < 0 || start >= s.pf.Meta.Size {
		return 0, 0, fmt.Errorf("download: block start %d out of range", start)
	}
	part := int(start / partfile.PartSize)
	partStart := int64(part) * partfile.PartSize
	block := int((start - partStart) / s.cfg.BlockSize)
	expectedStart, end := s.blockOffsets(part, block)
	if start != expectedStart {
		return 0, 0, fmt.Errorf("download: block start %d is not aligned", start)
	}
	return start, end, nil
}

func (s *session) writeBlock(start, end int64, data []byte) error {
	if start < 0 || end <= start || end > s.pf.Meta.Size {
		return fmt.Errorf("download: invalid block range %d-%d", start, end)
	}
	for len(data) > 0 {
		part := int(start / partfile.PartSize)
		partStart := int64(part) * partfile.PartSize
		block := int((start - partStart) / s.cfg.BlockSize)
		key := blockKey{part: part, block: block}
		pending, ok := s.pending[key]
		if !ok {
			return fmt.Errorf("download: block %d/%d was not requested", part, block)
		}
		if start < pending.start || start >= pending.end {
			return fmt.Errorf("download: chunk %d-%d outside the requested block", start, end)
		}
		chunkEnd := end
		if chunkEnd > pending.end {
			chunkEnd = pending.end
		}
		length := int(chunkEnd - start)
		if len(data) < length {
			return errors.New("download: short chunk")
		}
		offset := int(start - pending.start)
		copy(pending.data[offset:], data[:length])
		for i := 0; i < length; i++ {
			if !pending.have[offset+i] {
				pending.have[offset+i] = true
				pending.got++
			}
		}
		if pending.got == len(pending.data) {
			delete(s.pending, key)
			if err := s.completeBlock(part, block, pending); err != nil {
				return err
			}
		} else {
			s.pending[key] = pending
		}
		start = chunkEnd
		data = data[length:]
	}
	return nil
}

func (s *session) completeBlock(part, block int, pending pendingBlock) error {
	state, err := s.partState(part)
	if err != nil {
		return err
	}
	if state.have[block] {
		return nil
	}
	partStart := int64(part) * partfile.PartSize
	copy(state.data[pending.start-partStart:], pending.data)
	state.have[block] = true
	state.got++
	if state.got == len(state.have) {
		if err := s.pf.WritePart(part, state.data); err != nil {
			state.attempts++
			state.data = make([]byte, len(state.data))
			state.have = make([]bool, len(state.have))
			state.got = 0
			return nil
		}
		delete(s.parts, part)
		s.report()
	}
	return nil
}

func (s *session) logf(format string, args ...any) {
	if s.cfg.Logf != nil {
		s.cfg.Logf(format, args...)
	}
}

func (s *session) report() {
	if s.cfg.OnProgress == nil || s.pf == nil {
		return
	}
	var done int64
	for index := range s.pf.Meta.Parts {
		if s.pf.Have(index) {
			done += partfile.PartLength(s.pf.Meta.Size, index)
		}
	}
	s.cfg.OnProgress(done, s.pf.Meta.Size)
}

func (s *session) finish(resumed bool) (*Result, error) {
	ok, err := s.pf.Verify()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("download: final MD4 does not match the link")
	}
	name := SafeName(s.name)
	if name == "" {
		name = s.link.Hash.Hex()
	}
	if err := s.pf.Close(); err != nil {
		return nil, err
	}
	finalPath := uniquePath(filepath.Join(s.cfg.TempDir, name))
	if err := os.Rename(s.partPath, finalPath); err != nil {
		return nil, err
	}
	_ = os.Remove(s.partPath + ".met")
	return &Result{
		Path: finalPath, Name: filepath.Base(finalPath), Size: s.link.Size,
		Hash: s.link.Hash, Resumed: resumed, Verified: true,
	}, nil
}

// SafeName turns a peer-provided name into a safe single path element.
func SafeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = strings.TrimRight(strings.TrimLeft(name, " "), " .")
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if len(name) > 180 {
		name = name[:180]
	}
	return name
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return path
}
