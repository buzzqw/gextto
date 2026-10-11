package gextto

// i18n_tooltips_test.go enforces the rule that every indexed setting has its
// own tooltip and that the tooltip text is translated in every bundled
// language. Without this guard a new option could ship with an Italian-only
// tooltip (or with no tooltip at all) and stay untranslated in the other
// languages.

import (
	"sort"
	"strings"
	"testing"
)

// TestSettingTooltipsAreTranslated checks every settings-index option (and every
// score weight) has a non-empty tooltip and that the tooltip is a catalog key in
// all bundled languages.
func TestSettingTooltipsAreTranslated(t *testing.T) {
	catalogs := map[string]map[string]string{}
	for _, file := range bundledCatalogs() {
		_, entries := parseCatalog(t, file)
		catalogs[file.lang] = entries
	}
	langs := []string{"eng", "deu", "fra", "spa", "pol"}
	defs := append(append([]uiSettingDef{}, uiSettingsIndex...), uiScoreSettingDefs...)

	var noTooltip, untranslated []string
	seen := map[string]bool{}
	for _, def := range defs {
		tooltip := strings.TrimSpace(uiSettingTooltip(def.Key))
		if tooltip == "" {
			noTooltip = append(noTooltip, def.Key)
			continue
		}
		for _, lang := range langs {
			if strings.TrimSpace(catalogs[lang][tooltip]) == "" {
				entry := lang + ": " + tooltip
				if !seen[entry] {
					seen[entry] = true
					untranslated = append(untranslated, entry)
				}
				break
			}
		}
	}
	if len(noTooltip) > 0 {
		sort.Strings(noTooltip)
		t.Errorf("options without a tooltip:\n  %s", strings.Join(noTooltip, "\n  "))
	}
	if len(untranslated) > 0 {
		sort.Strings(untranslated)
		t.Errorf("tooltips without a translation in some language:\n  %s", strings.Join(untranslated, "\n  "))
	}
}
