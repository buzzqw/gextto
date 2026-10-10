package main

// i18n_html.go localizes the rendered daemon page the same way Gextto's web UI
// does: the templates stay in the source language (English), and the finished
// HTML has its text nodes and translatable attributes replaced through the
// dictionary of the selected language.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	xhtml "golang.org/x/net/html"
)

// uiClientDictPlaceholder marks where the client dictionary script goes in the
// page template. It is replaced at render time with window.__uiI18n. An empty
// <script> keeps html/template from stripping it as an HTML comment.
const uiClientDictPlaceholder = `<script id="ui-i18n"></script>`

// uiTranslatableAttr lists the attributes whose value is user-facing text.
func uiTranslatableAttr(name string) bool {
	switch name {
	case "title", "placeholder", "aria-label", "label":
		return true
	}
	return false
}

func uiIsSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

// uiTranslateText replaces a whole (trimmed) text value that matches a
// dictionary key, preserving the surrounding whitespace.
func uiTranslateText(value string, dict map[string]string) string {
	if value == "" || len(dict) == 0 {
		return value
	}
	start := 0
	for start < len(value) && uiIsSpace(value[start]) {
		start++
	}
	end := len(value)
	for end > start && uiIsSpace(value[end-1]) {
		end--
	}
	if start >= end {
		return value
	}
	core := value[start:end]
	translated, ok := dict[core]
	if !ok || translated == "" || translated == core {
		return value
	}
	return value[:start] + translated + value[end:]
}

// uiTranslateHTML walks the rendered HTML and translates text nodes and the
// translatable attributes, never touching <script>/<style> content.
func uiTranslateHTML(raw string, dict map[string]string) string {
	if len(dict) == 0 {
		return raw
	}
	var out bytes.Buffer
	tokenizer := xhtml.NewTokenizer(strings.NewReader(raw))
	rawTag := ""
	for {
		tokenType := tokenizer.Next()
		if tokenType == xhtml.ErrorToken {
			break
		}
		switch tokenType {
		case xhtml.TextToken:
			if rawTag != "" {
				out.Write(tokenizer.Raw())
				continue
			}
			out.WriteString(xhtml.EscapeString(uiTranslateText(string(tokenizer.Text()), dict)))
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, hasAttr := tokenizer.TagName()
			tag := strings.ToLower(string(name))
			out.WriteByte('<')
			out.Write(name)
			for hasAttr {
				key, value, more := tokenizer.TagAttr()
				text := string(value)
				if uiTranslatableAttr(strings.ToLower(string(key))) {
					text = uiTranslateText(text, dict)
				}
				out.WriteByte(' ')
				out.Write(key)
				out.WriteString(`="`)
				out.WriteString(xhtml.EscapeString(text))
				out.WriteByte('"')
				hasAttr = more
			}
			if tokenType == xhtml.SelfClosingTagToken {
				out.WriteString("/>")
			} else {
				out.WriteByte('>')
				if tag == "script" || tag == "style" {
					rawTag = tag
				}
			}
		case xhtml.EndTagToken:
			name, _ := tokenizer.TagName()
			out.WriteString("</")
			out.Write(name)
			out.WriteByte('>')
			if strings.EqualFold(string(name), rawTag) {
				rawTag = ""
			}
		case xhtml.CommentToken, xhtml.DoctypeToken:
			out.Write(tokenizer.Raw())
		}
	}
	return out.String()
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

// renderUI renders one template and writes the localized HTML. name is empty
// for the whole page, or a template name for a fragment.
func (d *Daemon) renderUI(w http.ResponseWriter, r *http.Request, name string, data any) {
	var buffer bytes.Buffer
	var err error
	if name == "" || name == "ui" {
		err = uiTemplates().Execute(&buffer, data)
	} else {
		err = uiTemplates().ExecuteTemplate(&buffer, name, data)
	}
	if err != nil {
		logf("ui render %s: %v", name, err)
		http.Error(w, "page render failed", http.StatusInternalServerError)
		return
	}
	lang := d.uiLang(r)
	body := buffer.String()
	if lang != "" && lang != "en" {
		// <html lang="en"> must reflect the language actually served.
		body = strings.Replace(body, `lang="en"`, `lang="`+lang+`"`, 1)
	}
	body = uiTranslateHTML(body, uiDictionary(lang))
	body = uiInjectClientDictionary(body, lang)
	_, _ = w.Write([]byte(body))
}

// uiInjectClientDictionary replaces the page placeholder with a small script
// exposing the client-side strings for the request language, so the page's
// JavaScript (confirm dialogs, toasts) stops being English-only. The server
// translator skips <script> content, so the values come straight from the
// catalog; fragments have no placeholder and are left untouched.
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
