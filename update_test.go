package gextto

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func clearUpdateEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"GEXTTO_VERSION", "GEXTTO_CHANNEL"} {
		if value, ok := os.LookupEnv(key); ok {
			old := value
			_ = os.Unsetenv(key)
			t.Cleanup(func() { _ = os.Setenv(key, old) })
		}
	}
}

func TestArchiveURLContinuousUsesTheTaggedDownloadPath(t *testing.T) {
	got := ArchiveURL("me/gextto", "continuous", "x86_64")
	want := "https://github.com/me/gextto/releases/download/continuous/gextto-linux-x86_64.tar.gz"
	if got != want {
		t.Fatalf("ArchiveURL = %q, want %q", got, want)
	}
}

func TestArchiveURLStableUsesTheLatestDownloadPath(t *testing.T) {
	got := ArchiveURL("me/gextto", "stable", "aarch64")
	want := "https://github.com/me/gextto/releases/latest/download/gextto-linux-aarch64.tar.gz"
	if got != want {
		t.Fatalf("ArchiveURL = %q, want %q", got, want)
	}
}

func TestReleaseNamePrefersExplicitValues(t *testing.T) {
	clearUpdateEnv(t)

	options := UpdateOptions{}
	if got := ReleaseName(options); got != "continuous" {
		t.Fatalf("ReleaseName = %q, want continuous", got)
	}
	options.Channel = testStrPtr("stable")
	if got := ReleaseName(options); got != "stable" {
		t.Fatalf("ReleaseName = %q, want stable", got)
	}
	options.Release = testStrPtr("v1.2.3")
	if got := ReleaseName(options); got != "v1.2.3" {
		t.Fatalf("ReleaseName = %q, want v1.2.3", got)
	}
}

func TestFindReleaseRootHandlesNestedArchives(t *testing.T) {
	base := filepath.Join(os.TempDir(), fmt.Sprintf("gextto-test-%d", os.Getpid()))
	nested := filepath.Join(base, "gextto-linux-x86_64")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "gexttod"), []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	got, ok := updateFindReleaseRoot(base)
	if !ok {
		t.Fatalf("updateFindReleaseRoot did not find the nested release root")
	}
	if got != nested {
		t.Fatalf("updateFindReleaseRoot = %q, want %q", got, nested)
	}
}

func TestUpdateValidateRelease(t *testing.T) {
	// A complete payload carries both executables, the launcher and the marker.
	complete := t.TempDir()
	for _, name := range []string{"gexttod", "gx-torrent", "run.sh", "VERSION"} {
		if err := os.WriteFile(filepath.Join(complete, name), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := updateValidateRelease(complete); err != nil {
		t.Fatalf("complete payload rejected: %v", err)
	}

	// Every essential component is required: installing only the daemon would
	// leave an update that cannot launch its default engine or identify itself.
	for _, missing := range []string{"gexttod", "gx-torrent", "run.sh", "VERSION"} {
		payload := t.TempDir()
		for _, name := range []string{"gexttod", "gx-torrent", "run.sh", "VERSION"} {
			if name == missing {
				continue
			}
			if err := os.WriteFile(filepath.Join(payload, name), []byte(name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := updateValidateRelease(payload); err == nil {
			t.Fatalf("payload without %s was accepted", missing)
		}
	}
}

func TestSha256FileMatchesTheKnownDigest(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "payload")
	if err := os.WriteFile(file, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := updateSha256File(file)
	if err != nil {
		t.Fatal(err)
	}
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Fatalf("updateSha256File = %q, want %q", got, want)
	}
}

func TestWorkDirCleanupRemovesTheDirectory(t *testing.T) {
	work, err := NewWorkDir()
	if err != nil {
		t.Fatal(err)
	}
	if !updateIsDir(work.Path()) {
		t.Fatalf("work dir %q was not created", work.Path())
	}
	work.Cleanup()
	if updatePathExists(work.Path()) {
		t.Fatalf("work dir %q was not removed", work.Path())
	}
}

// The health check after an update connects through loopback to the port the
// unit listens on, whatever wildcard the listen address uses.
func TestUpdateLoopbackAddress(t *testing.T) {
	for listen, want := range map[string]string{
		"0.0.0.0:5000":   "127.0.0.1:5000",
		"[::]:5000":      "127.0.0.1:5000",
		":5000":          "127.0.0.1:5000",
		"127.0.0.1:8080": "127.0.0.1:8080",
		"192.0.2.7:5000": "192.0.2.7:5000",
	} {
		if got := updateLoopbackAddress(listen); got != want {
			t.Errorf("updateLoopbackAddress(%q) = %q, want %q", listen, got, want)
		}
	}
}
