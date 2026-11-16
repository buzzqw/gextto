package gextto

// uiweb_v2_inline_style_test.go keeps presentation out of the markup: a
// template carries a class, and the stylesheet holds what the class looks like.
// A value that comes from the data (an advance, a colour of a theme preview)
// may ride as a custom property — style="--pct:42%" — because it is data, not a
// decision about how a piece of the page should look.

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestV2TemplatesCarryNoInlineStyles(t *testing.T) {
	inline := regexp.MustCompile(`style="`)
	files := 0
	err := fs.WalkDir(v2TemplatesFS, "uiweb/v2/templates", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		files++
		body, err := v2TemplatesFS.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, loc := range inline.FindAllStringIndex(text, -1) {
			value := text[loc[1]:]
			if end := strings.IndexByte(value, '"'); end >= 0 {
				value = value[:end]
			}
			if strings.HasPrefix(value, "--") {
				continue
			}
			line := strings.Count(text[:loc[0]], "\n") + 1
			t.Errorf("%s:%d carries an inline style: %q — use a class, or a custom property when the value comes from the data", path, line, value)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the templates: %v", err)
	}
	if files == 0 {
		t.Fatal("no template was read: the guard is not looking at the real templates")
	}
}
