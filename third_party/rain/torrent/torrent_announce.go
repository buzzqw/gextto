package torrent

import (
	"context"
	"math"

	"github.com/cenkalti/rain/v2/internal/tracker"
)

func (t *torrent) handleNewTrackers(trackers []tracker.Tracker) {
	t.trackers = append(t.trackers, trackers...)
	status := t.status()
	if status != Stopping && status != Stopped {
		for _, tr := range trackers {
			t.startNewAnnouncer(tr)
		}
	}
}

// handleSetTrackers replaces the tracker list at runtime (gextto fork): the
// current announcers are closed, the list is swapped and the announcers are
// restarted on the new one. The trackers that are gone get a best-effort
// "stopped" announce so they drop us from their peer list. It runs in the
// torrent goroutine (see sendCommand).
func (t *torrent) handleSetTrackers(trackers []tracker.Tracker, raw [][]string) {
	kept := make(map[string]struct{}, len(trackers))
	for _, tr := range trackers {
		kept[tr.URL()] = struct{}{}
	}
	var leaving []tracker.Tracker
	for _, an := range t.announcers {
		if _, ok := kept[an.Tracker.URL()]; !ok && an.HasAnnounced {
			leaving = append(leaving, an.Tracker)
		}
	}
	for _, an := range t.announcers {
		an.Close()
	}
	t.announcers = nil
	t.trackers = trackers
	t.rawTrackers = raw
	if status := t.status(); status != Stopping && status != Stopped {
		t.startAnnouncers()
	}
	t.announceStoppedTo(leaving)
}

// announceStoppedTo sends a best-effort stopped event to trackers that left the
// list, each with its own timeout, without touching the torrent state (unlike
// stop()'s StopAnnouncer, which drives the Stopping transition).
func (t *torrent) announceStoppedTo(trackers []tracker.Tracker) {
	if len(trackers) == 0 {
		return
	}
	fields := t.announcerFields()
	timeout := t.session.config.TrackerStopTimeout
	for _, tr := range trackers {
		go func(tr tracker.Tracker) {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			_, _ = tr.Announce(ctx, tracker.AnnounceRequest{Torrent: fields, Event: tracker.EventStopped})
		}(tr)
	}
}

func (t *torrent) announcerFields() tracker.Torrent {
	tr := tracker.Torrent{
		InfoHash:        t.infoHash,
		PeerID:          t.peerID,
		Port:            t.port,
		BytesDownloaded: t.bytesDownloaded.Count(),
		BytesUploaded:   t.bytesUploaded.Count(),
	}
	// t.bytesComplete() uses t.bitfied for calculation.
	t.mBitfield.RLock()
	if t.bitfield == nil {
		// Some trackers don't send any peer address if don't tell we have missing bytes.
		tr.BytesLeft = math.MaxUint32
	} else {
		tr.BytesLeft = t.info.Length - t.bytesComplete()
	}
	t.mBitfield.RUnlock()
	return tr
}
