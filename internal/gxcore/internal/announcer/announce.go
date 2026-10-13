package announcer

import (
	"context"
	"errors"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// announce delivers one announce to trk and routes the outcome: replies to
// responseC, failures to errC. A cancelled context aborts silently — the
// torrent is going away, so nobody is listening anymore.
func announce(
	ctx context.Context,
	trk tracker.Tracker,
	e tracker.Event,
	numWant int,
	torrent tracker.Torrent,
	responseC chan *tracker.AnnounceResponse,
	errC chan error,
) {
	// BEP 21: a partial seed reports "paused" on every announce so the
	// tracker can tell it from an active downloader. "stopped" keeps its
	// terminal meaning and is never rewritten.
	if torrent.PartialSeed && e != tracker.EventStopped {
		e = tracker.EventPaused
	}
	annResp, err := trk.Announce(ctx, tracker.AnnounceRequest{
		Torrent: torrent,
		Event:   e,
		NumWant: numWant,
	})
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
