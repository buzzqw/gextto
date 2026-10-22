package main

import (
	"os"
	"testing"
	"time"
)

// TestMain shortens the gap between two requests to the same feed host: the
// test feeds are local servers, and the production two seconds made every
// poll test wait. The pacer's own tests build their pacer with explicit gaps.
func TestMain(m *testing.M) {
	feedHostGap = 5 * time.Millisecond
	os.Exit(m.Run())
}
