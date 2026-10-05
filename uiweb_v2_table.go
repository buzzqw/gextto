package gextto

// uiweb_v2_table.go adds a generic, server-side table renderer to the v2 UI.
//
// The classic interface loads every list page from a JSON endpoint and builds
// the rows in JavaScript (uiTableSpec + renderTable). v2 keeps the same
// uiTableSpec definitions and the same endpoints, but fetches them on the
// server through the internal router and renders the rows with Go formatters.
// This is what lets a single implementation cover every list page (Serie TV,
// Film, Mancanti, Archivio, Blocklist, Fumetti) and the dashboard/health panels
// without duplicating data access.

import (
	"bytes"
	"encoding/json"
	stdhtml "html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// v2Routers maps an AppState to its router so v2 handlers can call the existing
// JSON APIs internally (same handlers, same validation) without a loopback
// network call and without re-implementing data access.
var v2Routers sync.Map

func v2InternalJSON(s *AppState, method, path string, query url.Values, jsonBody []byte) ([]byte, int) {
	muxAny, ok := v2Routers.Load(s)
	if !ok {
		return []byte(`{"ok":false,"error":"v2 router not registered"}`), http.StatusInternalServerError
	}
	mux, ok := muxAny.(*http.ServeMux)
	if !ok {
		return []byte(`{"ok":false,"error":"invalid v2 router"}`), http.StatusInternalServerError
	}
	var body io.Reader
	if len(jsonBody) > 0 {
		body = bytes.NewReader(jsonBody)
	}
	request, err := http.NewRequest(method, path, body)
	if err != nil {
		return []byte(`{"ok":false,"error":"bad internal request"}`), http.StatusInternalServerError
	}
	if len(query) > 0 {
		request.URL.RawQuery = query.Encode()
	}
	if len(jsonBody) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := &v2Recorder{}
	mux.ServeHTTP(recorder, request)
	return recorder.buf.Bytes(), recorder.statusCode()
}

type v2Recorder struct {
	header http.Header
	buf    bytes.Buffer
	code   int
}

func (r *v2Recorder) Header() http.Header {
	if r.header == nil {
		r.header = http.Header{}
	}
	return r.header
}
func (r *v2Recorder) Write(b []byte) (int, error) { return r.buf.Write(b) }
func (r *v2Recorder) WriteHeader(code int)        { r.code = code }
func (r *v2Recorder) statusCode() int {
	if r.code == 0 {
		return http.StatusOK
	}
	return r.code
}

// v2Cell holds one already-formatted (escaped) table cell.
type v2Cell struct{ HTML template.HTML }

type v2TableRow struct {
	Cells   []v2Cell
	Actions template.HTML
}

type v2TableData struct {
	View       string
	BaseView   string
	FooterView string
	Title      string
	Class      string
	Empty      string
	Note       string
	Query      string
	Columns    []uiColumn
	Rows       []v2TableRow
	HasActions bool
	Search     bool
	Page       int
	Pages      int
	Prev       int
	Next       int
	Total      int
	Colspan    int
	// Refresh shows the "Aggiorna" button; Manual marks a table that must not
	// load automatically (the source health check is slow and on demand).
	Refresh       bool
	Manual        bool
	Filter        bool
	FilterQuery   string
	Sort          string
	Dir           string
	FooterForm    *uiFormSection
	FooterActions []uiActionButton
	WebSearch     bool
	Notice        *v2ActionNotice
}

type v2ActionNotice struct {
	Title   string
	Message string
	Error   bool
}

// v2SpecFor returns the table spec of a view. It covers the list pages via
// uiTableSpecFor and the on-demand panels (dashboard calendar, source health,
// providers, download history) by reusing the very same specs.
func v2SpecFor(s *AppState, view string) (uiTableSpec, bool) {
	if spec, ok := uiTableSpecFor(view); ok {
		return spec, true
	}
	switch view {
	case "dashboard-calendar":
		if panels := uiDashboardDataFrom(s).Panels; len(panels) > 0 {
			return panels[0].Table, true
		}
	case "health-sources":
		if panels := uiHealthDataFrom(s).Panels; len(panels) > 0 {
			return panels[0].Table, true
		}
	case "health-providers":
		if panels := uiHealthDataFrom(s).Panels; len(panels) > 1 {
			return panels[1].Table, true
		}
	case "downloads-history":
		if panels := uiDownloadsPageFor(s).Panels; len(panels) > 0 {
			return panels[0].Table, true
		}
	case "comics-downloads":
		if spec, ok := uiTableSpecFor("comics"); ok {
			return v2ComicsDownloadSpec(spec), true
		}
	}
	// Section-composed pages use stable view names such as integrations-t0
	// because one page can contain several tables. Resolve those names back to
	// the original section specs so sorting, pagination and row actions keep
	// working after an HTMX swap.
	if separator := strings.LastIndex(view, "-t"); separator > 0 {
		base := view[:separator]
		index, err := strconv.Atoi(view[separator+2:])
		if err == nil && index >= 0 {
			if page, ok := uiPanelsPageFor(base, s); ok {
				specs := make([]uiTableSpec, 0)
				var collect func([]uiPageSection)
				collect = func(sections []uiPageSection) {
					for _, section := range sections {
						if section.Kind == "table" {
							specs = append(specs, section.Table)
						}
						if section.Kind == "integration" {
							collect(section.Integration.Children)
						}
					}
				}
				collect(page.Sections)
				if index < len(specs) {
					return specs[index], true
				}
			}
		}
	}
	return uiTableSpec{}, false
}

// v2TableDataFrom builds a server-rendered table from a uiTableSpec.
func v2TableDataFrom(s *AppState, r *http.Request, view string, spec uiTableSpec) v2TableData {
	query := url.Values{}
	if spec.SearchParam != "" {
		if q := strings.TrimSpace(r.FormValue("q")); q != "" {
			query.Set(spec.SearchParam, q)
		}
	}
	page := 1
	if value, err := strconv.Atoi(r.FormValue("page")); err == nil && value > 0 {
		page = value
	}
	if spec.PageSize > 0 {
		query.Set("page", strconv.Itoa(page))
		query.Set("limit", strconv.Itoa(spec.PageSize))
	}
	web := r.FormValue("web") == "1" || strings.EqualFold(r.FormValue("web"), "true")
	if web {
		query.Set("web", "1")
	}

	var columns []uiColumn
	if spec.ColumnsJSON != "" {
		_ = json.Unmarshal([]byte(spec.ColumnsJSON), &columns)
	}
	var actions []uiAction
	if spec.ActionsJSON != "" {
		_ = json.Unmarshal([]byte(spec.ActionsJSON), &actions)
	}

	data := v2TableData{
		View:       view,
		BaseView:   view,
		Title:      spec.Title,
		Class:      spec.Class,
		Empty:      spec.Empty,
		Note:       spec.Note,
		Query:      r.FormValue("q"),
		Columns:    columns,
		HasActions: len(actions) > 0,
		Search:     spec.Search,
		Page:       page,
		Pages:      1,
		Prev:       page - 1,
		Next:       page + 1,
		Colspan:    len(columns),
		Total:      0,
		WebSearch:  web,
	}
	if data.HasActions {
		data.Colspan++
	}
	data.Manual = spec.ManualOnly
	data.Refresh = spec.Search || spec.PageSize > 0 || spec.ManualOnly || spec.RefreshHint != "" || spec.Filter
	data.Filter = spec.Filter
	data.FilterQuery = r.FormValue("f")
	data.FooterForm = spec.FooterForm
	data.FooterActions = spec.FooterActions
	data.FooterView = view
	if separator := strings.LastIndex(view, "-t"); separator > 0 {
		data.FooterView = view[:separator]
		data.BaseView = view[:separator]
	}

	// Manual tables (source health) must not load on their own: the check is
	// slow and is triggered explicitly by the Aggiorna button.
	if spec.ManualOnly && r.FormValue("load") != "1" {
		data.Empty = spec.Initial
		if data.Empty == "" {
			data.Empty = "Premi Aggiorna per caricare i dati."
		}
		return data
	}

	raw, status := v2InternalJSON(s, http.MethodGet, spec.Endpoint, query, nil)
	var payload any
	if status >= 400 || json.Unmarshal(raw, &payload) != nil {
		data.Empty = "Caricamento non riuscito."
		return data
	}

	items := v2TableItems(payload, spec.ItemsKey)
	if len(columns) == 0 && len(items) > 0 {
		columns = v2DeriveColumns(items[0])
		data.Columns = columns
		data.Colspan = len(columns)
		if data.HasActions {
			data.Colspan++
		}
	}

	if filter := strings.ToLower(strings.TrimSpace(data.FilterQuery)); filter != "" {
		items = v2FilterItems(items, filter)
	}
	// Server-side sorting of the loaded rows (the classic client sorted in the
	// browser). The header is re-rendered with the current direction.
	data.Sort = r.FormValue("sort")
	data.Dir = r.FormValue("dir")
	if data.Dir != "desc" {
		data.Dir = "asc"
	}
	// The series library is always alphabetical: default to the name column when
	// no sort is active, instead of showing the raw configuration order.
	if data.Sort == "" && spec.ItemsKey == "series" {
		data.Sort = "name"
	}
	if column, ok := v2SortableColumn(columns, data.Sort); ok {
		v2SortItems(items, column, data.Dir)
	}

	for _, item := range items {
		row := v2TableRow{}
		for _, column := range columns {
			row.Cells = append(row.Cells, v2Cell{HTML: v2FormatCell(item, column)})
		}
		if data.HasActions {
			row.Actions = v2RenderActions(view, item, actions, spec)
		}
		data.Rows = append(data.Rows, row)
	}

	if spec.PageSize > 0 {
		if object, ok := payload.(map[string]any); ok {
			if total, ok := object["total"].(float64); ok {
				data.Total = int(total)
			}
			if pages, ok := object["pages"].(float64); ok && pages > 0 {
				data.Pages = int(pages)
			}
			if current, ok := object["page"].(float64); ok && current > 0 {
				data.Page = int(current)
				data.Prev = data.Page - 1
				data.Next = data.Page + 1
			}
		}
	}
	if data.Total == 0 {
		data.Total = len(items)
	}
	if data.Pages < 1 {
		data.Pages = 1
	}
	return data
}

// v2TableItems extracts the row slice from the endpoint payload.
func v2TableItems(payload any, itemsKey string) []map[string]any {
	var list []any
	switch typed := payload.(type) {
	case []any:
		list = typed
	case map[string]any:
		if itemsKey == "" {
			return nil
		}
		if value, ok := typed[itemsKey].([]any); ok {
			list = value
		}
	}
	out := make([]map[string]any, 0, len(list))
	for _, entry := range list {
		switch typed := entry.(type) {
		case map[string]any:
			out = append(out, typed)
		}
	}
	return out
}

func v2SortableColumn(columns []uiColumn, key string) (uiColumn, bool) {
	for _, column := range columns {
		if column.Sortable && column.Key == key {
			return column, true
		}
	}
	return uiColumn{}, false
}

func v2ItemSortValue(item map[string]any, column uiColumn) any {
	switch column.Format {
	case "episodes":
		return v2Float(item["episodes_downloaded"])
	case "completion":
		total := v2Float(item["episodes_total"])
		if total > 0 {
			return v2Float(item["episodes_downloaded"]) / total
		}
		return 0.0
	}
	if column.Key == "" {
		return ""
	}
	return item[column.Key]
}

func v2CompareValues(a, b any) int {
	af, aIsNumber := v2Numeric(a)
	bf, bIsNumber := v2Numeric(b)
	if aIsNumber && bIsNumber {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		}
		return 0
	}
	as := strings.ToLower(v2String(a))
	bs := strings.ToLower(v2String(b))
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	}
	return 0
}

func v2Numeric(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func v2SortItems(items []map[string]any, column uiColumn, dir string) {
	sort.SliceStable(items, func(i, j int) bool {
		compared := v2CompareValues(v2ItemSortValue(items[i], column), v2ItemSortValue(items[j], column))
		if dir == "desc" {
			return compared > 0
		}
		return compared < 0
	})
}

// v2FilterItems keeps the items whose JSON representation contains the filter
// text (case-insensitive), mirroring the classic client-side row filter.
func v2FilterItems(items []map[string]any, filter string) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		encoded, _ := json.Marshal(item)
		if strings.Contains(strings.ToLower(string(encoded)), filter) {
			out = append(out, item)
		}
	}
	return out
}

func v2DeriveColumns(item map[string]any) []uiColumn {
	columns := []uiColumn{}
	for key, value := range item {
		switch value.(type) {
		case nil, string, float64, bool:
			columns = append(columns, uiColumn{Key: key, Label: key})
		}
		if len(columns) >= 8 {
			break
		}
	}
	return columns
}

// ---------------------------------------------------------------------------
// Formatters (Go port of the classic client's fmt/format functions)
// ---------------------------------------------------------------------------

func v2Truthy(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "", "0", "false", "no", "off":
			return false
		}
		return true
	case float64:
		return typed != 0
	}
	return true
}

func v2String(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, entry := range typed {
			parts = append(parts, v2String(entry))
		}
		return strings.Join(parts, ",")
	case map[string]any:
		for _, key := range []string{"name", "title", "label", "path"} {
			if text := v2String(typed[key]); text != "" {
				return text
			}
		}
		return "—"
	}
	return ""
}

func v2Float(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed
	case bool:
		if typed {
			return 1
		}
		return 0
	}
	return 0
}

func v2HumanBytes(value any) string {
	n := v2Float(value)
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	index := 0
	for n >= 1024 && index < len(units)-1 {
		n /= 1024
		index++
	}
	if index == 0 {
		return strconv.FormatFloat(n, 'f', 0, 64) + " " + units[index]
	}
	return strconv.FormatFloat(n, 'f', 1, 64) + " " + units[index]
}

func v2SourceLabel(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	candidate := raw
	host := raw
	if !strings.Contains(raw, "://") {
		host = "https://" + raw
	}
	if parsed, err := url.Parse(host); err == nil && parsed.Hostname() != "" {
		labels := strings.Split(strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www."), ".")
		filtered := labels[:0]
		for _, label := range labels {
			if label != "" {
				filtered = append(filtered, label)
			}
		}
		if len(filtered) >= 3 {
			lastTwo := filtered[len(filtered)-2] + "." + filtered[len(filtered)-1]
			switch lastTwo {
			case "co.uk", "com.au", "com.br", "co.jp", "co.nz", "com.mx", "net.au", "org.uk":
				if len(filtered) >= 4 {
					candidate = filtered[len(filtered)-3]
				} else {
					candidate = filtered[0]
				}
			default:
				candidate = filtered[len(filtered)-2]
			}
		} else if len(filtered) >= 2 {
			candidate = filtered[len(filtered)-2]
		} else if len(filtered) == 1 {
			candidate = filtered[0]
		}
	}
	candidate = strings.TrimSpace(strings.NewReplacer("-", " ", "_", " ").Replace(candidate))
	return candidate
}

func v2CompactDateTime(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	normalized := strings.Replace(raw, " ", "T", 1)
	if !strings.HasSuffix(normalized, "Z") && !strings.ContainsAny(normalized, "+") {
		if !strings.Contains(normalized, "-") || strings.Count(normalized, "-") == 2 {
			normalized += "Z"
		}
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			return parsed.Local().Format("02/01/2006 15:04")
		}
	}
	return raw
}

func v2CompactDate(value string) string {
	compact := v2CompactDateTime(value)
	if fields := strings.Fields(compact); len(fields) > 0 {
		return fields[0]
	}
	return compact
}

func v2FolderLabel(path string) string {
	value := strings.TrimRight(strings.TrimSpace(path), `/\`)
	if value == "" {
		return ""
	}
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	lower := strings.ToLower(last)
	for _, ext := range []string{".mkv", ".mp4", ".avi", ".m4v", ".ts", ".mov", ".wmv", ".flv", ".srt", ".nfo", ".jpg", ".jpeg", ".png", ".webp"} {
		if strings.HasSuffix(lower, ext) {
			if len(parts) >= 2 {
				return parts[len(parts)-2]
			}
			return last
		}
	}
	return last
}

func v2SafeHref(value string) string {
	href := strings.TrimSpace(value)
	lower := strings.ToLower(href)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "magnet:") {
		return ""
	}
	return href
}

func v2FormatCell(item map[string]any, column uiColumn) template.HTML {
	var raw any
	if column.Key == "" {
		raw = item
	} else {
		raw = item[column.Key]
	}
	switch column.Format {
	case "episodes":
		value := v2String(item["episodes_downloaded"]) + "/" + v2String(item["episodes_total"])
		if v2String(item["episodes_downloaded"]) == "" {
			value = "0/0"
		}
		return template.HTML(stdhtml.EscapeString(value))
	case "completion":
		total := v2Float(item["episodes_total"])
		done := v2Float(item["episodes_downloaded"])
		percent := 0.0
		if total > 0 {
			percent = done / total * 100
		}
		return template.HTML(stdhtml.EscapeString(strconv.FormatFloat(percent, 'f', 0, 64) + "%"))
	case "enabled":
		if v2Truthy(item["enabled"]) {
			return "attiva"
		}
		return "in pausa"
	case "nas_tag":
		hasNAS := strings.TrimSpace(v2String(item["processed_path"])) != ""
		tag := strings.TrimSpace(v2String(item["tag"]))
		html := ""
		if hasNAS {
			html += `<span class="badge ok">NAS</span>`
		}
		if tag != "" {
			if html != "" {
				html += " "
			}
			html += `<span class="badge">` + stdhtml.EscapeString(tag) + `</span>`
		}
		if html == "" {
			html = `<span class="muted">—</span>`
		}
		return template.HTML(html)
	case "series_status":
		total := v2Float(item["episodes_total"])
		done := v2Float(item["episodes_downloaded"])
		complete := total > 0 && done >= total
		status := strings.ToLower(v2String(item["tmdb_status"]))
		ended := status == "ended" || status == "canceled" || status == "cancelled"
		enabled := v2Truthy(item["enabled"])
		switch {
		case ended && complete:
			return `<span class="badge ok" title="Serie terminata e completa">🏁 ✓✓</span>`
		case ended:
			return `<span class="badge" title="Serie terminata: mancano ancora episodi">🏁 terminata</span>`
		case complete:
			return `<span class="badge" title="Al passo con gli episodi pubblicati">✓ in pari</span>`
		}
		class := ""
		label := "in pausa"
		if enabled {
			class = "ok"
			label = "attiva"
		}
		html := `<span class="badge ` + class + `">` + label + `</span>`
		if !enabled {
			html += ` <span class="badge" title="Serie in pausa">in pausa</span>`
		}
		return template.HTML(html)
	case "status_badge":
		if item["ok"] != false {
			return `<span class="badge ok">ok</span>`
		}
		return `<span class="badge err">errore</span>`
	case "source_detail":
		detail := ""
		switch {
		case v2String(item["error"]) != "":
			detail = v2String(item["error"])
		case item["status"] != nil:
			detail = "HTTP " + v2String(item["status"])
		case item["results"] != nil:
			detail = v2String(item["results"]) + " risultati"
		}
		if detail == "" {
			detail = "—"
		}
		return template.HTML(`<span class="cell-truncate" title="` + stdhtml.EscapeString(detail) + `">` + stdhtml.EscapeString(detail) + `</span>`)
	case "source":
		return template.HTML(`<span class="source-label" title="` + stdhtml.EscapeString(v2String(raw)) + `">` + stdhtml.EscapeString(v2SourceLabel(v2String(raw))) + `</span>`)
	case "datetime":
		return template.HTML(`<span title="` + stdhtml.EscapeString(v2String(raw)) + `">` + stdhtml.EscapeString(v2CompactDateTime(v2String(raw))) + `</span>`)
	case "truncate":
		return template.HTML(`<span class="cell-truncate" title="` + stdhtml.EscapeString(v2String(raw)) + `">` + stdhtml.EscapeString(v2String(raw)) + `</span>`)
	case "folder":
		full := v2String(raw)
		if full == "" {
			return ""
		}
		return template.HTML(`<span title="` + stdhtml.EscapeString(full) + `">` + stdhtml.EscapeString(v2FolderLabel(full)) + `</span>`)
	case "provider_link":
		name := v2String(item[column.Key])
		href := v2SafeHref(v2String(item["url"]))
		if href == "" {
			return template.HTML(stdhtml.EscapeString(name))
		}
		return template.HTML(`<a href="` + stdhtml.EscapeString(href) + `" target="_blank" rel="noopener" title="Apri il sito del provider">` + stdhtml.EscapeString(name) + `</a>`)
	case "series_link":
		name := v2String(item[column.Key])
		return template.HTML(`<a href="/?view=series&amp;series=` + url.QueryEscape(name) + `" title="Apri il dettaglio della serie">` + stdhtml.EscapeString(name) + `</a>`)
	case "movie_link":
		name := v2String(item[column.Key])
		return template.HTML(`<a href="/?view=movies&amp;movie=` + url.QueryEscape(v2String(item["id"])) + `" title="Apri il dettaglio del film">` + stdhtml.EscapeString(name) + `</a>`)
	case "url", "getcomics":
		href := v2String(raw)
		if href == "" {
			return ""
		}
		if column.Format == "getcomics" && strings.HasPrefix(href, "/") {
			href = "https://getcomics.org" + href
		}
		href = v2SafeHref(href)
		if href == "" {
			return ""
		}
		return template.HTML(`<a href="` + stdhtml.EscapeString(href) + `" target="_blank" rel="noopener">apri</a>`)
	}
	return template.HTML(stdhtml.EscapeString(v2Fmt(raw, column.Format)))
}

func v2Fmt(value any, format string) string {
	switch format {
	case "bool":
		if v2Truthy(value) {
			return "Sì"
		}
		return "No"
	case "bytes":
		return v2HumanBytes(value)
	case "rate":
		return v2HumanBytes(value) + "/s"
	case "percent":
		return strconv.FormatFloat(v2Float(value), 'f', 1, 64) + "%"
	case "source":
		return v2SourceLabel(v2String(value))
	case "datetime":
		return v2CompactDateTime(v2String(value))
	case "date":
		return v2CompactDate(v2String(value))
	}
	return v2String(value)
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

func v2RenderActions(view string, item map[string]any, actions []uiAction, spec uiTableSpec) template.HTML {
	var out strings.Builder
	for _, action := range actions {
		out.WriteString(v2RenderAction(view, item, action, spec))
		out.WriteString(" ")
	}
	return template.HTML(out.String())
}

func v2RenderAction(view string, item map[string]any, action uiAction, spec uiTableSpec) string {
	switch action.Kind {
	case "library-toggle", "library-remove":
		scope := "series"
		if spec.ItemsKey == "movies" {
			scope = "movies"
		}
		name := v2String(item["name"])
		mode := "toggle"
		label := action.Label
		class := action.Class
		confirm := ""
		if action.Kind == "library-remove" {
			mode = "remove"
			confirm = action.Confirm
		} else if !v2Truthy(item["enabled"]) {
			label = "Attiva"
		} else if label == "" {
			label = "Pausa"
		}
		attrs := `hx-post="/table/library" hx-vals='{"view":"` + view + `","scope":"` + scope + `","name":"` + templateEscapeJSAttr(name) + `","mode":"` + mode + `"}' hx-target="#v2-table-body-` + view + `" hx-swap="outerHTML"`
		if confirm != "" {
			attrs += ` hx-confirm="` + stdhtml.EscapeString(confirm) + `"`
		}
		return `<button class="btn sm ` + stdhtml.EscapeString(class) + `" type="button" ` + attrs + `>` + stdhtml.EscapeString(label) + `</button>`
	case "gap-search":
		series := v2String(item["series"])
		season := v2String(item["season"])
		episode := v2String(item["episode"])
		vals := `{"view":"` + view + `","series":"` + templateEscapeJSAttr(series) + `","season":"` + templateEscapeJSAttr(season) + `","episode":"` + templateEscapeJSAttr(episode) + `"}`
		return `<button class="btn sm ` + stdhtml.EscapeString(action.Class) + `" type="button" hx-post="/table/gap-search" hx-vals='` + vals + `' hx-target="#v2-table-body-` + view + `" hx-swap="outerHTML">` + stdhtml.EscapeString(action.Label) + `</button>`
	case "comic-edit":
		id := v2String(item["id"])
		return `<button class="btn sm" type="button" hx-get="/comics/edit?id=` + url.QueryEscape(id) + `" hx-target="#v2-modal" hx-swap="innerHTML" title="Modifica il fumetto monitorato">` + stdhtml.EscapeString(action.Label) + `</button>`
	case "release-explain":
		release := item
		if nested, ok := item["release"].(map[string]any); ok {
			release = nested
		}
		encoded, _ := json.Marshal(release)
		escapedJSON := strings.ReplaceAll(stdhtml.EscapeString(string(encoded)), `"`, "&#34;")
		return `<form method="post" action="/search/explain" hx-post="/search/explain" hx-target="#v2-modal" hx-swap="innerHTML" style="display:inline"><input type="hidden" name="release" value="` + escapedJSON + `" /><button class="btn sm" type="submit">` + stdhtml.EscapeString(action.Label) + `</button></form>`
	case "copy-magnet":
		magnet := strings.TrimSpace(v2String(item["magnet"]))
		disabled := ""
		if magnet == "" {
			disabled = " disabled"
		}
		return `<button class="btn sm" type="button" data-v2-copy="` + stdhtml.EscapeString(magnet) + `"` + disabled + ` title="Copia il magnet negli appunti">` + stdhtml.EscapeString(action.Label) + `</button>`
	case "comic-weekly-force":
		magnet := strings.TrimSpace(v2String(item["magnet"]))
		torrent := strings.TrimSpace(v2String(item["torrent_url"]))
		disabled := ""
		if magnet == "" && torrent == "" {
			disabled = " disabled"
		}
		return `<form hx-post="/comics/weekly/force" hx-target="#v2-toast-region" hx-swap="innerHTML" style="display:inline"><input type="hidden" name="date" value="` + stdhtml.EscapeString(v2String(item["pack_date"])) + `"><input type="hidden" name="magnet" value="` + stdhtml.EscapeString(magnet) + `"><input type="hidden" name="torrent" value="` + stdhtml.EscapeString(torrent) + `"><button class="btn sm" type="submit" title="Scarica di nuovo questo Weekly Pack"` + disabled + `>` + stdhtml.EscapeString(action.Label) + `</button></form>`
	}

	path := v2Substitute(action.Path, item, true)
	body := v2Substitute(action.Body, item, false)
	if body == "" {
		body = "{}"
	}
	vals := `{"view":"` + view + `","path":"` + templateEscapeJSAttr(path) + `","method":"` + stdhtml.EscapeString(action.Method) + `","body":"` + templateEscapeJSAttr(body) + `"}`
	attrs := `hx-post="/table/action" hx-vals='` + vals + `' hx-include="closest .panel form.toolbar" hx-target="#v2-table-body-` + view + `" hx-swap="outerHTML"`
	if action.Confirm != "" {
		attrs += ` hx-confirm="` + stdhtml.EscapeString(action.Confirm) + `"`
	}
	return `<button class="btn sm ` + stdhtml.EscapeString(action.Class) + `" type="button" ` + attrs + `>` + stdhtml.EscapeString(action.Label) + `</button>`
}

// v2Substitute replaces {key} placeholders with the row value (URL-escaped for
// paths, JSON-escaped for bodies), mirroring the classic client.
func v2Substitute(templateText string, item map[string]any, pathMode bool) string {
	if !strings.Contains(templateText, "{") {
		return templateText
	}
	var out strings.Builder
	for i := 0; i < len(templateText); i++ {
		if templateText[i] != '{' {
			out.WriteByte(templateText[i])
			continue
		}
		end := strings.IndexByte(templateText[i:], '}')
		if end < 0 {
			out.WriteString(templateText[i:])
			break
		}
		key := templateText[i+1 : i+end]
		if !v2IsValidPlaceholder(key) {
			out.WriteByte('{')
			continue
		}
		value := v2String(item[key])
		if pathMode {
			out.WriteString(url.PathEscape(value))
		} else {
			encoded, _ := json.Marshal(value)
			text := string(encoded)
			if len(text) >= 2 {
				text = text[1 : len(text)-1]
			}
			out.WriteString(text)
		}
		i += end
	}
	return out.String()
}

func v2IsValidPlaceholder(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// V2Table renders only the table body (search, pagination, actions).
func V2Table(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.FormValue("view"))
	spec, ok := v2SpecFor(s, view)
	if !ok {
		http.Error(w, "tabella sconosciuta", http.StatusNotFound)
		return
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_table_body", v2TableDataFrom(s, r, view, spec), dict, eng)
}

// V2TableAction forwards a generic table action to the existing JSON API and
// returns the refreshed table body.
func V2TableAction(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.FormValue("view"))
	spec, ok := v2SpecFor(s, view)
	if !ok {
		http.Error(w, "tabella sconosciuta", http.StatusNotFound)
		return
	}
	path := strings.TrimSpace(r.FormValue("path"))
	method := strings.ToUpper(strings.TrimSpace(r.FormValue("method")))
	if method == "" {
		method = http.MethodPost
	}
	body := []byte(r.FormValue("body"))
	var notice *v2ActionNotice
	// Only allow the daemon's own API paths: never let the form reach arbitrary
	// URLs (the value is client-controlled).
	if strings.HasPrefix(path, "/api/") {
		raw, status := v2InternalJSON(s, method, path, nil, body)
		switch {
		case strings.HasPrefix(path, "/api/archive/batch-download"):
			if status >= 200 && status < 300 {
				var res struct {
					Accepted int   `json:"accepted"`
					Rejected []any `json:"rejected"`
				}
				_ = json.Unmarshal(raw, &res)
				if res.Accepted > 0 {
					notice = &v2ActionNotice{Title: "Download", Message: "Torrent aggiunto ai download con successo."}
				} else {
					notice = &v2ActionNotice{Title: "Download", Message: "Torrent non avviato o già presente nei download.", Error: true}
				}
			} else {
				notice = &v2ActionNotice{Title: "Download non riuscito", Message: "Impossibile avviare il download del torrent.", Error: true}
			}
		case strings.HasPrefix(path, "/api/archive/delete"):
			if status >= 200 && status < 300 {
				notice = &v2ActionNotice{Title: "Archivio", Message: "Elemento rimosso dall'archivio."}
			} else {
				notice = &v2ActionNotice{Title: "Archivio", Message: "Impossibile rimuovere l'elemento.", Error: true}
			}
		case strings.HasPrefix(path, "/api/blocklist/"):
			if status >= 200 && status < 300 {
				notice = &v2ActionNotice{Title: "Blocklist", Message: "Elemento rimosso dalla blocklist."}
			}
		default:
			if status >= 400 {
				notice = &v2ActionNotice{Title: "Errore", Message: "Operazione non riuscita.", Error: true}
			}
		}
	}
	dict, eng := v2Dictionaries(s)
	data := v2TableDataFrom(s, r, view, spec)
	data.Notice = notice
	v2Render(w, http.StatusOK, "v2_table_body", data, dict, eng)
}

// V2TableLibrary toggles or removes a series/movie, mirroring the classic
// read-modify-write of /api/config/library.
func V2TableLibrary(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.FormValue("view"))
	scope := strings.TrimSpace(r.FormValue("scope"))
	name := r.FormValue("name")
	mode := strings.TrimSpace(r.FormValue("mode"))

	if raw, status := v2InternalJSON(s, http.MethodGet, "/api/config/library", nil, nil); status < 400 {
		var library struct {
			Series []map[string]any `json:"series"`
			Movies []map[string]any `json:"movies"`
		}
		if json.Unmarshal(raw, &library) == nil {
			list := library.Series
			if scope == "movies" {
				list = library.Movies
			}
			updated := make([]map[string]any, 0, len(list))
			for _, entry := range list {
				if v2String(entry["name"]) != name {
					updated = append(updated, entry)
					continue
				}
				if mode == "remove" {
					continue
				}
				entry["enabled"] = !v2Truthy(entry["enabled"])
				updated = append(updated, entry)
			}
			if scope == "movies" {
				library.Movies = updated
			} else {
				library.Series = updated
			}
			payload, _ := json.Marshal(map[string]any{"series": library.Series, "movies": library.Movies})
			v2InternalJSON(s, http.MethodPost, "/api/config/library", nil, []byte(payload))
		}
	}
	if spec, ok := v2SpecFor(s, view); ok {
		dict, eng := v2Dictionaries(s)
		v2Render(w, http.StatusOK, "v2_table_body", v2TableDataFrom(s, r, view, spec), dict, eng)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// V2TableGapSearch starts a manual search for a missing episode.
func V2TableGapSearch(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.FormValue("view"))
	series := url.PathEscape(r.FormValue("series"))
	season := url.PathEscape(r.FormValue("season"))
	episode := url.PathEscape(r.FormValue("episode"))
	if series != "" && season != "" && episode != "" {
		v2InternalJSON(s, http.MethodPost, "/api/episodes/"+series+"/"+season+"/"+episode+"/search", nil, []byte("{}"))
	}
	if spec, ok := v2SpecFor(s, view); ok {
		dict, eng := v2Dictionaries(s)
		v2Render(w, http.StatusOK, "v2_table_body", v2TableDataFrom(s, r, view, spec), dict, eng)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// templateEscapeJSAttr escapes a value for use inside a single-quoted
// hx-vals JSON attribute.
func templateEscapeJSAttr(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `'`, `\'`, "<", `\u003c`, ">", `\u003e`, "&", `\u0026`)
	return replacer.Replace(value)
}
