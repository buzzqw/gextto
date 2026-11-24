package gextto

// uiweb_v2_tooltip_label_test.go keeps the tooltips useful: a tooltip that says
// the same words as the label it belongs to explains nothing, and the page is
// better without it. The rule is the operator's, and the mechanical part is
// this test: it compares a `trTitle` with the visible text of its element.

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// tooltipElementText returns the first visible `tr "…"` text inside the element
// that carries the tooltip at position start.
func tooltipElementText(text string, start int) string {
	open := strings.LastIndex(text[:start], "<")
	if open < 0 {
		return ""
	}
	matches := regexp.MustCompile(`^<([a-zA-Z0-9]+)`).FindStringSubmatch(text[open:])
	if matches == nil {
		return ""
	}
	name := strings.ToLower(matches[1])
	bodyStart := strings.Index(text[start:], ">")
	if bodyStart < 0 {
		return ""
	}
	bodyStart += start + 1
	bodyEnd := strings.Index(text[bodyStart:], "</"+name)
	if bodyEnd < 0 {
		return ""
	}
	label := regexp.MustCompile(`\{\{(?:tr|ed2kTr) "([^"]+)"`).FindStringSubmatch(text[bodyStart : bodyStart+bodyEnd])
	if label == nil {
		return ""
	}
	return label[1]
}

// tooltipPhrase normalises a phrase for the comparison: case, punctuation and
// repeated spaces must not hide a repetition.
func tooltipPhrase(value string) string {
	value = strings.ToLower(value)
	value = regexp.MustCompile(`[^\p{L}\p{N}\s]+`).ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(value), " ")
}

func TestV2TooltipsDoNotRepeatTheirLabel(t *testing.T) {
	tooltip := regexp.MustCompile(`\{\{(?:trTitle|ed2kTitle) "([^"]+)"`)
	pages := 0
	err := fs.WalkDir(v2TemplatesFS, "uiweb/v2/templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		pages++
		body, err := v2TemplatesFS.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, match := range tooltip.FindAllStringSubmatchIndex(text, -1) {
			tip := text[match[2]:match[3]]
			label := tooltipElementText(text, match[0])
			if label == "" || tooltipPhrase(label) != tooltipPhrase(tip) {
				continue
			}
			line := strings.Count(text[:match[0]], "\n") + 1
			t.Errorf("%s:%d: the tooltip repeats the label %q — explain what the control does, or drop the tooltip", path, line, label)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the templates: %v", err)
	}
	if pages == 0 {
		t.Fatal("no template was read: the guard is not looking at the real templates")
	}
}
