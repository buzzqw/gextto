package gextto

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHookEnvDoesNotLeakDaemonSecrets(t *testing.T) {
	t.Setenv("GEXTTO_API_TOKEN", "top-secret-token")
	t.Setenv("GEXTTO_NOTIFY_WEBHOOK_SECRET", "hush")

	env := runHookEnv(map[string]string{"title": "Show"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "top-secret-token") || strings.Contains(joined, "hush") {
		t.Fatalf("hook environment leaked a daemon secret: %s", joined)
	}
	if !strings.Contains(joined, "GEXTTO_TITLE=Show") {
		t.Fatalf("hook payload variable missing: %s", joined)
	}
}

// jsonUnmarshalString decodes a JSON document into target, failing the test on
// malformed input.
func jsonUnmarshalString(t *testing.T, raw string, target any) error {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", raw, err)
		return err
	}
	return nil
}

// TestHookExpandExpandsKnownPlaceholdersOnly ports the
// `expands_known_placeholders_and_keeps_unknown_ones` unit test.
func TestHookExpandExpandsKnownPlaceholdersOnly(t *testing.T) {
	vars := map[string]string{
		"title": "Movie.2020",
		"hash":  "abc123",
	}
	if got, want := Expand("{title} [{hash}]", vars), "Movie.2020 [abc123]"; got != want {
		t.Fatalf("Expand = %q, want %q", got, want)
	}
	// Unknown placeholders survive verbatim so a typo is visible.
	if got, want := Expand("{nope}", vars), "{nope}"; got != want {
		t.Fatalf("Expand unknown = %q, want %q", got, want)
	}
	// A lone brace is literal.
	if got, want := Expand("a { b", vars), "a { b"; got != want {
		t.Fatalf("Expand lone brace = %q, want %q", got, want)
	}
}

// TestHookSplitArgsHonorsQuotesAndEscapes ports the
// `splits_arguments_with_quotes_and_escapes` unit test.
func TestHookSplitArgsHonorsQuotesAndEscapes(t *testing.T) {
	got := SplitArgs(`--path /media/film.mkv --title "The Film"`)
	want := []string{"--path", "/media/film.mkv", "--title", "The Film"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitArgs = %#v, want %#v", got, want)
	}

	got = SplitArgs(`'single quoted' plain "with \"escape\""`)
	want = []string{"single quoted", "plain", `with "escape"`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitArgs quotes = %#v, want %#v", got, want)
	}

	if got := SplitArgs("  "); !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("SplitArgs blanks = %#v, want empty slice", got)
	}

	// A backslash escapes the next character outside quotes too.
	if got, want := SplitArgs(`one\ two`), []string{"one two"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitArgs escaped space = %#v, want %#v", got, want)
	}
}

// TestHookVariablesFlattenScalarsAndNestedMaps ports the
// `flatten_variables_includes_nested_and_scalar_fields` unit test.
func TestHookVariablesFlattenScalarsAndNestedMaps(t *testing.T) {
	payload := map[string]any{
		"title":  "Movie.2020",
		"hash":   "abc",
		"season": 2,
		"meta":   map[string]any{"group": "NTb"},
		"tags":   []any{"a", "b"},
	}
	vars := Variables("torrent_completed", payload)
	if got := vars["event"]; got != "torrent_completed" {
		t.Fatalf("event = %q, want torrent_completed", got)
	}
	if got := vars["title"]; got != "Movie.2020" {
		t.Fatalf("title = %q, want Movie.2020", got)
	}
	if got := vars["season"]; got != "2" {
		t.Fatalf("season = %q, want 2", got)
	}
	if got := vars["meta.group"]; got != "NTb" {
		t.Fatalf("meta.group = %q, want NTb", got)
	}
	if got := vars["tags"]; got != `["a","b"]` {
		t.Fatalf("tags = %q, want JSON array", got)
	}
	if vars["date"] == "" {
		t.Fatal("date variable should be populated")
	}
}

// TestHookMatchesEventRespectsSubscriptions ports the
// `event_filter_respects_subscriptions` unit test.
func TestHookMatchesEventRespectsSubscriptions(t *testing.T) {
	hook := EventHook{
		Name:        "test",
		Enabled:     true,
		Events:      []string{"torrent_completed"},
		Program:     "/bin/true",
		TimeoutSecs: 5,
	}
	if !HookMatchesEvent(hook, "torrent_completed") {
		t.Fatal("expected the hook to match its subscribed event")
	}
	if HookMatchesEvent(hook, "download_started") {
		t.Fatal("expected the hook not to match an unsubscribed event")
	}
	all := hook
	all.Events = []string{}
	if !HookMatchesEvent(all, "anything") {
		t.Fatal("an empty event list should match every event")
	}
}

// TestValidateHooksRejectsInvalidLists covers implements the
// `validation_rejects_missing_program` test plus the other limits.
func TestValidateHooksRejectsInvalidLists(t *testing.T) {
	if got := ValidateHooks(nil); got != "" {
		t.Fatalf("empty hook list should be valid, got %q", got)
	}
	withProgram := DefaultEventHook()
	withProgram.Program = "/bin/true"
	if got := ValidateHooks([]EventHook{withProgram}); got != "" {
		t.Fatalf("default hook with a program should be valid, got %q", got)
	}

	broken := []EventHook{{Name: "broken", Program: "  "}}
	if got := ValidateHooks(broken); got != "hook 'broken' has no program" {
		t.Fatalf("missing program = %q", got)
	}

	unnamed := []EventHook{{Program: " "}}
	if got := ValidateHooks(unnamed); got != "hook 'unnamed' has no program" {
		t.Fatalf("unnamed missing program = %q", got)
	}

	many := make([]EventHook, 101)
	for index := range many {
		many[index] = EventHook{Name: "n", Program: "/bin/true"}
	}
	if got := ValidateHooks(many); got != "too many event hooks (max 100)" {
		t.Fatalf("too many hooks = %q", got)
	}

	if got := ValidateHooks([]EventHook{{Program: strings.Repeat("p", 4097)}}); got != "an event hook is too large" {
		t.Fatalf("oversized program = %q", got)
	}
	if got := ValidateHooks([]EventHook{{Name: strings.Repeat("n", 201), Program: "/bin/true"}}); got != "an event hook is too large" {
		t.Fatalf("oversized name = %q", got)
	}
	if got := ValidateHooks([]EventHook{{Program: "/bin/true", TimeoutSecs: 86_401}}); got != "an event hook timeout exceeds 24h" {
		t.Fatalf("oversized timeout = %q", got)
	}
}

// TestHookDefaultsMatchJSON verifies the JSON defaults: enabled=true and
// timeout_secs=60 when the keys are absent.
func TestHookDefaultsMatchJSON(t *testing.T) {
	hook := DefaultEventHook()
	if !hook.Enabled {
		t.Fatal("DefaultEventHook().Enabled should be true")
	}
	if hook.TimeoutSecs != 60 {
		t.Fatalf("DefaultEventHook().TimeoutSecs = %d, want 60", hook.TimeoutSecs)
	}
	if hook.Events == nil || len(hook.Events) != 0 {
		t.Fatalf("DefaultEventHook().Events = %#v, want non-nil empty", hook.Events)
	}

	legacy := EventHook{}
	if err := jsonUnmarshalString(t, `{"name":"legacy","program":"/bin/true"}`, &legacy); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if !legacy.Enabled {
		t.Fatal("enabled should default to true")
	}
	if legacy.TimeoutSecs != 60 {
		t.Fatalf("timeout_secs should default to 60, got %d", legacy.TimeoutSecs)
	}
	if legacy.Events == nil {
		t.Fatal("events should default to a non-nil empty slice")
	}

	explicit := EventHook{}
	if err := jsonUnmarshalString(t, `{"enabled":false,"timeout_secs":5,"events":["a"]}`, &explicit); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if explicit.Enabled {
		t.Fatal("explicit enabled=false should be honored")
	}
	if explicit.TimeoutSecs != 5 {
		t.Fatalf("explicit timeout_secs = %d, want 5", explicit.TimeoutSecs)
	}
	if !reflect.DeepEqual(explicit.Events, []string{"a"}) {
		t.Fatalf("explicit events = %#v", explicit.Events)
	}
}

// TestLoadHooksAppliesDefaults verifies the settings JSON parsing path.
func TestLoadHooksAppliesDefaults(t *testing.T) {
	if got := LoadHooks(map[string]string{}); got != nil {
		t.Fatalf("missing key should yield nil, got %#v", got)
	}
	if got := LoadHooks(map[string]string{"event_hooks": "not json"}); got != nil {
		t.Fatalf("bad JSON should yield nil, got %#v", got)
	}
	hooks := LoadHooks(map[string]string{
		"event_hooks": `[{"name":"a","program":"/bin/true"}]`,
	})
	if len(hooks) != 1 {
		t.Fatalf("hooks = %#v, want 1 entry", hooks)
	}
	if !hooks[0].Enabled || hooks[0].TimeoutSecs != 60 {
		t.Fatalf("defaults not applied: %#v", hooks[0])
	}
}

func TestHookEnvKey(t *testing.T) {
	cases := map[string]string{
		"title":         "GEXTTO_TITLE",
		"meta.group":    "GEXTTO_META_GROUP",
		"quality_score": "GEXTTO_QUALITY_SCORE",
		"a-b.c":         "GEXTTO_A_B_C",
		"AlreadyUpper":  "GEXTTO_ALREADYUPPER",
		"9lives":        "GEXTTO_9LIVES",
	}
	for input, want := range cases {
		if got := hookEnvKey(input); got != want {
			t.Fatalf("hookEnvKey(%q) = %q, want %q", input, got, want)
		}
	}
}

// writeHookScript writes an executable shell script and returns its path.
func writeHookScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+content), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

// TestRunHookExecutesShellScriptWithEnvironment runs a temporary script through
// `/bin/sh` and checks the expanded argument, the GEXTTO_* environment and the
// captured stdout/exit code.
func TestRunHookExecutesShellScriptWithEnvironment(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat(shell); err != nil {
		t.Skipf("%s unavailable: %v", shell, err)
	}
	script := writeHookScript(t, `printf '%s|%s|%s\n' "$GEXTTO_TITLE" "$GEXTTO_HASH" "$1"`+"\n")

	hook := DefaultEventHook()
	hook.Name = "echo"
	hook.Program = shell
	// Quote the placeholder so a title containing spaces stays one argument.
	hook.Args = script + ` "{title}"`
	hook.TimeoutSecs = 5

	run, err := RunHook(hook, "download_started", map[string]any{"title": "Env Movie", "hash": "abc123"})
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if !run.Success {
		t.Fatalf("hook should succeed: %#v", run)
	}
	if run.Hook != "echo" || run.Program != shell {
		t.Fatalf("run metadata = %#v", run)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Fatalf("ExitCode = %v, want 0", run.ExitCode)
	}
	if want := "Env Movie|abc123|Env Movie"; run.Stdout != want {
		t.Fatalf("stdout = %q, want %q", run.Stdout, want)
	}
}

// TestRunHookReportsNonZeroExitCode checks the exit status is captured without
// turning the run into an infrastructure error.
func TestRunHookReportsNonZeroExitCode(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat(shell); err != nil {
		t.Skipf("%s unavailable: %v", shell, err)
	}
	script := writeHookScript(t, "exit 3\n")
	hook := DefaultEventHook()
	hook.Name = "fails"
	hook.Program = shell
	hook.Args = script
	hook.TimeoutSecs = 5

	run, err := RunHook(hook, "torrent_completed", map[string]any{})
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if run.Success {
		t.Fatal("hook with exit 3 should not be successful")
	}
	if run.ExitCode == nil || *run.ExitCode != 3 {
		t.Fatalf("ExitCode = %v, want 3", run.ExitCode)
	}
}

// TestRunHookFailsForMissingProgram checks the spawn error path.
func TestRunHookFailsForMissingProgram(t *testing.T) {
	hook := DefaultEventHook()
	hook.Name = "missing"
	hook.Program = filepath.Join(t.TempDir(), "definitely-not-here")

	if _, err := RunHook(hook, "message", map[string]any{}); err == nil {
		t.Fatal("expected an error for a missing program")
	}
}

// TestRunHookEnforcesTimeout checks a slow hook is killed and reported.
func TestRunHookEnforcesTimeout(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat(shell); err != nil {
		t.Skipf("%s unavailable: %v", shell, err)
	}
	// A shell-internal busy loop has no child process, so killing the shell
	// releases the pipe immediately instead of waiting for an orphaned sleep.
	script := writeHookScript(t, "while :; do :; done\n")
	hook := DefaultEventHook()
	hook.Name = "slow"
	hook.Program = shell
	hook.Args = script
	hook.TimeoutSecs = 1

	_, err := RunHook(hook, "message", map[string]any{})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if !strings.Contains(err.Error(), "timed out after 1s") {
		t.Fatalf("timeout error = %v", err)
	}
}

// TestRunHookZeroTimeoutFallsBackToDefault checks that a legacy `0` timeout is
// treated as the documented 60s default rather than a one-second process: the
// program completes instead of being killed.
func TestRunHookZeroTimeoutFallsBackToDefault(t *testing.T) {
	shell := "/bin/sh"
	if _, err := os.Stat(shell); err != nil {
		t.Skipf("%s unavailable: %v", shell, err)
	}
	script := writeHookScript(t, "printf ok\n")
	hook := DefaultEventHook()
	hook.Name = "legacy"
	hook.Program = shell
	hook.Args = script
	hook.TimeoutSecs = 0

	run, err := RunHook(hook, "message", map[string]any{})
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if run.Stdout != "ok" {
		t.Fatalf("stdout = %q, want ok", run.Stdout)
	}
}

// TestDefaultEventHookProgramValidation exercises the exported default path
// helper once more with a real executable.
func TestDefaultEventHookProgramValidation(t *testing.T) {
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("true binary unavailable")
	}
	hook := DefaultEventHook()
	hook.Name = "true"
	hook.Program = truePath
	if got := ValidateHooks([]EventHook{hook}); got != "" {
		t.Fatalf("valid hook rejected: %q", got)
	}
	run, err := RunHook(hook, "message", map[string]any{"text": "one"})
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if !run.Success {
		t.Fatalf("true should succeed: %#v", run)
	}
}
