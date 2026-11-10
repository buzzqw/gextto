package main

// i18n_tr.go translates the daemon pages while they are rendered.
//
// The page source is English, so a template text is also its catalog key:
// `{{tr "Save"}}` looks the phrase up in the dictionary of the language in
// force and renders the translation in its place. With arguments it fills the
// verbs after the lookup, so a value can sit inside a translated sentence
// (`{{tr "%d torrents" .N}}`) and a translator can move it where the language
// wants it.
//
// A parsed template set is cached per (page, language): `tr` has to be bound to
// one dictionary, and the page is big enough that re-parsing it on every poll
// would cost more than it saves. Nothing is translated after rendering, so a
// value (a category, a torrent name, a path) is never mistaken for a phrase.

import (
	"fmt"
	"html/template"
	"sync"

	"github.com/buzzqw/gextto/internal/webui/assets"
)

// uiPageSource maps the name used by the renderers to the template source of
// one page. Each entry is parsed on its own, so a page can be changed without
// touching the others.
var uiPageSource = map[string]func() string{
	"ui":       assets.UITemplate,
	"rss":      assets.RSSPage,
	"settings": assets.SettingsPage,
	"wanted":   assets.WantedPage,
	"archive":  assets.ArchivePage,
	"stats":    assets.StatsPage,
	"setup":    assets.SetupPage,
	"login":    assets.LoginPage,
	"token":    assets.TokenPage,
}

// uiTemplateSetKey identifies one parsed page in one language: "en" (the
// source language) needs no dictionary and is cached like the others.
type uiTemplateSetKey struct {
	page string
	lang string
}

var uiTemplateSets sync.Map // uiTemplateSetKey -> *template.Template

// uiTemplatesFor returns the parsed set of one page, with `tr` bound to the
// dictionary of the language in force. A language the catalog does not know
// renders the page as written (English).
func uiTemplatesFor(page, lang string) *template.Template {
	key := uiTemplateSetKey{page: page, lang: lang}
	if cached, ok := uiTemplateSets.Load(key); ok {
		return cached.(*template.Template)
	}
	source, ok := uiPageSource[page]
	if !ok {
		panic("uiTemplatesFor: unknown page " + page)
	}
	funcs := uiTemplateFuncMap()
	funcs["tr"] = uiTranslateFunc(lang)
	set, err := template.New(page).Funcs(funcs).Parse(source())
	if err != nil {
		// The pages are embedded, so a parse error is a build-time defect:
		// failing here beats serving a broken page.
		panic(fmt.Sprintf("parse the %q page for %q: %v", page, lang, err))
	}
	actual, _ := uiTemplateSets.LoadOrStore(key, set)
	return actual.(*template.Template)
}

// uiTemplates keeps the old name for the main page in the source language: the
// tests that execute it directly do not need a dictionary.
func uiTemplates() *template.Template { return uiTemplatesFor("ui", "") }

// uiTemplateFuncMap is the set of helpers every page can use.
func uiTemplateFuncMap() template.FuncMap {
	return template.FuncMap{
		"percent": func(p float64) string { return fmt.Sprintf("%.1f", p) },
		"bytes":   uiBytes,
		"rate":    uiRate,
		"dur":     uiDuration,
		// stateLabel prints the human name of a state, a catalog key.
		"stateLabel": uiStateLabel,
		// stateIcon picks the row's state icon and its label (a catalog key).
		"stateIcon": uiStateIcon,
		// kibrate prints a KiB/s limit as a rate ("∞" when unlimited).
		"kibrate": func(kib int64) string {
			if kib <= 0 {
				return "∞"
			}
			return uiRate(kib * 1024)
		},
		// pct is done over size in percent, for the Files tab bars.
		"pct": func(done, size int64) string {
			if size <= 0 {
				return "0.0"
			}
			return fmt.Sprintf("%.1f", min(100, float64(done)*100/float64(size)))
		},
	}
}

// uiTranslateFunc builds the `tr` helper for one language. The first argument
// is the English phrase, which is the catalog key; the optional arguments fill
// its printf verbs after the translation.
func uiTranslateFunc(lang string) func(value any, args ...any) any {
	return func(value any, args ...any) any {
		phrase, ok := value.(string)
		if !ok {
			return value
		}
		return uiTr(lang, phrase, args...)
	}
}

// uiTr translates one phrase and fills its verbs: it is `tr` for the strings a
// handler or an HTML fragment builds in Go instead of a template. A phrase
// without a translation is returned as written, in English.
func uiTr(lang, phrase string, args ...any) string {
	text := phrase
	if translated, ok := uiDictionary(lang)[phrase]; ok && translated != "" {
		text = translated
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}
