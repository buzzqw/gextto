package ed2k

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// ParseNodesFile decodes an eMule/aMule `nodes.dat`. Versions 1 and 2 are
// supported; version 2 entries carry an extra Kad UDP key and verification
// flag, which are skipped because this client speaks in clear. Contacts with
// an invalid address or a Kad1 version are dropped.
//
// Layout:
//
//	uint32 count (0 on version >= 1)
//	uint32 version (1..3)
//	uint32 count
//	count × (id16, ip4, udp2, tcp2, version1 [, key4, verified1])
func ParseNodesFile(data []byte) ([]Contact, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("ed2k: nodes.dat too short")
	}
	r := newReader(data, binary.LittleEndian)
	first, err := r.u32()
	if err != nil {
		return nil, err
	}
	version := uint32(0)
	count := first
	if first == 0 {
		if r.remaining() < 8 {
			return nil, fmt.Errorf("ed2k: nodes.dat header truncated")
		}
		version, _ = r.u32()
		if version < 1 || version > 3 {
			return nil, fmt.Errorf("ed2k: unsupported nodes.dat version %d", version)
		}
		count, err = r.u32()
		if err != nil {
			return nil, err
		}
	}
	if count > 100000 {
		return nil, fmt.Errorf("ed2k: implausible nodes.dat contact count %d", count)
	}
	entrySize := 25
	if version >= 2 {
		entrySize += 9
	}
	if int(count)*entrySize > r.remaining() {
		return nil, fmt.Errorf("ed2k: nodes.dat declares %d contacts but only %d bytes remain", count, r.remaining())
	}
	contacts := make([]Contact, 0, count)
	for i := uint32(0); i < count; i++ {
		contact, err := r.contact()
		if err != nil {
			return nil, err
		}
		if version >= 2 {
			// Kad UDP key: 4-byte key + 4-byte IP, then the verified flag.
			if _, err := r.n(9); err != nil {
				return nil, err
			}
		}
		if contact.valid() {
			contacts = append(contacts, contact)
		}
	}
	return contacts, nil
}

// WriteNodesFile writes a version 2 nodes.dat. It exists so the daemon can
// keep the contacts it learned and bootstrap faster next time.
func WriteNodesFile(path string, contacts []Contact) error {
	if len(contacts) > 100000 {
		contacts = contacts[:100000]
	}
	w := newWriter(binary.LittleEndian)
	w.u32(0)
	w.u32(2)
	w.u32(uint32(len(contacts)))
	for _, contact := range contacts {
		w.contact(contact)
		w.u32(0) // Kad UDP key: none, we do not obfuscate
		w.u32(0) // key IP
		w.u8(0)  // not verified
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, w.buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// nodesFileFresh reports whether the cached contact file can be reused.
func nodesFileFresh(path string, maxAge time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return time.Since(info.ModTime()) < maxAge
}

// LoadNodesFile reads a nodes.dat, returning nil (not an error) when it is
// absent or unreadable: a missing cache only means a slower bootstrap.
func LoadNodesFile(path string) []Contact {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	contacts, err := ParseNodesFile(data)
	if err != nil {
		return nil
	}
	return contacts
}

// ParseServerMet decodes the server.met distribution format. The current
// format starts with a version byte (0xE0 or 0x0E) and a little-endian count,
// then each entry is IPv4, port, a 4-byte tag count and its tags. The older
// header-less format (a bare count followed by IPv4+port pairs) is still
// accepted.
func ParseServerMet(data []byte) ([]string, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("ed2k: server.met too short")
	}
	r := newReader(data, binary.LittleEndian)
	versioned := data[0] == 0xE0 || data[0] == 0x0E
	if versioned {
		if _, err := r.u8(); err != nil {
			return nil, err
		}
	}
	count, err := r.u32()
	if err != nil {
		return nil, err
	}
	if count > 10000 {
		return nil, fmt.Errorf("ed2k: implausible server.met count %d", count)
	}
	var servers []string
	for i := uint32(0); i < count; i++ {
		raw, err := r.n(6)
		if err != nil {
			return nil, err
		}
		ip := net.IPv4(raw[0], raw[1], raw[2], raw[3]).String()
		port := binary.LittleEndian.Uint16(raw[4:])
		tagCount := uint32(0)
		if versioned {
			tagCount, err = r.u32()
			if err != nil {
				return nil, err
			}
			if tagCount > 200 {
				return nil, fmt.Errorf("ed2k: implausible server tag count %d", tagCount)
			}
			for j := uint32(0); j < tagCount; j++ {
				if _, err := r.tag(); err != nil {
					return nil, err
				}
			}
		}
		if port == 0 {
			continue
		}
		servers = append(servers, net.JoinHostPort(ip, fmt.Sprintf("%d", port)))
	}
	return servers, nil
}

// looksLikeServerMet is a cheap sanity check used when a configured server
// list URL returns HTML instead of a server.met.
func looksLikeServerMet(data []byte) bool {
	if len(data) < 4 || len(data) > 4<<20 {
		return false
	}
	if _, err := ParseServerMet(data); err != nil {
		return false
	}
	return true
}
