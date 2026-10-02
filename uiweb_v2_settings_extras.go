package gextto

// uiweb_v2_settings_extras.go adds the structured settings editors to the v2
// Configurazione page: the RSS feed editor, the checkbox groups (web engines,
// content filters), the structured list editors (indexers, source filters,
// tag->folder rules, event hooks, watched folders), the rename composer and the
// translations editor. Each editor is a plain server-rendered form; HTMX only
// adds rows or re-renders the touched panel.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// --- feeds ------------------------------------------------------------------

func v2FeedsFrom(s *AppState) string {
	if raw, status := v2InternalJSON(s, http.MethodGet, "/api/config", nil, nil); status < 400 {
		var config struct {
			FeedURLs []string `json:"feed_urls"`
		}
		if json.Unmarshal(raw, &config) == nil {
			return strings.Join(config.FeedURLs, "\n")
		}
	}
	return ""
}

// V2SettingsFeed saves the RSS feed list as the `url` setting (JSON array).
func V2SettingsFeed(w http.ResponseWriter, r *http.Request, s *AppState) {
	feeds := []string{}
	for _, line := range strings.Split(r.FormValue("feeds"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			feeds = append(feeds, trimmed)
		}
	}
	encoded, _ := json.Marshal(feeds)
	if err := saveConfigSetting(s.cfg.DataDir, "url", string(encoded)); err != nil {
		v2SettingsRedirect(w, r, "sources")
		return
	}
	v2SettingsRedirect(w, r, "sources")
}

func v2SettingsRedirect(w http.ResponseWriter, r *http.Request, tab string) {
	target := "/?view=settings"
	if tab != "" {
		target += "&tab=" + url.QueryEscape(tab)
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	w.Header().Set("HX-Redirect", target)
	w.WriteHeader(http.StatusNoContent)
}

// --- checkbox groups --------------------------------------------------------

type v2CheckboxGroup struct {
	Key, Title, Hint  string
	Options           []uiCheckboxOption
	Custom            bool
	CustomPlaceholder string
	TestQuery         string
	TestKind          string
	TestLabel         string
	Status            string
	Error             bool
}

func v2CheckboxGroupsFrom(groups []uiCheckboxGroup) []v2CheckboxGroup {
	out := make([]v2CheckboxGroup, 0, len(groups))
	for _, group := range groups {
		out = append(out, v2CheckboxGroup{
			Key: group.Key, Title: group.Title, Hint: group.Hint,
			Options: group.Options, Custom: group.Custom, CustomPlaceholder: group.CustomPlaceholder,
			TestQuery: group.TestQuery, TestKind: group.TestKind, TestLabel: group.TestLabel,
		})
	}
	return out
}

type v2SourceTestItem struct {
	Name  string
	OK    bool
	Error string
}

type v2SourceTestView struct {
	Items []v2SourceTestItem
}

func V2SettingsSourceTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	query := url.Values{}
	query.Set("q", strings.TrimSpace(r.FormValue("q")))
	if kind := strings.TrimSpace(r.FormValue("kind")); kind != "" {
		query.Set("kind", kind)
	}
	raw, status := v2InternalJSON(s, http.MethodGet, "/api/sources/health", query, nil)
	view := v2SourceTestView{}
	if status < 400 {
		var payload struct {
			Items []v2SourceTestItem `json:"items"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			view.Items = payload.Items
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_source_test_result", view, dict, eng)
}

// V2SettingsCheckbox saves a checkbox group immediately.
func V2SettingsCheckbox(w http.ResponseWriter, r *http.Request, s *AppState) {
	key := strings.TrimSpace(r.FormValue("key"))
	values := r.Form["value"]
	if custom := strings.TrimSpace(r.FormValue("custom")); custom != "" {
		values = append(values, custom)
	}
	if key != "" && gh7_setting_key_allowed(key) {
		encoded, _ := json.Marshal(values)
		_ = saveConfigSetting(s.cfg.DataDir, key, string(encoded))
	}
	if r.Header.Get("HX-Request") == "" {
		v2SettingsRedirect(w, r, "sources")
		return
	}
	// Re-render the group with a status.
	cfg := latestConfig(s)
	group := v2CheckboxGroup{Key: key, Status: "salvato"}
	for _, candidate := range uiSourcesCheckboxGroups(cfg) {
		if candidate.Key == key {
			group = v2CheckboxGroup{Key: candidate.Key, Title: candidate.Title, Hint: candidate.Hint, Options: candidate.Options, Custom: candidate.Custom, CustomPlaceholder: candidate.CustomPlaceholder, Status: "salvato"}
			break
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_checkbox_group", group, dict, eng)
}

// --- list editors -----------------------------------------------------------

type v2ListFieldValue struct {
	Name, Label, Kind, Placeholder, Value string
	Options                               []uiListFieldOption
	Wide                                  bool
}

type v2ListRow struct {
	Index  int
	Fields []v2ListFieldValue
}

type v2ListEditorView struct {
	Key, Title, Hint, View, Tab, Redirect string
	HasTest                               bool
	Rows                                  []v2ListRow
	NextIndex                             int
}

func v2ListEditorByKey(key string) (uiListEditor, bool) {
	if key == "indexers" {
		return uiIndexerEditor, true
	}
	for _, editor := range uiAdvancedEditors {
		switch editor.GetPath {
		case "/api/config/source-filters":
			if key == "source_filters" {
				return editor, true
			}
		case "/api/tag-dir-rules":
			if key == "tag_dir_rules" {
				return editor, true
			}
		case "/api/event-hooks":
			if key == "event_hooks" {
				return editor, true
			}
		case "/api/watched-folders":
			if key == "watched_folders" {
				return editor, true
			}
		}
	}
	return uiListEditor{}, false
}

func v2FieldValueString(value any, kind string) string {
	switch kind {
	case "bool":
		return ternaryString(v2Truthy(value), "true", "false")
	case "tags":
		if list, ok := value.([]any); ok {
			parts := make([]string, 0, len(list))
			for _, item := range list {
				parts = append(parts, v2String(item))
			}
			return strings.Join(parts, ", ")
		}
	}
	return v2String(value)
}

func ternaryString(cond bool, whenTrue, whenFalse string) string {
	if cond {
		return whenTrue
	}
	return whenFalse
}

func v2ListRowFrom(fields []uiListField, item map[string]any, index int) v2ListRow {
	row := v2ListRow{Index: index}
	for _, field := range fields {
		row.Fields = append(row.Fields, v2ListFieldValue{
			Name: field.Name, Label: field.Label, Kind: field.Kind,
			Placeholder: field.Placeholder, Options: field.Options, Wide: field.Wide,
			Value: v2FieldValueString(item[field.Name], field.Kind),
		})
	}
	return row
}

func v2ListEditorViewFrom(s *AppState, editor uiListEditor, key, view, tab string) v2ListEditorView {
	items := []map[string]any{}
	if raw, status := v2InternalJSON(s, http.MethodGet, editor.GetPath, nil, nil); status < 400 {
		var payload any
		if json.Unmarshal(raw, &payload) == nil {
			if editor.Unwrap != "" {
				if object, ok := payload.(map[string]any); ok {
					payload = object[editor.Unwrap]
				}
			}
			if list, ok := payload.([]any); ok {
				for _, entry := range list {
					if object, ok := entry.(map[string]any); ok {
						items = append(items, object)
					}
				}
			}
		}
	}
	redirect := "/?view=" + view
	if tab != "" {
		redirect += "&tab=" + url.QueryEscape(tab)
	}
	view2 := v2ListEditorView{Key: key, Title: editor.Title, Hint: editor.Hint, View: view, Tab: tab, Redirect: redirect, HasTest: editor.TestEndpoint != ""}
	for index, item := range items {
		view2.Rows = append(view2.Rows, v2ListRowFrom(editor.Fields, item, index))
	}
	view2.NextIndex = len(view2.Rows)
	return view2
}

// V2SettingsEditorRow returns one empty row (and advances the add button index).
func V2SettingsEditorRow(w http.ResponseWriter, r *http.Request, s *AppState) {
	key := r.FormValue("editor")
	editor, ok := v2ListEditorByKey(key)
	if !ok {
		http.Error(w, "editor sconosciuto", http.StatusNotFound)
		return
	}
	index, _ := strconv.Atoi(r.FormValue("index"))
	view := v2ListEditorView{Key: key, View: r.FormValue("view"), Tab: r.FormValue("tab"), NextIndex: index + 1, HasTest: editor.TestEndpoint != ""}
	row := v2ListRowFrom(editor.Fields, map[string]any{}, index)
	dict, eng := v2Dictionaries(s)
	var buffer bytes.Buffer
	if err := v2Templates.ExecuteTemplate(&buffer, "v2_list_row", map[string]any{"Editor": view, "Row": row}); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	body := v2TranslateHTML(buffer.String(), dict, eng)
	safeKey := html.EscapeString(key)
	safeView := html.EscapeString(url.QueryEscape(r.FormValue("view")))
	safeTab := html.EscapeString(url.QueryEscape(r.FormValue("tab")))
	addButton := fmt.Sprintf(`<button class="btn sm" type="button" id="v2-editor-%s-add" hx-swap-oob="true" hx-get="/v2/settings/editor-row?editor=%s&amp;index=%d&amp;view=%s&amp;tab=%s" hx-target="#v2-editor-%s-rows" hx-swap="beforeend">Aggiungi riga</button>`,
		safeKey, url.QueryEscape(key), index+1, safeView, safeTab, safeKey)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body + addButton))
}

func V2SettingsEditorTest(w http.ResponseWriter, r *http.Request, s *AppState) {
	key := strings.TrimSpace(r.FormValue("editor"))
	editor, ok := v2ListEditorByKey(key)
	if !ok || editor.TestEndpoint == "" {
		http.Error(w, "test editor sconosciuto", http.StatusNotFound)
		return
	}
	index := strings.TrimSpace(r.FormValue("index"))
	values := map[string]any{}
	for _, field := range editor.Fields {
		values[field.Name] = r.FormValue(field.Name + "__" + index)
	}
	body, _ := json.Marshal(values)
	raw, status := v2InternalJSON(s, http.MethodPost, editor.TestEndpoint, nil, body)
	okResult := false
	message := "Test non riuscito"
	if status < 400 {
		var payload struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			okResult = payload.OK
			if okResult {
				message = "OK"
			} else if payload.Error != "" {
				message = "Errore: " + payload.Error
			}
		}
	} else {
		message += ": " + v2JSONError(raw)
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_list_test_result", map[string]any{"OK": okResult, "Message": message}, dict, eng)
}

// V2SettingsEditorSave saves a structured list editor.
func V2SettingsEditorSave(w http.ResponseWriter, r *http.Request, s *AppState) {
	key := r.FormValue("editor")
	editor, ok := v2ListEditorByKey(key)
	if !ok {
		v2SettingsRedirect(w, r, "advanced")
		return
	}
	_ = r.ParseForm()
	rows := map[int]map[string]any{}
	for name, values := range r.Form {
		dataName, index, ok := v2SplitIndexedField(name)
		if !ok || len(values) == 0 {
			continue
		}
		if rows[index] == nil {
			rows[index] = map[string]any{}
		}
		rows[index][dataName] = v2FieldRawValue(editor.Fields, dataName, values[0])
	}
	indexes := make([]int, 0, len(rows))
	for index := range rows {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	items := make([]map[string]any, 0, len(indexes))
	for _, index := range indexes {
		items = append(items, rows[index])
	}
	encoded, _ := json.Marshal(items)
	var body []byte
	switch {
	case editor.PostKey != "":
		body, _ = json.Marshal(map[string]string{"key": editor.PostKey, "value": string(encoded)})
	case editor.Wrap != "":
		body, _ = json.Marshal(map[string]json.RawMessage{editor.Wrap: json.RawMessage(encoded)})
	default:
		body = encoded
	}
	if strings.HasPrefix(editor.PostPath, "/api/") {
		v2InternalJSON(s, http.MethodPost, editor.PostPath, nil, body)
	}
	if redirect := safeV2Redirect(r.FormValue("redirect"), ""); redirect != "" {
		if r.Header.Get("HX-Request") == "" {
			http.Redirect(w, r, redirect, http.StatusSeeOther)
			return
		}
		w.Header().Set("HX-Redirect", redirect)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	v2SettingsRedirect(w, r, "advanced")
}

func v2SplitIndexedField(name string) (string, int, bool) {
	position := strings.LastIndex(name, "__")
	if position <= 0 {
		return "", 0, false
	}
	index, err := strconv.Atoi(name[position+2:])
	if err != nil {
		return "", 0, false
	}
	return name[:position], index, true
}

func v2FieldRawValue(fields []uiListField, name, value string) any {
	for _, field := range fields {
		if field.Name != name {
			continue
		}
		switch field.Kind {
		case "bool":
			return value == "true"
		case "number":
			if number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				return number
			}
			return 0
		case "tags":
			items := []string{}
			for _, part := range strings.Split(value, ",") {
				if trimmed := strings.TrimSpace(part); trimmed != "" {
					items = append(items, trimmed)
				}
			}
			return items
		}
		return value
	}
	return value
}

// --- rename composer --------------------------------------------------------

type v2RenameView struct {
	Format   string
	Template string
	Formats  []uiFormOption
	Tokens   []uiRenameToken
	Status   string
	Preview  string
}

// V2SettingsRenamePreview recomputes the example file name without touching the
// saved settings, so the composer shows a live preview while the user edits.
func V2SettingsRenamePreview(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := v2RenameView{
		Format:   r.FormValue("format"),
		Template: r.FormValue("template"),
	}
	view.Preview = v2RenameCompose(view.Format, view.Template)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_rename_preview_code", view, dict, eng)
}

// v2RenameCompose builds the sample file name exactly like the classic client.
func v2RenameCompose(format, template string) string {
	series, title := "Nome Serie", "Titolo Episodio"
	res, codec, audio := "1080p", "x265", "DDP5.1"
	hdr, lang, channels := "HDR10", "ita", "5.1"
	source, group := "WEB-DL", "GRP"
	var stem string
	switch format {
	case "standard":
		stem = series + " - S01E02 - " + title + " [" + res + "][" + codec + "]"
	case "full", "completo":
		stem = series + " - S01E02 - " + title + " [" + res + "][" + audio + "][" + hdr + "][" + codec + "][" + lang + "]"
	case "custom":
		replacer := strings.NewReplacer(
			"{Serie}", series, "{Stagione}", "S01", "{Episodio}", "E02", "{Titolo}", title,
			"{Source}", source, "{Sorgente}", source, "{Gruppo}", group, "{Risoluzione}", res,
			"{VideoCodec}", codec, "{AudioCodec}", audio, "{Audio}", audio, "{Canali}", channels,
			"{HDR}", hdr, "{Lingue}", lang,
		)
		stem = replacer.Replace(template)
	default:
		stem = series + " - S01E02 - " + title
	}
	return stem + ".mkv"
}

// V2SettingsRenameToken appends a placeholder to the rename template.
func V2SettingsRenameToken(w http.ResponseWriter, r *http.Request, s *AppState) {
	editor := uiRenameEditorFrom(latestConfig(s))
	template := r.FormValue("template")
	if token := r.FormValue("token"); token != "" {
		template += token
	}
	selected := r.FormValue("format")
	formats := make([]uiFormOption, 0, len(editor.Formats))
	for _, format := range editor.Formats {
		formats = append(formats, uiFormOption{Value: format.Value, Label: format.Label, Selected: format.Value == selected})
	}
	view := v2RenameView{Format: selected, Template: template, Formats: formats, Tokens: editor.Tokens}
	view.Preview = v2RenameCompose(view.Format, view.Template)
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_rename_editor", view, dict, eng)
}

// V2SettingsRenameSave saves the rename format and template.
func V2SettingsRenameSave(w http.ResponseWriter, r *http.Request, s *AppState) {
	if key := strings.TrimSpace(r.FormValue("format")); key != "" {
		_ = saveConfigSetting(s.cfg.DataDir, "rename_format", key)
	}
	_ = saveConfigSetting(s.cfg.DataDir, "rename_template", r.FormValue("template"))
	v2SettingsRedirect(w, r, "rename")
}

// --- translations editor ----------------------------------------------------

type v2TranslationRow struct {
	Key, Value string
}

type v2I18nView struct {
	Language  string
	Languages []string
	Rows      []v2TranslationRow
	Status    string
}

func v2I18nViewFrom(s *AppState, language string) v2I18nView {
	if language == "" {
		language, _ = s.i18n.Language()
	}
	view := v2I18nView{Language: language, Languages: []string{"it", "en", "de", "fr", "es", "pl"}}
	if items, err := s.i18n.List(language); err == nil {
		for _, item := range items {
			view.Rows = append(view.Rows, v2TranslationRow{Key: item.Key, Value: item.Value})
		}
	}
	return view
}

// V2SettingsI18nTable returns the translations table for a language.
func V2SettingsI18nTable(w http.ResponseWriter, r *http.Request, s *AppState) {
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_i18n_table", v2I18nViewFrom(s, r.FormValue("lang")), dict, eng)
}

// V2SettingsI18nImport imports a YAML dictionary for a language.
func V2SettingsI18nImport(w http.ResponseWriter, r *http.Request, s *AppState) {
	language := r.FormValue("lang")
	values := map[string]string{}
	if err := yaml.Unmarshal([]byte(r.FormValue("yaml")), &values); err == nil && len(values) > 0 {
		_ = s.i18n.SetBulk(language, values)
	}
	v2SettingsRedirect(w, r, "i18n")
}

// V2SettingsI18nDelete removes every translation of a language.
func V2SettingsI18nDelete(w http.ResponseWriter, r *http.Request, s *AppState) {
	language := r.FormValue("lang")
	if language != "" {
		_, _ = s.i18n.DeleteLang(language)
	}
	v2SettingsRedirect(w, r, "i18n")
}
