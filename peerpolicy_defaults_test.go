package gextto

import (
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/torrent"
)

// TestDefaultClientFilterMatchesEngine keeps the list Gextto prefills in the
// settings the same as the one the engine suggests.
func TestDefaultClientFilterMatchesEngine(t *testing.T) {
	var rules []string
	for _, rule := range strings.Split(defaultClientFilterList, ",") {
		rules = append(rules, strings.TrimSpace(rule))
	}
	if strings.Join(rules, ",") != strings.Join(torrent.DefaultClientFilter, ",") {
		t.Fatalf("Gextto default %v, engine default %v", rules, torrent.DefaultClientFilter)
	}
}
