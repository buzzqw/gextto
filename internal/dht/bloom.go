package dht

import (
	"crypto/sha1"
	"errors"
	"math"
	"math/bits"
)

// BEP 33 (DHT Scrapes): a 2048-bit Bloom filter with k = 2 hash functions over
// IP addresses, used to estimate the size of a swarm (seeders / peers) without
// a tracker.
//
// https://www.bittorrent.org/beps/bep_0033.html

const (
	scrapeBloomBits = 256 * 8
	scrapeBloomK    = 2
)

// scrapeBloom is a BEP 33 bloom filter (256 bytes).
type scrapeBloom [256]byte

// insert adds an IP address (4 or 16 raw bytes) to the filter.
func (b *scrapeBloom) insert(ip []byte) {
	if len(ip) == 0 {
		return
	}
	h := sha1.Sum(ip)
	index1 := (int(h[0]) | int(h[1])<<8) % scrapeBloomBits
	index2 := (int(h[2]) | int(h[3])<<8) % scrapeBloomBits
	b[index1/8] |= 1 << uint(index1%8)
	b[index2/8] |= 1 << uint(index2%8)
}

// union ORs another filter into this one (estimating a union of sets).
func (b *scrapeBloom) union(o *scrapeBloom) {
	for i := range b {
		b[i] |= o[i]
	}
}

// estimate returns the estimated number of items in the filter (BEP 33 formula).
func (b *scrapeBloom) estimate() float64 {
	zeros := 0
	for _, by := range b {
		zeros += 8 - bits.OnesCount8(by)
	}
	if zeros >= scrapeBloomBits {
		// An empty filter: no set bits, no items estimated (the clamp below
		// would otherwise turn it into ~0.5, rounding up to 1 to consumers).
		return 0
	}
	if zeros <= 0 {
		return math.Inf(1)
	}
	m := float64(scrapeBloomBits)
	return math.Log(float64(zeros)/m) / (scrapeBloomK * math.Log(1-1/m))
}

// unionFilters ORs a set of 256-byte BEP 33 filters into a new one.
func unionFilters(filters [][]byte) (*scrapeBloom, error) {
	var b scrapeBloom
	for _, f := range filters {
		if len(f) != 256 {
			return nil, errors.New("dht: BEP 33 bloom filter must be 256 bytes")
		}
		for i := range b {
			b[i] |= f[i]
		}
	}
	return &b, nil
}

// EstimateScrape unions the BFsd (seeds) and BFpe (peers) filters collected from
// several DHT nodes and returns the estimated swarm sizes (BEP 33). It is the
// client-side counterpart of the scrape reply.
func EstimateScrape(seedFilters, peerFilters [][]byte) (seeders, peers float64, err error) {
	sd, err := unionFilters(seedFilters)
	if err != nil {
		return 0, 0, err
	}
	pe, err := unionFilters(peerFilters)
	if err != nil {
		return 0, 0, err
	}
	return sd.estimate(), pe.estimate(), nil
}
