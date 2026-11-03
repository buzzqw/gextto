package gextto

// uiweb_v2_tr.go translates a page while it is rendered.
//
// The interface is written in Italian, so a template text is also its catalog
// key. `tr "…"` looks the phrase up in the dictionary of the language in force
// and renders the translation in its place; with arguments it fills the values
// after the lookup, so a sentence that carries a number can be translated too.
//
// The parsed template set is cached per (i18n database, language, dictionary
// generation): `tr` has to be bound to one dictionary, and re-parsing 140
// templates on every request would cost more than it saves.

import (
	"fmt"
	"html/template"
	"sync"
)

type v2TemplateSetKey struct {
	i18n *I18nDb
	lang string
}

// v2TemplateSet is the parsed set together with the dictionary generation it
// captured: a translation import or edit bumps the generation and the next
// render parses a new set, so the cached one can never serve stale strings.
type v2TemplateSet struct {
	generation uint64
	set        *template.Template
}

var v2TemplateSets sync.Map // v2TemplateSetKey -> v2TemplateSet

// v2TemplatesFor returns the parsed template set of the active language, with
// `tr` bound to that language's dictionary.
func v2TemplatesFor(s *AppState) *template.Template {
	lang := "it"
	var db *I18nDb
	generation := uint64(0)
	if s != nil {
		db = s.i18n
		lang = v2Language(s)
		generation = db.generation()
	}
	key := v2TemplateSetKey{i18n: db, lang: lang}
	if cached, ok := v2TemplateSets.Load(key); ok {
		if entry := cached.(v2TemplateSet); entry.generation == generation {
			return entry.set
		}
	}
	dict, eng := v2Dictionaries(s)
	funcs := v2TemplateFuncMap()
	funcs["tr"] = v2TranslateFunc(dict, eng)
	set, err := template.New("v2").Funcs(funcs).ParseFS(v2TemplatesFS, "uiweb/v2/templates/*.html")
	if err != nil {
		// The templates are embedded, so a parse error is a build-time defect:
		// failing here beats serving a broken page.
		panic(fmt.Sprintf("parse the v2 templates for %q: %v", lang, err))
	}
	v2TemplateSets.Store(key, v2TemplateSet{generation: generation, set: set})
	return set
}

// v2TranslateFunc builds the `tr` helper for one dictionary pair. The first
// argument is the Italian phrase, which is the catalog key; the optional
// arguments fill its printf verbs after the translation, so a translator can
// move the values inside the sentence.
func v2TranslateFunc(dict, eng map[string]string) func(phrase string, args ...any) string {
	lookup := func(phrase string) string {
		if text, ok := dict[phrase]; ok && text != "" {
			return text
		}
		if text, ok := eng[phrase]; ok && text != "" {
			return text
		}
		return phrase
	}
	return func(phrase string, args ...any) string {
		text := lookup(phrase)
		if len(args) == 0 {
			return text
		}
		return fmt.Sprintf(text, args...)
	}
}
