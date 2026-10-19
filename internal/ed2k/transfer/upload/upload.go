// Package upload serves completed eD2k files to other peers over the eMule
// client protocol. It is the minimal upload plane: no queue, no credits, no
// sharing of partial files. Its purpose is to keep the client from behaving
// as a pure leech and to make the advertised HighID useful.
package upload

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buzzqw/gextto/internal/ed2k"
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
}

// Server indexes completed files and answers client connections.
type Server struct {
	mu    sync.RWMutex
	files map[string]*File
	slots chan struct{}

	ClientName string
	UserHash   ed2k.ID
	TCPPort    uint16
	Idle       time.Duration
}

// New returns a server with the given number of concurrent upload slots.
func New(slots int) *Server {
	if slots <= 0 {
		slots = 1
	}
	return &Server{
		files:      make(map[string]*File),
		slots:      make(chan struct{}, slots),
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

// Len is the number of shared files.
func (s *Server) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.files)
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
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return
	}
	client := peer.NewConn(conn)
	_ = client.SetDeadline(time.Now().Add(30 * time.Second))

	opcode, payload, err := client.Receive()
	if err != nil || opcode != peer.OpHello {
		return
	}
	hello, err := peer.DecodeHello(payload)
	if err != nil || hello.UserHash == s.UserHash {
		return
	}
	answer := peer.Hello{
		UserHash: s.UserHash, Port: s.TCPPort, Name: s.ClientName,
		Version: ed2k.KadProtocolVersion,
	}
	if err := client.Send(peer.OpHelloAnswer, peer.EncodeHello(answer)); err != nil {
		return
	}

	for {
		_ = client.SetDeadline(time.Now().Add(s.Idle))
		opcode, payload, err := client.Receive()
		if err != nil {
			return
		}
		switch opcode {
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
		case peer.OpStartUploadReq:
			request, err := peer.DecodeFileRequest(payload)
			if err != nil {
				return
			}
			if s.lookup(request.Hash) == nil {
				_ = client.Send(peer.OpFileNoSuchFile, request.Hash[:])
				continue
			}
			if err := client.Send(peer.OpQueueRanking, peer.EncodeQueueRanking(peer.QueueRanking{Rank: 0})); err != nil {
				return
			}
			if err := client.Send(peer.OpAcceptUploadReq, nil); err != nil {
				return
			}
		case peer.OpRequestParts:
			request, err := peer.DecodePartRequest(payload)
			if err != nil {
				return
			}
			if err := s.sendParts(client, request); err != nil {
				return
			}
		case peer.OpCancelTransfer, peer.OpEndOfDownload:
			return
		}
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

func (s *Server) sendParts(client *peer.Conn, request peer.PartRequest) error {
	file := s.lookup(request.Hash)
	if file == nil {
		return nil
	}
	handle, err := os.Open(file.Path)
	if err != nil {
		return err
	}
	defer handle.Close()
	for i := 0; i < 3; i++ {
		start, end := int64(request.Starts[i]), int64(request.Ends[i])
		if end <= start {
			continue
		}
		if start < 0 || end > file.Size {
			return fmt.Errorf("upload: range %d-%d out of file", start, end)
		}
		data := make([]byte, end-start)
		if _, err := handle.ReadAt(data, start); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		raw, err := peer.EncodeSendingPart(peer.SendingPart{
			Hash: file.Hash, Start: uint32(start), End: uint32(end), Data: data,
		})
		if err != nil {
			return err
		}
		if err := client.Send(peer.OpSendingPart, raw); err != nil {
			return err
		}
	}
	return nil
}
