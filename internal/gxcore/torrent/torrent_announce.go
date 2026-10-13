package torrent

import (
	"context"
	"math"

	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

// handleNewTrackers appends trackers and announces to them right away,
// unless the torrent is (going) stopped.
func (t *torrent) handleNewTrackers(trackers []tracker.Tracker) {
	t.trackers = append(t.trackers, trackers...)
	if status := t.status(); status != Stopping && status != Stopped {
		for _, tr := range trackers {
			t.startNewAnnouncer(tr)
		}
	}
}

// handleSetTrackers hot-swaps the tracker list: current announcers close,
// the list swaps, announcers restart on the survivors. Trackers that left
// the list get a best-effort stopped announce so they drop us. Runs on the
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

// announceStoppedTo says goodbye to departed trackers, each on its own
// goroutine with its own timeout. Unlike stop()'s StopAnnouncer it touches
// no torrent state and drives no Stopping transition.
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

// announcerFields snapshots the counters every announce reports. Without a
// bitfield yet the torrent claims MaxUint32 bytes left — some trackers send
// no peers to a leecher that will not admit missing anything. A completed
// torrent whose bitfield is not full is a BEP 21 partial seed.
func (t *torrent) announcerFields() tracker.Torrent {
	tr := tracker.Torrent{
		InfoHash:        t.infoHash,
		PeerID:          t.peerID,
		Port:            t.port,
		BytesDownloaded: t.bytesDownloaded.Count(),
		BytesUploaded:   t.bytesUploaded.Count(),
	}
	// t.bytesComplete() reads t.bitfield; hold the lock across both uses.
	t.mBitfield.RLock()
	if t.bitfield == nil {
		tr.BytesLeft = math.MaxUint32
	} else {
		tr.BytesLeft = t.info().Length - t.bytesComplete()
		tr.PartialSeed = t.completed && !t.bitfield.All()
	}
	t.mBitfield.RUnlock()
	return tr
}
