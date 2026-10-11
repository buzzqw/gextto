package torrent

import "time"

// DHTScrape returns the approximate swarm size of this torrent from the DHT
// (BEP 33): the estimated number of seeders and peers, without a tracker.
func (t *Torrent) DHTScrape(timeout time.Duration) (seeders, peers int, err error) {
	s, p, err := t.torrent.session.DHTScrape(t.torrent.infoHash, timeout)
	if err != nil {
		return 0, 0, err
	}
	return int(s + 0.5), int(p + 0.5), nil
}
