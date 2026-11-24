//go:build windows

package filestorage

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestOpenRefusesEscapingJunction covers the Windows equivalent of an
// intermediate symlink. Junctions can be created without developer mode or
// elevated symlink privileges, so torrent data must not traverse them.
func TestOpenRefusesEscapingJunction(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "downloads")
	outside := filepath.Join(base, "outside")
	link := filepath.Join(root, "junction")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/C", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Fatalf("cannot create test junction: %v: %s", err, out)
	}
	s, err := New(root, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Open(filepath.Join("junction", "escaped.bin"), 16); err == nil {
		t.Fatal("an intermediate junction escaping the storage root was accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.bin")); !os.IsNotExist(err) {
		t.Fatalf("a file appeared outside the storage root: %v", err)
	}
}
