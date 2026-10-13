package tracker

// Event is the announce lifecycle event. The numeric values double as the
// BEP 15 UDP event ids; the names double as the BEP 23 HTTP event strings.
type Event int32

// Announce events, in UDP protocol order.
const (
	EventNone Event = iota
	EventCompleted
	EventStarted
	EventStopped
	// EventPaused is the BEP 21 "paused" event of a partial seed, sent on
	// every announce. The original BEP 15 enum stops at EventStopped.
	EventPaused
)

// eventNames renders events for HTTP trackers, index-aligned with the enum.
var eventNames = [...]string{
	"empty",
	"completed",
	"started",
	"stopped",
	"paused",
}

// String returns the HTTP tracker spelling of the event.
func (e Event) String() string {
	return eventNames[e]
}
