package httptracker

// scrapeResponse is the bencoded answer to an HTTP scrape (BEP 48). The "files"
// dictionary is keyed by the binary info hash of each requested torrent.
type scrapeResponse struct {
	FailureReason string                `bencode:"failure reason"`
	RetryIn       string                `bencode:"retry in"`
	Files         map[string]scrapeFile `bencode:"files"`
}

type scrapeFile struct {
	Complete   int32 `bencode:"complete"`
	Incomplete int32 `bencode:"incomplete"`
	Downloaded int32 `bencode:"downloaded"`
}
