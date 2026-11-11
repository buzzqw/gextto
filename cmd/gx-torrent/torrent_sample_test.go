package main

import (
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// TestTorrentSampleMatchesTheSeparateReads: the tick reads a torrent in one
// round trip, and that read must say what the four separate calls say.
func TestTorrentSampleMatchesTheSeparateReads(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 50_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	handle := d.session.ListTorrents()[0]
	handle.SetMaxConnections(7)
	sample := handle.Sample()
	stats := handle.Stats()
	if sample.Stats.Name != stats.Name || sample.Stats.Status != stats.Status || sample.Stats.Bytes.Total != stats.Bytes.Total {
		t.Fatalf("sample stats %+v, separate %+v", sample.Stats, stats)
	}
	if len(sample.Peers) != len(handle.Peers()) || len(sample.Trackers) != len(handle.Trackers()) {
		t.Fatalf("sample peers %d trackers %d, separate %d and %d",
			len(sample.Peers), len(sample.Trackers), len(handle.Peers()), len(handle.Trackers()))
	}
	if sample.MaxConnections != 7 || handle.MaxConnections() != 7 {
		t.Fatalf("connection cap: sample %d, separate %d, want 7", sample.MaxConnections, handle.MaxConnections())
	}
}

// TestTickKeepsTheSampleOfAnUnchangedStoppedTorrent: a paused torrent is not
// woken again while nothing happens to it, and the first change after the
// pause is read at the next tick.
func TestTickKeepsTheSampleOfAnUnchangedStoppedTorrent(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 50_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })
	if err := d.pause(hash); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "paused", func() bool { return stateOf(d, hash) == "paused" })
	handle := d.session.ListTorrents()[0]
	waitFor(t, "a stopped sample", func() bool {
		return d.sampleTorrents([]*torrent.Torrent{handle})[handle.ID()].stats.Status == torrent.Stopped
	})

	before := handle.Changes()
	handle.Sample()
	if after := handle.Changes(); after != before {
		t.Fatalf("a sample counted as a change: %d -> %d", before, after)
	}
	// A recognisable cached read is kept as long as the count stands still.
	// The daemon's own tick can replace the cache between the two steps, so
	// the check gets a few tries; without the reuse it can never pass.
	marked := sampledTorrent{changes: before}
	marked.sample.stats.Status = torrent.Stopped
	marked.sample.maxConns = 12345
	reused := false
	for try := 0; try < 5 && !reused; try++ {
		d.samplesMu.Lock()
		d.samples = map[*torrent.Torrent]sampledTorrent{handle: marked}
		d.samplesMu.Unlock()
		reused = d.sampleTorrents([]*torrent.Torrent{handle})[handle.ID()].maxConns == 12345
	}
	if !reused {
		t.Fatal("an unchanged stopped torrent was read again")
	}

	handle.SetMaxConnections(9)
	waitFor(t, "the change counted", func() bool { return handle.Changes() != before })
	if got := d.sampleTorrents([]*torrent.Torrent{handle})[handle.ID()].maxConns; got != 9 {
		t.Fatalf("the tick after a change read cap %d, want 9", got)
	}
}
