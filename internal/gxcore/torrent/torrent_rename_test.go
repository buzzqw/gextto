package torrent

import (
	"testing"

	"github.com/buzzqw/gextto/internal/gxcore/internal/metainfo"
)

func renameTestTorrent() *torrent {
	tor := &torrent{id: "x", session: &Session{}}
	tor.setInfo(&metainfo.Info{
		Name: "Show",
		Files: []metainfo.File{
			{Path: "Show/a.mkv", Length: 1},
			{Path: "Show/.pad/0", Length: 8, Padding: true},
			{Path: "Show/sub/c.mkv", Length: 1},
		},
	})
	return tor
}

func TestTorrentPlanRenameFolder(t *testing.T) {
	tor := renameTestTorrent()
	plan, err := tor.planRename("Show/sub", "Show/season1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moves) != 1 || plan.Moves[0].Old != "Show/sub/c.mkv" || plan.Moves[0].New != "Show/season1/c.mkv" {
		t.Fatalf("moves = %+v", plan.Moves)
	}
	if plan.Paths[2] != "Show/season1/c.mkv" || plan.Paths[0] != "Show/a.mkv" {
		t.Fatalf("paths = %+v", plan.Paths)
	}
}

func TestTorrentPlanRenameTorrent(t *testing.T) {
	tor := renameTestTorrent()
	plan, err := tor.planRenameTorrent("Film")
	if err != nil {
		t.Fatal(err)
	}
	// The padding file is renamed in the list but has no on-disk move.
	if len(plan.Moves) != 2 {
		t.Fatalf("moves = %+v", plan.Moves)
	}
	if plan.Name != "Film" {
		t.Fatalf("name = %q", plan.Name)
	}
	if plan.Paths[0] != "Film/a.mkv" || plan.Paths[1] != "Film/.pad/0" || plan.Paths[2] != "Film/sub/c.mkv" {
		t.Fatalf("paths = %+v", plan.Paths)
	}
}

func TestTorrentPlanRenameValidation(t *testing.T) {
	cases := [][2]string{
		{"../evil", "ok"},
		{"Show", "/abs"},
		{"Show", ".."},
		{"Show", ""},
		{"nope", "x"},
	}
	for _, c := range cases {
		tor := renameTestTorrent()
		if _, err := tor.planRename(c[0], c[1]); err == nil {
			t.Errorf("planRename(%q, %q) should fail", c[0], c[1])
		}
	}
}

func TestInfoSetFilePaths(t *testing.T) {
	info := &metainfo.Info{Files: []metainfo.File{{Path: "a"}, {Path: "b"}}}
	if info.SetFilePaths([]string{"only-one"}) {
		t.Fatal("a wrong-length list must be refused")
	}
	if !info.SetFilePaths([]string{"X/a", "X/b"}) {
		t.Fatal("a matching list must be accepted")
	}
	if info.Files[0].Path != "X/a" || info.Files[1].Path != "X/b" {
		t.Fatalf("files = %+v", info.Files)
	}
}

// TestTorrentPlanRenameCollisions refuses renames that would make two files
// share a path or turn a file into a folder.
func TestTorrentPlanRenameCollisions(t *testing.T) {
	cases := [][2]string{
		{"Show/a.mkv", "Show/sub/c.mkv"}, // onto another file
		{"Show/sub", "Show/a.mkv"},       // folder onto a file
		{"Show/sub/c.mkv", "Show/a.mkv/c.mkv"},
	}
	for _, c := range cases {
		tor := renameTestTorrent()
		if _, err := tor.planRename(c[0], c[1]); err == nil {
			t.Errorf("planRename(%q, %q) should be refused", c[0], c[1])
		}
	}
	// Moving a file into an existing folder is fine.
	if _, err := renameTestTorrent().planRename("Show/a.mkv", "Show/sub/a.mkv"); err != nil {
		t.Fatalf("move into a folder: %v", err)
	}
}

// TestTorrentDisplayNameOverride: a rename overrides the shown name, the
// original stays the fallback (the stats side is covered by the daemon's
// TestDaemonRenameTorrent).
func TestTorrentDisplayNameOverride(t *testing.T) {
	tor := renameTestTorrent()
	tor.name = "Show"
	if tor.Name() != "Show" {
		t.Fatalf("name before rename = %q", tor.Name())
	}
	tor.displayName = "Film"
	if tor.Name() != "Film" {
		t.Fatalf("name after rename = %q", tor.Name())
	}
}
