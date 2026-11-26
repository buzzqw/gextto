package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// crossDeviceRenames makes every payload rename fail as it does across
// filesystems, so the copy path runs on a single test filesystem.
func crossDeviceRenames(t *testing.T) {
	t.Helper()
	previous := renamePayloadFile
	renamePayloadFile = func(from, to string) error {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	t.Cleanup(func() { renamePayloadFile = previous })
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPayloadMoveAcrossFilesystemsUndoesCopiesOnFailure covers a move of loose
// files to another filesystem that fails on the second file: the first one,
// already copied, must not stay split between the two places. The payload
// stays whole at the source, where the state still points.
func TestPayloadMoveAcrossFilesystemsUndoesCopiesOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files: the failure cannot be staged")
	}
	crossDeviceRenames(t)
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.bin"), "first")
	writeTestFile(t, filepath.Join(src, "b.bin"), "second")
	// The second copy fails: its source cannot be read.
	if err := os.Chmod(filepath.Join(src, "b.bin"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "b.bin"), 0o644) })

	p := payload{files: []string{"a.bin", "b.bin"}}
	if err := p.move(src, dst); err == nil {
		t.Fatal("the move must fail when a file cannot be copied")
	}
	if body, err := os.ReadFile(filepath.Join(src, "a.bin")); err != nil || string(body) != "first" {
		t.Fatalf("first file at the source = %q, %v: the payload must stay whole there", body, err)
	}
	if _, err := os.Lstat(filepath.Join(src, "b.bin")); err != nil {
		t.Fatalf("second file at the source: %v", err)
	}
	for _, name := range []string{"a.bin", "a.bin.gxpart", "b.bin", "b.bin.gxpart"} {
		if _, err := os.Lstat(filepath.Join(dst, name)); err == nil {
			t.Fatalf("%s left at the destination after a failed move", name)
		}
	}
}

// TestPayloadMoveAcrossFilesystemsRemovesSourcesOnlyAtTheEnd covers the
// successful copy: every file reaches the destination and only then are the
// sources removed.
func TestPayloadMoveAcrossFilesystemsRemovesSourcesOnlyAtTheEnd(t *testing.T) {
	crossDeviceRenames(t)
	src, dst := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(src, "a.bin"), "first")
	writeTestFile(t, filepath.Join(src, "sub", "b.bin"), "second")

	p := payload{files: []string{"a.bin", "sub/b.bin"}}
	if err := p.move(src, dst); err != nil {
		t.Fatalf("move: %v", err)
	}
	for rel, want := range map[string]string{"a.bin": "first", "sub/b.bin": "second"} {
		if body, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel))); err != nil || string(body) != want {
			t.Fatalf("%s at the destination = %q, %v", rel, body, err)
		}
		if _, err := os.Lstat(filepath.Join(src, filepath.FromSlash(rel))); err == nil {
			t.Fatalf("%s still at the source after a completed move", rel)
		}
	}
}

// TestMoveRefusesATorrentThatIsStillStopping covers a torrent whose engine
// does not report it stopped within the wait (a slow or stuck disk): its files
// may still be written, so nothing is moved and the move reports an error the
// operator can retry, instead of copying data that is still changing.
func TestMoveRefusesATorrentThatIsStillStopping(t *testing.T) {
	d := newTestDaemon(t)
	src := filepath.Join(t.TempDir(), "src")
	data := makeTorrent(t, src, "payload.bin", 100_000)
	hash, _, err := d.add(addRequest{TorrentData: data, Destination: src, SeedRatio: -1, SeedDays: -1})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "seeding", func() bool { return stateOf(d, hash) == "seeding" })

	previous := stopWaitLimit
	stopWaitLimit = 0 // the running torrent is "still stopping" at once
	t.Cleanup(func() { stopWaitLimit = previous })

	d.mu.Lock()
	tor, meta := d.findLocked(hash)
	id := tor.ID()
	d.moving[id] = true
	d.mu.Unlock()
	dst := filepath.Join(t.TempDir(), "library")
	d.finishMove(id, tor, src, dst, "payload.bin", payloadOf(tor, meta, "payload.bin"), false, false, false)

	if _, err := os.Stat(filepath.Join(src, "payload.bin")); err != nil {
		t.Fatalf("the payload left the source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "payload.bin")); err == nil {
		t.Fatal("the payload was copied while the torrent was still stopping")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if meta.SavePath != src || !strings.Contains(meta.Error, "did not stop in time") {
		t.Fatalf("save path %q, error %q: want the source and a retryable error", meta.SavePath, meta.Error)
	}
	if d.moving[id] {
		t.Fatal("the torrent is still marked as moving")
	}
}

// TestRenameIsUndoneWhenItsPathsCannotBeSaved covers a rename whose files
// moved but whose new paths could not be written to the resume data: the
// files go back, or the next start would recreate empty files at the old
// paths and download again.
func TestRenameIsUndoneWhenItsPathsCannotBeSaved(t *testing.T) {
	base := t.TempDir()
	oldPath, newPath := filepath.Join(base, "Old Name", "file.bin"), filepath.Join(base, "New Name", "file.bin")
	writeTestFile(t, oldPath, "payload")

	previous := persistRename
	persistRename = func(string, string, []string, string) error { return os.ErrPermission }
	t.Cleanup(func() { persistRename = previous })

	d := &Daemon{pendingRename: &pendingRename{
		id:    "x",
		moves: []absRename{{old: oldPath, new: newPath}},
		paths: []string{"New Name/file.bin"},
		name:  "New Name",
	}}
	d.applyPendingRename()
	if body, err := os.ReadFile(oldPath); err != nil || string(body) != "payload" {
		t.Fatalf("file at the old path = %q, %v: it must be put back", body, err)
	}
	if _, err := os.Lstat(newPath); err == nil {
		t.Fatal("the file stayed at the new path after the save failed")
	}
}
