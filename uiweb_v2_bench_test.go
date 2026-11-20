package gextto

// uiweb_v2_bench_test.go benchmarks the v2 rendering hot paths: HTML
// translation, cell formatting and full fragment/shell rendering. They do not
// need an AppState, so they run anywhere.

import (
	"bytes"
	"html/template"
	"strconv"
	"strings"
	"testing"
)

func benchTranslateDict() map[string]string {
	dict := make(map[string]string, 3000)
	for index := 0; index < 3000; index++ {
		dict["chiave-"+strconv.Itoa(index)] = "valore"
	}
	dict["Percorsi"] = "Paths"
	return dict
}

func BenchmarkV2TranslateHTML(b *testing.B) {
	dict := benchTranslateDict()
	raw := `<div class="panel"><div class="panel-head"><h3>Percorsi</h3></div><div class="panel-body">` +
		strings.Repeat(`<div class="row"><span>Percorsi</span><strong title="Percorsi">valore</strong></div>`, 200) +
		`<script>var x = "Percorsi";</script></div></div>`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := v2TranslateHTML(raw, dict, nil); !strings.Contains(out, "Paths") {
			b.Fatal("translation failed")
		}
	}
}

func BenchmarkV2FormatCell(b *testing.B) {
	item := map[string]any{
		"title": "Una release di esempio", "source": "https://www.example-tracker.org/x",
		"quality_score": float64(42), "size_bytes": float64(5_000_000_000), "added_at": "2026-01-02 03:04:05",
		"episodes_downloaded": float64(5), "episodes_total": float64(10), "enabled": true,
	}
	columns := []uiColumn{
		{Key: "title", Format: "truncate"}, {Key: "source", Format: "source"},
		{Key: "quality_score", Format: "number"}, {Key: "", Format: "completion"},
		{Key: "added_at", Format: "datetime"}, {Key: "size_bytes", Format: "bytes"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, column := range columns {
			_ = v2FormatCell(item, column)
		}
	}
}

// benchTableItems is a synthetic page: 100 rows over a handful of trackers and
// timestamps, the shape of a search or blocklist table. The repeated values are
// what the per-render cache can reuse and the unique titles are what it cannot.
func benchTableItems(rows int) []map[string]any {
	sources := []string{
		"https://www.example-tracker.org/x", "tracker.example.co.uk", "https://mirror.example.net:8443/a",
		"another-tracker.example.com", "https://www.example.org/b",
	}
	items := make([]map[string]any, 0, rows)
	for index := 0; index < rows; index++ {
		items = append(items, map[string]any{
			"title":               "Release " + strconv.Itoa(index) + " di esempio",
			"source":              sources[index%len(sources)],
			"quality_score":       float64(40 + index%10),
			"size_bytes":          float64(1_000_000_000 + index*4096),
			"added_at":            "2026-01-" + strconv.Itoa(1+index%20) + " 03:04:05",
			"released_at":         "2026-02-" + strconv.Itoa(1+index%20) + " 03:04:05",
			"episodes_downloaded": float64(index % 10),
			"episodes_total":      float64(10),
			"enabled":             index%2 == 0,
			"name":                "Serie " + strconv.Itoa(index),
		})
	}
	return items
}

// BenchmarkV2TableRows measures the per-row work of the table body with a
// non-nil dictionary, which is what a page in any language but Italian pays:
// eight cells plus the row actions for a hundred rows. The "cached" sub-case is
// the code the table runs (one dictionary resolution and memoized fragments),
// "per-cell" is the path it replaced (a translation per cell, no reuse).
func BenchmarkV2TableRows(b *testing.B) {
	dict := benchTranslateDict()
	for key, value := range map[string]string{
		"attiva": "active", "in pausa": "paused", "Attiva": "Enable", "Pausa": "Pause",
		"Apri il dettaglio": "Open the detail", "Azioni": "Actions",
	} {
		dict[key] = value
	}
	items := benchTableItems(100)
	columns := []uiColumn{
		{Key: "title", Format: "truncate"}, {Key: "source", Format: "source"},
		{Key: "quality_score", Format: "number"}, {Key: "", Format: "completion"},
		{Key: "added_at", Format: "datetime"}, {Key: "size_bytes", Format: "bytes"},
		{Key: "enabled", Format: "enabled"}, {Key: "released_at", Format: "date"},
	}
	actions := []uiAction{{Label: "Attiva", Method: "POST", Path: "/table/library", Kind: "library-toggle"}}
	spec := uiTableSpec{ItemsKey: "series"}

	b.Run("cached", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache := &v2RenderCache{dict: dict}
			for _, item := range items {
				for _, column := range columns {
					_ = cache.fragment(v2FormatCellCached(item, column, cache))
				}
				_ = cache.fragment(v2RenderActions("bench", item, actions, spec))
			}
		}
	})

	b.Run("per-cell", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, item := range items {
				for _, column := range columns {
					_ = template.HTML(v2TranslateHTML(string(v2FormatCell(item, column)), dict, nil))
				}
				_ = template.HTML(v2TranslateHTML(string(v2RenderActions("bench", item, actions, spec)), dict, nil))
			}
		}
	})
}

func benchTableData(rows int) v2TableData {
	data := v2TableData{View: "bench", Title: "Benchmark", Columns: []uiColumn{{Key: "name", Label: "Nome"}, {Key: "score", Label: "Punteggio"}}, Colspan: 2, Total: rows}
	for index := 0; index < rows; index++ {
		data.Rows = append(data.Rows, v2TableRow{Cells: []v2Cell{{HTML: template.HTML("Riga")}, {HTML: template.HTML("42")}}})
	}
	return data
}

func BenchmarkV2RenderTableFragment(b *testing.B) {
	data := benchTableData(500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buffer bytes.Buffer
		if err := v2Templates.ExecuteTemplate(&buffer, "v2_table_body", data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkV2RenderShell(b *testing.B) {
	page := v2ShellData{
		Title:   "Licenza",
		Page:    "license",
		Body:    "v2_license",
		Groups:  v2NavGroups(nil, "license", nil),
		Content: uiLicenseData{Text: strings.Repeat("testo della licenza\n", 500)},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buffer bytes.Buffer
		if err := v2Templates.ExecuteTemplate(&buffer, "v2_shell", page); err != nil {
			b.Fatal(err)
		}
	}
}
