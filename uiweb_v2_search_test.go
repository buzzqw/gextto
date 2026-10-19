package gextto

import "testing"

func TestV2SearchRedirectTarget(t *testing.T) {
	cases := map[string]string{
		"/?view=search":      "/?view=search",
		"/?view=dashboard":   "/?view=dashboard",
		"/?view=downloads":   "/?view=downloads",
		"https://evil.test/": "/?view=search",
		"//evil.test":        "/?view=search",
		"":                   "/?view=search",
	}
	for value, want := range cases {
		if got := v2SearchRedirectTarget(value, "/?view=search"); got != want {
			t.Errorf("v2SearchRedirectTarget(%q) = %q, want %q", value, got, want)
		}
	}
}
