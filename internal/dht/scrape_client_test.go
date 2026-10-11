package dht

import "testing"

// TestScrapeCollectorAggregate checks the client aggregation: filters collected
// from several nodes are unioned and estimated.
func TestScrapeCollectorAggregate(t *testing.T) {
	d := &DHT{scrapes: map[InfoHash]*scrapeCollector{}}
	ih := InfoHash("01234567890123456789")
	resultC := make(chan SwarmEstimate, 1)
	d.scrapes[ih] = &scrapeCollector{result: resultC}

	var s1, s2, p1 scrapeBloom
	for i := 0; i < 50; i++ {
		s1.insert([]byte{10, 1, 0, byte(i)})
		s2.insert([]byte{10, 1, 1, byte(i)})
		p1.insert([]byte{10, 2, 0, byte(i)})
	}
	d.collectScrape(ih, getPeersResponse{BFsd: string(s1[:]), BFpe: string(p1[:])})
	d.collectScrape(ih, getPeersResponse{BFsd: string(s2[:])})

	// A wrongly sized filter is ignored.
	d.collectScrape(ih, getPeersResponse{BFsd: "short"})

	d.finishScrape(ih)
	est := <-resultC
	if est.Err != nil {
		t.Fatalf("estimate error: %v", est.Err)
	}
	if est.Seeders < 90 || est.Seeders > 110 {
		t.Fatalf("seeders = %.1f, want ~100", est.Seeders)
	}
	if est.Peers < 40 || est.Peers > 60 {
		t.Fatalf("peers = %.1f, want ~50", est.Peers)
	}
	if _, ok := d.scrapes[ih]; ok {
		t.Fatal("the collector was not removed after finish")
	}
}
