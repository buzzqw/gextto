package udptracker

type udpAnnounceResponse struct {
	udpMessageHeader
	Interval int32
	Leechers int32
	Seeders  int32
}

// udpScrapeResponse is the per-hash part of a scrape response (BEP 15): the
// order is seeders, completed, leechers.
type udpScrapeResponse struct {
	Seeders   int32
	Completed int32
	Leechers  int32
}
