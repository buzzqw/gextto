package udptracker

import "time"

// udpBackOff spaces out UDP tracker retries exponentially: 15s, 30s, 1m,
// ... up to a ~64m ceiling, then holds there. Reset it after any success.
type udpBackOff int

const (
	// udpBackOffBase is the first retry delay; udpBackOffMaxSteps caps the
	// doubling so the delay never exceeds base * 2^maxSteps (~64 minutes).
	udpBackOffBase     = 15 * time.Second
	udpBackOffMaxSteps = 8
)

// NextBackOff returns the current retry delay and advances the sequence.
func (b *udpBackOff) NextBackOff() time.Duration {
	defer func() { *b++ }()
	if steps := int(*b); steps > udpBackOffMaxSteps {
		*b = udpBackOffMaxSteps
	}
	return udpBackOffBase * (1 << uint(*b))
}

// Reset restarts the sequence from the base delay.
func (b *udpBackOff) Reset() { *b = 0 }
