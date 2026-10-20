//go:debug rsa1024min=0

package secident

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateSignsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cryptkey.pem")
	key, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(key.PublicKey()) == 0 {
		t.Fatal("empty public key")
	}
	data := []byte("peer key and challenge")
	signature, err := key.Sign(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(key.PublicKey(), data, signature); err != nil {
		t.Fatalf("round trip failed: %v", err)
	}

	reloaded, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(reloaded.PublicKey()) != string(key.PublicKey()) {
		t.Fatal("reloaded key differs from the generated one")
	}
	if err := Verify(reloaded.PublicKey(), data, signature); err != nil {
		t.Fatalf("signature from the reloaded key failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode = %o, want 600", info.Mode().Perm())
	}
}

func TestVerifyRejectsTamperedData(t *testing.T) {
	key, err := LoadOrCreate(filepath.Join(t.TempDir(), "cryptkey.pem"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := key.Sign([]byte("original"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(key.PublicKey(), []byte("changed"), signature); err == nil {
		t.Fatal("tampered data verified")
	}
}

func TestLoadOrCreateRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cryptkey.pem")
	if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("malformed key file accepted")
	}
}
