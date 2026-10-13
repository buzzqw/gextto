// Package metainfo reads and writes .torrent files: v1 (BEP 3), hybrids and
// v2 (BEP 52), plus the BEP 38 collection hints.
package metainfo

import (
	"errors"
	"io"
	"strings"
	"time"

	"github.com/zeebo/bencode"
)

// Creator stamps torrents minted by NewBytes; set it to the client name.
var Creator string

// MetaInfo is the decoded .torrent: info dict, tracker tiers, webseeds and,
// for v2, the top-level piece layers.
type MetaInfo struct {
	Info         Info
	AnnounceList [][]string
	URLList      []string
	// PieceLayers maps each v2 file's merkle root to its layer hashes
	// (BEP 52), binary. Empty for v1.
	PieceLayers map[string][]byte
}

// New decodes a .torrent stream, accepting v1, hybrid and v2-only payloads.
func New(r io.Reader) (*MetaInfo, error) {
	var t struct {
		Info         bencode.RawMessage `bencode:"info"`
		Announce     bencode.RawMessage `bencode:"announce"`
		AnnounceList bencode.RawMessage `bencode:"announce-list"`
		URLList      bencode.RawMessage `bencode:"url-list"`
		PieceLayers  bencode.RawMessage `bencode:"piece layers"`
		Collections  bencode.RawMessage `bencode:"collections"`
		Similar      bencode.RawMessage `bencode:"similar"`
	}
	if err := bencode.NewDecoder(r).Decode(&t); err != nil {
		return nil, err
	}
	if len(t.Info) == 0 {
		return nil, errors.New("no info dict in torrent file")
	}
	info, err := NewInfo(t.Info, true, true)
	switch {
	case errors.Is(err, ErrV2Only):
		info, err = buildV2(t.Info, t.PieceLayers)
		if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	}
	ret := &MetaInfo{Info: *info}
	// BEP 38 hints may live at top level too; both places merge.
	ret.Info.Collections = mergeCollections(ret.Info.Collections, decodeCollections(t.Collections))
	ret.Info.Similar = mergeSimilar(ret.Info.Similar, decodeSimilar(t.Similar))
	ret.AnnounceList = supportedTrackers(t.Announce, t.AnnounceList)
	ret.URLList = supportedWebseeds(t.URLList)
	// BEP 52 layers ride top-level for v2 torrents.
	if len(t.PieceLayers) > 0 {
		var layers map[string][]byte
		if lerr := bencode.DecodeBytes(t.PieceLayers, &layers); lerr == nil {
			ret.PieceLayers = layers
		}
	}
	return ret, nil
}

// buildV2 assembles a v2-only info from its dict plus the top-level layers.
func buildV2(infoDict, layersRaw bencode.RawMessage) (*Info, error) {
	v2, err := NewV2Info(infoDict)
	if err != nil {
		return nil, err
	}
	var layers map[string][]byte
	if len(layersRaw) > 0 {
		if err := bencode.DecodeBytes(layersRaw, &layers); err != nil {
			return nil, err
		}
	}
	if err := v2.AttachV2Pieces(layers); err != nil {
		return nil, err
	}
	return v2, nil
}

// supportedTrackers collects the announce tiers, keeping UDP/HTTP(S) only
// and dropping emptied tiers; a lone announce becomes a single tier.
func supportedTrackers(announce, announceList bencode.RawMessage) [][]string {
	if len(announceList) > 0 {
		var ll [][]string
		if err := bencode.DecodeBytes(announceList, &ll); err != nil {
			return nil
		}
		var ret [][]string
		for _, tier := range ll {
			var ti []string
			for _, u := range tier {
				if isTrackerSupported(u) {
					ti = append(ti, u)
				}
			}
			if len(ti) > 0 {
				ret = append(ret, ti)
			}
		}
		return ret
	}
	var s string
	if err := bencode.DecodeBytes(announce, &s); err == nil && isTrackerSupported(s) {
		return [][]string{{s}}
	}
	return nil
}

// supportedWebseeds collects HTTP(S) webseeds from the string or list form.
func supportedWebseeds(urlList bencode.RawMessage) []string {
	if len(urlList) == 0 {
		return nil
	}
	if urlList[0] == 'l' {
		var l []string
		if err := bencode.DecodeBytes(urlList, &l); err != nil {
			return nil
		}
		var ret []string
		for _, s := range l {
			if isWebseedSupported(s) {
				ret = append(ret, s)
			}
		}
		return ret
	}
	var s string
	if err := bencode.DecodeBytes(urlList, &s); err == nil && isWebseedSupported(s) {
		return []string{s}
	}
	return nil
}

func isTrackerSupported(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "udp://")
}

func isWebseedSupported(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// NewBytes mints a .torrent from a bencoded info dict.
func NewBytes(info []byte, trackers [][]string, webseeds []string, comment string) ([]byte, error) {
	return NewBytesWithLayers(info, trackers, webseeds, nil, comment)
}

// NewBytesWithLayers adds the BEP 52 piece layers, so v2 torrents export
// with their layers intact.
func NewBytesWithLayers(info []byte, trackers [][]string, webseeds []string, layers map[string][]byte, comment string) ([]byte, error) {
	return NewBytesWithHints(info, trackers, webseeds, layers, comment, nil, nil)
}

// NewBytesWithHints further preserves the BEP 38 hints at top level, so an
// export keeps collections/similar even when the info dict lacks them.
func NewBytesWithHints(info []byte, trackers [][]string, webseeds []string, layers map[string][]byte, comment string, collections []string, similar [][20]byte) ([]byte, error) {
	similarStrings := make([]string, len(similar))
	for i, h := range similar {
		similarStrings[i] = string(h[:])
	}
	mi := struct {
		Info         bencode.RawMessage `bencode:"info"`
		PieceLayers  map[string][]byte  `bencode:"piece layers,omitempty"`
		Collections  []string           `bencode:"collections,omitempty"`
		Similar      []string           `bencode:"similar,omitempty"`
		Announce     string             `bencode:"announce,omitempty"`
		AnnounceList [][]string         `bencode:"announce-list,omitempty"`
		URLList      bencode.RawMessage `bencode:"url-list,omitempty"`
		Comment      string             `bencode:"comment,omitempty"`
		CreationDate int64              `bencode:"creation date"`
		CreatedBy    string             `bencode:"created by,omitempty"`
	}{
		Info:         info,
		PieceLayers:  layers,
		Collections:  collections,
		Similar:      similarStrings,
		Comment:      comment,
		CreationDate: time.Now().UTC().Unix(),
		CreatedBy:    Creator,
	}
	switch {
	case len(trackers) == 1 && len(trackers[0]) == 1:
		mi.Announce = trackers[0][0]
	case len(trackers) > 0:
		mi.AnnounceList = trackers
	}
	switch {
	case len(webseeds) == 1:
		mi.URLList, _ = bencode.EncodeBytes(webseeds[0])
	case len(webseeds) > 1:
		mi.URLList, _ = bencode.EncodeBytes(webseeds)
	}
	return bencode.EncodeBytes(mi)
}
