// Package magnet provides support for parsing magnet links.
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

// Magnet link contains the information to download torrent metadata from network.
type Magnet struct {
	InfoHash [20]byte
	Name     string
	Trackers [][]string
	Peers    []string
	// HasV1 reports whether the link carried a v1 "urn:btih:" topic. For a
	// v2-only link InfoHash is the truncated SHA-256 of the btmh topic.
	HasV1 bool
	// V2InfoHash is the SHA-256 info hash from a "urn:btmh:" topic (BEP 52),
	// present on hybrid and v2-only links. HasV2 reports whether it was found.
	V2InfoHash [32]byte
	HasV2      bool
	// SelectOnly is the sorted list of file indices to download, from the
	// BEP 53 "so" parameter (Select Only). Empty means every file is wanted.
	// Indices address the non-padding files, the same convention the engine's
	// Config.FileSelection uses.
	SelectOnly []int
	// WebSeeds are the HTTP web seed URLs from the BEP 53 "ws" parameters.
	WebSeeds []string
	// DHTNodes are the DHT bootstrap nodes (host:port) from the magnet "dht"
	// parameters, like libtorrent's add_torrent_params.dht_nodes.
	DHTNodes []string
	// ExactSources are the magnet "xs" values: URLs (usually HTTP/HTTPS) that
	// point at the exact .torrent this link refers to. AcceptableSources are
	// the "as" values, a set of interchangeable sources. gx-core fetches the
	// metainfo from them before waiting for a peer or the DHT.
	ExactSources      []string
	AcceptableSources []string
	// BTPKPublicKey is the ed25519 public key from a BEP 46 "xs=urn:btpk:"
	// value: the link then carries no "xt" and the info hash is resolved from
	// the DHT mutable item at add time. BTPKSalt is the optional "s" salt.
	BTPKPublicKey []byte
	BTPKSalt      []byte
}

// New parses the string and returns new Magnet.
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
	// BEP 46: a link may carry "xs=urn:btpk:<public key>" instead of an "xt";
	// the info hash is then resolved from the DHT mutable item at add time.
	magnet.BTPKPublicKey, magnet.BTPKSalt = parseBTPK(params["xs"], params["s"])
	if len(xts) == 0 && len(magnet.BTPKPublicKey) == 0 {
		return nil, errors.New("missing xt param")
	}

	var h [20]byte
	ok := false
	if len(xts) > 0 {
		h, ok, err = parseInfoHash(xts)
		if err != nil {
			return nil, err
		}
	}
	if ok {
		magnet.InfoHash = h
		magnet.HasV1 = true
	}
	magnet.V2InfoHash, magnet.HasV2 = parseV2InfoHash(xts)
	if !magnet.HasV1 {
		switch {
		case magnet.HasV2:
			// A v2-only link: the identity used on the wire (handshake, DHT,
			// tracker) is the first 20 bytes of the SHA-256 info hash.
			copy(magnet.InfoHash[:], magnet.V2InfoHash[:20])
		case len(magnet.BTPKPublicKey) > 0:
			// BEP 46: resolved from the DHT by the caller.
		default:
			return nil, errors.New("invalid xt param: must start with \"urn:btih:\" or \"urn:btmh:\"")
		}
	}

	names := params["dn"]
	if len(names) != 0 {
		magnet.Name = names[0]
	}

	var tiers []trackerTier
	for key, tier := range params {
		if key == "tr" {
			for i, tr := range tier {
				tiers = append(tiers, trackerTier{trackers: []string{tr}, index: i - len(tier)})
			}
		} else if strings.HasPrefix(key, "tr.") {
			index, err := strconv.Atoi(key[3:])
			if err == nil && index >= 0 {
				tiers = append(tiers, trackerTier{trackers: tier, index: index})
			}
		}
	}

	slices.SortFunc(tiers, func(a, b trackerTier) int { return cmp.Compare(a.index, b.index) })

	magnet.Trackers = make([][]string, len(tiers))
	for i, ti := range tiers {
		magnet.Trackers[i] = ti.trackers
	}

	magnet.Peers = params["x.pe"]

	// BEP 53 "so": Select Only, a list of file indices (comma separated, with
	// ranges like "0,2,4-7"). Repeated parameters and repeated indices are
	// accepted and de-duplicated.
	magnet.SelectOnly = parseSelectOnly(params["so"])

	// BEP 53 "ws": HTTP web seeds for the torrent.
	for _, ws := range params["ws"] {
		if ws = strings.TrimSpace(ws); ws != "" {
			magnet.WebSeeds = append(magnet.WebSeeds, ws)
		}
	}

	// Magnet "dht": DHT bootstrap nodes ("host:port"), like libtorrent's
	// add_torrent_params.dht_nodes.
	for _, node := range params["dht"] {
		if node = strings.TrimSpace(node); node != "" {
			magnet.DHTNodes = append(magnet.DHTNodes, node)
		}
	}

	// Magnet "xs" (exact source) and "as" (acceptable source): URLs where the
	// .torrent can be fetched, mostly HTTP(S).
	magnet.ExactSources = cleanSourceList(params["xs"])
	magnet.AcceptableSources = cleanSourceList(params["as"])

	return &magnet, nil
}

// cleanSourceList trims a magnet source list and drops empty entries.
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

// parseBTPK parses a BEP 46 "xs=urn:btpk:<public key hex>" value together with
// the optional "s" salt (hex). It returns a nil key when no valid urn:btpk is
// present.
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

// HasBTPK reports whether the link carries a BEP 46 "urn:btpk:" source, so its
// info hash must be resolved from the DHT before the torrent is added.
func (m *Magnet) HasBTPK() bool { return len(m.BTPKPublicKey) > 0 }

// MetadataSources returns the HTTP(S) URLs from "xs" (exact) then "as"
// (acceptable), de-duplicated and in order: gx-core tries them to fetch the
// .torrent without depending on a peer or the DHT. Non-HTTP values (for example
// a nested magnet) are ignored.
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

// maxSelectOnlyIndex is the highest file index "so" may name, and
// maxSelectOnlyTotal the most indices it may expand to: a hostile link must not
// make the parser loop forever (a range ending at the largest int would wrap
// around) or allocate millions of entries. No real torrent has this many files.
const (
	maxSelectOnlyIndex = 1<<20 - 1
	maxSelectOnlyTotal = 1 << 16
)

// parseSelectOnly parses the BEP 53 "so" values into a sorted, de-duplicated
// list of file indices. Each value is a comma-separated list of indices, each
// either a number ("3") or an inclusive range ("4-7"). Malformed parts are
// ignored; an empty result means the parameter was absent or unusable.
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

type trackerTier struct {
	trackers []string
	index    int
}

// parseInfoHash returns the v1 info hash found in xts, if any. Hybrid magnet
// links carry both a "urn:btih:" (v1) and a "urn:btmh:" (v2) topic in no
// particular order; a v2-only link carries only the latter and returns ok=false.
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

// parseV2InfoHash returns the SHA-256 info hash of a "urn:btmh:" topic (BEP
// 52). The value is a multihash: "1220" (SHA2-256, 32 bytes) followed by the
// 64 hex characters of the digest. A hybrid magnet link carries both a btih and
// a btmh topic; the v1 hash is the one usable today.
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

// infoHashString returns a new info hash value from a string.
// s must be 40 (hex encoded) or 32 (base32 encoded) characters, otherwise it returns error.
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
