package magnet

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestSelectOnlyHostileRanges: a range ending at the largest int must not loop
// forever, and many ranges must not expand to millions of indices.
func TestSelectOnlyHostileRanges(t *testing.T) {
	done := make(chan []int, 1)
	go func() {
		done <- parseSelectOnly([]string{"9223372036854775806-9223372036854775807,1"})
	}()
	select {
	case got := <-done:
		if len(got) != 1 || got[0] != 1 {
			t.Fatalf("huge range: got %v, want [1]", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parseSelectOnly does not terminate on a range at the int limit")
	}

	var parts []string
	for i := 0; i < 500; i++ {
		lo := i * (1 << 20)
		parts = append(parts, strconv.Itoa(lo)+"-"+strconv.Itoa(lo+(1<<20)-1))
	}
	got := parseSelectOnly([]string{strings.Join(parts, ",")})
	if len(got) > maxSelectOnlyTotal {
		t.Fatalf("expanded to %d indices, cap is %d", len(got), maxSelectOnlyTotal)
	}

	if got := parseSelectOnly([]string{"0-3,7"}); len(got) != 5 || got[4] != 7 {
		t.Fatalf("ordinary ranges: %v", got)
	}
}
