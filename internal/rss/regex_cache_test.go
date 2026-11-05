package rss

import (
	"fmt"
	"testing"
	"time"
)

func TestCompiledRegexCachesAndReportsErrors(t *testing.T) {
	first, err := compiledRegex(`(?i)series[\s._\-]+a`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compiledRegex(`(?i)series[\s._\-]+a`)
	if err != nil || first != second {
		t.Fatalf("the same pattern must return the cached regexp")
	}
	if _, err := compiledRegex(`(unclosed`); err == nil {
		t.Fatalf("an invalid pattern must keep failing")
	}
	if _, err := compiledRegex(`(unclosed`); err == nil {
		t.Fatalf("an invalid pattern must keep failing from the cache too")
	}
}

func TestCompiledRegexCacheStaysBounded(t *testing.T) {
	for i := 0; i < regexCacheLimit*3; i++ {
		if _, err := compiledRegex(fmt.Sprintf("pattern-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	regexCache.mu.Lock()
	size := len(regexCache.entries)
	regexCache.mu.Unlock()
	if size > regexCacheLimit {
		t.Fatalf("cache holds %d patterns, limit %d", size, regexCacheLimit)
	}
}

func BenchmarkCriteriaMatchRegex(b *testing.B) {
	c := Criteria{
		Regex:          `(?i)series[\s._\-]+a`,
		NotRegex:       `(?i)\bcam\b`,
		Exclude:        []string{"hdcam"},
		EpisodeFilter:  "1x1-",
		RequireEpisode: true,
	}
	item := Item{Title: "Series.A.S01E05.1080p.WEB.h264-GROUP"}
	now := time.Now()
	b.ReportAllocs()
	for b.Loop() {
		if !c.Match(item, now) {
			b.Fatal("must match")
		}
	}
}
