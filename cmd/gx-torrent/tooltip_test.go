package main

// tooltip_test.go enforces the rule that every option shown in the daemon's
// forms carries its own tooltip. The page translator turns a translated title
// (or aria-label) into the localized tooltip, so a control without one would
// fall back to the (translated) label only, and a tooltip without a catalog key
// would stay in English (TestUIPageIsFullyTranslatable already blocks the
// latter). A control wrapped in a <label> whose opening tag carries the tooltip
// is covered too: the label is the option.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/webui/assets"
)

// TestEveryOptionHasATooltip checks every option form (main page, RSS page,
// settings page and setup wizard) exposes a title or an aria-label, either on
// the control or on the wrapping label.
func TestEveryOptionHasATooltip(t *testing.T) {
	pages := []struct{ name, html string }{
		{"ui.tmpl", assets.UITemplate()},
		{"rss.html", assets.RSSPage()},
		{"settings.html", assets.SettingsPage()},
		{"setup.html", assets.SetupPage()},
	}
	control := regexp.MustCompile(`<(?:input|select|textarea)\b[^>]*>`)
	for _, page := range pages {
		for _, loc := range control.FindAllStringIndex(page.html, -1) {
			tag := page.html[loc[0]:loc[1]]
			lower := strings.ToLower(tag)
			// Buttons and hidden fields carry no option: they are actions or
			// state, not settings.
			if strings.Contains(lower, `type="hidden"`) || strings.Contains(lower, `type="submit"`) ||
				strings.Contains(lower, `type="button"`) || strings.Contains(lower, `type="reset"`) {
				continue
			}
			if hasTooltip(tag) || labelHasTooltipBefore(page.html, loc[0]) {
				continue
			}
			t.Errorf("%s: control without a tooltip: %s", page.name, tag)
		}
	}
}

// hasTooltip reports whether a tag carries a tooltip the translator localizes.
func hasTooltip(tag string) bool {
	lower := strings.ToLower(tag)
	return strings.Contains(lower, " title=") || strings.Contains(lower, " aria-label=")
}

// labelHasTooltipBefore reports whether the control starting at idx sits inside
// an open <label> whose opening tag carries a tooltip. A </label> between the
// last <label and the control means the control is not inside that label.
func labelHasTooltipBefore(html string, idx int) bool {
	seg := html[:idx]
	open := strings.LastIndex(seg, "<label")
	if open < 0 {
		return false
	}
	if closeIdx := strings.LastIndex(seg, "</label>"); closeIdx > open {
		return false
	}
	end := strings.Index(seg[open:], ">")
	if end < 0 {
		return false
	}
	return hasTooltip(seg[open : open+end+1])
}
