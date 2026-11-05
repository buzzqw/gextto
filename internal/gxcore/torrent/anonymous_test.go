package torrent

import "testing"

// TestAnonymousPeerIDPrefix proves the fingerprint is the only thing removed:
// the client code survives, the version does not.
func TestAnonymousPeerIDPrefix(t *testing.T) {
	cases := map[string]string{
		"-GX1.2.2066-": "-GX0000-",
		"-qB5230-":     "-qB0000-",
		"-TR4130-":     "-TR0000-",
		"":             "-GX0000-",
	}
	for prefix, want := range cases {
		if got := anonymousPeerIDPrefix(prefix); got != want {
			t.Errorf("anonymousPeerIDPrefix(%q) = %q, want %q", prefix, got, want)
		}
	}
}

// TestAnonymousModeHidesTheClientVersion covers the BEP 10 handshake and the
// HTTP tracker agent: public trackers and peers see no version, private
// trackers keep the configured agent (qBittorrent's behavior).
func TestAnonymousModeHidesTheClientVersion(t *testing.T) {
	s := &Session{config: Config{
		AnonymousMode:                          true,
		PublicExtensionHandshakeClientVersion:  "gx-core 1.2.2066",
		PrivateExtensionHandshakeClientVersion: "gx-core 1.2.2066",
		PublicTrackerHTTPUserAgent:             "gx-core/1.2.2066",
		TrackerHTTPPrivateUserAgent:            "gx-core/1.2.2066",
	}}
	if got := s.clientVersion(false); got != "" {
		t.Errorf("public client version = %q, want empty", got)
	}
	if got := s.clientVersion(true); got != "" {
		t.Errorf("private client version = %q, want empty", got)
	}
	if got := s.getTrackerUserAgent(false); got != "" {
		t.Errorf("public tracker agent = %q, want empty", got)
	}
	if got := s.getTrackerUserAgent(true); got != "gx-core/1.2.2066" {
		t.Errorf("private tracker agent = %q, want the configured one", got)
	}
}

// TestAnonymousModeOffKeepsTheIdentity pins that the default is untouched.
func TestAnonymousModeOffKeepsTheIdentity(t *testing.T) {
	s := &Session{config: Config{
		PublicExtensionHandshakeClientVersion:  "gx-core 1.2.2066",
		PrivateExtensionHandshakeClientVersion: "gx-core private",
		PublicTrackerHTTPUserAgent:             "gx-core/1.2.2066",
		TrackerHTTPPrivateUserAgent:            "gx-core/private",
	}}
	if got := s.clientVersion(false); got != "gx-core 1.2.2066" {
		t.Errorf("public client version = %q", got)
	}
	if got := s.clientVersion(true); got != "gx-core private" {
		t.Errorf("private client version = %q", got)
	}
	if got := s.getTrackerUserAgent(false); got != "gx-core/1.2.2066" {
		t.Errorf("public tracker agent = %q", got)
	}
	if got := s.getTrackerUserAgent(true); got != "gx-core/private" {
		t.Errorf("private tracker agent = %q", got)
	}
}
