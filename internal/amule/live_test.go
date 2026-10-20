package amule

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveCore runs against a real amuled when GEXTTO_AMULE_EC is set to
// host:port (and GEXTTO_AMULE_PASSWORD to its EC password). It is skipped in
// normal runs: the default suite never needs aMule installed.
func TestLiveCore(t *testing.T) {
	address := os.Getenv("GEXTTO_AMULE_EC")
	if address == "" {
		t.Skip("GEXTTO_AMULE_EC not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := New(Config{Address: address, Password: os.Getenv("GEXTTO_AMULE_PASSWORD")})
	defer client.Close()

	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	t.Logf("status: %+v", status)
	prefs, err := client.Preferences(ctx)
	if err != nil {
		t.Fatalf("preferences: %v", err)
	}
	t.Logf("preferences: %+v", prefs)
	servers, err := client.Servers(ctx)
	if err != nil {
		t.Fatalf("servers: %v", err)
	}
	t.Logf("%d servers", len(servers))

	const link = "ed2k://|file|gextto-ec-test.bin|1048576|0123456789abcdef0123456789abcdef|/"
	if err := client.AddLink(ctx, link, 0); err != nil {
		t.Fatalf("add link: %v", err)
	}
	downloads, err := client.Downloads(ctx)
	if err != nil {
		t.Fatalf("downloads: %v", err)
	}
	found := false
	for _, download := range downloads {
		t.Logf("download: %+v", download)
		if download.Hash == "0123456789abcdef0123456789abcdef" {
			found = true
			if download.Name != "gextto-ec-test.bin" || download.Size != 1048576 {
				t.Fatalf("download = %+v", download)
			}
		}
	}
	if !found {
		t.Fatal("added link is not in the queue")
	}
	if err := client.Pause(ctx, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	downloads, _ = client.Downloads(ctx)
	for _, download := range downloads {
		if download.Hash == "0123456789abcdef0123456789abcdef" && !download.Paused() {
			t.Fatalf("not paused: %+v", download)
		}
	}
	if err := client.Resume(ctx, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := client.Delete(ctx, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	downloads, _ = client.Downloads(ctx)
	for _, download := range downloads {
		if download.Hash == "0123456789abcdef0123456789abcdef" {
			t.Fatalf("still queued after delete: %+v", download)
		}
	}
	if _, err := client.Search(ctx, "ubuntu", SearchOptions{Type: SearchKad, Wait: 2 * time.Second}); err == nil {
		t.Log("Kad search accepted")
	} else {
		t.Logf("Kad search: %v", err)
	}
	if err := client.SetLimits(ctx, 500, 50); err != nil {
		t.Fatalf("set limits: %v", err)
	}
	prefs, _ = client.Preferences(ctx)
	if prefs.MaxDownload != 500 || prefs.MaxUpload != 50 {
		t.Fatalf("limits not applied: %+v", prefs)
	}
	lines, err := client.LogLines(ctx)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	t.Logf("%d log lines", len(lines))

	wrong := New(Config{Address: address, Password: "wrong"})
	defer wrong.Close()
	if err := wrong.Ping(ctx); err == nil {
		t.Fatal("a wrong password was accepted")
	}
}
