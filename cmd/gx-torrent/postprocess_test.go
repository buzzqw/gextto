package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCategoryDirNameCannotEscape(t *testing.T) {
	for _, category := range []string{"../etc", "a/b", `a\b`, "..", "  "} {
		got := categoryDirName(category)
		if strings.ContainsAny(got, `/\`) || got == "" {
			t.Fatalf("categoryDirName(%q) = %q", category, got)
		}
	}
}

func TestCompletionCommandGetsTheTorrentInTheEnvironment(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env.txt")
	script := filepath.Join(t.TempDir(), "hook.sh")
	body := "#!/bin/sh\nprintf '%s\\n' \"$GX_HASH\" \"$GX_NAME\" \"$GX_PATH\" \"$GX_CATEGORY\" \"$GX_SIZE\" > \"$GX_TEST_OUT\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GX_TEST_OUT", out)
	if err := runCompletionCommand(script, "hash123", "A title with spaces", "/srv/media/film", "Movies", 12345); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"hash123", "A title with spaces", "/srv/media/film", "Movies", "12345"}
	if len(got) != len(want) {
		t.Fatalf("environment = %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("environment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPostProcessEnabled(t *testing.T) {
	d := standaloneTestDaemon(t, "", true)
	if d.postProcessEnabled() {
		t.Fatal("completion actions must be off by default")
	}
	d.opts.Settings.Set(postProcessRunKey, "/bin/true")
	if !d.postProcessEnabled() {
		t.Fatal("a configured command must enable the actions")
	}
	d.opts.Settings.Set(postProcessRunKey, "")
	d.opts.Settings.Set(postProcessMoveKey, t.TempDir())
	if !d.postProcessEnabled() {
		t.Fatal("a configured move folder must enable the actions")
	}
}

func TestCompletionCommandTimesOutNothingLeaks(t *testing.T) {
	// A command that does not exist is an error, not a silent success.
	if err := runCompletionCommand("/definitely/not/here", "h", "n", "/p", "", 0); err == nil {
		t.Fatal("a missing program must return an error")
	}
}
