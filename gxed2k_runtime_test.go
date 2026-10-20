package gextto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEd2kIPFilterArg(t *testing.T) {
	dir := t.TempDir()
	if got := ed2kIPFilterArg(dir); got != "" {
		t.Fatalf("filter without a file = %q", got)
	}
	path := filepath.Join(dir, "ipfilter.dat")
	if err := os.WriteFile(path, []byte("1.2.3.0 - 1.2.3.255 , 000 , test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ed2kIPFilterArg(dir); got != path {
		t.Fatalf("filter path = %q, want %q", got, path)
	}
}
