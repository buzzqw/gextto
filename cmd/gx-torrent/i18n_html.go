package main

// i18n_html.go localizes the rendered daemon page the same way Gextto's web UI
// does: the templates stay in the source language (English), and the finished
// HTML has its text nodes and translatable attributes replaced through the
// dictionary of the selected language.

import (
	"bytes"
	"net/http"
	"strings"

	xhtml "golang.org/x/net/html"
)

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
		return
	}
	body := uiTranslateHTML(buffer.String(), uiDictionary(d.uiLang(r)))
	_, _ = w.Write([]byte(body))
}
