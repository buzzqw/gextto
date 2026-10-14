package torrent

import (
	"fmt"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

// v1MatchInfo builds a v1 info with n files named prefix/i, all of the same
// length (the worst case: the length never short-circuits a comparison).
func v1MatchInfo(prefix string, n int) *metainfo.Info {
	info := &metainfo.Info{Files: make([]metainfo.File, n)}
	for i := range info.Files {
		info.Files[i] = metainfo.File{Path: fmt.Sprintf("%s/season/episode-%05d.mkv", prefix, i), Length: 1 << 20}
	}
	return info
}

// BenchmarkV1FilesMatchNoMatch measures the candidate filter on two unrelated
// torrents of a few files and of a large pack.
func BenchmarkV1FilesMatchNoMatch(b *testing.B) {
	for _, n := range []int{1, 8, 64, 2000} {
		a, o := v1MatchInfo("a", n), v1MatchInfo("b", n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if v1FilesMatch(a, o) {
					b.Fatal("unexpected match")
				}
			}
		})
	}
}

// TestV1FilesMatchBothPaths: the nested scan and the map path agree, padding
// files never match, and a shared path with a different length is no match.
func TestV1FilesMatchBothPaths(t *testing.T) {
	for _, n := range []int{4, 200} { // 16 pairs: nested scan; 40000: map
		a, b := v1MatchInfo("a", n), v1MatchInfo("b", n)
		if v1FilesMatch(a, b) {
			t.Fatalf("n=%d: unrelated torrents matched", n)
		}
		b.Files[n-1] = metainfo.File{Path: a.Files[n/2].Path, Length: a.Files[n/2].Length + 1}
		if v1FilesMatch(a, b) || v1FilesMatch(b, a) {
			t.Fatalf("n=%d: a different length matched", n)
		}
		b.Files[n-1].Length = a.Files[n/2].Length
		if !v1FilesMatch(a, b) || !v1FilesMatch(b, a) {
			t.Fatalf("n=%d: a shared file did not match", n)
		}
		a.Files[n/2].Padding, b.Files[n-1].Padding = true, true
		if v1FilesMatch(a, b) {
			t.Fatalf("n=%d: padding files matched", n)
		}
	}
	// Asymmetric sizes take the map path with the smaller side indexed.
	big, small := v1MatchInfo("x", 2000), v1MatchInfo("y", 1)
	small.Files[0] = big.Files[1999]
	if !v1FilesMatch(big, small) || !v1FilesMatch(small, big) {
		t.Fatal("a shared file between a pack and a single file did not match")
	}
}
