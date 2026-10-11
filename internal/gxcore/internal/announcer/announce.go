package announcer

import (
	"context"
	"errors"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

func announce(
	ctx context.Context,
	trk tracker.Tracker,
	e tracker.Event,
	numWant int,
	torrent tracker.Torrent,
	responseC chan *tracker.AnnounceResponse,
	errC chan error,
) {
	// BEP 21: a partial seed announces "paused" on every announce, so the
	// tracker can tell it apart from an active downloader. A "stopped" event
	// keeps its meaning.
	if torrent.PartialSeed && e != tracker.EventStopped {
		e = tracker.EventPaused
	}
	annReq := tracker.AnnounceRequest{
		Torrent: torrent,
		Event:   e,
		NumWant: numWant,
	}
	annResp, err := trk.Announce(ctx, annReq)
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		select {
		case errC <- err:
		case <-ctx.Done():
		}
		return
	}
	select {
	case responseC <- annResp:
	case <-ctx.Done():
	}
}
