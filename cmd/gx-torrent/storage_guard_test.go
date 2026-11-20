package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateDestinationResolvesExistingSymlinks proves a destination is
// accepted or refused on the real location, not on the string: a link inside
// an allowed root must not open a door to a folder outside it.
func TestValidateDestinationResolvesExistingSymlinks(t *testing.T) {
	d := newQuietDaemon(t)
	root := filepath.Join(t.TempDir(), "root")
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "tv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	d.opts.AllowedRoots = []string{root}

	if _, err := d.validateDestination(filepath.Join(root, "tv", "new")); err != nil {
		t.Fatalf("a real subfolder of an allowed root was refused: %v", err)
	}
	if _, err := d.validateDestination(filepath.Join(root, "escape", "new")); err == nil {
		t.Fatal("a destination behind a symlink that leaves the root was accepted")
	}

	// A root that is itself a link is compared at its target: a destination on
	// the real folder is still inside it.
	linkRoot := filepath.Join(t.TempDir(), "linked-root")
	if err := os.Symlink(root, linkRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	d.opts.AllowedRoots = []string{linkRoot}
	if _, err := d.validateDestination(filepath.Join(root, "tv")); err != nil {
		t.Fatalf("the real target of an allowed root was refused: %v", err)
	}
}

// TestValidateDestinationRefusesDaemonFolders locks in the overlap rule: a save
// path may live under the data folder (the default downloads folder does), but
// it cannot be the data folder or one of its internal working folders.
func TestValidateDestinationRefusesDaemonFolders(t *testing.T) {
	d := newQuietDaemon(t)
	for name, dest := range map[string]string{
		"data":  d.opts.DataDir,
		"links": d.opts.LinkDir,
		"parts": d.opts.PartsDir,
		"db":    d.opts.DBPath,
		"state": d.opts.StatePath,
	} {
		if _, err := d.validateDestination(dest); err == nil {
			t.Errorf("%s folder %q was accepted as a destination", name, dest)
		}
	}
	if _, err := d.validateDestination(filepath.Join(d.opts.DataDir, "downloads", "tv")); err != nil {
		t.Fatalf("a normal folder under the data folder was refused: %v", err)
	}
}

// TestProtectedTargetRefusesState guards the deletion path: a save path that
// overlaps the data folder, or a torrent named like a state file, must not take
// the session with it.
func TestProtectedTargetRefusesState(t *testing.T) {
	d := newQuietDaemon(t)
	state := filepath.Join(d.opts.DataDir, "state.json")
	if err := os.WriteFile(state, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.protectedTarget(state); err == nil {
		t.Fatal("the state file was accepted as a payload target")
	}
	if err := d.protectedTarget(filepath.Join(d.opts.DataDir, "links")); err == nil {
		t.Fatal("the links folder was accepted as a payload target")
	}
	if err := d.protectedTarget(d.opts.DownloadDir); err == nil {
		t.Fatal("the download folder itself was accepted as a payload target")
	}
	if err := d.protectedTarget(filepath.Dir(d.opts.DataDir)); err == nil {
		t.Fatal("an ancestor of the data folder was accepted as a payload target")
	}

	payload := filepath.Join(d.opts.DownloadDir, "movie.bin")
	if err := os.MkdirAll(d.opts.DownloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(payload, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := d.protectedTarget(payload); err != nil {
		t.Fatalf("a normal payload was refused: %v", err)
	}

	// The same target built by deletePayload never removes the state file.
	if err := deletePayload(d.opts.DataDir, "state.json", d.protectedTarget); err == nil {
		t.Fatal("deletePayload deleted the state file")
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("the state file is gone: %v", err)
	}
}

// TestDeletePayloadRefusesStateThroughHardLink covers the identity check: a
// payload renamed or hard-linked to the state file has a different path.
func TestDeletePayloadRefusesStateThroughHardLink(t *testing.T) {
	d := newQuietDaemon(t)
	state := filepath.Join(d.opts.DataDir, "state.json")
	if err := os.WriteFile(state, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(d.opts.DownloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(d.opts.DownloadDir, "alias.bin")
	if err := os.Link(state, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := d.protectedTarget(alias); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("a hard link to the state file was accepted: %v", err)
	}
}
