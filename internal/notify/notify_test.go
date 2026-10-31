package notify

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSplitArgs checks the optional argument string of the custom script:
// spaces split, quotes keep a value together, a backslash escapes.
func TestSplitArgs(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"--quiet /tmp/out", []string{"--quiet", "/tmp/out"}},
		{`--title "two words"`, []string{"--title", "two words"}},
		{`--title 'two words'`, []string{"--title", "two words"}},
		{`/tmp/a\ b`, []string{"/tmp/a b"}},
	}
	for _, c := range cases {
		got := splitArgs(c.raw)
		if len(got) != len(c.want) {
			t.Fatalf("splitArgs(%q) = %#v, want %#v", c.raw, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("splitArgs(%q) = %#v, want %#v", c.raw, got, c.want)
			}
		}
	}
}

// TestWebhookBodyKeepsTheGenericPayload locks the JSON webhook: its shape is
// what the existing configurations and scripts already read.
func TestWebhookBodyKeepsTheGenericPayload(t *testing.T) {
	event := Event{Name: "feed_match", Text: "added", Fields: map[string]any{"feed": "f"}}
	body, contentType, headers, err := webhookBody("json", "gx-torrent", "", "", event)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "application/json" || len(headers) != 0 {
		t.Fatalf("contentType = %q, headers = %v", contentType, headers)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["app"] != "gx-torrent" || payload["event"] != "feed_match" || payload["feed"] != "f" || payload["text"] != "added" {
		t.Fatalf("payload = %v", payload)
	}
	if _, ok := payload["time"].(string); !ok {
		t.Fatalf("missing time: %v", payload)
	}
}

// TestEventTitleFallsBackToTheFirstLine checks a channel that needs a subject
// (Apprise, the custom script) always gets a readable one.
func TestEventTitleFallsBackToTheFirstLine(t *testing.T) {
	if got := eventTitle("gx-torrent", Event{Title: "Download complete: X"}); got != "Download complete: X" {
		t.Fatalf("title = %q", got)
	}
	got := eventTitle("gx-torrent", Event{Text: "✅ Done\n• more"})
	if !strings.HasPrefix(got, "gx-torrent: ✅ Done") {
		t.Fatalf("title = %q", got)
	}
}
