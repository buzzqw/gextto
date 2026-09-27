package gextto

// User-configurable event hooks.
//
// The same idea appears in every reference project: qBittorrent's "Run
// external program on torrent added/finished", Sonarr's "Custom Script"
// notification, BiglyBT's tag exec-on-assign actions and autobrr's exec
// actions. Gextto already emits a rich event stream through
// [`Notifier`]; this module lets a user attach an external program to those
// events with a stable variable contract:
//
// - `{event}`, `{title}`, `{hash}`, `{path}`, `{series}`, `{season}`,
// `{episode}`, `{kind}`, `{source}`, `{quality_score}`, ... — every scalar
// field of the notification payload plus its flattened `a.b` forms.
// - The same values are exported as environment variables prefixed with
// `GEXTTO_` (dots become underscores), e.g. `GEXTTO_EVENT`,
// `GEXTTO_TITLE`, `GEXTTO_HASH`.
//
// Programs are executed directly (never through a shell), with a timeout and
// captured output so a misbehaving hook cannot wedge the daemon.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/buzzqw/gextto/internal/logging"
)

func defaultEventHookEnabled() bool { return true }

func defaultEventHookTimeoutSecs() uint64 { return 60 }

// EventHook is one external program bound to a set of events.
type EventHook struct {
	Name string `json:"name"`
	// Enabled defaults to true when the key is absent.
	Enabled bool `json:"enabled"`
	// Events are the event names to react to; empty means every event.
	Events []string `json:"events"`
	// Program is the executable path (no shell interpretation).
	Program string `json:"program"`
	// Args is the argument template; supimplements `{placeholder}` expansion and
	// quotes.
	Args string `json:"args"`
	// TimeoutSecs kills the process after this many seconds (default 60, max
	// 86400).
	TimeoutSecs uint64 `json:"timeout_secs"`
}

// UnmarshalJSON applies the JSON defaults: `enabled` defaults to true and
// `timeout_secs` to 60 when the keys are absent.
func (h *EventHook) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name        string   `json:"name"`
		Enabled     *bool    `json:"enabled"`
		Events      []string `json:"events"`
		Program     string   `json:"program"`
		Args        string   `json:"args"`
		TimeoutSecs *uint64  `json:"timeout_secs"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	h.Name = raw.Name
	h.Enabled = true
	if raw.Enabled != nil {
		h.Enabled = *raw.Enabled
	}
	h.Events = raw.Events
	if h.Events == nil {
		h.Events = []string{}
	}
	h.Program = raw.Program
	h.Args = raw.Args
	h.TimeoutSecs = defaultEventHookTimeoutSecs()
	if raw.TimeoutSecs != nil {
		h.TimeoutSecs = *raw.TimeoutSecs
	}
	return nil
}

// DefaultEventHook returns the `EventHook::default()` value.
func DefaultEventHook() EventHook {
	return EventHook{
		Name:        "",
		Enabled:     defaultEventHookEnabled(),
		Events:      []string{},
		Program:     "",
		Args:        "",
		TimeoutSecs: defaultEventHookTimeoutSecs(),
	}
}

// LoadHooks loads hooks from the `event_hooks` settings key (a JSON array).
// Bad or missing input yields an empty list.
func LoadHooks(settings map[string]string) []EventHook {
	value, ok := settings["event_hooks"]
	if !ok {
		return nil
	}
	var hooks []EventHook
	if err := json.Unmarshal([]byte(value), &hooks); err != nil {
		return nil
	}
	return hooks
}

// SaveHooks persists hooks into the settings table.
func SaveHooks(dataDir string, hooks []EventHook) error {
	if hooks == nil {
		hooks = []EventHook{}
	}
	encoded, err := json.Marshal(hooks)
	if err != nil {
		return fmt.Errorf("serialize event hooks: %w", err)
	}
	return SaveSetting(dataDir, "event_hooks", string(encoded))
}

// ValidateHooks validates a hook list before it is persisted. It returns an
// error message, or an empty string when the list is valid.
func ValidateHooks(hooks []EventHook) string {
	if len(hooks) > 100 {
		return "too many event hooks (max 100)"
	}
	for _, hook := range hooks {
		if strings.TrimSpace(hook.Program) == "" {
			name := strings.TrimSpace(hook.Name)
			if name == "" {
				name = "unnamed"
			}
			return fmt.Sprintf("hook '%s' has no program", name)
		}
		if len(hook.Program) > 4096 || len(hook.Args) > 8192 || len(hook.Name) > 200 {
			return "an event hook is too large"
		}
		// The program is executed directly (never through a shell): require an
		// absolute path so a relative/ambiguous entry cannot resolve to an
		// unexpected executable from PATH.
		if !filepath.IsAbs(strings.TrimSpace(hook.Program)) {
			return fmt.Sprintf("hook '%s' program must be an absolute path", strings.TrimSpace(hook.Name))
		}
		if hook.TimeoutSecs > 86_400 {
			return "an event hook timeout exceeds 24h"
		}
	}
	return ""
}

// Expand expands `{key}` placeholders. Unknown keys are left untouched so a
// typo is visible in the executed arguments instead of silently vanishing.
func Expand(template string, vars map[string]string) string {
	var out strings.Builder
	out.Grow(len(template))
	rest := template
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			break
		}
		out.WriteString(rest[:open])
		after := rest[open+1:]
		if close := strings.Index(after, "}"); close >= 0 {
			key := after[:close]
			if value, ok := vars[key]; ok {
				out.WriteString(value)
				rest = after[close+1:]
				continue
			}
		}
		// Not a known placeholder: emit the brace literally and continue.
		out.WriteByte('{')
		rest = after
	}
	out.WriteString(rest)
	return out.String()
}

// SplitArgs splits an argument template honoring single/double quotes and
// backslash escapes, without invoking a shell.
func SplitArgs(input string) []string {
	args := []string{}
	var current strings.Builder
	var quote rune
	escaped := false
	hasToken := false
	for _, character := range input {
		if escaped {
			current.WriteRune(character)
			escaped = false
			hasToken = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else if character == '\\' && quote == '"' {
				escaped = true
			} else {
				current.WriteRune(character)
			}
			hasToken = true
			continue
		}
		switch {
		case character == '\\':
			escaped = true
		case character == '\'' || character == '"':
			quote = character
			hasToken = true
		case unicode.IsSpace(character):
			if hasToken {
				args = append(args, current.String())
				current.Reset()
				hasToken = false
			}
		default:
			current.WriteRune(character)
			hasToken = true
		}
	}
	if hasToken {
		args = append(args, current.String())
	}
	return args
}

// deref unwraps pointers/interfaces until a concrete value is reached. A nil
// pointer or nil interface becomes an untyped nil.
func deref(value any) any {
	for {
		rv := reflect.ValueOf(value)
		if !rv.IsValid() {
			return nil
		}
		if rv.Kind() == reflect.Ptr || rv.Kind() == reflect.Interface {
			if rv.IsNil() {
				return nil
			}
			value = rv.Elem().Interface()
			continue
		}
		return value
	}
}

// formatJSONNumber renders a float the way encoding/json displays a number:
// integral floats keep a fractional part (`2.0`).
func formatJSONNumber(value float64) string {
	text := strconv.FormatFloat(value, 'g', -1, 64)
	if !strings.ContainsAny(text, ".eE") {
		text += ".0"
	}
	return text
}

// scalarString renders a JSON scalar (string, bool or number) as text. The
// bool reports whether the value is a scalar at all; `null` and containers
// return false.
func scalarString(value any) (string, bool) {
	value = deref(value)
	switch v := value.(type) {
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int:
		return strconv.FormatInt(int64(v), 10), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float32:
		return formatJSONNumber(float64(v)), true
	case float64:
		return formatJSONNumber(v), true
	case json.Number:
		return v.String(), true
	default:
		return "", false
	}
}

// jsonString returns the string value of a JSON-ish value, or an empty string
// when it is not a string.
func jsonString(value any) string {
	value = deref(value)
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// jsonInt reads an integer from any JSON-ish numeric value (including
// pointers, as Go ports of `Option<i64>` payloads use them).
func jsonInt(value any) (int64, bool) {
	value = deref(value)
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case float32:
		return int64(v), true
	case float64:
		return int64(v), true
	case json.Number:
		if integer, err := v.Int64(); err == nil {
			return integer, true
		}
		if float, err := v.Float64(); err == nil {
			return int64(float), true
		}
	}
	return 0, false
}

// mapLookup reads a field from a map-ish value, returning nil when the value
// is not a map or the key is absent.
func mapLookup(value any, key string) any {
	value = deref(value)
	switch m := value.(type) {
	case map[string]any:
		return m[key]
	case map[string]string:
		return m[key]
	}
	rv := reflect.ValueOf(value)
	if rv.IsValid() && rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		got := rv.MapIndex(reflect.ValueOf(key))
		if got.IsValid() {
			return got.Interface()
		}
	}
	return nil
}

// jsonDisplay renders a value the way encoding/json's Display does (compact JSON).
func jsonDisplay(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

// walkVariables flattens a JSON payload into `key` and `a.b` string variables.
func walkVariables(prefix string, value any, vars map[string]string) {
	value = deref(value)
	if value == nil {
		return
	}
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			walkVariables(next, item, vars)
		}
		return
	case []any:
		if encoded, err := json.Marshal(v); err == nil {
			vars[prefix] = string(encoded)
		}
		return
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map:
		iter := rv.MapRange()
		for iter.Next() {
			key, ok := iter.Key().Interface().(string)
			if !ok {
				continue
			}
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			walkVariables(next, iter.Value().Interface(), vars)
		}
		return
	case reflect.Slice, reflect.Array:
		if encoded, err := json.Marshal(value); err == nil {
			vars[prefix] = string(encoded)
		}
		return
	}
	if text, ok := scalarString(value); ok {
		vars[prefix] = text
	}
}

// Variables flattens a JSON payload into `key` and `a.b` string variables and
// adds the event/time fields every hook can rely on.
func Variables(event string, payload map[string]any) map[string]string {
	vars := map[string]string{}
	vars["event"] = event
	vars["date"] = rfc3339Now()
	// Top-level convenience keys (the payloads emitted by Gextto are flat).
	for key, value := range payload {
		if text, ok := scalarString(value); ok {
			vars[key] = text
		}
	}
	walkVariables("", payload, vars)
	return vars
}

// rfc3339Now formats the current UTC instant in the same shape as chrono's
// `to_rfc3339()` (SecondsFormat::AutoSi and a `+00:00` offset).
func rfc3339Now() string {
	now := time.Now().UTC()
	base := now.Format("2006-01-02T15:04:05")
	nanos := now.Nanosecond()
	switch {
	case nanos == 0:
		return base + "+00:00"
	case nanos%1_000_000 == 0:
		return fmt.Sprintf("%s.%03d+00:00", base, nanos/1_000_000)
	case nanos%1_000 == 0:
		return fmt.Sprintf("%s.%06d+00:00", base, nanos/1_000)
	default:
		return fmt.Sprintf("%s.%09d+00:00", base, nanos)
	}
}

// HookRun is the result of executing one hook.
type HookRun struct {
	Hook     string `json:"hook"`
	Program  string `json:"program"`
	ExitCode *int   `json:"exit_code"`
	Success  bool   `json:"success"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// truncateHookOutput limits captured output to 4096 bytes, replacing invalid
// UTF-8 like the `String::from_utf8_lossy`, then trims whitespace.
func truncateHookOutput(text []byte) string {
	const limit = 4096
	slice := text
	if len(slice) > limit {
		slice = slice[:limit]
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(slice), "\uFFFD"))
}

// hookEnvKey builds the `GEXTTO_`-prefixed environment key for a variable name.
func hookEnvKey(key string) string {
	var sb strings.Builder
	sb.WriteString("GEXTTO_")
	for _, r := range key {
		c := r
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			sb.WriteRune(c)
		} else {
			sb.WriteByte('_')
		}
	}
	return sb.String()
}

// RunHook runs a hook, expanding its program and arguments and exporting the
// variables as environment. It returns an error only when the process cannot
// be spawned or times out.
func RunHook(hook EventHook, event string, payload map[string]any) (HookRun, error) {
	vars := Variables(event, payload)
	program := Expand(strings.TrimSpace(hook.Program), vars)
	args := SplitArgs(Expand(hook.Args, vars))
	// `0` was the JSON default in older configurations. Treat it as the
	// documented default too, so upgrading does not silently turn hooks into
	// one-second processes.
	timeoutSecs := hook.TimeoutSecs
	if timeoutSecs == 0 {
		timeoutSecs = defaultEventHookTimeoutSecs()
	}
	if timeoutSecs < 1 {
		timeoutSecs = 1
	}
	if timeoutSecs > 86_400 {
		timeoutSecs = 86_400
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = runHookEnv(vars)

	var stderrBuffer bytes.Buffer
	cmd.Stderr = &stderrBuffer
	output, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return HookRun{}, fmt.Errorf("'%s' timed out after %ds", program, timeoutSecs)
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return HookRun{}, fmt.Errorf("could not run '%s': %w", program, err)
	}
	exitCode := 0
	signaled := false
	if exitErr != nil {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				signaled = true
			} else {
				exitCode = status.ExitStatus()
			}
		} else {
			exitCode = exitErr.ExitCode()
			if exitCode < 0 {
				signaled = true
			}
		}
	}
	var codePtr *int
	if !signaled {
		code := exitCode
		codePtr = &code
	}
	return HookRun{
		Hook:     hook.Name,
		Program:  program,
		ExitCode: codePtr,
		Success:  err == nil,
		Stdout:   truncateHookOutput(output),
		Stderr:   truncateHookOutput(stderrBuffer.Bytes()),
	}, nil
}

// runHookEnv merges the hook variables (as `GEXTTO_*`) into the inherited
// environment, with later variables overriding earlier ones exactly like
// the `Command::env` (a sorted map).
func runHookEnv(vars map[string]string) []string {
	merged := map[string]string{}
	for _, entry := range os.Environ() {
		if index := strings.IndexByte(entry, '='); index >= 0 {
			key := entry[:index]
			// Never hand the daemon's own secrets to a hook script.
			if hookEnvSensitive(key) {
				continue
			}
			merged[key] = entry[index+1:]
		}
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		merged[hookEnvKey(key)] = vars[key]
	}
	envKeys := make([]string, 0, len(merged))
	for key := range merged {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	env := make([]string, 0, len(merged))
	for _, key := range envKeys {
		env = append(env, key+"="+merged[key])
	}
	return env
}

// hookEnvSensitive reports whether an inherited environment variable carries a
// daemon secret that must not be exposed to a hook program.
func hookEnvSensitive(key string) bool {
	if !strings.HasPrefix(key, "GEXTTO_") {
		return false
	}
	upper := strings.ToUpper(key)
	for _, needle := range []string{"TOKEN", "PASSWORD", "SECRET", "API_KEY", "APIKEY"} {
		if strings.Contains(upper, needle) {
			return true
		}
	}
	return false
}

// HookMatchesEvent reports whether a hook subscribes to an event; an empty
// list means all events.
func HookMatchesEvent(hook EventHook, event string) bool {
	if len(hook.Events) == 0 {
		return true
	}
	for _, name := range hook.Events {
		if name == event {
			return true
		}
	}
	return false
}

// cloneJSON deep-copies the JSON-ish containers so a detached hook run cannot
// observe later mutations of the caller's payload ( passed `data.clone()`).
func cloneJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneJSON(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for index, item := range v {
			out[index] = cloneJSON(item)
		}
		return out
	default:
		return value
	}
}

// Dispatch dispatches hooks for one event on a detached goroutine. It never
// blocks the caller (notification delivery) and logs each outcome.
func Dispatch(hooks []EventHook, event string, payload map[string]any) {
	selected := make([]EventHook, 0, len(hooks))
	for _, hook := range hooks {
		if hook.Enabled && strings.TrimSpace(hook.Program) != "" {
			selected = append(selected, hook)
		}
	}
	if len(selected) == 0 {
		return
	}
	owned, _ := cloneJSON(payload).(map[string]any)
	go func() {
		for _, hook := range selected {
			if !HookMatchesEvent(hook, event) {
				continue
			}
			run, err := RunHook(hook, event, owned)
			if err != nil {
				logging.Warn("🪝 event hook could not run",
					"hook", hook.Name, "program", hook.Program, "event", event, "error", err)
				continue
			}
			if run.Success {
				logging.Info("🪝 event hook completed",
					"hook", run.Hook, "program", run.Program, "event", event, "stdout", run.Stdout)
			} else {
				logging.Warn("🪝 event hook failed",
					"hook", run.Hook, "program", run.Program, "event", event,
					"exit_code", run.ExitCode, "stderr", run.Stderr)
			}
		}
	}()
}
