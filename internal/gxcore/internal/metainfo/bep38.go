package metainfo

import (
	"strings"

	"github.com/zeebo/bencode"
)

// BEP 38 ("Finding Local Data Via Torrent File Hints") adds two optional keys,
// "collections" (identifiers of the groups a torrent belongs to) and "similar"
// (info hashes of torrents that might share files). Either key may appear in the
// info dictionary or at the top level of the torrent; when both are present the
// values are merged. gx-core parses and exposes the hints so the app can find
// local data to reuse; the actual block reuse is a later step.

// normalizeCollections drops empty and duplicate collection identifiers and
// trims surrounding spaces, preserving order.
func normalizeCollections(in []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(in))
	for _, c := range in {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// normalizeSimilar keeps the 20-byte info hashes of a "similar" list, dropping
// entries of any other length and duplicates, preserving order.
func normalizeSimilar(in []string) [][20]byte {
	var out [][20]byte
	seen := make(map[[20]byte]struct{}, len(in))
	for _, s := range in {
		if len(s) != 20 {
			continue
		}
		var h [20]byte
		copy(h[:], s)
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

// mergeCollections appends extra to base, skipping values already present.
func mergeCollections(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base))
	for _, c := range base {
		seen[c] = struct{}{}
	}
	for _, c := range extra {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		base = append(base, c)
	}
	return base
}

// mergeSimilar appends extra to base, skipping info hashes already present.
func mergeSimilar(base, extra [][20]byte) [][20]byte {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[[20]byte]struct{}, len(base))
	for _, h := range base {
		seen[h] = struct{}{}
	}
	for _, h := range extra {
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		base = append(base, h)
	}
	return base
}

// decodeCollections decodes a top-level bencoded "collections" value.
func decodeCollections(raw bencode.RawMessage) []string {
	var list []string
	if err := bencode.DecodeBytes(raw, &list); err != nil {
		return nil
	}
	return normalizeCollections(list)
}

// decodeSimilar decodes a top-level bencoded "similar" value.
func decodeSimilar(raw bencode.RawMessage) [][20]byte {
	var list []string
	if err := bencode.DecodeBytes(raw, &list); err != nil {
		return nil
	}
	return normalizeSimilar(list)
}
