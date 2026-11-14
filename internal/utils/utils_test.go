package utils

import (
	"strings"
	"testing"
)

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

// TestSimpleMagnetHashMatchesTheFullParser pins that the fast path over the
// plain magnet form never disagrees with url.Parse, including the forms that
// must fall back (escapes, fragment, ';' separator).
func TestSimpleMagnetHashMatchesTheFullParser(t *testing.T) {
	hex40 := "0123456789012345678901234567890123456789"
	base32 := "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	digest := strings.Repeat("a", 64)
	cases := []string{
		"magnet:?xt=urn:btih:" + hex40,
		"magnet:?xt=urn:btih:" + strings.ToUpper(hex40),
		"magnet:?xt=urn:btih:" + base32,
		"magnet:?xt=urn:btih:" + base32 + "&dn=Name&tr=udp://tracker.example:80",
		"magnet:?dn=Name&xt=urn:btih:" + hex40,
		"magnet:?xt=urn:btmh:1220" + digest,
		"magnet:?xt=urn:btmh:1220" + digest + "&xt=urn:btih:" + hex40,
		"magnet:?xt=urn:btih:" + hex40 + "&xt=urn:btmh:1220" + digest,
		"magnet:?xt=urn:btih:short",
		"magnet:?xt=urn%3Abtih%3A" + hex40,
		"magnet:?xt=urn:btih:" + hex40 + "#fragment",
		"magnet:?xt=urn:btih:" + hex40 + ";dn=x",
		"magnet:?xt=urn:btih:" + hex40 + "&dn=Name+With+Space",
		"not a magnet",
		"magnet:?",
	}
	for _, magnet := range cases {
		want, wantOK := parsedMagnetHashSlow(magnet)
		got, gotOK := ParsedMagnetHash(magnet)
		if got != want || gotOK != wantOK {
			t.Errorf("%q: got %q,%v want %q,%v", magnet, got, gotOK, want, wantOK)
		}
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

// TestSanitizeMagnetRepairsEscapedSeparators: an upstream may hand over a
// magnet that travelled through HTML or a feed sanitiser, with `&amp;` where
// the separator should be. Go's url.ParseQuery then drops everything after the
// semicolon ("invalid semicolon separator in query"), so without the repair the
// name falls back to the title and the trackers are lost.
func TestSanitizeMagnetRepairsEscapedSeparators(t *testing.T) {
	hash := "0123456789012345678901234567890123456789"
	input := "magnet:?xt=urn:btih:" + hash +
		"&amp;dn=Example.Show" +
		"&amp;tr=udp%3A%2F%2Ftracker.example%3A80%2Fannounce" +
		"&amp;tr=https%3A%2F%2Fother.example%2Fannounce"
	out, ok := SanitizeMagnet(input, nil)
	if !ok {
		t.Fatal("sanitize failed")
	}
	if !strings.Contains(out, "&dn=Example.Show") {
		t.Errorf("the escaped name was lost: %q", out)
	}
	if count := strings.Count(out, "&tr="); count != 2 {
		t.Errorf("trackers = %d, want 2: %q", count, out)
	}
}

func TestUnescapeMagnetSeparators(t *testing.T) {
	cases := []struct{ in, want string }{
		{"magnet:?xt=urn:btih:aa&amp;dn=Name", "magnet:?xt=urn:btih:aa&dn=Name"},
		{"magnet:?xt=urn:btih:aa&#38;tr=x", "magnet:?xt=urn:btih:aa&tr=x"},
		{"magnet:?xt=urn:btih:aa&#x26;tr=x", "magnet:?xt=urn:btih:aa&tr=x"},
		{"magnet:?xt=urn:btih:aa&dn=AC&amp;DC", "magnet:?xt=urn:btih:aa&dn=AC&amp;DC"},
		{"magnet:?xt=urn:btih:aa&amp;x1=y", "magnet:?xt=urn:btih:aa&amp;x1=y"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := unescapeMagnetSeparators(tc.in); got != tc.want {
			t.Errorf("unescapeMagnetSeparators(%q) = %q, want %q", tc.in, got, tc.want)
		}
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
func TestIndexASCIIFold(t *testing.T) {
	cases := []struct {
		s, needle string
		want      int
	}{
		{"abc</Script>", "</script", 3},
		{"\xe9\xe9</STYLE>", "</style", 2},
		{"</scrip", "</script", -1},
		{"", "</script", -1},
		{"x", "", 0},
	}
	for _, c := range cases {
		if got := IndexASCIIFold(c.s, c.needle); got != c.want {
			t.Errorf("IndexASCIIFold(%q, %q) = %d, want %d", c.s, c.needle, got, c.want)
		}
	}
}

func TestRedactURLSecretsCatchesAnAPIKeyBeforeAnApikey(t *testing.T) {
	got := RedactURLSecrets("first http://a/api?api_key=FIRST then http://b/api?apikey=SECOND")
	if strings.Contains(got, "FIRST") || strings.Contains(got, "SECOND") {
		t.Fatalf("a key stayed in the clear: %q", got)
	}
}

func TestRedactURLSecretsKeepsOffsetsPastInvalidUTF8(t *testing.T) {
	prefix := strings.Repeat("\xe9", 20)
	got := RedactURLSecrets(prefix + " http://a/api?ApiKey=SECRET&t=search")
	if got != prefix+" http://a/api?ApiKey=[redacted]&t=search" {
		t.Fatalf("redaction landed in the wrong place: %q", got)
	}
}
