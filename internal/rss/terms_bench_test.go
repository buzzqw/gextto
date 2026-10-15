package rss

import (
	"testing"
	"time"
)

// BenchmarkCriteriaMatch measures one rule over typical release titles: the
// cost a feed poll and the rule preview (5,000 archived items) pay per item.
func BenchmarkCriteriaMatch(b *testing.B) {
	c := Criteria{Include: []string{"1080p", "2160p"}, Exclude: []string{"cam", "ts"}}
	titles := []string{
		"The.Office.US.S03E01.1080p.BluRay.x265-GRP",
		"Ted Lasso S01E07 720p WEB-DL ITA ENG x264-GRP",
		"Dune.Part.Two.2024.2160p.UHD.BluRay.ITA.ENG.HDR.x265",
		"Some.Movie.2022.CAM.ITA",
	}
	now := time.Now()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Match(Item{Title: titles[i%len(titles)]}, now)
	}
}
