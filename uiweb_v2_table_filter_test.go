package gextto

import (
	"fmt"
	"testing"
)

// TestV2FilterItemsSearchesValues pins the row filter: it looks into the
// values (nested ones too, numbers included), not the field names, and finds
// the characters JSON would have escaped.
func TestV2FilterItemsSearchesValues(t *testing.T) {
	items := []map[string]any{
		{"name": "Series A", "size": float64(1080), "tags": []any{"Rock & Roll"}},
		{"name": "Series B", "meta": map[string]any{"group": "GRUPPO"}},
		{"name": "Città <bella>", "size": float64(720)},
	}
	cases := map[string][]string{
		"series":  {"Series A", "Series B"},
		"1080":    {"Series A"},
		"& roll":  {"Series A"},
		"gruppo":  {"Series B"},
		"città":   {"Città <bella>"},
		"<bella>": {"Città <bella>"},
		"name":    {},
		"size":    {},
	}
	for filter, want := range cases {
		got := v2FilterItems(items, filter)
		names := []string{}
		for _, item := range got {
			names = append(names, item["name"].(string))
		}
		if fmt.Sprint(names) != fmt.Sprint(want) {
			t.Errorf("filter %q kept %v, want %v", filter, names, want)
		}
	}
}

// TestV2SortItemsOrdersNumbersAndText pins the sort: numbers by value, text
// case-insensitively, equal keys in their original order, desc reversed.
func TestV2SortItemsOrdersNumbersAndText(t *testing.T) {
	items := []map[string]any{
		{"id": "1", "v": float64(10)}, {"id": "2", "v": float64(9)}, {"id": "3", "v": "100"},
		{"id": "4", "v": float64(9)},
	}
	v2SortItems(items, uiColumn{Key: "v"}, "asc")
	order := ""
	for _, item := range items {
		order += item["id"].(string)
	}
	if order != "2413" {
		t.Errorf("numeric ascending order = %s, want 2413", order)
	}
	text := []map[string]any{{"id": "1", "v": "beta"}, {"id": "2", "v": "Alpha"}, {"id": "3", "v": "gamma"}}
	v2SortItems(text, uiColumn{Key: "v"}, "desc")
	order = ""
	for _, item := range text {
		order += item["id"].(string)
	}
	if order != "312" {
		t.Errorf("text descending order = %s, want 312", order)
	}
}

func BenchmarkV2FilterItems(b *testing.B) {
	items := make([]map[string]any, 2000)
	for i := range items {
		items[i] = map[string]any{"name": fmt.Sprintf("Series %d S01E%02d 1080p WEB-DL", i, i%24), "size": float64(i * 1000), "state": "seeding", "tags": []any{"tv"}}
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v2FilterItems(items, "e05")
	}
}

func BenchmarkV2SortItems(b *testing.B) {
	base := make([]map[string]any, 2000)
	for i := range base {
		base[i] = map[string]any{"name": fmt.Sprintf("Series %d", (i*7919)%2000)}
	}
	items := make([]map[string]any, len(base))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		copy(items, base)
		v2SortItems(items, uiColumn{Key: "name"}, "asc")
	}
}
