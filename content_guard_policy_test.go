package gextto

// The ingress policy controls how a program in a media payload is handled.
// Quarantine must keep the good video and remove only the suspicious file;
// "off" must report without changing the queue or the blocklist.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/buzzqw/gextto/internal/models"
)

func TestUnsafePayloadReasonFollowsIngressPolicy(t *testing.T) {
	names := []string{"Show.S01E01.mkv", "Setup.exe"}
	for _, policy := range []string{ingressPolicyBlock, ingressPolicyQuarantine, ingressPolicyOff} {
		reason := unsafePayloadReasonForPolicy(names, "series", policy)
		if policy == ingressPolicyOff {
			if reason != "" {
				t.Fatalf("off policy still refused the payload: %q", reason)
			}
		} else if reason == "" {
			t.Fatalf("%s policy accepted a payload with an executable", policy)
		}
	}
}

func TestInspectActiveContentQuarantinesOnlyTheSuspiciousFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "Show.S01E01.mkv")
	bad := filepath.Join(dir, "Setup.exe")
	if err := os.WriteFile(good, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("program"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, db, engine, hash := contentInspectFixture(t, []models.FileView{{Path: good}, {Path: bad}})
	trash := filepath.Join(dir, "trash")
	cfg.TrashPath = &trash
	cfg.Settings["ingress_content_policy"] = ingressPolicyQuarantine

	InspectActiveContent(cfg, engine, db, nil, map[string]contentInspectionCheck{})

	if deleteFiles, ok := engine.removed[hash]; !ok || deleteFiles {
		t.Fatalf("removed = %v; the torrent must be detached without deleting the video", engine.removed)
	}
	if _, err := os.Stat(good); err != nil {
		t.Fatalf("the good video was removed: %v", err)
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("the suspicious file is still in the download folder: %v", err)
	}
	blocked, err := db.IsBlocklisted(hash)
	if err != nil || !blocked {
		t.Fatalf("the release was not blocklisted after quarantine: blocked=%v err=%v", blocked, err)
	}
}

func TestInspectActiveContentReportOnlyChangesNothing(t *testing.T) {
	cfg, db, engine, _ := contentInspectFixture(t, []models.FileView{{Path: "Setup.exe"}})
	cfg.Settings["ingress_content_policy"] = ingressPolicyOff

	InspectActiveContent(cfg, engine, db, nil, map[string]contentInspectionCheck{})

	if len(engine.removed) != 0 {
		t.Fatalf("report-only policy removed a torrent: %v", engine.removed)
	}
}

func TestValidCompletedVideoBesideSkipsTheBadNamedFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.mkv")
	good := filepath.Join(dir, "good.mkv")
	if err := os.WriteFile(bad, []byte("not a video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, []byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F, 0x43, 0xA5, 0x42, 0x86, 0x81, 0x01, 0x42}, 0o644); err != nil {
		t.Fatal(err)
	}
	found, err := validCompletedVideoBeside(bad)
	if err != nil {
		t.Fatalf("validCompletedVideoBeside: %v", err)
	}
	if found != good {
		t.Fatalf("found %q, want %q", found, good)
	}
}
