package rpctypes

import (
	"encoding/json"
	"time"
)

// Time is a time.Time that crosses the JSON-RPC boundary as an RFC3339
// string instead of Go's default timestamp format.
type Time struct {
	time.Time
}

var (
	_ json.Marshaler   = (*Time)(nil)
	_ json.Unmarshaler = (*Time)(nil)
)

// MarshalJSON implements json.Marshaler.
func (t Time) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Format(time.RFC3339))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339, s)
	t.Time = parsed
	return err
}
