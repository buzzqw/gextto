package torrent

import (
	"fmt"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/webseedsource"
)

// testWebseedSources builds n sources with distinct URLs.
func testWebseedSources(n int) []*webseedsource.WebseedSource {
	sources := make([]*webseedsource.WebseedSource, 0, n)
	for i := 0; i < n; i++ {
		sources = append(sources, &webseedsource.WebseedSource{URL: fmt.Sprintf("http://seed%d.test/file", i)})
	}
	return sources
}

// TestCapWebseedSources covers the configured ceiling: zero means "no ceiling"
// and a ceiling below the list length must truncate to that ceiling instead of
// slicing past the end.
func TestCapWebseedSources(t *testing.T) {
	cases := []struct {
		name     string
		have     int
		max      int
		want     int
		wantSame bool
	}{
		{name: "no ceiling keeps every source", have: 20, max: 0, want: 20, wantSame: true},
		{name: "negative ceiling keeps every source", have: 3, max: -1, want: 3, wantSame: true},
		{name: "list below the ceiling is untouched", have: 5, max: 20, want: 5, wantSame: true},
		{name: "list at the ceiling is untouched", have: 4, max: 4, want: 4, wantSame: true},
		{name: "ceiling below the list truncates", have: 6, max: 4, want: 4},
		{name: "default ceiling truncates", have: 25, max: 10, want: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources := testWebseedSources(tc.have)
			got := capWebseedSources(sources, tc.max)
			if len(got) != tc.want {
				t.Fatalf("capWebseedSources(%d sources, max %d) returned %d sources, want %d", tc.have, tc.max, len(got), tc.want)
			}
			if tc.wantSame && len(got) > 0 && &got[0] != &sources[0] {
				t.Fatal("capWebseedSources copied a list that already fitted the ceiling")
			}
			for i, source := range got {
				if want := fmt.Sprintf("http://seed%d.test/file", i); source.URL != want {
					t.Fatalf("source %d is %q, want %q: the surviving sources must keep their order", i, source.URL, want)
				}
			}
		})
	}
}
