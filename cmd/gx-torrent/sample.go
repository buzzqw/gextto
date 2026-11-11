package main

import "github.com/buzzqw/gextto/internal/gxcore/torrent"

// sampledTorrent is the tick's last read of one torrent handle, with the
// engine's change count at the time it was read.
type sampledTorrent struct {
	changes uint64
	sample  collectedTorrent
}

// sampleTorrents reads every torrent for the tick, one round trip of its run
// loop each. A stopped torrent whose loop has handled nothing since the last
// read would answer the same, so its last sample is kept and the loop is not
// woken: a long list of queued torrents costs nothing between two changes.
// The cache is keyed by handle, so a rebuilt session never reuses an old read.
func (d *Daemon) sampleTorrents(torrents []*torrent.Torrent) map[string]collectedTorrent {
	d.samplesMu.Lock()
	previous := d.samples
	d.samplesMu.Unlock()
	next := make(map[*torrent.Torrent]sampledTorrent, len(torrents))
	collected := make(map[string]collectedTorrent, len(torrents))
	for _, t := range torrents {
		// The count is read before the sample: a change landing in between
		// leaves the sample newer than the count, and the next tick reads again.
		changes := t.Changes()
		if last, ok := previous[t]; ok && last.changes == changes && last.sample.stats.Status == torrent.Stopped {
			next[t] = last
			collected[t.ID()] = last.sample
			continue
		}
		sample := t.Sample()
		read := collectedTorrent{
			stats:    sample.Stats,
			peers:    sample.Peers,
			trackers: sample.Trackers,
			maxConns: sample.MaxConnections,
		}
		next[t] = sampledTorrent{changes: changes, sample: read}
		collected[t.ID()] = read
	}
	d.samplesMu.Lock()
	d.samples = next
	d.samplesMu.Unlock()
	return collected
}
