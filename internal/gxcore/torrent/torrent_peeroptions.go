package torrent

import (
	"github.com/buzzqw/gextto/internal/gxcore/internal/bandwidth"
)

// Per-torrent peer options: the bandwidth weight (the torrent's share of a
// limited line) and the DHT/PEX switches, plus the trading/seeding upload
// classes the session's split reads.

// clampWeight keeps a bandwidth weight within 10..1000 percent; 0 (or any
// value <= 0) means the normal share.
func clampWeight(percent int) int {
	switch {
	case percent <= 0:
		return 0
	case percent < 10:
		return 10
	case percent > 1000:
		return 1000
	default:
		return percent
	}
}

// applyPeerOptions sets the options on the torrent's own state and limiters.
// It runs before the torrent starts (load) or on its goroutine.
func (t *torrent) applyPeerOptions(weight int, noDHT, noPEX bool) {
	t.bandwidthWeight = clampWeight(weight)
	t.noDHT = noDHT
	t.noPEX = noPEX
	t.bucketDownload.SetWeight(t.bandwidthWeight)
	t.bucketUpload.SetWeight(t.bandwidthWeight)
}

// setPeerOptions applies a change on a running torrent: the DHT announcer
// stops or starts, PEX exchanges end at once, and the record is saved.
func (t *torrent) setPeerOptions(weight int, noDHT, noPEX bool) error {
	t.applyPeerOptions(weight, noDHT, noPEX)
	if t.noDHT && t.dhtAnnouncer != nil {
		t.dhtAnnouncer.Close()
		t.dhtAnnouncer = nil
	}
	if !t.noDHT && t.dhtAnnouncer == nil {
		switch t.status() {
		case Downloading, DownloadingMetadata, Seeding:
			t.startAnnouncers()
		}
	}
	if t.noPEX {
		for pe := range t.peers {
			pe.StopPEX()
		}
	}
	if t.session.resumer == nil {
		return nil
	}
	return t.session.resumer.WritePeerOptions(t.id, t.bandwidthWeight, t.noDHT, t.noPEX)
}

// SetPeerOptions changes the torrent's bandwidth weight in percent (0 or 100
// = the normal share, 200 = twice the share of a normal torrent when the line
// is limited, 10..1000) and turns the DHT and PEX on or off for this torrent.
// Private torrents never use DHT or PEX, whatever is set here.
func (t *Torrent) SetPeerOptions(weightPercent int, dht, pex bool) error {
	return query(t.torrent, func() error {
		return t.torrent.setPeerOptions(weightPercent, !dht, !pex)
	})
}

// PeerOptions reports the weight (100 when normal) and the DHT/PEX switches.
func (t *Torrent) PeerOptions() (weightPercent int, dht, pex bool) {
	type options struct {
		weight   int
		dht, pex bool
	}
	got := query(t.torrent, func() options {
		w := t.torrent.bandwidthWeight
		if w == 0 {
			w = 100
		}
		return options{w, !t.torrent.noDHT, !t.torrent.noPEX}
	})
	return got.weight, got.dht, got.pex
}

// Automatic reciprocity: with no fixed trading share, a trading peer's own
// weight grows with what it sends back compared with the other trading peers
// of the torrent, from 100 % (it gives little) to maxReciprocityWeight (it
// gives several times the average). Seeding peers keep 100 %, so they are
// never starved; the weights only matter while an upload limit is in place,
// and a peer that stops giving falls back at the next round.
const maxReciprocityWeight = 400

// reciprocityWeight is the weight of a trading peer sending rate bytes/s
// when the trading peers of the torrent send avg on average.
func reciprocityWeight(rate, avg int) int32 {
	if rate <= 0 || avg <= 0 {
		return 100
	}
	extra := 100 * float64(rate) / float64(avg)
	return int32(min(float64(maxReciprocityWeight), 100+extra))
}

// updateUploadClasses sorts the peers we upload to into trading (a torrent
// still downloading, and the peer sends data back) and seeding (everyone
// else), gives each trading peer its reciprocity weight when the split is
// automatic, and moves the session counts by the difference since the last
// round. stopped clears this torrent's share of the counts.
func (t *torrent) updateUploadClasses(stopped bool) {
	var counts [3]int64
	if !stopped {
		downloading := !t.completed
		automatic := t.session.uploadClasses.TradingShare() == 0
		total, traders := 0, 0
		for pe := range t.peers {
			class := bandwidth.ClassNone
			if !pe.ClientChoking && pe.PeerInterested {
				class = bandwidth.ClassSeeding
				if rate := pe.DownloadSpeed(); downloading && rate > 0 {
					class = bandwidth.ClassTrading
					total += rate
					traders++
				}
			}
			pe.SetUploadClass(class)
			counts[class]++
		}
		avg := 0
		if traders > 0 {
			avg = total / traders
		}
		for pe := range t.peers {
			weight := int32(100)
			if automatic && pe.UploadClass() == bandwidth.ClassTrading {
				weight = reciprocityWeight(pe.DownloadSpeed(), avg)
			}
			pe.SetUploadWeight(weight)
		}
	}
	classes := &t.session.uploadClasses
	for class := bandwidth.ClassTrading; class <= bandwidth.ClassSeeding; class++ {
		classes.Add(class, counts[class]-t.uploadClassCounts[class])
	}
	t.uploadClassCounts = counts
}
