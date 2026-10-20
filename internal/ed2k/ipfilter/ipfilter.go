// Package ipfilter blocks IPv4 ranges listed in an eMule/aMule ipfilter.dat.
// The file is the same one eMule and aMule read: "first - last , level , name",
// with plain ranges and CIDR also accepted. Ranges with a level of 128 or more
// are allow rules, matching the convention used by Gextto's torrent blocklist.
package ipfilter

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
)

// maxRanges bounds a hostile file. Real lists stay well below this.
const maxRanges = 1 << 20

// maxFileSize bounds Load. Public lists are a few MiB.
const maxFileSize = 64 << 20

// allowLevel is the eMule level from which a rule allows traffic.
const allowLevel = 128

// Filter is an immutable, merged set of blocked IPv4 ranges.
type Filter struct {
	ranges []interval
}

type interval struct {
	first uint32
	last  uint32
}

// Load reads an ipfilter.dat from path.
func Load(path string) (*Filter, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ipfilter: %w", err)
	}
	defer file.Close()
	if info, err := file.Stat(); err == nil && info.Size() > maxFileSize {
		return nil, fmt.Errorf("ipfilter: %s is larger than %d bytes", path, maxFileSize)
	}
	filter, err := Parse(io.LimitReader(file, maxFileSize))
	if err != nil {
		return nil, fmt.Errorf("ipfilter: %s: %w", path, err)
	}
	return filter, nil
}

// Parse reads the eMule/aMule ipfilter format from r.
func Parse(r io.Reader) (*Filter, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)

	var ranges []interval
	invalid := 0
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		entry, ok := parseLine(line)
		if !ok {
			invalid++
			continue
		}
		ranges = append(ranges, entry)
		if len(ranges) > maxRanges {
			return nil, errors.New("too many ranges")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(ranges) == 0 {
		if invalid > 0 {
			return nil, errors.New("no valid ranges")
		}
		return &Filter{}, nil
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].first == ranges[j].first {
			return ranges[i].last < ranges[j].last
		}
		return ranges[i].first < ranges[j].first
	})
	merged := ranges[:1]
	for _, entry := range ranges[1:] {
		last := &merged[len(merged)-1]
		// Merge overlaps and adjacent ranges.
		if entry.first <= last.last || (last.last != ^uint32(0) && entry.first == last.last+1) {
			if entry.last > last.last {
				last.last = entry.last
			}
			continue
		}
		merged = append(merged, entry)
	}
	return &Filter{ranges: merged}, nil
}

func parseLine(line string) (interval, bool) {
	if comma := strings.Index(line, ","); comma >= 0 {
		fields := strings.Split(line, ",")
		if len(fields) >= 2 {
			if level, err := strconv.Atoi(strings.TrimSpace(fields[1])); err == nil && level >= allowLevel {
				return interval{}, false
			}
		}
		line = strings.TrimSpace(fields[0])
	} else if strings.Contains(line, "-") && strings.Contains(line, ".") {
		// PeerGuardian: "Some name:1.2.3.0-1.2.3.255".
		if colon := strings.LastIndex(line, ":"); colon >= 0 {
			line = strings.TrimSpace(line[colon+1:])
		}
	}
	if left, right, ok := strings.Cut(line, "-"); ok {
		first := parseIPv4(left)
		last := parseIPv4(right)
		if first == nil || last == nil {
			return interval{}, false
		}
		start := binary.BigEndian.Uint32(first)
		end := binary.BigEndian.Uint32(last)
		if end < start {
			start, end = end, start
		}
		return interval{first: start, last: end}, true
	}
	_, network, err := net.ParseCIDR(line)
	if err != nil || len(network.IP) != 4 || len(network.Mask) != 4 {
		return interval{}, false
	}
	start := binary.BigEndian.Uint32(network.IP)
	return interval{first: start, last: start | ^binary.BigEndian.Uint32(network.Mask)}, true
}

// parseIPv4 accepts the zero-padded octets eMule writes in ipfilter.dat.
func parseIPv4(text string) net.IP {
	parts := strings.Split(strings.TrimSpace(text), ".")
	if len(parts) != 4 {
		return nil
	}
	ip := make(net.IP, 4)
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 {
			return nil
		}
		ip[i] = byte(value)
	}
	return ip
}

// Blocked reports whether ip falls in a blocked range. A nil filter allows
// everything, and IPv6 addresses are not filtered.
func (f *Filter) Blocked(ip net.IP) bool {
	if f == nil || len(f.ranges) == 0 {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	value := binary.BigEndian.Uint32(v4)
	index := sort.Search(len(f.ranges), func(i int) bool {
		return f.ranges[i].last >= value
	})
	return index < len(f.ranges) && f.ranges[index].first <= value
}

// Len returns the number of merged ranges, useful for health output.
func (f *Filter) Len() int {
	if f == nil {
		return 0
	}
	return len(f.ranges)
}
