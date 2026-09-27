package tui

import "testing"

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		value float64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{-5, "0 B"},
	}
	for _, tc := range cases {
		if got := HumanBytes(tc.value); got != tc.want {
			t.Errorf("HumanBytes(%v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		seconds float64
		want    string
	}{
		{0, "0m"},
		{30, "0m"},
		{90, "1m"},
		{3600, "1h 0m"},
		{3660, "1h 1m"},
		{90000, "1d 1h"},
		{-10, "0m"},
	}
	for _, tc := range cases {
		if got := HumanDuration(tc.seconds); got != tc.want {
			t.Errorf("HumanDuration(%v) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}

func TestShortenAndPadding(t *testing.T) {
	if got := Shorten("hello", 10); got != "hello" {
		t.Errorf("Shorten short = %q", got)
	}
	if got := Shorten("hello world", 8); got != "hello w…" {
		t.Errorf("Shorten long = %q", got)
	}
	if got := Shorten("hello", 1); got != "…" {
		t.Errorf("Shorten width 1 = %q", got)
	}
	if got := Shorten("ciao", 0); got != "" {
		t.Errorf("Shorten width 0 = %q", got)
	}
	if got := PadRight("ab", 5); got != "ab   " {
		t.Errorf("PadRight = %q", got)
	}
	if got := PadLeft("ab", 5); got != "   ab" {
		t.Errorf("PadLeft = %q", got)
	}
	if got := PadRight("abcdef", 3); got != "ab…" {
		t.Errorf("PadRight truncation = %q", got)
	}
}

func TestOptionalNumber(t *testing.T) {
	if got := OptionalNumber(nil, "%", 1); got != "-" {
		t.Errorf("nil = %q", got)
	}
	if got := OptionalNumber(12.34, "%", 1); got != "12.3%" {
		t.Errorf("float = %q", got)
	}
	if got := OptionalNumber(12.34, "", 0); got != "12" {
		t.Errorf("rounded = %q", got)
	}
	if got := OptionalNumber("nope", "", 0); got != "-" {
		t.Errorf("unparsable = %q", got)
	}
}
