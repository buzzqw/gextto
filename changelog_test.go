package gextto

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// TestChangelogLanguagesShareSections keeps the Italian and English changelogs
// in step: the AGENTS rule requires the same sections and the same substance,
// and a missing translation is exactly the kind of drift that is easy to miss.
func TestChangelogLanguagesShareSections(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.TrimSpace(string(data)) == "" {
			t.Fatalf("%s is empty", name)
		}
		return string(data)
	}
	dated := regexp.MustCompile(`^(\d{1,2})\s+\S+\s+(\d{4})$`)
	headings := func(text string) []string {
		sections := make([]string, 0)
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "## ") {
				continue
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if match := dated.FindStringSubmatch(heading); match != nil {
				heading = fmt.Sprintf("%s-%s", match[1], match[2])
			}
			sections = append(sections, heading)
		}
		return sections
	}
	italian := headings(read("changelog.md"))
	english := headings(read("changelog.en.md"))
	if len(italian) == 0 {
		t.Fatal("changelog.md has no dated sections")
	}
	if !reflect.DeepEqual(italian, english) {
		t.Fatalf("changelog sections differ:\nitalian: %v\nenglish: %v", italian, english)
	}
}
