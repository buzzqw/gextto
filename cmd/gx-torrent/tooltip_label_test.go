package main

// tooltip_label_test.go keeps the daemon's tooltips useful: one that says the
// same words as the label it belongs to explains nothing, and the row is better
// without it. It compares a title with the visible text of the same element.

import (
	"os"
	"path/filepath"
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
	label := regexp.MustCompile(`\{\{tr "([^"]+)"`).FindStringSubmatch(text[bodyStart : bodyStart+bodyEnd])
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

func TestDaemonTooltipsDoNotRepeatTheirLabel(t *testing.T) {
	dir := filepath.Join("..", "..", "internal", "webui", "assets")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the assets folder: %v", err)
	}
	tooltip := regexp.MustCompile(`title="\{\{tr "([^"]+)"\}\}"`)
	pages := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".html") || strings.HasSuffix(name, ".tmpl")) {
			continue
		}
		pages++
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(body)
		for _, match := range tooltip.FindAllStringSubmatchIndex(text, -1) {
			tip := text[match[2]:match[3]]
			label := tooltipElementText(text, match[0])
			if label == "" || tooltipPhrase(label) != tooltipPhrase(tip) {
				continue
			}
			line := strings.Count(text[:match[0]], "\n") + 1
			t.Errorf("%s:%d: the tooltip repeats the label %q — explain what the control does, or drop the tooltip", name, line, label)
		}
	}
	if pages == 0 {
		t.Fatal("no page was read: the guard is not looking at the real pages")
	}
}
