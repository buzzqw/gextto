package main

// bep38.go implements BEP 38 ("Finding Local Data Via Torrent File Hints") on
// the daemon side: it exposes the "collections" and "similar" hints of a torrent
// and finds the other torrents already in the session whose local data could be
// reused. The block-level reuse itself (Swarm Merging) is a later step; this is
// the discovery of the candidates, as the BEP describes.

import (
	"encoding/hex"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// bep38Hints is the BEP 38 metadata of one torrent: the "collections" it belongs
// to and the "similar" info hashes it hints at.
type bep38Hints struct {
	infoHash    torrent.InfoHash
	collections []string
	similar     [][20]byte
}

// hintsOf reads the BEP 38 hints of a torrent.
func hintsOf(t *torrent.Torrent) bep38Hints {
	return bep38Hints{
		infoHash:    t.InfoHash(),
		collections: t.Collections(),
		similar:     t.SimilarTorrents(),
	}
}

// matchLocalData returns the indices of the others that share a BEP 38 hint with
// target: an info hash present in either "similar" list (the hint is symmetric,
// as BEP 38 requires) or at least one common collection. The target itself is
// skipped.
func matchLocalData(target bep38Hints, others []bep38Hints) []int {
	similar := make(map[[20]byte]struct{}, len(target.similar))
	for _, h := range target.similar {
		similar[h] = struct{}{}
	}
	collections := make(map[string]struct{}, len(target.collections))
	for _, c := range target.collections {
		collections[c] = struct{}{}
	}
	var out []int
	for i, o := range others {
		if o.infoHash == target.infoHash {
			continue
		}
		if sharesHint(target, o, similar, collections) {
			out = append(out, i)
		}
	}
	return out
}

// sharesHint reports whether other shares a hint with target.
func sharesHint(target, other bep38Hints, similar map[[20]byte]struct{}, collections map[string]struct{}) bool {
	if _, ok := similar[other.infoHash]; ok {
		return true
	}
	for _, h := range other.similar {
		if h == target.infoHash {
			return true
		}
	}
	for _, c := range other.collections {
		if _, ok := collections[c]; ok {
			return true
		}
	}
	return false
}

// similarHex renders a list of info hashes as lowercase hex strings.
func similarHex(hashes [][20]byte) []string {
	if len(hashes) == 0 {
		return nil
	}
	out := make([]string, len(hashes))
	for i, h := range hashes {
		out[i] = hex.EncodeToString(h[:])
	}
	return out
}

// localDataInfo is one torrent whose local data could be reused, from a BEP 38
// hint match.
type localDataInfo struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
}

// collectionsInfo is the payload of inspect?what=collections: the BEP 38 hints
// of a torrent plus the local torrents they point at.
type collectionsInfo struct {
	Collections []string        `json:"collections"`
	Similar     []string        `json:"similar"`
	Local       []localDataInfo `json:"local"`
}

// collectionsLocked builds the BEP 38 view of t. The caller holds d.mu.
func (d *Daemon) collectionsLocked(t *torrent.Torrent) collectionsInfo {
	target := hintsOf(t)
	out := collectionsInfo{
		Collections: target.collections,
		Similar:     similarHex(target.similar),
	}
	// Keep the JSON shape stable (empty arrays, not null) for API consumers.
	if out.Collections == nil {
		out.Collections = []string{}
	}
	if out.Similar == nil {
		out.Similar = []string{}
	}
	out.Local = []localDataInfo{}
	if d.session == nil {
		return out
	}
	var others []bep38Hints
	var list []*torrent.Torrent
	for _, o := range d.session.ListTorrents() {
		others = append(others, hintsOf(o))
		list = append(list, o)
	}
	for _, i := range matchLocalData(target, others) {
		out.Local = append(out.Local, localDataInfo{
			Hash: list[i].InfoHash().String(),
			Name: list[i].Name(),
		})
	}
	return out
}
