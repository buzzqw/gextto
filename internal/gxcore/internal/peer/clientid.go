package peer

import (
	"strings"
)

// clientID renders a 20-byte peer id in compact human form: the 8-byte
// client tag for standard ids, the version tag for ours, the raw id
// otherwise. Callers always pass full 20-byte ids.
func clientID(id string) string {
	// BEP 20 Azureus-style: "-XX0000-".
	if id[7] == '-' {
		return id[:8]
	}
	// gx-core style: "-GX....-".
	if strings.HasPrefix(id, "-GX") {
		if i := strings.IndexRune(id[1:], '-'); i != -1 {
			return id[:i+2]
		}
	}
	return id
}
