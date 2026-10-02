package gextto

// uiweb_v2_sections.go renders the section-composed pages (Manutenzione,
// Integrazioni) in the v2 interface. It normalises the existing uiPageSection
// values into a small view type the templates can render, reusing the generic
// table renderer for table sections and forwarding actions/forms to the very
// same JSON APIs the classic UI calls.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/buzzqw/gextto/internal/models"
)

// v2Button is a uiActionButton plus the view it belongs to, so the button form
// can carry the view back to the action endpoint.
type v2Button struct {
	uiActionButton
	View string
}

func v2Buttons(view string, buttons []uiActionButton) []v2Button {
	out := make([]v2Button, 0, len(buttons))
	for _, button := range buttons {
		out = append(out, v2Button{uiActionButton: button, View: view})
	}
	return out
}

type v2Section struct {
	Kind          string
	Title         string
	Hint          string
	View          string
	Table         *v2TableData
	Buttons       []v2Button
	Fields        []uiFormField
	SettingFields []v2Field
	Links         []uiLinkItem
	OAuth         uiOAuthSection
	FormPath      string
	FormWrap      string
	FormSubmit    string
	FormRender    string
	ManualAdd     string
	TestFTP       bool
	ListEditor    *v2ListEditorView
	// Integration cards (Simkl) carry their child sections inline.
	Children    []v2Section
	Status      string
	StatusClass string
	// Dedicated maintenance widgets.
	Duplicates   *v2DuplicatesView
	DB           *v2DBView
	Ramdisk      *v2RamdiskView
	FolderRename *v2FolderRenameView
	Progress     *v2ProgressView
	Jobs         *v2JobsView
}

type v2Group struct {
	Title    string
	Sections []v2Section
}

// v2SectionGroups converts a page's sections into renderable groups.
func v2SectionGroups(s *AppState, r *http.Request, view string, sections []uiPageSection) []v2Group {
	groups := uiGroupSections(sections)
	out := make([]v2Group, 0, len(groups))
	tableIndex := 0
	for _, group := range groups {
		converted := v2Group{Title: group.Title}
		for _, section := range group.Sections {
			converted.Sections = append(converted.Sections, v2ConvertSection(s, r, view, section, &tableIndex))
		}
		out = append(out, converted)
	}
	return out
}

func v2ConvertSection(s *AppState, r *http.Request, view string, section uiPageSection, tableIndex *int) v2Section {
	item := v2Section{Kind: section.Kind, View: view}
	switch section.Kind {
	case "table":
		data := v2TableDataFrom(s, r, view+"-t"+strconv.Itoa(*tableIndex), section.Table)
		*tableIndex++
		item.Table = &data
		item.Title = section.Table.Title
		item.Hint = section.Table.Note
	case "actions":
		item.Title = section.Action.Label
		item.Hint = section.Action.Hint
		item.Buttons = v2Buttons(view, section.Action.Buttons)
	case "form":
		item.Title = section.Form.Title
		item.Hint = section.Form.Hint
		item.Fields = section.Form.Fields
		item.FormPath = section.Form.Path
		item.FormWrap = section.Form.Wrap
		item.FormSubmit = section.Form.Submit
		item.FormRender = section.Form.Render
		item.ManualAdd = section.Form.ManualAdd
		item.TestFTP = section.Form.TestFTP
		if item.FormSubmit == "" {
			item.FormSubmit = "Salva"
		}
	case "settings":
		item.Title = section.Settings.Title
		item.Hint = section.Settings.Hint
		item.SettingFields = v2Fields(section.Settings.Fields)
		item.Buttons = v2Buttons(view, section.Settings.Buttons)
	case "links":
		item.Title = section.Links.Title
		item.Hint = section.Links.Hint
		item.Links = section.Links.Links
	case "comics_links":
		item.Title = "Scarica da GetComics"
		item.Hint = "Incolla l'URL di un post GetComics per risolvere i link disponibili e avviare un download."
	case "oauth":
		item.Title = section.OAuth.Name
		item.OAuth = section.OAuth
		item.Buttons = v2Buttons(view, section.OAuth.Buttons)
	case "integration":
		item.Title = section.Integration.Title
		item.Status = section.Integration.Status
		item.StatusClass = section.Integration.StatusClass
		for _, child := range section.Integration.Children {
			item.Children = append(item.Children, v2ConvertSection(s, r, view, child, tableIndex))
		}
	case "list_editor":
		if key, ok := v2ListEditorKey(section.Editor); ok {
			editorView := v2ListEditorViewFrom(s, section.Editor, key, view, "")
			item.ListEditor = &editorView
			item.Title = section.Editor.Title
			item.Hint = section.Editor.Hint
		}
	case "sources_check", "trash_panel":
		// Rendered with dedicated server-side markup.
	case "progress":
		item.Title = section.Progress.Title
		progress := v2ProgressViewFrom(s, section.Progress)
		item.Progress = &progress
	case "duplicates":
		// The scan is on demand (the classic UI loads it only on click); never
		// walk the filesystem while rendering a page.
		item.Duplicates = &v2DuplicatesView{}
		item.Title = "Duplicati video in libreria"
	case "db_optimize":
		widget := v2DBViewFrom(s, r)
		item.DB = &widget
		item.Title = "Ottimizzazione database"
	case "ramdisk":
		widget := v2RamdiskViewFrom(s, r)
		item.Ramdisk = &widget
		item.Title = "RAM disk"
	case "folder_rename":
		item.FolderRename = &v2FolderRenameView{}
		item.Title = "Rinomina contenuto cartella"
	default:
		// Keep unknown section kinds visible without exposing a legacy fallback.
	}
	return item
}

// V2SectionTestFTP tests the backup FTP values currently displayed in the
// section without saving them first.
func V2SectionTestFTP(w http.ResponseWriter, r *http.Request, s *AppState) {
	body, _ := json.Marshal(FtpTestInput{
		Host:     v2StringPointer(strings.TrimSpace(r.FormValue("backup_ftp_host"))),
		User:     v2StringPointer(strings.TrimSpace(r.FormValue("backup_ftp_user"))),
		Password: v2StringPointer(r.FormValue("backup_ftp_password")),
		Path:     v2StringPointer(strings.TrimSpace(r.FormValue("backup_ftp_path"))),
	})
	raw, status := v2InternalJSON(s, http.MethodPost, "/api/backup/test-ftp", nil, body)
	message := "FTP non riuscito: " + v2JSONError(raw)
	ok := false
	if status < 400 {
		var payload struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &payload) == nil && payload.OK {
			ok = true
			message = "FTP OK: connessione, login, trasferimento e rimozione completati"
		} else if payload.Error != "" {
			message = "FTP non riuscito: " + payload.Error
		}
	}
	dict, eng := v2Dictionaries(s)
	v2Render(w, http.StatusOK, "v2_ftp_test_result", map[string]any{"OK": ok, "Message": message}, dict, eng)
}

func v2StringPointer(value string) *string {
	return &value
}

// V2SectionAction forwards a maintenance/settings action to the existing API and
// tells HTMX to reload the page.
func V2SectionAction(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.FormValue("view"))
	path := strings.TrimSpace(r.FormValue("path"))
	method := strings.ToUpper(strings.TrimSpace(r.FormValue("method")))
	if method == "" {
		method = http.MethodPost
	}
	body := r.FormValue("body")
	if body == "" {
		body = "{}"
	}
	flash := ""
	flashErr := false
	if strings.HasPrefix(path, "/api/") {
		raw, status := v2InternalJSON(s, method, path, nil, []byte(body))
		flash, flashErr = v2ActionFlash(path, raw, status)
	}
	target := strings.TrimSpace(r.FormValue("redirect"))
	if !strings.HasPrefix(target, "/v2") {
		if view == "" {
			view = "dashboard"
		}
		target = "/?view=" + url.QueryEscape(view)
	}
	if flash != "" {
		if parsed, err := url.Parse(target); err == nil {
			query := parsed.Query()
			query.Set("toast", flash)
			if flashErr {
				query.Set("toast_err", "1")
			} else {
				query.Del("toast_err")
			}
			parsed.RawQuery = query.Encode()
			target = parsed.String()
		}
	}
	w.Header().Set("HX-Redirect", target)
	w.WriteHeader(http.StatusNoContent)
}

// v2ActionFlash turns the JSON result of a forwarded action into a short toast
// message, so a click never looks like it did nothing.
func v2ActionFlash(path string, raw []byte, status int) (string, bool) {
	if status >= http.StatusBadRequest {
		if message := v2JSONError(raw); message != "" {
			return message, true
		}
		return "Operazione non riuscita.", true
	}
	if path == "/api/maintenance/clean-trash" {
		var payload struct {
			Files int `json:"files"`
		}
		if json.Unmarshal(raw, &payload) == nil {
			if payload.Files == 0 {
				return "Cestino già vuoto: nessun elemento da eliminare.", false
			}
			return fmt.Sprintf("Cestino svuotato: %d elementi eliminati.", payload.Files), false
		}
	}
	return "Operazione completata.", false
}

// V2SectionForm forwards a rendered form to the existing API. Numbers are sent
// as JSON numbers, strings as strings; Wrap nests them under an object key,
// exactly like the classic client did.
func V2SectionForm(w http.ResponseWriter, r *http.Request, s *AppState) {
	view := strings.TrimSpace(r.FormValue("view"))
	path := strings.TrimSpace(r.FormValue("path"))
	wrap := strings.TrimSpace(r.FormValue("wrap"))

	values := map[string]any{}
	kinds := map[string]string{}
	for key, list := range r.Form {
		switch key {
		case "view", "path", "wrap", "render", "redirect":
			continue
		}
		if strings.HasPrefix(key, "_v2_kind_") {
			if len(list) > 0 {
				kinds[strings.TrimPrefix(key, "_v2_kind_")] = list[0]
			}
			continue
		}
		if len(list) == 0 {
			continue
		}
		value := list[0]
		switch kinds[key] {
		case "number":
			if number, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
				values[key] = number
			} else {
				values[key] = value
			}
		case "select":
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "true":
				values[key] = true
			case "false":
				values[key] = false
			default:
				values[key] = value
			}
		default:
			values[key] = value
		}
	}
	var payload any = values
	if wrap != "" {
		payload = map[string]any{wrap: values}
	}
	encoded, _ := json.Marshal(payload)
	render := strings.TrimSpace(r.FormValue("render"))
	if render != "" && strings.HasPrefix(path, "/api/") {
		raw, status := v2InternalJSON(s, http.MethodPost, path, nil, encoded)
		if r.Header.Get("HX-Request") == "" {
			http.Redirect(w, r, "/?view="+url.QueryEscape(view), http.StatusSeeOther)
			return
		}
		dict, eng := v2Dictionaries(s)
		switch render {
		case "tmdb":
			result := v2SectionTMDBResult(r.FormValue("kind"), raw, status)
			v2Render(w, http.StatusOK, "v2_tmdb_results", result, dict, eng)
			return
		case "releases":
			result := v2SectionReleaseResult(raw, status)
			v2Render(w, http.StatusOK, "v2_search_results", result, dict, eng)
			return
		case "comics":
			result := v2SectionComicsResult(raw, status)
			v2Render(w, http.StatusOK, "v2_comics_explore_results", result, dict, eng)
			return
		}
	}
	if strings.HasPrefix(path, "/api/") {
		v2InternalJSON(s, http.MethodPost, path, nil, encoded)
	}
	target := strings.TrimSpace(r.FormValue("redirect"))
	if !strings.HasPrefix(target, "/v2") {
		if view == "" {
			view = "dashboard"
		}
		target = "/?view=" + url.QueryEscape(view)
	}
	w.Header().Set("HX-Redirect", target)
	w.WriteHeader(http.StatusNoContent)
}

// v2SectionTMDBResult adapts the JSON returned by /api/tmdb/search to the same
// cards used by the dedicated Esplora page. The API also returns TVDB fallback
// entries, which use an absolute "poster" field instead of poster_path.
func v2SectionTMDBResult(kind string, raw []byte, status int) map[string]any {
	result := map[string]any{"Kind": kind}
	if status >= 400 {
		result["Error"] = v2JSONError(raw)
		return result
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		result["Error"] = "risposta TMDB non valida"
		return result
	}
	items := make([]v2TMDBItem, 0, len(payload.Items))
	for _, item := range payload.Items {
		items = append(items, v2TMDBItemFromMap(kind, item))
	}
	result["Items"] = items
	return result
}

func v2SectionReleaseResult(raw []byte, status int) v2SearchView {
	result := v2SearchView{Searched: true}
	if status >= 400 {
		result.Error = v2JSONError(raw)
		return result
	}
	var payload struct {
		Results []models.Release `json:"results"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return result
	}
	for _, release := range payload.Results {
		encoded, _ := json.Marshal(release)
		result.Results = append(result.Results, v2SearchResult{
			Title: release.Title, Source: v2SourceLabel(release.Source), Score: release.Score,
			Seeders: release.Seeders, SizeBytes: release.SizeBytes, ReleaseJSON: string(encoded),
		})
	}
	return result
}
