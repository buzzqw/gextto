package gextto

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeUpdatePayload(t *testing.T, root, marker string) map[string]string {
	t.Helper()
	files := map[string]string{
		"gexttod":                  marker + " gexttod",
		"gx-torrent":               marker + " gx-torrent",
		"run.sh":                   marker + " launcher",
		"VERSION":                  marker + "\n",
		"README.md":                marker + " readme",
		"lib/engine.so":            marker + " library",
		"ui/legacy.html":           marker + " legacy ui",
		"gx-ed2k":                  marker + " retired engine",
		"gexttod.prev":             marker + " older daemon",
		"gx-torrent.prev":          marker + " older engine",
		"VERSION.prev":             marker + "-older\n",
		"custom-operator-file.txt": "keep me",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if name == "gexttod" || name == "gx-torrent" || name == "run.sh" || strings.HasSuffix(name, ".prev") && name != "VERSION.prev" {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func assertUpdateFile(t *testing.T, root, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}

func assertNoUpdateTransaction(t *testing.T, installDir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(installDir, ".gextto-update-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("update transaction leftovers: %v", matches)
	}
}

func TestUpdateInstallStagesEverythingBeforeFirstCommitAndRollsBack(t *testing.T) {
	root := t.TempDir()
	installDir := t.TempDir()
	writeUpdatePayload(t, root, "new")
	old := writeUpdatePayload(t, installDir, "old")
	// README.md and lib/engine.so are introduced by the new release: the old
	// install must not have them, so a rollback proves new files are removed
	// instead of leaving the new payload behind.
	for _, introduced := range []string{"README.md", "lib/engine.so"} {
		delete(old, introduced)
		if err := os.RemoveAll(filepath.Join(installDir, filepath.FromSlash(introduced))); err != nil {
			t.Fatal(err)
		}
	}

	transaction, err := updateInstallReleaseWithCheckpoint(root, installDir, func(committed int) error {
		if committed != 1 {
			return nil
		}
		stages, err := filepath.Glob(filepath.Join(installDir, ".gextto-update-stage-*"))
		if err != nil || len(stages) != 1 {
			t.Fatalf("staging directory before first commit: %v, %v", stages, err)
		}
		// The daemon has already been replaced, but every other payload entry
		// must still be staged and ready before the transaction started.
		assertUpdateFile(t, stages[0], "gx-torrent", "new gx-torrent")
		assertUpdateFile(t, stages[0], "lib/engine.so", "new library")
		assertUpdateFile(t, stages[0], "VERSION", "new\n")
		return nil
	})
	if err != nil {
		t.Fatalf("install transaction: %v", err)
	}
	for name, want := range map[string]string{
		"gexttod": "new gexttod", "gx-torrent": "new gx-torrent", "run.sh": "new launcher",
		"VERSION": "new\n", "README.md": "new readme", "lib/engine.so": "new library",
		"gexttod.prev": "old gexttod", "gx-torrent.prev": "old gx-torrent", "VERSION.prev": "old\n",
		"custom-operator-file.txt": "keep me",
	} {
		assertUpdateFile(t, installDir, name, want)
	}
	if _, err := os.Stat(filepath.Join(installDir, "ui")); err != nil {
		t.Fatalf("replacement ui directory was not installed: %v", err)
	}
	assertUpdateFile(t, installDir, "ui/legacy.html", "new legacy ui")
	if _, err := os.Stat(filepath.Join(installDir, "gx-ed2k")); !os.IsNotExist(err) {
		t.Fatalf("retired engine survived the update: %v", err)
	}

	if err := transaction.rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	for name, want := range old {
		assertUpdateFile(t, installDir, name, want)
	}
	for _, name := range []string{"README.md", "lib/engine.so"} {
		if _, err := os.Stat(filepath.Join(installDir, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("new %s survived rollback: %v", name, err)
		}
	}
	assertNoUpdateTransaction(t, installDir)
}

func TestUpdateInstallFailureAfterCommitRestoresWholePayload(t *testing.T) {
	root := t.TempDir()
	installDir := t.TempDir()
	writeUpdatePayload(t, root, "new")
	old := writeUpdatePayload(t, installDir, "old")

	_, err := updateInstallReleaseWithCheckpoint(root, installDir, func(committed int) error {
		if committed == 2 {
			return errors.New("injected failure after two commits")
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("checkpoint failure = %v", err)
	}
	for name, want := range old {
		assertUpdateFile(t, installDir, name, want)
	}
	assertNoUpdateTransaction(t, installDir)
}

// updateTestHealthLimit keeps the rollback/health tests bounded to
// milliseconds. The production limit is three minutes and must never be
// reached by a test; the hooks below also assert the value they receive.
const updateTestHealthLimit = 5 * time.Millisecond

func TestUpdateRestartFailureRollsBackAndIsPropagated(t *testing.T) {
	root := t.TempDir()
	installDir := t.TempDir()
	writeUpdatePayload(t, root, "new")
	old := writeUpdatePayload(t, installDir, "old")
	transaction, err := updateInstallReleaseTransaction(root, installDir)
	if err != nil {
		t.Fatal(err)
	}

	restarts := 0
	err = updateRestartServiceWithHooks(UpdateOptions{Restart: true, InstallDir: &installDir}, transaction, updateTestHealthLimit,
		func() bool { return true },
		func(args ...string) error {
			if len(args) != 2 || args[0] != "restart" || args[1] != ServiceName {
				t.Fatalf("unexpected systemctl arguments: %v", args)
			}
			restarts++
			if restarts == 1 {
				return errors.New("injected restart failure")
			}
			return nil
		},
		func(limit time.Duration) updateHealth {
			if limit != updateTestHealthLimit {
				t.Fatalf("health limit = %s, want %s", limit, updateTestHealthLimit)
			}
			return updateHealthy
		},
	)
	if err == nil || !strings.Contains(err.Error(), "injected restart failure") || !strings.Contains(err.Error(), "previous version was restored") {
		t.Fatalf("restart failure was not propagated with rollback: %v", err)
	}
	if restarts != 2 {
		t.Fatalf("restart attempts = %d, want 2", restarts)
	}
	for name, want := range old {
		assertUpdateFile(t, installDir, name, want)
	}
	assertNoUpdateTransaction(t, installDir)
}

func TestUpdateUnhealthyServiceRollsBackAndIsPropagated(t *testing.T) {
	root := t.TempDir()
	installDir := t.TempDir()
	writeUpdatePayload(t, root, "new")
	old := writeUpdatePayload(t, installDir, "old")
	transaction, err := updateInstallReleaseTransaction(root, installDir)
	if err != nil {
		t.Fatal(err)
	}

	healthChecks := 0
	err = updateRestartServiceWithHooks(UpdateOptions{Restart: true, InstallDir: &installDir}, transaction, updateTestHealthLimit,
		func() bool { return true },
		func(...string) error { return nil },
		func(limit time.Duration) updateHealth {
			if limit != updateTestHealthLimit {
				t.Fatalf("health limit = %s, want %s", limit, updateTestHealthLimit)
			}
			healthChecks++
			if healthChecks == 1 {
				return updateSlow
			}
			return updateHealthy
		},
	)
	if err == nil || !strings.Contains(err.Error(), "expected status response") || !strings.Contains(err.Error(), "previous version was restored") {
		t.Fatalf("unhealthy update was not propagated with rollback: %v", err)
	}
	if healthChecks != 2 {
		t.Fatalf("health checks = %d, want 2", healthChecks)
	}
	for name, want := range old {
		assertUpdateFile(t, installDir, name, want)
	}
	assertNoUpdateTransaction(t, installDir)
}

func TestUpdateHealthAcceptsOnlyExpectedStatusResponse(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   updateHealth
	}{
		{"expected", http.StatusOK, `{"name":"gextto","version":"1.2.3"}`, updateHealthy},
		{"unauthorized", http.StatusUnauthorized, `{"name":"gextto","version":"1.2.3"}`, updateHealthy},
		{"wrong service", http.StatusOK, `{"name":"something-else","version":"1.2.3"}`, updateSlow},
		{"missing version", http.StatusOK, `{"name":"gextto"}`, updateSlow},
		{"html", http.StatusOK, `<html>login</html>`, updateSlow},
		{"server error", http.StatusInternalServerError, `{"name":"gextto","version":"1.2.3"}`, updateSlow},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()
			if got := updateWaitHealthyWith(10*time.Millisecond, server.URL, func() bool { return true }); got != testCase.want {
				t.Fatalf("health = %v, want %v", got, testCase.want)
			}
		})
	}
	if got := updateWaitHealthyWith(time.Millisecond, "http://127.0.0.1:1", func() bool { return false }); got != updateCrashed {
		t.Fatalf("inactive service health = %v, want crashed", got)
	}
}

func TestUpdateWorkDirectoryIsPrivateRegardlessOfUmask(t *testing.T) {
	previous := syscall.Umask(0)
	defer syscall.Umask(previous)
	work, err := NewWorkDir()
	if err != nil {
		t.Fatal(err)
	}
	defer work.Cleanup()
	info, err := os.Stat(work.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("work directory mode = %04o, want 0700", got)
	}
}
