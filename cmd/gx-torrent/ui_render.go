package main

// ui_render.go serves the daemon pages: one template set per language (built in
// i18n_tr.go), plus the small script that hands the page's JavaScript the
// strings it looks up at runtime. Nothing is translated after rendering, so a
// value can never be mistaken for a phrase.

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
)

// uiClientDictPlaceholder marks where the client dictionary script goes in the
// page template. It is replaced at render time with window.__uiI18n. An empty
// <script> keeps html/template from stripping it as an HTML comment.
const uiClientDictPlaceholder = `<script id="ui-i18n"></script>`

// uiLanguageNames lists the interface languages with their endonyms, in the
// order the selectors show them. It is shared by the wizard, the settings page
// and the sidebar picker, so a new language is added in one place.
var uiLanguageNames = []struct{ Code, Name string }{
	{"en", "English"}, {"it", "Italiano"}, {"de", "Deutsch"},
	{"fr", "Français"}, {"es", "Español"}, {"pl", "Polski"},
}

// uiLangChoice is one entry of the sidebar's language picker (standalone).
type uiLangChoice struct {
	Code     string
	Name     string
	Selected bool
}

// uiLanguageChoices returns the language picker entries for the page, the one
// in use marked. Empty or unknown means English, like uiLang.
func uiLanguageChoices(current string) []uiLangChoice {
	current = strings.ToLower(strings.TrimSpace(current))
	if !validStandaloneLang(current) {
		current = "en"
	}
	out := make([]uiLangChoice, 0, len(uiLanguageNames))
	for _, item := range uiLanguageNames {
		out = append(out, uiLangChoice{Code: item.Code, Name: item.Name, Selected: item.Code == current})
	}
	return out
}

// uiLang is the language for one request: a valid ?lang= wins, otherwise the
// standalone settings (live, so the wizard applies at once), otherwise the
// daemon's configured language (empty or unknown means English).
func (d *Daemon) uiLang(r *http.Request) string {
	if v := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang"))); v != "" {
		if v == "en" {
			return "en"
		}
		if _, ok := uiLangIndex[v]; ok {
			return v
		}
	}
	if d.opts.Mode == ModeStandalone && d.opts.Settings != nil {
		switch v := strings.ToLower(strings.TrimSpace(d.opts.Settings.Get("lang", ""))); {
		case v == "en":
			return "en"
		case v != "":
			if _, ok := uiLangIndex[v]; ok {
				return v
			}
		}
	}
	return d.opts.Lang
}

// renderUI renders one template of the daemon's main page and writes the HTML.
// name is empty for the whole page, or a template name for a fragment.
func (d *Daemon) renderUI(w http.ResponseWriter, r *http.Request, name string, data any) {
	lang := d.uiLang(r)
	whole := name == "" || name == "ui"
	if name == "" {
		name = "ui"
	}
	// The "updated" popup belongs to the whole page, not to a fragment.
	if page, ok := data.(uiPageData); ok && whole {
		page.UpdateNotes = template.HTML(d.updateNotesDialogHTML(lang))
		data = page
	}
	var buffer bytes.Buffer
	if err := uiTemplatesFor("ui", lang).ExecuteTemplate(&buffer, name, data); err != nil {
		logf("ui render %s: %v", name, err)
		http.Error(w, "page render failed", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(uiFinishPage(buffer.String(), lang)))
}

// uiWritePage renders one of the daemon's secondary pages in the page language
// and writes it with the standard headers.
func (d *Daemon) uiWritePage(w http.ResponseWriter, r *http.Request, page string, data any) {
	lang := d.uiLang(r)
	var buffer bytes.Buffer
	if err := uiTemplatesFor(page, lang).ExecuteTemplate(&buffer, page, data); err != nil {
		logf("ui render %s: %v", page, err)
		http.Error(w, "page render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(uiFinishPage(buffer.String(), lang)))
}

// uiFinishPage adds the client dictionary and marks the language actually
// served on the <html> element.
func uiFinishPage(body, lang string) string {
	if lang != "" && lang != "en" {
		body = strings.Replace(body, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	return uiInjectClientDictionary(body, lang)
}

// uiInjectClientDictionary replaces the page placeholder with a small script
// exposing the client-side strings for the request language, so the page's
// JavaScript (confirm dialogs, toasts) is translated too. The values come
// straight from the catalog; fragments have no placeholder and are left
// untouched.
func uiInjectClientDictionary(body, lang string) string {
	if !strings.Contains(body, uiClientDictPlaceholder) {
		return body
	}
	dict := uiDictionary(lang)
	values := make(map[string]string, len(uiClientKeys))
	for _, key := range uiClientKeys {
		if dict != nil {
			if translated, ok := dict[key]; ok && translated != "" {
				values[key] = translated
				continue
			}
		}
		values[key] = key
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return strings.Replace(body, uiClientDictPlaceholder, "", 1)
	}
	script := "<script>window.__uiI18n=" + string(raw) + ";</script>"
	return strings.Replace(body, uiClientDictPlaceholder, script, 1)
}
