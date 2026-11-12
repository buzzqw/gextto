package torrent

import (
	"strings"
	"sync/atomic"
)

// The client filter refuses peers whose client is on the operator's list:
// the peer ID prefix (checked at the handshake) or the client name of the
// BEP 10 extension handshake. It is meant for clients that only take
// (offline-download services and the like); the list is the operator's.

// DefaultClientFilter is the suggested list: peer ID prefixes and client
// names of clients known to download without ever uploading back.
var DefaultClientFilter = []string{"-XL", "-SD", "-XF", "-QD", "-BN", "Xunlei", "Thunder", "QQDownload", "Xfplay"}

// clientFilter is an immutable, lower-cased copy of the list.
type clientFilter struct {
	rules []string
}

// newClientFilter cleans a list: trimmed, lower-cased, without empties or
// duplicates. An empty list returns nil (the filter is off).
func newClientFilter(list []string) *clientFilter {
	seen := map[string]bool{}
	var rules []string
	for _, rule := range list {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" || seen[rule] {
			continue
		}
		seen[rule] = true
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return nil
	}
	return &clientFilter{rules: rules}
}

// matchPeerID reports a rule matching the printable start of a peer ID. A
// rule that starts with "-" is an Azureus-style prefix ("-XL" matches
// "-XL0012-…"); any other rule is looked for in the printable part.
func (f *clientFilter) matchPeerID(id [20]byte) (string, bool) {
	if f == nil {
		return "", false
	}
	printable := make([]byte, 0, 8)
	for _, b := range id[:8] {
		if b < 0x20 || b > 0x7e {
			break
		}
		printable = append(printable, b)
	}
	head := strings.ToLower(string(printable))
	for _, rule := range f.rules {
		if strings.HasPrefix(rule, "-") {
			if strings.HasPrefix(head, rule) {
				return rule, true
			}
			continue
		}
		if len(rule) >= 3 && strings.Contains(head, rule) {
			return rule, true
		}
	}
	return "", false
}

// matchClientName reports a rule found in the client name a peer announced.
func (f *clientFilter) matchClientName(name string) (string, bool) {
	if f == nil || name == "" {
		return "", false
	}
	name = strings.ToLower(name)
	for _, rule := range f.rules {
		if strings.HasPrefix(rule, "-") {
			continue
		}
		if strings.Contains(name, rule) {
			return rule, true
		}
	}
	return "", false
}

// clientFilterState holds the live filter and the refusal counter.
type clientFilterState struct {
	filter  atomic.Pointer[clientFilter]
	refused atomic.Int64
}

// SetClientFilter replaces the list of refused clients, live: new peers are
// checked against it, connected peers that match are dropped at their next
// handshake check. An empty list turns the filter off.
func (s *Session) SetClientFilter(list []string) {
	s.clientFilter.filter.Store(newClientFilter(list))
}

// ClientFilterRefused counts the peers refused by the client filter since
// the session started.
func (s *Session) ClientFilterRefused() int64 {
	return s.clientFilter.refused.Load()
}

// refuseClientByID reports whether the filter refuses this peer ID.
func (s *Session) refuseClientByID(id [20]byte) bool {
	if _, ok := s.clientFilter.filter.Load().matchPeerID(id); ok {
		s.clientFilter.refused.Add(1)
		return true
	}
	return false
}

// refuseClientByName reports whether the filter refuses this client name.
func (s *Session) refuseClientByName(name string) bool {
	if _, ok := s.clientFilter.filter.Load().matchClientName(name); ok {
		s.clientFilter.refused.Add(1)
		return true
	}
	return false
}
