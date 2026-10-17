package dht

import (
	"errors"
	"time"
)

// BEP 33 client side: request a scrape from the closest nodes and aggregate the
// returned bloom filters into a swarm-size estimate.

// SwarmEstimate is the estimated swarm size from a DHT scrape.
type SwarmEstimate struct {
	Seeders float64
	Peers   float64
	Err     error
}

type scrapeRequest struct {
	ih      InfoHash
	timeout time.Duration
	result  chan SwarmEstimate
}

type scrapeCollector struct {
	seeds  [][]byte
	peers  [][]byte
	result chan SwarmEstimate
}

// Scrape queries the DHT for the approximate swarm size of an infohash (BEP 33)
// and blocks until the answers are collected or the timeout elapses.
func (d *DHT) Scrape(ih InfoHash, timeout time.Duration) (SwarmEstimate, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	result := make(chan SwarmEstimate, 1)
	select {
	case d.scrapeRequest <- scrapeRequest{ih: ih, timeout: timeout, result: result}:
	case <-d.stop:
		return SwarmEstimate{}, errors.New("dht: stopped")
	}
	select {
	case est := <-result:
		return est, est.Err
	case <-time.After(timeout + time.Second):
		return SwarmEstimate{}, errors.New("dht: scrape timeout")
	}
}

// startScrape is called in the run loop: it sends a scrape get_peers to the
// closest nodes and schedules the final aggregation.
func (d *DHT) startScrape(req scrapeRequest) {
	d.scrapes[req.ih] = &scrapeCollector{result: req.result}

	closest := d.routingTable.lookupFiltered(req.ih)
	if len(closest) == 0 {
		// Fall back to the bootstrap routers.
		for _, s := range d.routers() {
			if r, e := d.routingTable.getOrCreateNode("", s, d.config.UDPProto); e == nil {
				closest = append(closest, r)
			}
		}
	}
	for _, r := range closest {
		d.getPeersFromScrape(r, req.ih)
	}

	go func() {
		timer := time.NewTimer(req.timeout)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-d.stop:
		}
		select {
		case d.scrapeDone <- req.ih:
		case <-d.stop:
		}
	}()
}

// finishScrape is called in the run loop when the scrape window closes.
func (d *DHT) finishScrape(ih InfoHash) {
	c, ok := d.scrapes[ih]
	if !ok {
		return
	}
	delete(d.scrapes, ih)
	seeders, peers, err := EstimateScrape(c.seeds, c.peers)
	c.result <- SwarmEstimate{Seeders: seeders, Peers: peers, Err: err}
}

// collectScrape adds the bloom filters of a scrape response to the collector.
func (d *DHT) collectScrape(ih InfoHash, resp getPeersResponse) {
	c, ok := d.scrapes[ih]
	if !ok {
		return
	}
	if len(resp.BFsd) == 256 {
		c.seeds = append(c.seeds, []byte(resp.BFsd))
	}
	if len(resp.BFpe) == 256 {
		c.peers = append(c.peers, []byte(resp.BFpe))
	}
}

// getPeersFromScrape sends a scrape get_peers (BEP 33) to a single node.
func (d *DHT) getPeersFromScrape(r *remoteNode, ih InfoHash) {
	if r == nil {
		return
	}
	transId := r.newQuery("get_peers")
	if q, ok := r.pendingQueries[transId]; ok {
		q.ih = ih
		q.scrape = true
	}
	queryArguments := map[string]interface{}{
		"id":        d.nodeId,
		"info_hash": ih,
		"scrape":    1,
	}
	if d.dualStack() {
		queryArguments["want"] = []string{"n4", "n6"}
	}
	query := queryMessage{transId, "q", "get_peers", queryArguments, d.readOnlyFlag()}
	r.lastSearchTime = time.Now()
	sendMsg(d.conn, r.address, query, d.DebugLogger)
}
