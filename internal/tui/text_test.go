package tui

import (
	"strings"
	"testing"
)

func TestSanitizeRemovesTerminalSequences(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"plain title", "plain title"},
		{"evil\x1b]52;c;aGVsbG8=\x07 title", "evil title"},
		{"evil\x1b]0;window\x1b\\ title", "evil title"},
		{"\x1b[31mred\x1b[0m", "red"},
		{"\x1b[2J\x1b[Hwipe", "wipe"},
		{"a\tb", "a b"},
		{"line1\nline2", "line1 line2"},
		{"bell\x07", "bell"},
		{"c1\u009b31mx", "c1x"},
		{"àè 日本", "àè 日本"},
		{"bad\xffbyte", "bad?byte"},
		{"WARN ⚠️ stuck ⏸️ ♻️", "WARN ⚠ stuck ⏸ ♻"},
	}
	for _, tc := range cases {
		if got := Sanitize(tc.input); got != tc.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestStringWidthCountsCells(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"abc", 3},
		{"àèì", 3},
		{"日本語", 6},
		{"🎬 film", 7},
		{"é", 1}, // e + combining acute
		{"한국", 4},
	}
	for _, tc := range cases {
		if got := StringWidth(tc.input); got != tc.want {
			t.Errorf("StringWidth(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestShortenAndPadUseCells(t *testing.T) {
	if got := Shorten("日本語のタイトル", 7); StringWidth(got) > 7 || !strings.HasSuffix(got, "…") {
		t.Fatalf("Shorten wide = %q (width %d)", got, StringWidth(got))
	}
	if got := Shorten("short", 10); got != "short" {
		t.Fatalf("Shorten short = %q", got)
	}
	if got := PadRight("日本", 6); StringWidth(got) != 6 {
		t.Fatalf("PadRight width = %d (%q)", StringWidth(got), got)
	}
	if got := PadLeft("日本", 6); StringWidth(got) != 6 || !strings.HasPrefix(got, "  ") {
		t.Fatalf("PadLeft = %q", got)
	}
}

func TestWrapText(t *testing.T) {
	lines := WrapText("one two three four five six", 10, 0)
	want := []string{"one two", "three four", "five six"}
	if strings.Join(lines, "|") != strings.Join(want, "|") {
		t.Fatalf("WrapText words = %q", lines)
	}
	long := "path: /very/long/path/without/any/space/inside/it"
	lines = WrapText(long, 20, 6)
	for index, line := range lines {
		if StringWidth(line) > 20 {
			t.Fatalf("line %d too wide: %q", index, line)
		}
		if index > 0 && !strings.HasPrefix(line, "      ") {
			t.Fatalf("continuation without indent: %q", line)
		}
	}
	if joined := strings.ReplaceAll(strings.Join(lines, ""), " ", ""); joined != strings.ReplaceAll(long, " ", "") {
		t.Fatalf("WrapText lost text: %q", lines)
	}
	if lines := WrapText("日本語日本語日本語", 7, 0); len(lines) < 3 || StringWidth(lines[0]) > 7 {
		t.Fatalf("wide wrap = %q", lines)
	}
	if lines := WrapText("fits", 10, 2); len(lines) != 1 || lines[0] != "fits" {
		t.Fatalf("short wrap = %q", lines)
	}
}

func TestASCIIFoldKeepsCellCount(t *testing.T) {
	input := "Modalità · ↓▁▃█ …"
	got := ASCIIFold(input)
	if got != "Modalita | v_:# ~" {
		t.Fatalf("ASCIIFold = %q", got)
	}
	if StringWidth(got) != StringWidth(input) {
		t.Fatalf("ASCIIFold changed the width: %d vs %d", StringWidth(got), StringWidth(input))
	}
	if got := ASCIIFold("日本"); got != "????" {
		t.Fatalf("wide runes should keep two cells, got %q", got)
	}
}

func TestLocaleIsUTF8(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	cases := []struct {
		values map[string]string
		want   bool
	}{
		{map[string]string{}, true},
		{map[string]string{"LANG": "it_IT.UTF-8"}, true},
		{map[string]string{"LANG": "C"}, false},
		{map[string]string{"LANG": "en_US.utf8"}, true},
		{map[string]string{"LC_ALL": "POSIX", "LANG": "it_IT.UTF-8"}, false},
		{map[string]string{"LC_CTYPE": "C.UTF-8", "LANG": "C"}, true},
	}
	for _, tc := range cases {
		if got := LocaleIsUTF8(env(tc.values)); got != tc.want {
			t.Errorf("LocaleIsUTF8(%v) = %v, want %v", tc.values, got, tc.want)
		}
	}
}
