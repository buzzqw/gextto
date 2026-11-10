package main

import (
	"errors"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// A tracker failure is logged when it starts and folded while it repeats; the
// recovery is logged once; the first success is logged, later ones are not.
func TestTorrentLogTrackerTransitions(t *testing.T) {
	var l torrentLog
	const h, url = "abc", "udp://tracker.example:1337/announce"
	at := time.Unix(1_800_000_000, 0)
	ann := func(err error, seeds int) {
		ev := torrent.Event{Type: torrent.EventTrackerAnnounce, Torrent: h, Tracker: url, Seeders: seeds, At: at}
		if err != nil {
			ev.Error = err.Error()
		}
		l.record(ev)
		at = at.Add(time.Minute)
	}
	ann(nil, 5)
	ann(nil, 6)
	ann(errors.New("timeout"), 0)
	ann(errors.New("timeout"), 0)
	ann(errors.New("timeout"), 0)
	ann(nil, 7)
	ann(nil, 7)
	got := l.entries(h)
	if len(got) != 3 {
		t.Fatalf("%d lines, want 3: %+v", len(got), got)
	}
	if got[2].Msg != "Tracker answered" || got[1].Msg != "Tracker not answering" || got[0].Msg != "Tracker answering again" {
		t.Fatalf("lines (newest first): %q, %q, %q", got[0].Msg, got[1].Msg, got[2].Msg)
	}
	if got[1].Count != 3 || !got[1].Bad {
		t.Fatalf("the failure line counts %d (bad %v), want 3 and bad", got[1].Count, got[1].Bad)
	}
	l.forget(h)
	if l.entries(h) != nil {
		t.Fatal("a removed torrent kept its history")
	}
}

// The history keeps the last torrentLogSize lines and names the peer of a
// piece that failed its hash.
func TestTorrentLogBounded(t *testing.T) {
	var l torrentLog
	for i := 0; i < torrentLogSize+10; i++ {
		l.record(torrent.Event{Type: torrent.EventPieceHashFailed, Torrent: "h", Peer: "10.0.0." + string(rune('0'+i%10)) + ":6881", Piece: uint32(i)})
		l.record(torrent.Event{Type: torrent.EventTorrentStopped, Torrent: "h"})
	}
	got := l.entries("h")
	if len(got) != torrentLogSize {
		t.Fatalf("%d lines kept, want %d", len(got), torrentLogSize)
	}
	if got[1].Msg != "A piece failed its hash check and is downloaded again" || got[1].Detail == "" {
		t.Fatalf("hash failure line: %+v", got[1])
	}
}
