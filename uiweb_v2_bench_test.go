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
		Groups:  v2NavGroups("license", nil),
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
