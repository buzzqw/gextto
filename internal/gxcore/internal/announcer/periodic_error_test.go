package announcer

// periodic_error_test.go locks the announce failure messages: a tracker
// rejected by the IP filter must say so (the dialer's error arrives wrapped in
// *url.Error), and an unclassified failure must show its reason without the
// request URL, which carries a private tracker's passkey.

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/resolver"
	"github.com/buzzqw/gextto/internal/gxcore/internal/tracker"
)

func TestAnnounceErrorNamesABlockedTracker(t *testing.T) {
	a := &PeriodicalAnnouncer{Tracker: &fakeTracker{}}
	blocked := &url.Error{
		Op:  "Get",
		URL: "https://tracker.test/secret-key/announce?info_hash=abc",
		Err: resolver.ErrBlocked,
	}
	if got := a.newAnnounceError(blocked); got.Message != "tracker IP is blocked" || got.Unknown {
		t.Fatalf("blocked tracker -> %q (unknown=%v), want the blocked message", got.Message, got.Unknown)
	}
	wrapped := &url.Error{Op: "Get", URL: "https://tracker.test/announce", Err: tracker.ErrDecode}
	if got := a.newAnnounceError(wrapped); got.Message != "invalid response from tracker" {
		t.Fatalf("decode failure -> %q", got.Message)
	}
}

func TestAnnounceErrorUnknownKeepsTheReasonAndHidesTheURL(t *testing.T) {
	a := &PeriodicalAnnouncer{Tracker: &fakeTracker{}}
	weird := &url.Error{
		Op:  "Get",
		URL: "https://tracker.test/secret-key/announce",
		Err: errors.New("something unexpected"),
	}
	got := a.newAnnounceError(weird)
	if got.Message != "announce failed: something unexpected" {
		t.Fatalf("unknown failure -> %q", got.Message)
	}
	if !got.Unknown {
		t.Fatal("an unclassified failure must stay marked unknown")
	}
	if strings.Contains(got.Message, "secret-key") {
		t.Fatalf("the message leaks the tracker URL: %q", got.Message)
	}
}

// TestAnnounceErrorClassifiesTLSWithoutTheURL checks the TLS case: the message
// names the host, not the whole (private) announce URL.
func TestAnnounceErrorClassifiesTLSWithoutTheURL(t *testing.T) {
	a := &PeriodicalAnnouncer{Tracker: &fakeTracker{}}
	tlsErr := &url.Error{
		Op:  "Get",
		URL: "https://tracker.test/secret-key/announce",
		Err: errors.New("tls: failed to verify certificate: x509: certificate is not valid for tracker.test"),
	}
	got := a.newAnnounceError(tlsErr)
	if got.Message != "TLS certificate rejected: tracker.test" {
		t.Fatalf("tls failure -> %q", got.Message)
	}
}
