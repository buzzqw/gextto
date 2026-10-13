package gextto

import (
	"strings"
	"testing"
)

// TestUIMergeReuseRow: the Swarm Merging row appears only when something was
// reused, and reports the pieces and the bytes.
func TestUIMergeReuseRow(t *testing.T) {
	if _, ok := uiMergeReuseRow(map[string]any{}); ok {
		t.Fatal("no merged pieces must hide the row")
	}
	if _, ok := uiMergeReuseRow(map[string]any{"merged_pieces": int64(0)}); ok {
		t.Fatal("zero merged pieces must hide the row")
	}
	row, ok := uiMergeReuseRow(map[string]any{"merged_pieces": int64(3), "merged_bytes": int64(2048)})
	if !ok {
		t.Fatal("merged pieces must show the row")
	}
	if !strings.Contains(row, "3 pezzi") || !strings.Contains(row, "2.0 KB") {
		t.Fatalf("row = %q, want the piece count and the bytes", row)
	}
	withPartial, _ := uiMergeReuseRow(map[string]any{
		"merged_pieces": int64(3), "merged_bytes": int64(2048), "merged_partial_pieces": int64(2),
	})
	if !strings.Contains(withPartial, "2 saltati per sorgente parziale") {
		t.Fatalf("row = %q, want the partial-source count", withPartial)
	}
}
