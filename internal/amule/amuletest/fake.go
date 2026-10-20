// Package amuletest is a fake aMule core speaking the EC protocol, for tests
// that must not depend on amuled being installed. It keeps a download queue,
// a server list and a canned search result list, and answers the requests
// internal/amule sends with packets laid out like the real core's.
package amuletest

import (
	"bufio"
	"encoding/hex"
	"net"
	"net/url"
	"strings"
	"sync"

	"github.com/buzzqw/gextto/internal/amule"
)

// File is one download in the fake queue.
type File struct {
	Hash    string
	Name    string
	Size    int64
	Done    int64
	Speed   int64
	Status  int
	Sources int64
	// LastRecv is the Unix time of the last data received (0 = never).
	LastRecv int64
}

// Result is one canned search result.
type Result struct {
	Hash    string
	Name    string
	Size    int64
	Sources int64
}

// Core is the fake amuled.
type Core struct {
	Password string
	Incoming string

	mu        sync.Mutex
	listener  net.Listener
	downloads []File
	results   []Result
	searches  []string
	searchErr string
	connected bool
	kad       bool
	commands  []string
	conns     map[net.Conn]struct{}
	shared    []sharedFile
}

type sharedFile struct {
	File
	Dir string
}

// Start listens on a loopback port.
func Start(password string) (*Core, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	core := &Core{Password: password, Incoming: "/tmp/incoming", listener: listener, connected: true, kad: true}
	go core.serve()
	return core, nil
}

// Address is host:port of the EC listener.
func (c *Core) Address() string { return c.listener.Addr().String() }

// Close stops the listener and drops the open sessions, like an amuled that
// exits.
func (c *Core) Close() error {
	err := c.listener.Close()
	c.mu.Lock()
	for conn := range c.conns {
		_ = conn.Close()
	}
	c.mu.Unlock()
	return err
}

// SetResults sets the results of every following search; a non-empty
// refusal makes the core decline searches with that message.
func (c *Core) SetResults(results []Result, refusal string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results = results
	c.searchErr = refusal
}

// SetDownloads replaces the download queue.
func (c *Core) SetDownloads(files []File) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.downloads = append([]File(nil), files...)
}

// Finish completes a download the way aMule 3.x does: the file leaves the
// download queue and is shared from dir.
func (c *Core) Finish(hash, dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for index, file := range c.downloads {
		if file.Hash == hash {
			c.downloads = append(c.downloads[:index], c.downloads[index+1:]...)
			file.Done, file.Status = file.Size, amule.StatusComplete
			c.shared = append(c.shared, sharedFile{File: file, Dir: dir})
			return
		}
	}
}

// Downloads returns a copy of the queue.
func (c *Core) Downloads() []File {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]File(nil), c.downloads...)
}

// Searches returns the queries received, in order.
func (c *Core) Searches() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.searches...)
}

// Commands returns the connection commands received ("connect", "kad_stop"…).
func (c *Core) Commands() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.commands...)
}

func (c *Core) serve() {
	for {
		conn, err := c.listener.Accept()
		if err != nil {
			return
		}
		go c.session(conn)
	}
}

func (c *Core) session(conn net.Conn) {
	c.mu.Lock()
	if c.conns == nil {
		c.conns = map[net.Conn]struct{}{}
	}
	c.conns[conn] = struct{}{}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.conns, conn)
		c.mu.Unlock()
		_ = conn.Close()
	}()
	reader := bufio.NewReader(conn)
	authenticated := false
	const salt = 0x1234ABCD
	for {
		request, err := amule.ReadPacket(reader)
		if err != nil {
			return
		}
		var reply amule.Packet
		switch {
		case request.Op == amule.OpAuthReq:
			reply = amule.Packet{Op: amule.OpAuthSalt, Tags: []amule.Tag{amule.IntTag(amule.TagPasswdSalt, salt)}}
		case request.Op == amule.OpAuthPasswd:
			tag, _ := request.Tag(amule.TagPasswdHash)
			got, _ := tag.Hash()
			if got == amule.PasswordResponse(c.Password, salt) {
				authenticated = true
				reply = amule.Packet{Op: amule.OpAuthOK, Tags: []amule.Tag{amule.StringTag(amule.TagServerVer, "2.3.3")}}
			} else {
				reply = amule.Packet{Op: amule.OpAuthFail, Tags: []amule.Tag{amule.StringTag(amule.TagString, "Authentication failed: wrong password.")}}
			}
		case !authenticated:
			reply = amule.Packet{Op: amule.OpAuthFail}
		default:
			reply = c.answer(request)
		}
		raw, err := reply.Encode()
		if err != nil {
			return
		}
		if _, err := conn.Write(raw); err != nil {
			return
		}
	}
}

func hashBytes(value string) [16]byte {
	var out [16]byte
	raw, _ := hex.DecodeString(value)
	copy(out[:], raw)
	return out
}

func failed(message string) amule.Packet {
	return amule.Packet{Op: amule.OpFailed, Tags: []amule.Tag{amule.StringTag(amule.TagString, message)}}
}

func (c *Core) connState() amule.Tag {
	bits := uint64(0)
	if c.connected {
		bits |= amule.ConnEd2kConnected
	}
	if c.kad {
		bits |= amule.ConnKadConnected | amule.ConnKadRunning
	}
	tag := amule.IntTag(amule.TagConnState, bits)
	if c.connected {
		server := amule.Tag{Name: amule.TagServer, Type: amule.TypeIPv4, Data: []byte{10, 0, 0, 1, 0x12, 0x6D},
			Children: []amule.Tag{amule.StringTag(amule.TagServerName, "Fake Server")}}
		tag.Children = append(tag.Children, server, amule.IntTag(amule.TagEd2kID, 0x7F000001))
	}
	return tag
}

func (c *Core) answer(request amule.Packet) amule.Packet {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch request.Op {
	case amule.OpStatReq:
		return amule.Packet{Op: amule.OpStats, Tags: []amule.Tag{
			amule.IntTag(amule.TagStatsDLSpeed, 2048),
			amule.IntTag(amule.TagStatsULSpeed, 512),
			amule.IntTag(amule.TagStatsKadNodes, 120),
			c.connState(),
		}}
	case amule.OpGetConnState:
		return amule.Packet{Op: amule.OpMiscData, Tags: []amule.Tag{c.connState()}}
	case amule.OpGetServerList:
		return amule.Packet{Op: amule.OpServerList, Tags: []amule.Tag{
			{Name: amule.TagServer, Type: amule.TypeIPv4, Data: []byte{10, 0, 0, 1, 0x12, 0x6D}, Children: []amule.Tag{
				amule.StringTag(amule.TagServerName, "Fake Server"),
				amule.IntTag(amule.TagServerUsers, 5000),
				amule.IntTag(amule.TagServerFiles, 900000),
			}},
		}}
	case amule.OpConnect:
		c.commands = append(c.commands, "connect")
		c.connected, c.kad = true, true
		return amule.Packet{Op: amule.OpStrings}
	case amule.OpDisconnect:
		c.commands = append(c.commands, "disconnect")
		c.connected, c.kad = false, false
		return amule.Packet{Op: amule.OpStrings}
	case amule.OpKadStart:
		c.commands = append(c.commands, "kad_start")
		c.kad = true
		return amule.Packet{Op: amule.OpNoop}
	case amule.OpKadStop:
		c.commands = append(c.commands, "kad_stop")
		c.kad = false
		return amule.Packet{Op: amule.OpNoop}
	case amule.OpServerConnect:
		c.commands = append(c.commands, "server_connect")
		c.connected = true
		return amule.Packet{Op: amule.OpNoop}
	case amule.OpServerDisconnect:
		c.commands = append(c.commands, "server_disconnect")
		c.connected = false
		return amule.Packet{Op: amule.OpNoop}
	case amule.OpServerUpdateFromURL, amule.OpKadUpdateFromURL:
		c.commands = append(c.commands, "update")
		return amule.Packet{Op: amule.OpNoop}
	case amule.OpGetPreferences:
		return amule.Packet{Op: amule.OpSetPreferences, Tags: []amule.Tag{
			amule.EmptyTag(amule.TagPrefsDirectories, amule.StringTag(amule.TagDirectoriesIncoming, c.Incoming)),
		}}
	case amule.OpAddLink:
		for _, tag := range request.Tags {
			link := tag.String()
			parts := strings.Split(link, "|")
			if len(parts) < 5 || parts[1] != "file" {
				return failed("Invalid link or already on list.")
			}
			hash := strings.ToLower(parts[4])
			for _, file := range c.downloads {
				if file.Hash == hash {
					return failed("Invalid link or already on list.")
				}
			}
			var size int64
			for _, r := range parts[3] {
				size = size*10 + int64(r-'0')
			}
			name, err := url.PathUnescape(parts[2])
			if err != nil {
				name = parts[2]
			}
			c.downloads = append(c.downloads, File{Hash: hash, Name: name, Size: size, Status: amule.StatusEmpty})
		}
		return amule.Packet{Op: amule.OpNoop}
	case amule.OpGetDloadQueue:
		reply := amule.Packet{Op: amule.OpDloadQueue}
		for _, file := range c.downloads {
			reply.Tags = append(reply.Tags, amule.IntTag(amule.TagPartfile, 1,
				amule.HashTag(amule.TagPartfileHash, hashBytes(file.Hash)),
				amule.StringTag(amule.TagPartfileName, file.Name),
				amule.IntTag(amule.TagPartfileSizeFull, uint64(file.Size)),
				amule.IntTag(amule.TagPartfileSizeDone, uint64(file.Done)),
				amule.IntTag(amule.TagPartfileSpeed, uint64(file.Speed)),
				amule.IntTag(amule.TagPartfileStatus, uint64(file.Status)),
				amule.IntTag(amule.TagPartfileSourceCount, uint64(file.Sources)),
				amule.IntTag(amule.TagPartfileLastRecv, uint64(file.LastRecv)),
			))
		}
		return reply
	case amule.OpGetSharedFiles:
		reply := amule.Packet{Op: amule.OpSharedFiles}
		for _, file := range c.shared {
			reply.Tags = append(reply.Tags, amule.IntTag(amule.TagKnownfile, 1,
				amule.StringTag(amule.TagPartfileName, file.Name),
				amule.HashTag(amule.TagPartfileHash, hashBytes(file.Hash)),
				amule.StringTag(amule.TagKnownfileFilename, file.Dir),
				amule.IntTag(amule.TagPartfileSizeFull, uint64(file.Size)),
			))
		}
		return reply
	case amule.OpPartfilePause, amule.OpPartfileResume, amule.OpPartfileDelete:
		tag, _ := request.Tag(amule.TagPartfile)
		value, _ := tag.Hash()
		hash := hex.EncodeToString(value[:])
		for index, file := range c.downloads {
			if file.Hash != hash {
				continue
			}
			switch request.Op {
			case amule.OpPartfilePause:
				c.downloads[index].Status = amule.StatusPaused
			case amule.OpPartfileResume:
				c.downloads[index].Status = amule.StatusReady
			default:
				c.downloads = append(c.downloads[:index], c.downloads[index+1:]...)
			}
			return amule.Packet{Op: amule.OpNoop}
		}
		return failed("FileHash not found: " + hash)
	case amule.OpClearCompleted:
		kept := c.downloads[:0]
		for _, file := range c.downloads {
			if file.Status != amule.StatusComplete {
				kept = append(kept, file)
			}
		}
		c.downloads = kept
		return amule.Packet{Op: amule.OpNoop}
	case amule.OpSearchStart:
		if c.searchErr != "" {
			return failed(c.searchErr)
		}
		tag, _ := request.Tag(amule.TagSearchType)
		c.searches = append(c.searches, tag.ChildString(amule.TagSearchName))
		return amule.Packet{Op: amule.OpStrings, Tags: []amule.Tag{amule.StringTag(amule.TagString, "Search in progress.")}}
	case amule.OpSearchProgress:
		return amule.Packet{Op: amule.OpSearchProgress, Tags: []amule.Tag{amule.IntTag(amule.TagSearchStatus, 0xfffe)}}
	case amule.OpSearchResults:
		reply := amule.Packet{Op: amule.OpSearchResults}
		for _, result := range c.results {
			reply.Tags = append(reply.Tags, amule.IntTag(amule.TagSearchFile, 1,
				amule.IntTag(amule.TagPartfileSourceCount, uint64(result.Sources)),
				amule.StringTag(amule.TagPartfileName, result.Name),
				amule.IntTag(amule.TagPartfileSizeFull, uint64(result.Size)),
				amule.HashTag(amule.TagPartfileHash, hashBytes(result.Hash)),
			))
		}
		return reply
	case amule.OpSearchStop:
		return amule.Packet{Op: amule.OpMiscData}
	}
	return failed("unsupported opcode")
}
