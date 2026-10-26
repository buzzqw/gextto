package gextto

import (
	stdhtml "html"
	"html/template"
	"regexp"
	"testing"
)

// v2LogHighlightReference and v2HighlightLogLineReference are the regular
// expression highlighter the word scan replaced: the tests hold the two to the
// same output.
var v2LogHighlightReference = []struct {
	re    *regexp.Regexp
	class string
}{
	{regexp.MustCompile(`\b(ERROR)\b`), "hl-err"},
	{regexp.MustCompile(`\b(WARN|WARNING)\b`), "hl-warn"},
	{regexp.MustCompile(`\b(completed|completato|archived|archiviato|moved|approved|approvato)\b`), "hl-ok"},
	{regexp.MustCompile(`\b(indexer|feed|source|sorgente|scraping|engine)\b`), "hl-src"},
	{regexp.MustCompile(`\b(upgrade|score|punteggio)\b`), "hl-score"},
	{regexp.MustCompile(`\b(filter|filtered|rejected|scartato|skipped|blocklist|stalled)\b`), "hl-filter"},
}

func v2HighlightLogLineReference(line string) template.HTML {
	escaped := stdhtml.EscapeString(line)
	for _, rule := range v2LogHighlightReference {
		if rule.re.MatchString(escaped) {
			escaped = rule.re.ReplaceAllString(escaped, `<span class="`+rule.class+`">$1</span>`)
		}
	}
	return template.HTML(escaped)
}

func TestV2HighlightLogLineMatchesRegexpReference(t *testing.T) {
	lines := []string{
		"2026-10-26 20:02:16  INFO ━━━━━━━━",
		"2026-10-26 20:02:16  ERROR the engine stalled: <b>bad</b> & 'quoted' \"x\"",
		"WARNING: feed indexer source skipped (score 120, upgrade) — filtered/rejected",
		"WARN_x xWARN WARN1 _WARN WARN.ERROR;completed,archived:moved approved",
		"completatoè sorgente_ scartato9 blocklist-stalled punteggio🙂engine",
		"ERRORS WARNINGS Completed ENGINE engines",
		"&lt;ERROR&gt; filter",
		"",
	}
	for _, line := range lines {
		if got, want := v2HighlightLogLine(line), v2HighlightLogLineReference(line); got != want {
			t.Fatalf("%q:\n got  %s\n want %s", line, got, want)
		}
	}
}

func FuzzV2HighlightLogLineMatchesRegexpReference(f *testing.F) {
	f.Add("2026-10-26 20:02:16  WARN feed skipped: score 120 <upgrade>")
	f.Add("ERROR_ ERROR é ERROR")
	f.Fuzz(func(t *testing.T, line string) {
		if got, want := v2HighlightLogLine(line), v2HighlightLogLineReference(line); got != want {
			t.Fatalf("%q:\n got  %s\n want %s", line, got, want)
		}
	})
}

func BenchmarkV2HighlightLogLine(b *testing.B) {
	line := "2026-10-26 20:02:16  INFO ✅ The indexer «indexarr» at 192.168.1.161 answered the test (HTTP 200): feed skipped, score 120"
	b.ReportAllocs()
	for b.Loop() {
		_ = v2HighlightLogLine(line)
	}
}

func BenchmarkV2HighlightLogLineRegexp(b *testing.B) {
	line := "2026-10-26 20:02:16  INFO ✅ The indexer «indexarr» at 192.168.1.161 answered the test (HTTP 200): feed skipped, score 120"
	b.ReportAllocs()
	for b.Loop() {
		_ = v2HighlightLogLineReference(line)
	}
}
