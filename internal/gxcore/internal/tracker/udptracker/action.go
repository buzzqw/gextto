package udptracker

// action identifies the UDP tracker request type (BEP 15, §"Actions").
// The value travels as the first int32 of every packet, right after the
// connection id (or the magic constant for connect requests).
type action int32

// UDP tracker actions, in protocol order.
const (
	actionConnect  action = 0
	actionAnnounce action = 1
	actionScrape   action = 2
	actionError    action = 3
)
