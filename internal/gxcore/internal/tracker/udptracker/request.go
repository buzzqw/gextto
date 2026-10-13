package udptracker

import (
	"context"
	"encoding/binary"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// transportRequest adapts a tracker-layer announce to the UDP wire.
type transportRequest struct {
	*requestBase
	transferAnnounceRequest
}

var _ udpRequest = (*transportRequest)(nil)

// newTransportRequest builds the announce packet for req. The key echoes in
// the peer id tail (bytes 16:20), a convention some trackers use to match
// replies to senders behind NAT.
func newTransportRequest(ctx context.Context, req tracker.AnnounceRequest, dest string, urlData string) *transportRequest {
	request := &announceRequest{
		InfoHash:   req.Torrent.InfoHash,
		PeerID:     req.Torrent.PeerID,
		Downloaded: req.Torrent.BytesDownloaded,
		Left:       req.Torrent.BytesLeft,
		Uploaded:   req.Torrent.BytesUploaded,
		Event:      req.Event,
		NumWant:    int32(req.NumWant),
		Port:       uint16(req.Torrent.Port),
	}
	binary.BigEndian.PutUint32(request.PeerID[16:20], request.Key)
	request.Action = actionAnnounce

	return &transportRequest{
		requestBase: newRequestBase(ctx, dest),
		transferAnnounceRequest: transferAnnounceRequest{
			announceRequest: request,
			urlData:         urlData,
		},
	}
}

// transportScrapeRequest adapts a single-info-hash scrape to the UDP wire.
type transportScrapeRequest struct {
	*requestBase
	*scrapeRequest
}

var _ udpRequest = (*transportScrapeRequest)(nil)

// newTransportScrapeRequest builds the scrape packet for one info hash.
func newTransportScrapeRequest(ctx context.Context, infoHash [20]byte, dest string) *transportScrapeRequest {
	request := &scrapeRequest{InfoHash: infoHash}
	request.Action = actionScrape
	return &transportScrapeRequest{
		requestBase:   newRequestBase(ctx, dest),
		scrapeRequest: request,
	}
}
