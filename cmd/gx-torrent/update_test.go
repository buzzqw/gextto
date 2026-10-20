package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/build"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/settings"
	"github.com/buzzqw/gextto/internal/updatecheck"
)

const (
	updOld   = "1111111111111111111111111111111111111111"
	updGx    = "2222222222222222222222222222222222222222"
	updOther = "3333333333333333333333333333333333333333"
	updHead  = "4444444444444444444444444444444444444444"
)

// The update tries the new binary with --version: both the old 1.1.<build>
// form and the rolled-over 1.2.0 are valid answers.
func TestGexVersionPatternAcceptsBothSchemes(t *testing.T) {
	for _, version := range []string{"1.1.242", "1.2.0", "1.2.999", "1.3.0", "gx-torrent 1.2.0"} {
		if !gexVersionPattern.MatchString(version) {
			t.Errorf("%q must be a valid version", version)
		}
	}
	for _, output := range []string{"", "unknown", "1.2", "version unknown"} {
		if gexVersionPattern.MatchString(output) {
			t.Errorf("%q must not be accepted as a version", output)
		}
	}
}

// updateRelease serves a fake release: release.json (head, a Gextto-only
// commit, a gx-torrent commit, then the running one), the standalone archive
// with a gx-torrent that prints a version, and its checksum.
func updateRelease(t *testing.T, binary string, badSum bool) *httptest.Server {
	t.Helper()
	manifest := updatecheck.Manifest{
		Channel: "continuous", Commit: updHead, GxCommit: updGx,
		Commits: []updatecheck.Commit{
			{Sha: updHead, Date: "2026-10-14T10:00:00Z", Subject: "build: 2"},
			{Sha: updOther, Date: "2026-10-14T09:00:00Z", Subject: "fix(ui): a Gextto change"},
			{Sha: updGx, Date: "2026-10-14T08:00:00Z", Subject: "feat(dlna): a daemon change", Gx: true},
			{Sha: updOld, Date: "2026-10-13T08:00:00Z", Subject: "build: 1"},
		},
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{{"./README.md", "readme"}, {"./gx-torrent", binary}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(f.body))
	}
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(archive.Bytes())
	sumHex := hex.EncodeToString(sum[:])
	if badSum {
		sumHex = strings.Repeat("0", 64)
	}
	asset := updateAsset()
	mux := http.NewServeMux()
	mux.HandleFunc("/continuous/release.json", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(manifest) })
	mux.HandleFunc("/continuous/"+asset, func(w http.ResponseWriter, r *http.Request) { w.Write(archive.Bytes()) })
	mux.HandleFunc("/continuous/"+asset+".sha256", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sumHex + "  " + asset + "\n")) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// updateDaemon is a standalone daemon whose running binary is a file in a
// temporary folder, built (as far as the updater knows) from updOld on the
// continuous channel.
func updateDaemon(t *testing.T, srv *httptest.Server) (*Daemon, string) {
	t.Helper()
	if runtime.GOOS != "linux" || updateAsset() == "" {
		t.Skip("the installation is Linux only")
	}
	oldCommit, oldChannel := constants.Commit, constants.GxReleaseChannel
	constants.Commit, constants.GxReleaseChannel = updOld, "continuous"
	t.Cleanup(func() { constants.Commit, constants.GxReleaseChannel = oldCommit, oldChannel })
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "gx-torrent")
	os.MkdirAll(filepath.Dir(exe), 0o755)
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := settings.Load(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{opts: Options{Mode: ModeStandalone, DataDir: dir, Settings: store}}
	d.updater.exe = exe
	d.updater.client = srv.Client()
	d.updater.assetURL = func(channel, asset string) string { return srv.URL + "/" + channel + "/" + asset }
	return d, exe
}

const updNewBinary = "#!/bin/sh\necho 1.1.999\n"

func TestUpdateChangesOnlyCountDaemonCommits(t *testing.T) {
	m := &updatecheck.Manifest{Commit: updHead, Commits: []updatecheck.Commit{
		{Sha: updHead}, {Sha: updOther}, {Sha: updGx, Gx: true}, {Sha: updOld},
	}}
	if ok, ch, complete := updatecheck.Changes(updOld, m, true); !ok || !complete || len(ch) != 1 || ch[0].Sha != updGx {
		t.Fatalf("gx changes = %v %v %v", ok, ch, complete)
	}
	// Running the gx commit: only Gextto changed after it, no update.
	if ok, _, _ := updatecheck.Changes(updGx[:7], m, true); ok {
		t.Fatal("a release without daemon changes must not be an update")
	}
	if ok, ch, _ := updatecheck.Changes(updGx, m, false); !ok || len(ch) != 2 {
		t.Fatalf("all changes = %v %v", ok, ch)
	}
	if ok, _, complete := updatecheck.Changes("5555555", m, true); !ok || complete {
		t.Fatal("a commit older than the list is an update with a partial list")
	}
	if ok, _, _ := updatecheck.Changes("", m, true); ok {
		t.Fatal("a local build is never offered an update")
	}
}

func TestUpdateCheckInstallAndRestart(t *testing.T) {
	srv := updateRelease(t, updNewBinary, false)
	d, exe := updateDaemon(t, srv)

	if err := d.checkUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := d.updateStatus()
	if !st.Available || len(st.Changes) != 1 || st.Changes[0].Subject != "feat(dlna): a daemon change" || st.Latest != updGx {
		t.Fatalf("status = %+v", st)
	}
	section := d.updateSectionHTML()
	for _, want := range []string{`data-update="apply"`, ">Update</button>", "feat(dlna): a daemon change", "Check again"} {
		if !strings.Contains(section, want) {
			t.Fatalf("section misses %q:\n%s", want, section)
		}
	}
	if strings.Contains(section, "a Gextto change") {
		t.Fatal("a Gextto-only commit is listed among the daemon changes")
	}

	if err := d.startUpdate(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.updater.restartC():
	case <-time.After(20 * time.Second):
		t.Fatalf("no restart; error %q", d.updateStatus().ApplyErr)
	}
	if b, _ := os.ReadFile(exe); string(b) != updNewBinary {
		t.Fatalf("binary not replaced: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".prev"); string(b) != "old binary" {
		t.Fatalf("previous binary not kept: %q", b)
	}
	if _, err := os.Stat(exe + ".new"); !os.IsNotExist(err) {
		t.Fatal("the .new file was left behind")
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 2 {
		t.Fatalf("left files in the program folder: %v", entries)
	}
	n, ok := readUpdateNotes(d.opts.DataDir)
	if !ok || !n.Pending || n.FromCommit != updOld || n.ToCommit != updGx || len(n.Changes) != 1 {
		t.Fatalf("notes = %+v", n)
	}
	// The old process does not show the popup; the new one does.
	if d.updateNotesDialogHTML() != "" {
		t.Fatal("the popup must not show before the restart")
	}
	constants.Commit = updGx
	dlg := d.updateNotesDialogHTML()
	if !strings.Contains(dlg, "gx-torrent updated") || !strings.Contains(dlg, "feat(dlna): a daemon change") || !strings.Contains(dlg, "<code>1111111</code>") {
		t.Fatalf("popup = %s", dlg)
	}
	// Both pages carry it: the main page (a Go template) and the settings.
	main := httptest.NewRecorder()
	d.renderUI(main, httptest.NewRequest("GET", "/", nil), "", uiPageData{Standalone: true})
	if !strings.Contains(main.Body.String(), `<dialog id="dlg-updated"`) || strings.Contains(main.Body.String(), "data-update-notes-slot") {
		t.Fatal("the main page does not show the popup")
	}
	settingsPage := httptest.NewRecorder()
	d.renderSettings(settingsPage, httptest.NewRequest("GET", uiSettingsPath, nil), "", false)
	if !strings.Contains(settingsPage.Body.String(), `<dialog id="dlg-updated"`) || !strings.Contains(settingsPage.Body.String(), `id="maintenance"`) {
		t.Fatal("the settings page does not show the popup or the Maintenance section")
	}
	// Closing it during the probation keeps the file (it counts the starts).
	req := httptest.NewRequest("POST", "/ui/update-notes", nil)
	rec := httptest.NewRecorder()
	d.handleUIUpdateNotes(rec, req)
	if n, ok := readUpdateNotes(d.opts.DataDir); rec.Code != http.StatusNoContent || !ok || !n.Seen || d.updateNotesDialogHTML() != "" {
		t.Fatalf("dismiss: %d %+v", rec.Code, n)
	}
}

func TestUpdateRefusesADamagedArchive(t *testing.T) {
	srv := updateRelease(t, updNewBinary, true)
	d, exe := updateDaemon(t, srv)
	if err := d.checkUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := d.installUpdate(context.Background(), d.updateStatus())
	if err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "old binary" {
		t.Fatal("a damaged download replaced the binary")
	}
	if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
		t.Fatalf("left files: %v", entries)
	}
}

func TestUpdateRefusesABinaryThatDoesNotRun(t *testing.T) {
	srv := updateRelease(t, "#!/bin/sh\nexit 3\n", false)
	d, exe := updateDaemon(t, srv)
	if err := d.checkUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := d.installUpdate(context.Background(), d.updateStatus()); err == nil {
		t.Fatal("a binary that does not run was installed")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old binary" {
		t.Fatal("the binary was replaced")
	}
}

func TestUpdateBootGuardRollsBack(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "gx-torrent")
	os.WriteFile(exe, []byte("new"), 0o755)
	os.WriteFile(exe+".prev", []byte("old"), 0o755)
	writeUpdateNotes(dir, &updateNotes{FromVersion: "1.1.1", Pending: true})
	for i := 1; i <= updateBootAttempts; i++ {
		if got := updateBootGuard(dir, exe); got != "" {
			t.Fatalf("start %d rolled back", i)
		}
	}
	if got := updateBootGuard(dir, exe); got != exe {
		t.Fatal("no rollback after the failed starts")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Fatalf("binary after rollback: %q", b)
	}
	n, _ := readUpdateNotes(dir)
	if n.Pending || !n.RolledBack || n.Error == "" {
		t.Fatalf("notes = %+v", n)
	}
	d := &Daemon{opts: Options{Mode: ModeStandalone, DataDir: dir}}
	if dlg := d.updateNotesDialogHTML(); !strings.Contains(dlg, "Update not installed") {
		t.Fatalf("rollback popup = %s", dlg)
	}
	// A normal start (nothing pending) is left alone.
	if got := updateBootGuard(dir, exe); got != "" {
		t.Fatal("a confirmed update must not be guarded again")
	}
}

func TestUpdateOffWhenManagedOrLocal(t *testing.T) {
	old := constants.GxReleaseChannel
	t.Cleanup(func() { constants.GxReleaseChannel = old })
	d := &Daemon{opts: Options{Mode: ModeManaged}}
	if !strings.Contains(d.updateUnavailable(), "Gextto") {
		t.Fatal("a managed daemon must defer to Gextto")
	}
	constants.GxReleaseChannel = ""
	d.opts.Mode = ModeStandalone
	if !strings.Contains(d.updateUnavailable(), "Local build") {
		t.Fatal("a local build must not update itself")
	}
	if err := d.checkUpdate(context.Background()); err == nil {
		t.Fatal("a local build checked for updates")
	}
	if html := d.updateSectionHTML(); strings.Contains(html, "data-update") {
		t.Fatalf("a local build shows the update button: %s", html)
	}
}

// TestReleaseManifestCoversDaemonPackages keeps scripts/release-manifest.sh's
// GX_PATHS in sync with what gx-torrent imports from this repository: a
// change in a package missing from the list would not be offered as an
// update to the standalone daemon.
func TestReleaseManifestCoversDaemonPackages(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(root, "scripts", "release-manifest.sh"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(script), "GX_PATHS=(")
	end := strings.Index(string(script)[start:], ")")
	paths := strings.Fields(string(script)[start+len("GX_PATHS=(") : start+end])
	const module = "github.com/buzzqw/gextto"
	seen := map[string]bool{}
	var walk func(dir string)
	walk = func(dir string) {
		if seen[dir] {
			return
		}
		seen[dir] = true
		pkg, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		rel, _ := filepath.Rel(root, dir)
		rel = filepath.ToSlash(rel)
		covered := false
		for _, p := range paths {
			if rel == p || strings.HasPrefix(rel, p+"/") {
				covered = true
			}
		}
		if !covered {
			t.Errorf("gx-torrent imports %s, missing from GX_PATHS in scripts/release-manifest.sh", rel)
		}
		for _, imp := range pkg.Imports {
			if imp == module || strings.HasPrefix(imp, module+"/") {
				walk(filepath.Join(root, strings.TrimPrefix(strings.TrimPrefix(imp, module), "/")))
			}
		}
	}
	walk(filepath.Join(root, "cmd", "gx-torrent"))
	if len(seen) < 10 {
		t.Fatalf("only %d packages walked", len(seen))
	}
}
