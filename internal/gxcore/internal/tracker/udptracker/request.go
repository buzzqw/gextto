package udptracker

import (
	"context"
	"encoding/binary"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

type transportRequest struct {
	*requestBase
	transferAnnounceRequest
}

var _ udpRequest = (*transportRequest)(nil)

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

// transportScrapeRequest carries one info hash to a UDP tracker (BEP 15, action 2).
type transportScrapeRequest struct {
	*requestBase
	*scrapeRequest
}

var _ udpRequest = (*transportScrapeRequest)(nil)

func newTransportScrapeRequest(ctx context.Context, infoHash [20]byte, dest string) *transportScrapeRequest {
	request := &scrapeRequest{InfoHash: infoHash}
	request.Action = actionScrape
	return &transportScrapeRequest{
		requestBase:   newRequestBase(ctx, dest),
		scrapeRequest: request,
	}
}
