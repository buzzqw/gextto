package gextto

import (
	"fmt"
	"strings"
	"testing"
)

func TestPackFileNames(t *testing.T) {
	if got := tev_packFileNames(nil); got != "nessuno" {
		t.Fatalf("empty = %q, want nessuno", got)
	}
	items := []PackFileResult{
		{Path: "/nas/Show/S01E01.mkv"},
		{Path: "/nas/Show/S01E02.mkv"},
	}
	if got := tev_packFileNames(items); got != "S01E01.mkv, S01E02.mkv" {
		t.Fatalf("names = %q", got)
	}
	many := make([]PackFileResult, 10)
	for index := range many {
		many[index] = PackFileResult{Path: fmt.Sprintf("/nas/Show/S01E%02d.mkv", index+1)}
	}
	got := tev_packFileNames(many)
	if !strings.Contains(got, "… e altri 2") {
		t.Fatalf("truncated list = %q, want '… e altri 2'", got)
	}
}
