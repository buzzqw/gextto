package udptracker

// udpAnnounceResponse is the fixed head of a UDP announce reply (BEP 15):
// action + transaction id, then interval and swarm counts.
type udpAnnounceResponse struct {
	udpMessageHeader
	Interval int32
	Leechers int32
	Seeders  int32
}

// udpScrapeResponse is the per-info-hash body of a UDP scrape reply
// (BEP 15): seeders, completed downloads, leechers — in that order.
type udpScrapeResponse struct {
	Seeders   int32
	Completed int32
	Leechers  int32
}
