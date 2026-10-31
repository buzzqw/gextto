// Package freeleech resolves the three states an indexer can report about the
// download quota: free, not free, and "not stated". The third state must stay
// distinct: an unknown release is not a paid one, and treating it as paid would
// make the search and the RSS rules lie.
package freeleech

import "strings"

// State is what one source said about a release's download quota.
type State struct {
	// Free is true only when the source says the download does not count.
	Free bool
	// Known is true when the source stated the quota (free or not free).
	// Known false and Free false means "not stated".
	Known bool
}

// Resolve applies the Torznab precedence: an explicit freeleechknown marker
// wins, then an explicit freeleech flag, then the standard download volume
// factor (0 or less means free). A source that says nothing yields the zero
// State, not "paid".
func Resolve(free, known *bool, factor *float64) State {
	if known != nil {
		if !*known {
			return State{}
		}
		if free != nil {
			return State{Free: *free, Known: true}
		}
		if factor != nil {
			return State{Free: *factor <= 0, Known: true}
		}
		// The source declared the state known but gave no value: it is not
		// free, otherwise it would have said so.
		return State{Known: true}
	}
	if free != nil {
		return State{Free: *free, Known: true}
	}
	if factor != nil {
		return State{Free: *factor <= 0, Known: true}
	}
	return State{}
}

// ParseFlag reads the 0/1, true/false and on/off spellings an indexer uses for
// a boolean attribute. The second result is false when the text is not a flag.
func ParseFlag(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "free", "freeleech":
		return true, true
	case "0", "false", "no", "off", "paid":
		return false, true
	}
	return false, false
}
