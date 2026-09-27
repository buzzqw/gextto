package gextto

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/buzzqw/gextto/internal/qbittorrent"
)

func TestSafeTorrentCopyName(t *testing.T) {
	hash := "eb1ba22b84d91184526567c7a06106c73ff9c8cd"

	if got := safeTorrentCopyName("Fondazione.S03.1080p", hash); got != "Fondazione.S03.1080p" {
		t.Fatalf("plain name = %q", got)
	}
	if got := safeTorrentCopyName("", hash); got != hash {
		t.Fatalf("empty name = %q, want hash", got)
	}
	if got := safeTorrentCopyName("magnet:?xt=urn:btih:ABC&dn=x&tr=udp://a", hash); got != hash {
		t.Fatalf("magnet name = %q, want hash", got)
	}
	// Already-sanitised magnet (colons and slashes stripped) must also fall back.
	if got := safeTorrentCopyName("magnetxt=urnbtihabcdef", hash); got != hash {
		t.Fatalf("sanitised magnet name = %q, want hash", got)
	}
	if got := safeTorrentCopyName("a/b:c*d?e", hash); got != "abcde" {
		t.Fatalf("invalid chars = %q, want abcde", got)
	}
	long := strings.Repeat("a", 400)
	got := safeTorrentCopyName(long, hash)
	if len(got) > 160 {
		t.Fatalf("truncated length = %d, want <= 160", len(got))
	}
	if !strings.HasSuffix(got, hash[:8]) {
		t.Fatalf("truncated name = %q, want hash suffix", got)
	}
}

func TestTorrentNotFoundError(t *testing.T) {
	if !torrentNotFoundError(fmt.Errorf("libtorrent action failed: torrent not found")) {
		t.Fatal("libtorrent 'torrent not found' must be detected")
	}
	if !torrentNotFoundError(qbittorrent.ErrNotFound) {
		t.Fatal("qbittorrent ErrNotFound must be detected")
	}
	if torrentNotFoundError(errors.New("disk full")) {
		t.Fatal("unrelated error must not be treated as missing torrent")
	}
	if torrentNotFoundError(nil) {
		t.Fatal("nil error must return false")
	}
}
