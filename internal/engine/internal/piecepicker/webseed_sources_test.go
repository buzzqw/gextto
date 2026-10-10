package piecepicker

import (
	"testing"

	"github.com/buzzqw/gextto/internal/engine/internal/webseedsource"
)

// gextto fork: adding or removing a web seed at runtime must not mutate the
// caller's slice (the torrent keeps its own list) nor lose the existing ones.
func TestAddRemoveWebseedSourcesDoNotAliasTheCaller(t *testing.T) {
	original := webseedsource.NewList([]string{"http://a/seed"})
	p := New(nil, 0, original, false, false)
	if len(original) != 1 || len(p.webseedSources) != 1 {
		t.Fatalf("setup: original=%d picker=%d", len(original), len(p.webseedSources))
	}

	added := &webseedsource.WebseedSource{URL: "http://b/seed"}
	p.AddWebseedSource(added)
	if len(original) != 1 {
		t.Fatalf("adding a source mutated the caller's slice: len=%d", len(original))
	}
	if len(p.webseedSources) != 2 || p.webseedSources[1] != added {
		t.Fatalf("added source not in the picker: %+v", p.webseedSources)
	}

	p.RemoveWebseedSource(added)
	if len(p.webseedSources) != 1 || p.webseedSources[0].URL != "http://a/seed" {
		t.Fatalf("remove failed: %+v", p.webseedSources)
	}
}
