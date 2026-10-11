package tracker

// Event type that is sent in an announce request.
type Event int32

// Tracker Announce Events. Numbers corresponds to constants in UDP tracker protocol.
const (
	EventNone Event = iota
	EventCompleted
	EventStarted
	EventStopped
	// EventPaused is the BEP 21 "paused" event, sent by a partial seed on every
	// announce (not part of the original BEP 15 enum, which stops at 3).
	EventPaused
)

var eventNames = [...]string{
	"empty",
	"completed",
	"started",
	"stopped",
	"paused",
}

// String returns the name of event as represented in HTTP tracker protocol.
func (e Event) String() string {
	return eventNames[e]
}
