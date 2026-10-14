package main

import "testing"

func TestMediaContentType(t *testing.T) {
	cases := map[string]string{
		"movie.mkv":           "video/x-matroska",
		"movie.MP4":           "video/mp4",
		"clip.webm":           "video/webm",
		"song.flac":           "audio/flac",
		"subs.srt":            "application/x-subrip",
		"report.pdf":          "application/octet-stream",
		"noext":               "application/octet-stream",
		"/path/to/series.mkv": "video/x-matroska",
	}
	for name, want := range cases {
		if got := mediaContentType(name); got != want {
			t.Errorf("mediaContentType(%q) = %q, want %q", name, got, want)
		}
	}
	if !isMediaFile("a.mkv") || isMediaFile("a.txt") {
		t.Fatal("isMediaFile is wrong")
	}
}

func TestSanitizeFilename(t *testing.T) {
	if got := sanitizeFilename("/x/y/na\"me\r\n.mkv"); got != "name.mkv" {
		t.Fatalf("sanitizeFilename = %q", got)
	}
}

func TestStreamRefcount(t *testing.T) {
	d := &Daemon{}
	d.beginStream("aa")
	d.beginStream("aa")
	if last := d.endStream("aa"); last {
		t.Fatal("the first close must not be the last stream")
	}
	if last := d.endStream("aa"); !last {
		t.Fatal("the second close must be the last stream")
	}
	if last := d.endStream("bb"); !last {
		t.Fatal("closing an unknown stream must report last (nothing to clear)")
	}
}

func TestParseByteRange(t *testing.T) {
	cases := []struct {
		header  string
		start   int64
		length  int64
		partial bool
		wantErr bool
	}{
		{"", 0, 1000, false, false},
		{"bytes=0-99", 0, 100, true, false},
		{"bytes=100-", 100, 900, true, false},
		{"bytes=-100", 900, 100, true, false},
		{"bytes=990-2000", 990, 10, true, false}, // clamped to the size
		{"bytes=1000-", 0, 0, false, true},       // start past the end
		{"bytes=50-10", 0, 0, false, true},       // end before start
		{"bytes=1-2,4-5", 0, 0, false, true},     // multiple ranges
		{"items=0-1", 0, 0, false, true},         // wrong unit
		{"bytes=abc", 0, 0, false, true},
	}
	for _, tc := range cases {
		start, length, partial, err := parseByteRange(tc.header, 1000)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: expected an error", tc.header)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.header, err)
		}
		if start != tc.start || length != tc.length || partial != tc.partial {
			t.Fatalf("%q = %d/%d partial=%v, want %d/%d partial=%v",
				tc.header, start, length, partial, tc.start, tc.length, tc.partial)
		}
	}
}
