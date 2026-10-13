package torrent

import (
	"github.com/buzzqw/gextto/internal/gxcore/internal/infodownloader"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peer"
	"github.com/buzzqw/gextto/internal/gxcore/internal/peerprotocol"
)

// nextInfoDownload picks a connected peer to fetch the torrent metadata
// (BEP 9) from, or returns nil when no peer currently qualifies.
func (t *torrent) nextInfoDownload() *infodownloader.InfoDownloader {
	for pe := range t.peers {
		if t.canFetchInfoFrom(pe) {
			t.log.Debugln("downloading info from", pe.String())
			return infodownloader.New(pe)
		}
	}
	return nil
}

// canFetchInfoFrom reports whether pe is eligible as a metadata source:
// not already serving us, speaking the extension protocol, advertising the
// metadata extension, and staying within the configured size cap.
func (t *torrent) canFetchInfoFrom(pe *peer.Peer) bool {
	if _, busy := t.infoDownloaders[pe]; busy {
		return false
	}
	hs := pe.ExtensionHandshake
	if hs == nil {
		return false
	}
	if hs.MetadataSize == 0 {
		return false
	}
	if hs.MetadataSize > int(t.session.config.MaxMetadataSize) {
		t.log.Debugf("metadata size larger than allowed: %d", hs.MetadataSize)
		return false
	}
	_, ok := hs.M[peerprotocol.ExtensionKeyMetadata]
	return ok
}
