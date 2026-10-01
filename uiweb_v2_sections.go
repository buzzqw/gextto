package gextto

// uiweb_v2_sections.go renders the section-composed pages (Manutenzione,
// Integrazioni) in the v2 interface. It normalises the existing uiPageSection
// values into a small view type the templates can render, reusing the generic
// table renderer for table sections and forwarding actions/forms to the very
// same JSON APIs the classic UI calls.

import (
	"encoding/json"
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
	ListEditor    *v2ListEditorView
	ClassicURL    string
	// Integration cards (Trakt, Simkl) carry their child sections inline.
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
		} else {
			item.ClassicURL = "/?view=" + view
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
		item.ClassicURL = "/?view=" + view
	}
	return item
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
	if strings.HasPrefix(path, "/api/") {
		v2InternalJSON(s, method, path, nil, []byte(body))
	}
	target := strings.TrimSpace(r.FormValue("redirect"))
	if !strings.HasPrefix(target, "/v2") {
		if view == "" {
			view = "dashboard"
		}
		target = "/v2?view=" + url.QueryEscape(view)
	}
	w.Header().Set("HX-Redirect", target)
	w.WriteHeader(http.StatusNoContent)
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
			http.Redirect(w, r, "/v2?view="+url.QueryEscape(view), http.StatusSeeOther)
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
		target = "/v2?view=" + url.QueryEscape(view)
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
