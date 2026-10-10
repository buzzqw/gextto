package blocklist

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/buzzqw/gextto/internal/gxcore/internal/blocklist/stree"
)

var errNotIPv4Address = errors.New("address is not ipv4")

var errAllowRule = errors.New("allow rule")

// Blocklist holds a list of IP ranges in a Segment Tree structure for faster lookups.
type Blocklist struct {
	logger Logger

	tree  stree.Stree
	v6    []ip6Range
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

	if ip4 := ip.To4(); ip4 != nil {
		val := binary.BigEndian.Uint32(ip4)
		return b.tree.Contains(stree.ValueType(val))
	}
	if ip16 := ip.To16(); ip16 != nil {
		return v6Contains(b.v6, ip16)
	}
	return false
}

// Reload the segment tree by reading new rules from a io.Reader.
func (b *Blocklist) Reload(r io.Reader) (int, error) {
	b.m.Lock()
	defer b.m.Unlock()

	tree, v6, nv4, nv6, err := load(r, b.logger)
	if err != nil {
		return nv4 + nv6, err
	}

	b.tree = *tree
	b.v6 = v6
	b.count = nv4 + nv6
	return b.count, nil
}

func load(r io.Reader, logger Logger) (*stree.Stree, []ip6Range, int, int, error) {
	var tree stree.Stree
	var v6 []ip6Range
	var nv4, nv6 int
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
		r4, err := parseCIDR(l)
		if err == errAllowRule {
			continue
		}
		if err == errNotIPv4Address {
			// Upstream skipped IPv6 rules entirely; the engine keeps the
			// IPv6 CIDR forms in a separate interval list.
			if r6, ok := parseV6CIDR(l); ok {
				v6 = append(v6, r6)
				nv6++
			}
			continue
		}
		if err != nil {
			hasError = true
			if logger != nil {
				logger("cannot parse blocklist line (%q): %q", string(l), err.Error())
			}
			continue
		}
		tree.AddRange(stree.ValueType(r4.first), stree.ValueType(r4.last))
		nv4++
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, 0, 0, err
	}
	if nv4+nv6 == 0 && hasError {
		// Probably we couln't decode the stream correctly.
		// At least one line must be correct before we consider the load operation as successful.
		return nil, nil, 0, 0, errors.New("no valid rules")
	}
	tree.Build()
	return &tree, sortMergeV6(v6), nv4, nv6, nil
}

type ipRange struct {
	first, last uint32
}

// parseCIDR parses one rule. Besides CIDR (upstream), the engine accepts
// the formats Gextto uses for libtorrent:
//
//	1.2.3.0-1.2.3.255              plain range
//	Some name:1.2.3.0-1.2.3.255    P2P / PeerGuardian
//	001.002.003.000 - 001.002.003.255 , 000 , name   eMule ipfilter.dat
//
// IPv4 rules and the formats below; IPv6 CIDR rules go to a separate list.
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

// ip6Range is an inclusive IPv6 range (F4 IPv6).
type ip6Range struct {
	first, last [net.IPv6len]byte
}

// parseV6CIDR parses an IPv6 CIDR rule. Only the CIDR form is accepted; the
// range and P2P/eMule forms are IPv4-only.
func parseV6CIDR(b []byte) (ip6Range, bool) {
	line := string(b)
	if strings.Contains(line, "-") {
		return ip6Range{}, false
	}
	_, ipnet, err := net.ParseCIDR(line)
	if err != nil || ipnet.IP.To4() != nil || len(ipnet.Mask) != net.IPv6len {
		return ip6Range{}, false
	}
	first := ipnet.IP.To16()
	if first == nil {
		return ip6Range{}, false
	}
	var r ip6Range
	copy(r.first[:], first)
	for i := 0; i < net.IPv6len; i++ {
		r.last[i] = first[i] | ^ipnet.Mask[i]
	}
	return r, true
}

// sortMergeV6 sorts the ranges and merges the overlapping ones, so a lookup is
// a single binary search.
func sortMergeV6(rs []ip6Range) []ip6Range {
	if len(rs) == 0 {
		return nil
	}
	slices.SortFunc(rs, func(a, b ip6Range) int { return bytes.Compare(a.first[:], b.first[:]) })
	out := rs[:1]
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if bytes.Compare(r.first[:], last.last[:]) <= 0 {
			if bytes.Compare(r.last[:], last.last[:]) > 0 {
				last.last = r.last
			}
			continue
		}
		out = append(out, r)
	}
	return out
}

// v6Contains reports whether ip16 falls in any merged IPv6 range.
func v6Contains(rs []ip6Range, ip16 net.IP) bool {
	if len(rs) == 0 || len(ip16) != net.IPv6len {
		return false
	}
	var key [net.IPv6len]byte
	copy(key[:], ip16)
	i := sort.Search(len(rs), func(i int) bool { return bytes.Compare(rs[i].first[:], key[:]) > 0 }) - 1
	if i < 0 {
		return false
	}
	return bytes.Compare(key[:], rs[i].last[:]) <= 0
}
