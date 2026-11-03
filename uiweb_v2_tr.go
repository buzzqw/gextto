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
	"reflect"
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
		generation = db.TranslationsGeneration()
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
	funcs["trTitle"] = v2TitleFunc(dict, eng)
	set, err := template.New("v2").Funcs(funcs).ParseFS(v2TemplatesFS, "uiweb/v2/templates/*.html")
	if err != nil {
		// The templates are embedded, so a parse error is a build-time defect:
		// failing here beats serving a broken page.
		panic(fmt.Sprintf("parse the v2 templates for %q: %v", lang, err))
	}
	v2TemplateSets.Store(key, v2TemplateSet{generation: generation, set: set})
	return set
}

// v2TranslateFragment translates markup that Go assembles itself (table cells
// and row actions, the OAuth hint) instead of a template: its texts and
// tooltips cannot go through `tr`, so the fragment is translated on its own
// before it is handed to the template as template.HTML.
func v2TranslateFragment(s *AppState, fragment template.HTML) template.HTML {
	dict, eng := v2Dictionaries(s)
	return template.HTML(v2TranslateHTML(string(fragment), dict, eng))
}

// v2TranslateFunc builds the `tr` helper for one dictionary pair. The first
// argument is the Italian phrase, which is the catalog key; the optional
// arguments fill its printf verbs after the translation, so a translator can
// move the values inside the sentence.
//
// The lookup trims the surrounding whitespace and keeps it, exactly like the
// whole-text lookup of a rendered node. A value that is not plain text (a
// number, or markup already typed as template.HTML) is returned unchanged, so
// `tr` can wrap any printed value without altering how it is escaped.
func v2TranslateFunc(dict, eng map[string]string) func(value any, args ...any) any {
	return func(value any, args ...any) any {
		phrase, ok := v2TranslatableString(value)
		if !ok {
			return value
		}
		text := v2TranslateText(phrase, dict, eng)
		if len(args) == 0 {
			return text
		}
		return fmt.Sprintf(text, args...)
	}
}

// v2TitleFunc builds `trTitle`, which renders a whole title attribute. When the
// catalog translated it, the attribute carries data-v2-title-translated: the
// client keeps such a tooltip and replaces an untranslated Italian one in the
// other languages.
func v2TitleFunc(dict, eng map[string]string) func(value any, args ...any) template.HTMLAttr {
	return func(value any, args ...any) template.HTMLAttr {
		phrase, _ := v2TranslatableString(value)
		if phrase == "" && value != nil {
			phrase = fmt.Sprint(value)
		}
		text := v2TranslateText(phrase, dict, eng)
		translated := text != phrase
		if len(args) > 0 {
			text = fmt.Sprintf(text, args...)
		}
		attr := `title="` + template.HTMLEscapeString(text) + `"`
		if translated {
			attr += ` data-v2-title-translated="true"`
		}
		return template.HTMLAttr(attr)
	}
}

// v2TranslatableString reports the text of a value `tr` may look up: a string
// or a named string type, never a typed safe-content value.
func v2TranslatableString(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case template.HTML, template.HTMLAttr, template.JS, template.JSStr, template.CSS, template.URL, template.Srcset:
		return "", false
	case fmt.Stringer:
		return "", false
	}
	rv := reflect.ValueOf(value)
	if rv.IsValid() && rv.Kind() == reflect.String {
		return rv.String(), true
	}
	return "", false
}
