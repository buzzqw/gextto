package gextto

import (
	"strings"
	"testing"
)

func TestFeedLabelKeepsTheDistinguishingQuery(t *testing.T) {
	a := "https://extto.org/browse/?filter=u=ppb1"
	b := "https://extto.org/browse/?filter=u=other"
	if feedLabel(a) == feedLabel(b) {
		t.Fatalf("two uploader feeds share the label %q", feedLabel(a))
	}
	if !strings.Contains(feedLabel(a), "filter=u=ppb1") {
		t.Fatalf("label %q lost the filter", feedLabel(a))
	}
	if feedStatKey(a) == feedStatKey(b) {
		t.Fatal("two uploader feeds share the statistics key")
	}
}

func TestFeedLabelMasksCredentialQueryValues(t *testing.T) {
	label := feedLabel("https://example.com/rss?apikey=secret-value&passkey=other")
	if strings.Contains(label, "secret-value") || strings.Contains(label, "other") {
		t.Fatalf("credential leaked in label %q", label)
	}
	if !strings.Contains(label, "apikey=***") || !strings.Contains(label, "passkey=***") {
		t.Fatalf("label %q does not show the masks", label)
	}
}
