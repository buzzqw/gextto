package tracker

import (
	"testing"
	"time"
)

// TestRetryInParsing covers BEP 31 "retry in": a number of minutes, or the
// string "never" meaning the tracker must not be retried.
func TestRetryInParsing(t *testing.T) {
	cases := []struct {
		in      string
		retryIn time.Duration
		never   bool
	}{
		{"", 0, false},
		{"7", 7 * time.Minute, false},
		{" 15 ", 15 * time.Minute, false},
		{"never", 0, true},
		{"NEVER", 0, true},
		{"Never", 0, true},
		{"0", 0, false},    // a zero retry is "no hint", not a pause
		{"-5", 0, false},   // invalid: fall back to the backoff
		{"soon", 0, false}, // invalid: fall back to the backoff
	}
	for _, c := range cases {
		e := NewError("failure", c.in)
		if e.FailureReason != "failure" {
			t.Errorf("NewError(%q) reason = %q", c.in, e.FailureReason)
		}
		if e.RetryIn != c.retryIn {
			t.Errorf("NewError(%q).RetryIn = %v, want %v", c.in, e.RetryIn, c.retryIn)
		}
		if e.NeverRetry != c.never {
			t.Errorf("NewError(%q).NeverRetry = %v, want %v", c.in, e.NeverRetry, c.never)
		}
	}
}
