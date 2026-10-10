package assets

import (
	"strings"
	"testing"
)

func TestAssetsAreEmbedded(t *testing.T) {
	tpl := UITemplate()
	if len(tpl) == 0 {
		t.Fatal("UITemplate is empty")
	}
	// The page body and the fragment definitions must be present: the daemon
	// renders both from the same parsed template set.
	for _, marker := range []string{"<!doctype html>", `{{define "fragments"}}`, `{{define "detail"}}`, `{{define "addopts"}}`} {
		if !strings.Contains(tpl, marker) {
			t.Errorf("UITemplate is missing %q", marker)
		}
	}
	if !strings.Contains(TokenPage(), "<form") {
		t.Error("TokenPage is missing its form")
	}
	if !strings.HasPrefix(FaviconSVG(), "<svg") {
		t.Error("FaviconSVG is not an SVG")
	}
}
