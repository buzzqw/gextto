package torrent

import "github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"

// torrentWithInfo builds a bare torrent carrying the given metadata. The info
// dictionary is an atomic pointer, so tests must publish it with setInfo.
func torrentWithInfo(info *metainfo.Info) *torrent {
	t := &torrent{}
	t.setInfo(info)
	return t
}
