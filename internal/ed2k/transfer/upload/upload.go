// Package upload serves completed eD2k files to other peers over the eMule
// client protocol. It has a bounded upload queue, per-user credit accounting
// and a shared-file index; partial files are never served.
package upload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
	"github.com/buzzqw/gextto/internal/ed2k/secident"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/partfile"
	"github.com/buzzqw/gextto/internal/ed2k/transfer/peer"
)

// File is one completed file the server is willing to upload.
type File struct {
	Name  string
	Path  string
	Size  int64
	Hash  ed2k.ID
	Parts []ed2k.ID

	aichMu   sync.Mutex
	aichTree *partfile.AICHTree
	aichErr  error
	aichDone bool
}

func (f *File) aich() (*partfile.AICHTree, error) {
	f.aichMu.Lock()
	defer f.aichMu.Unlock()
	if f.aichDone {
		return f.aichTree, f.aichErr
	}
	f.aichDone = true
	f.aichTree, f.aichErr = partfile.BuildAICHTreeFile(f.Path)
	return f.aichTree, f.aichErr
}

// Server indexes completed files and answers client connections.
type waiter struct {
	hash  ed2k.ID
	ready chan struct{}
	at    time.Time
}

type Server struct {
	mu    sync.RWMutex
	files map[string]*File

	queueMu    sync.Mutex
	maxSlots   int
	active     int
	queue      []*waiter
	credits    map[string]int64
	creditPath string

	ClientName string
	UserHash   ed2k.ID
	TCPPort    uint16
	Idle       time.Duration
	// SecIdent is the local RSA identity for secure identification. Nil
	// disables the handshake and its advertisement in the hello options.
	SecIdent *secident.Key
}

// New returns a server with the given number of concurrent upload slots.
func New(slots int) *Server {
	if slots <= 0 {
		slots = 1
	}
	return &Server{
		files:      make(map[string]*File),
		maxSlots:   slots,
		credits:    make(map[string]int64),
		ClientName: "gx-ed2k",
		TCPPort:    4662,
		Idle:       60 * time.Second,
	}
}

// IndexDir hashes every regular file in dir and makes it available. Files
// above 4 GiB are skipped because the legacy part opcodes cannot address
// them; partial and hidden files are ignored.
func (s *Server) IndexDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".met") {
			continue
		}
		path := filepath.Join(dir, name)
		if err := s.AddFile(path); err != nil {
			continue
		}
	}
	return nil
}

// AddFile hashes one file and adds it to the index.
func (s *Server) AddFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > math.MaxUint32 {
		return errors.New("upload: unsupported file")
	}
	hashes, err := partfile.HashFile(path)
	if err != nil {
		return err
	}
	file := &File{
		Name: filepath.Base(path), Path: path, Size: info.Size(),
		Hash: hashes.File, Parts: hashes.Parts,
	}
	s.mu.Lock()
	s.files[file.Hash.Hex()] = file
	s.mu.Unlock()
	return nil
}

// SetCreditPath enables persistence of uploaded-bytes credits.
func (s *Server) SetCreditPath(path string) {
	s.queueMu.Lock()
	s.creditPath = path
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &s.credits)
	}
	s.queueMu.Unlock()
}

// Len is the number of shared files.
func (s *Server) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.files)
}

// serveSecIdent advances the handshake and returns the replies to send. A
// signature that does not verify leaves the peer unverified but keeps the
// connection usable.
func serveSecIdent(ident *peer.SecIdent, opcode byte, payload []byte) ([]peer.Packet, error) {
	switch opcode {
	case peer.OpSecIdentState:
		return ident.HandleState(payload)
	case peer.OpPublicKey:
		return ident.HandlePublicKey(payload)
	case peer.OpSignature:
		_, err := ident.HandleSignature(payload)
		return nil, err
	}
	return nil, nil
}

func (s *Server) lookup(hash ed2k.ID) *File {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.files[hash.Hex()]
}

// Handle serves one incoming client connection. The caller owns the conn and
// this method closes it.
func (s *Server) Handle(conn net.Conn) {
	defer conn.Close()
	client := peer.NewConn(conn)
	_ = client.SetDeadline(time.Now().Add(30 * time.Second))

	opcode, payload, err := client.Receive()
	if err != nil || opcode != peer.OpHello {
		return
	}
	hello, err := peer.DecodeHelloRequest(payload)
	if err != nil || hello.UserHash == s.UserHash {
		return
	}
	var ident *peer.SecIdent
	if s.SecIdent != nil && hello.MiscOption&0x000F0000 != 0 {
		ident = peer.NewSecIdent(s.SecIdent)
		var localIP, peerIP net.IP
		if tcp, ok := conn.LocalAddr().(*net.TCPAddr); ok {
			localIP = tcp.IP
		}
		if tcp, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
			peerIP = tcp.IP
		}
		ident.SetPeerAddresses(localIP, peerIP)
	}
	answer := peer.Hello{
		UserHash: s.UserHash, Port: s.TCPPort, Name: s.ClientName,
		Version: ed2k.KadProtocolVersion, SecIdent: ident != nil,
	}
	if err := client.Send(peer.OpHelloAnswer, peer.EncodeHello(answer)); err != nil {
		return
	}
	_ = client.SendExtended(peer.OpEmuleInfo, peer.EncodeMuleInfo())
	if ident != nil {
		if packet, err := ident.Start(); err == nil {
			_ = client.SendExtended(packet.Opcode, packet.Payload)
		}
	}
	acquired := false
	defer func() {
		if acquired {
			s.release()
		}
	}()

	for {
		_ = client.SetDeadline(time.Now().Add(s.Idle))
		opcode, payload, err := client.Receive()
		if err != nil {
			return
		}
		switch opcode {
		case peer.OpSecIdentState, peer.OpPublicKey, peer.OpSignature:
			if ident == nil {
				continue
			}
			packets, err := serveSecIdent(ident, opcode, payload)
			if err != nil {
				return
			}
			for _, packet := range packets {
				if err := client.SendExtended(packet.Opcode, packet.Payload); err != nil {
					return
				}
			}
		case peer.OpRequestFileName:
			request, err := peer.DecodeFileRequest(payload)
			if err != nil {
				return
			}
			file := s.lookup(request.Hash)
			if file == nil {
				_ = client.Send(peer.OpFileNoSuchFile, request.Hash[:])
				continue
			}
			raw := peer.EncodeFileNameAnswer(peer.FileNameAnswer{Hash: file.Hash, Name: file.Name})
			if err := client.Send(peer.OpFileNameAnswer, raw); err != nil {
				return
			}
		case peer.OpSetReqFileID:
			request, err := peer.DecodeFileRequest(payload)
			if err != nil {
				return
			}
			file := s.lookup(request.Hash)
			if file == nil {
				_ = client.Send(peer.OpFileNoSuchFile, request.Hash[:])
				continue
			}
			status, err := fileStatus(file)
			if err != nil {
				return
			}
			if err := client.Send(peer.OpFileStatus, status); err != nil {
				return
			}
		case peer.OpHashsetRequest:
			request, err := peer.DecodeHashsetRequest(payload)
			if err != nil {
				return
			}
			file := s.lookup(request.Hash)
			if file == nil {
				_ = client.Send(peer.OpFileNoSuchFile, request.Hash[:])
				continue
			}
			raw, err := peer.EncodeHashsetAnswer(peer.HashsetAnswer{Hash: file.Hash, Hashes: file.Parts})
			if err != nil {
				return
			}
			if err := client.Send(peer.OpHashsetAnswer, raw); err != nil {
				return
			}
		case peer.OpAICHFileHashReq:
			hash, err := peer.DecodeAICHFileHashRequest(payload)
			if err != nil {
				return
			}
			file := s.lookup(hash)
			if file == nil {
				continue
			}
			tree, err := file.aich()
			if err != nil {
				continue
			}
			raw := peer.EncodeAICHFileHashAnswer(peer.AICHMasterHash{Hash: file.Hash, Root: [20]byte(tree.RootHash())})
			if err := client.Send(peer.OpAICHFileHashAns, raw); err != nil {
				return
			}
		case peer.OpAICHRequest:
			request, err := peer.DecodeAICHRequest(payload)
			if err != nil {
				return
			}
			file := s.lookup(request.Hash)
			if file == nil {
				continue
			}
			tree, err := file.aich()
			if err != nil || [20]byte(tree.RootHash()) != request.Root {
				continue
			}
			recovery, err := tree.RecoveryData(int(request.Part))
			if err != nil {
				continue
			}
			raw := peer.EncodeAICHAnswer(peer.AICHAnswer{
				Hash: file.Hash, Part: request.Part, Root: request.Root, Data: recovery,
			})
			if err := client.Send(peer.OpAICHAnswer, raw); err != nil {
				return
			}
		case peer.OpStartUploadReq:
			request, err := peer.DecodeFileRequest(payload)
			if err != nil {
				return
			}
			if s.lookup(request.Hash) == nil {
				_ = client.Send(peer.OpFileNoSuchFile, request.Hash[:])
				continue
			}
			if !acquired {
				if !s.acquire(client, hello.UserHash) {
					return
				}
				acquired = true
			}
			if err := client.Send(peer.OpQueueRanking, peer.EncodeQueueRanking(peer.QueueRanking{Rank: 0})); err != nil {
				return
			}
			if err := client.Send(peer.OpAcceptUploadReq, nil); err != nil {
				return
			}
		case peer.OpRequestParts:
			if !acquired {
				continue
			}
			request, err := peer.DecodePartRequest(payload)
			if err != nil {
				return
			}
			sent, err := s.sendParts(client, request)
			if err != nil {
				return
			}
			if sent > 0 {
				s.addCredit(hello.UserHash, sent)
			}
		case peer.OpCancelTransfer, peer.OpEndOfDownload:
			return
		}
	}
}

// acquire grants an upload slot or waits in the credit-ordered queue.
func (s *Server) acquire(client *peer.Conn, hash ed2k.ID) bool {
	s.queueMu.Lock()
	if s.active < s.maxSlots {
		s.active++
		s.queueMu.Unlock()
		return true
	}
	waiter := &waiter{hash: hash, ready: make(chan struct{}, 1), at: time.Now()}
	s.queue = append(s.queue, waiter)
	rank := len(s.queue)
	s.queueMu.Unlock()
	if err := client.Send(peer.OpQueueRanking, peer.EncodeQueueRanking(peer.QueueRanking{Rank: uint16(rank)})); err != nil {
		s.removeWaiter(waiter)
		return false
	}
	select {
	case <-waiter.ready:
		return true
	case <-time.After(5 * time.Minute):
		s.queueMu.Lock()
		for i, candidate := range s.queue {
			if candidate == waiter {
				s.queue = append(s.queue[:i], s.queue[i+1:]...)
				s.queueMu.Unlock()
				return false
			}
		}
		s.queueMu.Unlock()
		// The dispatcher granted the slot just as the timeout expired.
		return true
	}
}

func (s *Server) removeWaiter(waiter *waiter) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	for i, candidate := range s.queue {
		if candidate == waiter {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			return
		}
	}
}

func (s *Server) release() {
	s.queueMu.Lock()
	if s.active > 0 {
		s.active--
	}
	if len(s.queue) > 0 {
		sort.SliceStable(s.queue, func(i, j int) bool {
			left := s.credits[s.queue[i].hash.Hex()]
			right := s.credits[s.queue[j].hash.Hex()]
			if left == right {
				return s.queue[i].at.Before(s.queue[j].at)
			}
			return left > right
		})
		next := s.queue[0]
		s.queue = s.queue[1:]
		s.active++
		close(next.ready)
	}
	s.saveCreditsLocked()
	s.queueMu.Unlock()
}

func (s *Server) addCredit(hash ed2k.ID, bytes int64) {
	if bytes <= 0 {
		return
	}
	s.queueMu.Lock()
	s.credits[hash.Hex()] += bytes
	s.saveCreditsLocked()
	s.queueMu.Unlock()
}

func (s *Server) saveCreditsLocked() {
	if s.creditPath == "" {
		return
	}
	raw, err := json.Marshal(s.credits)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.creditPath), 0o755)
	tmp := s.creditPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err == nil {
		_ = os.Rename(tmp, s.creditPath)
	}
}

func fileStatus(file *File) ([]byte, error) {
	parts := int(file.Size/partfile.PartSize) + 1
	bits := make([]byte, (parts+7)/8)
	for i := range bits {
		bits[i] = 0xFF
	}
	if remainder := parts % 8; remainder != 0 {
		bits[len(bits)-1] = byte((1 << remainder) - 1)
	}
	return peer.EncodeFileStatus(peer.FileStatus{Hash: file.Hash, Parts: parts, Bits: bits})
}

func (s *Server) sendParts(client *peer.Conn, request peer.PartRequest) (int64, error) {
	file := s.lookup(request.Hash)
	if file == nil {
		return 0, nil
	}
	handle, err := os.Open(file.Path)
	if err != nil {
		return 0, err
	}
	defer handle.Close()
	var sent int64
	for i := 0; i < 3; i++ {
		start, end := int64(request.Starts[i]), int64(request.Ends[i])
		if end <= start {
			continue
		}
		if start < 0 || end > file.Size {
			return sent, fmt.Errorf("upload: range %d-%d out of file", start, end)
		}
		data := make([]byte, end-start)
		if _, err := handle.ReadAt(data, start); err != nil && !errors.Is(err, io.EOF) {
			return sent, err
		}
		raw, err := peer.EncodeSendingPart(peer.SendingPart{
			Hash: file.Hash, Start: uint32(start), End: uint32(end), Data: data,
		})
		if err != nil {
			return sent, err
		}
		if err := client.Send(peer.OpSendingPart, raw); err != nil {
			return sent, err
		}
		sent += int64(len(data))
	}
	return sent, nil
}
