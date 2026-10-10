package blocklist

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/buzzqw/gextto/internal/engine/internal/blocklist/stree"
)

var errNotIPv4Address = errors.New("address is not ipv4")

var errAllowRule = errors.New("allow rule")

// Blocklist holds a list of IP ranges in a Segment Tree structure for faster lookups.
type Blocklist struct {
	logger Logger

	tree  stree.Stree
	m     sync.RWMutex
	count int
}

// Logger prints error messages during loading. Arguments are handled in the manner of fmt.Printf.
type Logger func(format string, v ...any)

// New returns a new Blocklist.
func New() *Blocklist {
	return NewLogger(nil)
}

// NewLogger returns a new Blocklist with a logger that prints error messages during loading.
func NewLogger(logger Logger) *Blocklist {
	return &Blocklist{logger: logger}
}

// Len returns the number of rules in the Blocklist.
func (b *Blocklist) Len() int {
	b.m.RLock()
	defer b.m.RUnlock()
	return b.count
}

// Blocked returns true if ip is in Blocklist.
func (b *Blocklist) Blocked(ip net.IP) bool {
	b.m.RLock()
	defer b.m.RUnlock()

	ip = ip.To4()
	if ip == nil {
		return false
	}

	val := binary.BigEndian.Uint32(ip)
	return b.tree.Contains(stree.ValueType(val))
}

// Reload the segment tree by reading new rules from a io.Reader.
func (b *Blocklist) Reload(r io.Reader) (int, error) {
	b.m.Lock()
	defer b.m.Unlock()

	tree, n, err := load(r, b.logger)
	if err != nil {
		return n, err
	}

	b.tree = *tree
	b.count = n
	return n, nil
}

func load(r io.Reader, logger Logger) (*stree.Stree, int, error) {
	var tree stree.Stree
	var n int
	var hasError bool
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		l := bytes.TrimSpace(scanner.Bytes())
		if len(l) == 0 {
			continue
		}
		if l[0] == '#' {
			continue
		}
		r, err := parseCIDR(l)
		if err == errAllowRule || err == errNotIPv4Address {
			continue
		}
		if err != nil {
			hasError = true
			if logger != nil {
				logger("cannot parse blocklist line (%q): %q", string(l), err.Error())
			}
			continue
		}
		tree.AddRange(stree.ValueType(r.first), stree.ValueType(r.last))
		n++
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	if n == 0 && hasError {
		// Probably we couln't decode the stream correctly.
		// At least one line must be correct before we consider the load operation as successful.
		return nil, 0, errors.New("no valid rules")
	}
	tree.Build()
	return &tree, n, nil
}

type ipRange struct {
	first, last uint32
}

// parseCIDR parses one rule. Besides CIDR (upstream), the gextto fork accepts
// the formats Gextto uses for libtorrent:
//
//	1.2.3.0-1.2.3.255              plain range
//	Some name:1.2.3.0-1.2.3.255    P2P / PeerGuardian
//	001.002.003.000 - 001.002.003.255 , 000 , name   eMule ipfilter.dat
//
// IPv6 rules are skipped (rain blocks IPv4 only).
func parseCIDR(b []byte) (r ipRange, err error) {
	line := string(b)
	if !strings.Contains(line, "-") {
		_, ipnet, perr := net.ParseCIDR(line)
		if perr != nil {
			err = perr
			return
		}
		if len(ipnet.IP) != 4 || len(ipnet.Mask) != 4 {
			err = errNotIPv4Address
			return
		}
		r.first = binary.BigEndian.Uint32(ipnet.IP)
		r.last = r.first | ^binary.BigEndian.Uint32(ipnet.Mask)
		return
	}
	if comma := strings.Index(line, ","); comma >= 0 {
		// eMule: "first - last , level , description". Level >= 128 allows.
		fields := strings.Split(line, ",")
		if len(fields) >= 2 {
			if level, lerr := strconv.Atoi(strings.TrimSpace(fields[1])); lerr == nil && level >= 128 {
				err = errAllowRule
				return
			}
		}
		line = fields[0]
	} else if colon := strings.LastIndex(line, ":"); colon >= 0 {
		line = line[colon+1:]
	}
	left, right, _ := strings.Cut(line, "-")
	first := parseIPv4(left)
	last := parseIPv4(right)
	if first == nil || last == nil {
		err = errNotIPv4Address
		return
	}
	r.first = binary.BigEndian.Uint32(first)
	r.last = binary.BigEndian.Uint32(last)
	if r.last < r.first {
		r.first, r.last = r.last, r.first
	}
	return
}

// parseIPv4 accepts zero-padded octets (eMule writes 001.002.003.004).
func parseIPv4(s string) net.IP {
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) != 4 {
		return nil
	}
	ip := make(net.IP, 4)
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return nil
		}
		ip[i] = byte(n)
	}
	return ip
}
