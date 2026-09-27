package gextto

import (
	"strings"
	"testing"
)

func TestEngineCooldownBlocksAfterAFailure(t *testing.T) {
	if engine_in_cooldown("test-cooldown-engine") {
		t.Fatal("engine should not start in cooldown")
	}
	set_engine_cooldown("test-cooldown-engine", "HTTP 429 Too Many Requests")
	if !engine_in_cooldown("test-cooldown-engine") {
		t.Fatal("engine should be in cooldown after a 429")
	}
	// Un motore diverso non è toccato.
	if engine_in_cooldown("test-cooldown-other") {
		t.Fatal("unrelated engine must not be in cooldown")
	}
}

func TestBuildsSanitizedMagnetWithTrackers(t *testing.T) {
	magnet := build_magnet("0123456789012345678901234567890123456789", "Example Show")
	if magnet == "" {
		t.Fatal("build_magnet returned no magnet")
	}
	if !strings.Contains(magnet, "xt=urn:btih:0123456789012345678901234567890123456789") {
		t.Fatalf("magnet = %q", magnet)
	}
	if !strings.Contains(magnet, "&dn=Example%20Show") && !strings.Contains(magnet, "&dn=Example+Show") {
		t.Fatalf("magnet missing display name: %q", magnet)
	}
	if !strings.Contains(magnet, "&tr=") {
		t.Fatalf("magnet missing trackers: %q", magnet)
	}
}

func TestExtractsAndSanitizesHTMLMagnet(t *testing.T) {
	body := `<a href="magnet:?xt=urn:btih:0123456789012345678901234567890123456789&dn=Example%20Show&x=bad space">download</a>`
	magnet := first_magnet(body)
	if !strings.HasPrefix(magnet, "magnet:?xt=urn:btih:") {
		t.Fatalf("magnet = %q", magnet)
	}
	if strings.Contains(magnet, "bad space") {
		t.Fatalf("magnet should be sanitized: %q", magnet)
	}
}
