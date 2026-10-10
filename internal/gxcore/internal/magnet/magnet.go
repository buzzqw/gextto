// Package magnet provides support for parsing magnet links.
package magnet

import (
	"cmp"
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
	// V2InfoHash is the SHA-256 info hash from a "urn:btmh:" topic (BEP 52),
	// present on hybrid links. HasV2 reports whether it was found.
	V2InfoHash [32]byte
	HasV2      bool
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

	xts, ok := params["xt"]
	if !ok {
		return nil, errors.New("missing xt param")
	}
	if len(xts) == 0 {
		return nil, errors.New("empty xt param")
	}

	var magnet Magnet
	magnet.InfoHash, err = parseInfoHash(xts)
	if err != nil {
		return nil, err
	}
	magnet.V2InfoHash, magnet.HasV2 = parseV2InfoHash(xts)

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

	return &magnet, nil
}

func (m *Magnet) String() string {
	var b strings.Builder
	b.Grow(2048)
	b.WriteString("magnet:?xt=urn:btih:")
	b.WriteString(hex.EncodeToString(m.InfoHash[:]))
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
	return b.String()
}

type trackerTier struct {
	trackers []string
	index    int
}

// parseInfoHash returns the v1 info hash found in xts.
// Hybrid magnet links carry both a "urn:btih:" (v1) and a "urn:btmh:" (v2) topic
// in no particular order. Only the v1 topic is usable because v2 is not supported.
func parseInfoHash(xts []string) ([20]byte, error) {
	var v2 bool
	for _, xt := range xts {
		if s, ok := strings.CutPrefix(xt, "urn:btih:"); ok {
			return infoHashString(s)
		}
		if strings.HasPrefix(xt, "urn:btmh:") {
			v2 = true
		}
	}
	if v2 {
		return [20]byte{}, errors.New("magnet link has no v1 info hash: BitTorrent v2 is not supported")
	}
	return [20]byte{}, errors.New("invalid xt param: must start with \"urn:btih:\"")
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
