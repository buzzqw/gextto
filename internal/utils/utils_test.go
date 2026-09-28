package utils

import "testing"

func TestMagnetHashAcceptsHexBase32AndV2(t *testing.T) {
	hex40 := "0123456789012345678901234567890123456789"
	if got, ok := MagnetHash("magnet:?xt=urn:btih:" + hex40); !ok || got != hex40 {
		t.Fatalf("v1 hex: %q %v", got, ok)
	}
	base32 := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	if got, ok := MagnetHash("magnet:?xt=urn:btih:" + base32); !ok || len(got) != 32 {
		t.Fatalf("v1 base32: %q %v", got, ok)
	}
	if _, ok := MagnetHash("magnet:?xt=urn:btih:short"); ok {
		t.Fatal("short hash accepted")
	}
	if _, ok := MagnetHash("not a magnet"); ok {
		t.Fatal("non-magnet accepted")
	}
	digest := ""
	for i := 0; i < 64; i++ {
		digest += "a"
	}
	if got, ok := MagnetHash("magnet:?xt=urn:btmh:1220" + digest); !ok || got != digest {
		t.Fatalf("v2: %q %v", got, ok)
	}
	// Hybrid prefers v1 for stable DB keys.
	hybrid := "magnet:?xt=urn:btmh:1220" + digest + "&xt=urn:btih:" + hex40
	if got, _ := MagnetHash(hybrid); got != hex40 {
		t.Fatalf("hybrid = %q, want v1", got)
	}
}

func TestSanitizeMagnetKeepsNameAndTrackers(t *testing.T) {
	hash := "0123456789012345678901234567890123456789"
	input := "magnet:?xt=urn:btih:" + hash +
		"&dn=Example.Show&tr=udp://tracker.example:80&tr=udp://tracker.example:80&tr=https://other.example/announce"
	out, ok := SanitizeMagnet(input, nil)
	if !ok {
		t.Fatal("sanitize failed")
	}
	if len(out) == 0 || out[:len("magnet:?xt=urn:btih:")] != "magnet:?xt=urn:btih:" {
		t.Fatalf("unexpected prefix: %q", out)
	}
	count := 0
	for i := 0; i+4 <= len(out); i++ {
		if out[i:i+4] == "&tr=" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("trackers = %d, want 2 (sorted, deduped)", count)
	}
	if _, ok := SanitizeMagnet("http://example.com/file.torrent", nil); ok {
		t.Fatal("non-magnet accepted")
	}
}

func TestParseSizeMB(t *testing.T) {
	if got := ParseSizeMB("Size: 4.5 GB"); got != 4.5*1024 {
		t.Fatalf("4.5 GB = %v", got)
	}
	if got := ParseSizeMB("700 MB"); got != 700 {
		t.Fatalf("700 MB = %v", got)
	}
	if got := ParseSizeMB("500 KiB"); got < 0.48 || got > 0.49 {
		t.Fatalf("500 KiB = %v", got)
	}
	if got := ParseSizeMB("nessuna dimensione"); got != 0 {
		t.Fatalf("no size = %v", got)
	}
}

func TestParseDateAny(t *testing.T) {
	if _, ok := ParseDateAny("pubblicato oggi"); !ok {
		t.Fatal("oggi not parsed")
	}
	if _, ok := ParseDateAny("ieri"); !ok {
		t.Fatal("ieri not parsed")
	}
	date, ok := ParseDateAny("data: 2026-09-21")
	if !ok || date.Year() != 2026 || date.Month() != 9 || date.Day() != 21 {
		t.Fatalf("ymd = %v %v", date, ok)
	}
	date, ok = ParseDateAny("21/09/2026")
	if !ok || date.Day() != 21 || date.Month() != 9 || date.Year() != 2026 {
		t.Fatalf("dmy = %v %v", date, ok)
	}
	if _, ok := ParseDateAny("nessuna data qui"); ok {
		t.Fatal("unexpected parse")
	}
}

func TestRedactURLSecrets(t *testing.T) {
	value := RedactURLSecrets("request failed for http://x/api?query=Lanterns&apikey=secret123&type=search")
	if value == "" || contains(value, "secret123") {
		t.Fatalf("secret not redacted: %q", value)
	}
	if got := RedactURLSecrets("apikey=one&api_key=two"); got != "apikey=[redacted]&api_key=[redacted]" {
		t.Fatalf("multi redact = %q", got)
	}
}

func TestStableIDAndCondensedKey(t *testing.T) {
	id := StableID("example")
	if StableID("example") != id || len(id) != 40 {
		t.Fatal("stable id not deterministic hex40")
	}
	if StableID("a") == StableID("b") {
		t.Fatal("stable id collision")
	}
	if got := CondensedKey("The Veil (2024)"); got != "theveil2024" {
		t.Fatalf("condensed = %q", got)
	}
	if got := CondensedKey("F.B.I."); got != "fbi" {
		t.Fatalf("condensed = %q", got)
	}
}

func TestTorrentInfoHash(t *testing.T) {
	body := []byte("d8:announce12:http://t/ann4:infod6:lengthi100e4:name3:abcee")
	hash, ok := TorrentInfoHash(body)
	if !ok || len(hash) != 40 {
		t.Fatalf("infohash = %q %v", hash, ok)
	}
	// info dictionary SHA-1 is deterministic.
	if _, ok := TorrentInfoHash([]byte("not a torrent")); ok {
		t.Fatal("invalid torrent hashed")
	}
}

func TestExtractCleanMovieName(t *testing.T) {
	cases := map[string]string{
		"The.Veil.2024.1080p.BluRay [Jackett RSS]": "The Veil",
		"Example.Movie.1080p.WEB-DL.ITA":           "Example Movie",
		"Heat":                                     "Heat",
	}
	for input, want := range cases {
		if got := ExtractCleanMovieName(input); got != want {
			t.Errorf("ExtractCleanMovieName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestClassifyInterface(t *testing.T) {
	cases := map[string]string{"tun0": "VPN", "wg0": "VPN", "wlp3s0": "WiFi", "eth0": "Ethernet", "lo": "Loopback"}
	for name, want := range cases {
		if got := ClassifyInterface(name); got != want {
			t.Errorf("ClassifyInterface(%q) = %q, want %q", name, got, want)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
