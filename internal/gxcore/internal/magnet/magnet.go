// Package magnet parses and renders magnet links: v1 (BEP 5) and v2/BEP 52
// topics, BEP 53 file selection and webseeds, BEP 46 DHT-resolved keys,
// tracker tiers and peer hints.
package magnet

import (
	"cmp"
	"crypto/ed25519"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Magnet holds everything a link carries to fetch torrent metadata.
type Magnet struct {
	InfoHash [20]byte
	Name     string
	Trackers [][]string
	Peers    []string
	// HasV1 marks a "urn:btih:" topic. On v2-only links InfoHash holds the
	// truncated SHA-256 of the btmh topic instead.
	HasV1 bool
	// V2InfoHash is the SHA-256 info hash of a "urn:btmh:" topic (BEP 52),
	// on hybrid and v2-only links alike.
	V2InfoHash [32]byte
	HasV2      bool
	// SelectOnly lists the wanted file indices from BEP 53 "so", sorted and
	// de-duplicated. Empty means everything. Indices count non-padding
	// files, like Config.FileSelection.
	SelectOnly []int
	// WebSeeds holds the BEP 53 "ws" HTTP URLs.
	WebSeeds []string
	// DHTNodes holds "dht" bootstrap nodes (host:port).
	DHTNodes []string
	// ExactSources ("xs") point at the exact .torrent; AcceptableSources
	// ("as") at interchangeable sources. The engine fetches metainfo from
	// them before involving peers or the DHT.
	ExactSources      []string
	AcceptableSources []string
	// BTPKPublicKey is the ed25519 key of a BEP 46 "xs=urn:btpk:" value: the
	// link carries no "xt" and the info hash resolves from the DHT mutable
	// item at add time. BTPKSalt is the optional "s" salt.
	BTPKPublicKey []byte
	BTPKSalt      []byte
}

// New parses s into a Magnet, rejecting non-magnet schemes, links without
// an xt (unless BEP 46 keyed), and xt topics that are neither btih nor btmh.
func New(s string) (*Magnet, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "magnet" {
		return nil, errors.New("not a magnet link")
	}
	params := u.Query()

	var magnet Magnet
	xts := params["xt"]
	magnet.BTPKPublicKey, magnet.BTPKSalt = parseBTPK(params["xs"], params["s"])
	if len(xts) == 0 && len(magnet.BTPKPublicKey) == 0 {
		return nil, errors.New("missing xt param")
	}
	if err := magnet.adoptTopics(xts); err != nil {
		return nil, err
	}
	if names := params["dn"]; len(names) != 0 {
		magnet.Name = names[0]
	}
	magnet.Trackers = parseTiers(params)
	magnet.Peers = params["x.pe"]
	magnet.SelectOnly = parseSelectOnly(params["so"])
	for _, ws := range params["ws"] {
		if ws = strings.TrimSpace(ws); ws != "" {
			magnet.WebSeeds = append(magnet.WebSeeds, ws)
		}
	}
	for _, node := range params["dht"] {
		if node = strings.TrimSpace(node); node != "" {
			magnet.DHTNodes = append(magnet.DHTNodes, node)
		}
	}
	magnet.ExactSources = cleanSourceList(params["xs"])
	magnet.AcceptableSources = cleanSourceList(params["as"])
	return &magnet, nil
}

// adoptTopics resolves the xt topics into the v1/v2 identities: a btih
// topic (anywhere in a hybrid link) wins outright; a lone btmh topic lends
// its first 20 digest bytes as the wire identity; a bare BTPK key resolves
// later from the DHT.
func (m *Magnet) adoptTopics(xts []string) error {
	var h [20]byte
	ok := false
	var err error
	if len(xts) > 0 {
		if h, ok, err = parseInfoHash(xts); err != nil {
			return err
		}
	}
	if ok {
		m.InfoHash = h
		m.HasV1 = true
	}
	m.V2InfoHash, m.HasV2 = parseV2InfoHash(xts)
	if !m.HasV1 {
		switch {
		case m.HasV2:
			copy(m.InfoHash[:], m.V2InfoHash[:20])
		case len(m.BTPKPublicKey) > 0:
		default:
			return errors.New("invalid xt param: must start with \"urn:btih:\" or \"urn:btmh:\"")
		}
	}
	return nil
}

// trackerTier stages one tier with its sort position.
type trackerTier struct {
	trackers []string
	index    int
}

// parseTiers orders the "tr" (bare, in arrival order) and "tr.N" tracker
// parameters into tiers by their numeric position.
func parseTiers(params url.Values) [][]string {
	var tiers []trackerTier
	for key, tier := range params {
		switch {
		case key == "tr":
			for i, tr := range tier {
				tiers = append(tiers, trackerTier{trackers: []string{tr}, index: i - len(tier)})
			}
		case strings.HasPrefix(key, "tr."):
			if index, err := strconv.Atoi(key[3:]); err == nil && index >= 0 {
				tiers = append(tiers, trackerTier{trackers: tier, index: index})
			}
		}
	}
	slices.SortFunc(tiers, func(a, b trackerTier) int { return cmp.Compare(a.index, b.index) })
	out := make([][]string, len(tiers))
	for i, ti := range tiers {
		out[i] = ti.trackers
	}
	return out
}

// cleanSourceList trims a source list, dropping blanks.
func cleanSourceList(values []string) []string {
	var out []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// maxBTPKSalt mirrors BEP 44's 64-byte salt ceiling.
const maxBTPKSalt = 64

// parseBTPK reads a BEP 46 "xs=urn:btpk:<key hex>" value with the optional
// "s" salt (hex). Nil key means no usable urn:btpk was present.
func parseBTPK(xs, salts []string) (ed25519.PublicKey, []byte) {
	var pub ed25519.PublicKey
	for _, x := range xs {
		s, ok := strings.CutPrefix(strings.TrimSpace(x), "urn:btpk:")
		if !ok {
			continue
		}
		b, err := hex.DecodeString(strings.TrimSpace(s))
		if err != nil || len(b) != ed25519.PublicKeySize {
			continue
		}
		pub = b
		break
	}
	if pub == nil {
		return nil, nil
	}
	for _, s := range salts {
		b, err := hex.DecodeString(strings.TrimSpace(s))
		if err != nil || len(b) > maxBTPKSalt {
			continue
		}
		return pub, b
	}
	return pub, nil
}

// HasBTPK reports a BEP 46 keyed link, whose info hash resolves from the
// DHT before adding.
func (m *Magnet) HasBTPK() bool { return len(m.BTPKPublicKey) > 0 }

// MetadataSources lists the HTTP(S) URLs (exact first, then acceptable),
// de-duplicated in order, for fetching the .torrent directly. Non-HTTP
// values (nested magnets and the like) are ignored.
func (m *Magnet) MetadataSources() []string {
	seen := make(map[string]struct{}, len(m.ExactSources)+len(m.AcceptableSources))
	var out []string
	for _, src := range append(append([]string{}, m.ExactSources...), m.AcceptableSources...) {
		u, err := url.Parse(src)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		if _, ok := seen[src]; ok {
			continue
		}
		seen[src] = struct{}{}
		out = append(out, src)
	}
	return out
}

// String renders the canonical link: xt topics, name, tiers, peers, web
// seeds, DHT nodes, sources, salt, selection — in that order.
func (m *Magnet) String() string {
	var b strings.Builder
	b.Grow(2048)
	b.WriteString("magnet:?xt=")
	switch {
	case m.HasV1:
		b.WriteString("urn:btih:")
		b.WriteString(hex.EncodeToString(m.InfoHash[:]))
	case m.HasV2:
		b.WriteString("urn:btmh:1220")
		b.WriteString(hex.EncodeToString(m.V2InfoHash[:]))
	default:
		b.WriteString("urn:btih:")
		b.WriteString(hex.EncodeToString(m.InfoHash[:]))
	}
	if m.HasV1 && m.HasV2 {
		b.WriteString("&xt=urn:btmh:1220")
		b.WriteString(hex.EncodeToString(m.V2InfoHash[:]))
	}
	if m.Name != "" {
		b.WriteString("&dn=")
		b.WriteString(url.QueryEscape(m.Name))
	}
	for i, ti := range m.Trackers {
		if len(ti) == 1 {
			b.WriteString("&tr=")
			b.WriteString(url.QueryEscape(ti[0]))
		} else {
			for _, t := range ti {
				b.WriteString("&tr.")
				b.WriteString(strconv.Itoa(i))
				b.WriteString("=")
				b.WriteString(url.QueryEscape(t))
			}
		}
	}
	for _, p := range m.Peers {
		b.WriteString("&x.pe=")
		b.WriteString(p)
	}
	for _, ws := range m.WebSeeds {
		b.WriteString("&ws=")
		b.WriteString(url.QueryEscape(ws))
	}
	for _, node := range m.DHTNodes {
		b.WriteString("&dht=")
		b.WriteString(url.QueryEscape(node))
	}
	for _, src := range m.ExactSources {
		b.WriteString("&xs=")
		b.WriteString(url.QueryEscape(src))
	}
	for _, src := range m.AcceptableSources {
		b.WriteString("&as=")
		b.WriteString(url.QueryEscape(src))
	}
	if len(m.BTPKSalt) > 0 {
		b.WriteString("&s=")
		b.WriteString(hex.EncodeToString(m.BTPKSalt))
	}
	if len(m.SelectOnly) > 0 {
		b.WriteString("&so=")
		for i, idx := range m.SelectOnly {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Itoa(idx))
		}
	}
	return b.String()
}

// maxSelectOnlyIndex caps the file index "so" may name and maxSelectOnlyTotal
// caps the expansion: a hostile link must neither wrap the range loop (an end
// at max int) nor allocate millions of entries. No real torrent is that wide.
const (
	maxSelectOnlyIndex = 1<<20 - 1
	maxSelectOnlyTotal = 1 << 16
)

// parseSelectOnly parses BEP 53 "so" values into a sorted, de-duplicated
// index list. Each value holds comma-separated numbers ("3") or inclusive
// ranges ("4-7"); malformed parts are ignored, and an empty outcome reads as
// "parameter absent or unusable".
func parseSelectOnly(values []string) []int {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[int]struct{})
	var out []int
	add := func(n int) {
		if n < 0 || n > maxSelectOnlyIndex || len(out) >= maxSelectOnlyTotal {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			lo, hi, isRange := strings.Cut(part, "-")
			if !isRange {
				if n, err := strconv.Atoi(part); err == nil {
					add(n)
				}
				continue
			}
			a, err1 := strconv.Atoi(strings.TrimSpace(lo))
			b, err2 := strconv.Atoi(strings.TrimSpace(hi))
			if err1 != nil || err2 != nil {
				continue
			}
			if a > b {
				a, b = b, a
			}
			if a < 0 || a > maxSelectOnlyIndex {
				continue
			}
			b = min(b, maxSelectOnlyIndex)
			for n := a; n <= b && len(out) < maxSelectOnlyTotal; n++ {
				add(n)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return out
}

// parseInfoHash finds the v1 hash in the xt topics. Hybrids carry btih and
// btmh in any order; v2-only links carry just btmh and report ok=false.
func parseInfoHash(xts []string) ([20]byte, bool, error) {
	for _, xt := range xts {
		if s, ok := strings.CutPrefix(xt, "urn:btih:"); ok {
			h, err := infoHashString(s)
			if err != nil {
				return [20]byte{}, false, err
			}
			return h, true, nil
		}
	}
	return [20]byte{}, false, nil
}

// parseV2InfoHash reads the SHA-256 hash of a "urn:btmh:" multihash topic
// (BEP 52): "1220" plus 64 hex digest characters.
func parseV2InfoHash(xts []string) ([32]byte, bool) {
	for _, xt := range xts {
		s, ok := strings.CutPrefix(xt, "urn:btmh:")
		if !ok {
			continue
		}
		if len(s) != 68 || !strings.HasPrefix(s, "1220") {
			continue
		}
		b, err := hex.DecodeString(s[4:])
		if err != nil || len(b) != 32 {
			continue
		}
		var h [32]byte
		copy(h[:], b)
		return h, true
	}
	return [32]byte{}, false
}

// infoHashString decodes a 40-char hex or 32-char base32 info hash.
func infoHashString(s string) ([20]byte, error) {
	var ih [20]byte
	var b []byte
	var err error
	switch len(s) {
	case 40:
		b, err = hex.DecodeString(s)
	case 32:
		b, err = base32.StdEncoding.DecodeString(s)
	default:
		return ih, errors.New("info hash must be 32 or 40 characters")
	}
	if err != nil {
		return ih, err
	}
	copy(ih[:], b)
	return ih, nil
}
