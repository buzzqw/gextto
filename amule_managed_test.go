package gextto

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestManagedAmuleWritesConfigAndStarts checks the private amule.conf and the
// launch: a fake amuled records its arguments, so no real eD2k node is needed.
func TestManagedAmuleWritesConfigAndStarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("amuled is a Unix daemon")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-amuled")
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$(dirname \"$0\")/args.txt\"\ntrap 'exit 0' INT TERM\nsleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{DataDir: dir, Settings: map[string]string{
		"amule_binary":    script,
		"amule_port":      "4712",
		"amule_ed2k_port": "14662",
		"amule_kad_port":  "14672",
		"amule_host":      "127.0.0.1",
	}}
	settings, err := ensureAmuleManagedConfig(cfg)
	if err != nil {
		t.Fatalf("ensureAmuleManagedConfig: %v", err)
	}
	if settings.Password == "" || cfg.Settings["amule_password"] != settings.Password {
		t.Fatal("the EC password must be generated and kept")
	}
	conf, err := os.ReadFile(filepath.Join(dir, "amule", "amule.conf"))
	if err != nil {
		t.Fatal(err)
	}
	hash := md5.Sum([]byte(settings.Password))
	for _, want := range []string{
		"[eMule]", "[ExternalConnect]", "AcceptExternalConnections=1", "ECPort=4712", "Port=14662", "UDPPort=14672",
		"ECPassword=" + hex.EncodeToString(hash[:]), "IncomingDir=" + filepath.Join(dir, "amule", "Incoming"),
	} {
		if !strings.Contains(string(conf), want) {
			t.Errorf("amule.conf misses %q:\n%s", want, conf)
		}
	}
	proc, err := startManagedAmule(cfg)
	if err != nil {
		t.Fatalf("startManagedAmule: %v", err)
	}
	defer proc.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		args, err := os.ReadFile(filepath.Join(dir, "args.txt"))
		if err == nil {
			if !strings.Contains(string(args), "-c") || !strings.Contains(string(args), filepath.Join(dir, "amule")) {
				t.Fatalf("amuled args = %q", args)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake amuled did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestManagedAmuleRefusesRemoteHost(t *testing.T) {
	cfg := &Config{DataDir: t.TempDir(), Settings: map[string]string{"amule_host": "10.0.0.5"}}
	if _, err := amuleManagedConfig(cfg); err == nil {
		t.Fatal("a managed amuled must listen on loopback")
	}
}
