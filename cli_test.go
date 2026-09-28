package gextto

import (
	"reflect"
	"strings"
	"testing"
)

func parseStr(line string) Command {
	if strings.TrimSpace(line) == "" {
		return Parse(nil)
	}
	return Parse(strings.Fields(line))
}

func testStrPtr(value string) *string { return &value }

func TestDefaultsToServe(t *testing.T) {
	if got, want := parseStr(""), (Command{Kind: CommandServe}); !reflect.DeepEqual(got, want) {
		t.Fatalf("parse(\"\") = %#v, want %#v", got, want)
	}
	got := parseStr("--dry-run --config /tmp/gextto.json")
	want := Command{Kind: CommandServe, DryRun: true, Config: testStrPtr("/tmp/gextto.json")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse(--dry-run --config ...) = %#v, want %#v", got, want)
	}
}

func TestAcceptsInlineConfig(t *testing.T) {
	got := parseStr("--config=/etc/gextto.json")
	want := Command{Kind: CommandServe, Config: testStrPtr("/etc/gextto.json")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parse(--config=...) = %#v, want %#v", got, want)
	}
}

func TestRecognisesVersionAndHelp(t *testing.T) {
	if got := parseStr("--version"); got.Kind != CommandVersion {
		t.Fatalf("--version = %#v", got)
	}
	if got := parseStr("-V"); got.Kind != CommandVersion {
		t.Fatalf("-V = %#v", got)
	}
	if got := parseStr("--help"); got.Kind != CommandHelp {
		t.Fatalf("--help = %#v", got)
	}
	if got := parseStr("-h"); got.Kind != CommandHelp {
		t.Fatalf("-h = %#v", got)
	}
}

func TestParsesUpdateOptions(t *testing.T) {
	got := Parse(strings.Fields("--update --channel stable --install-dir /opt/gextto --no-restart"))
	want := Command{Kind: CommandUpdate, Options: UpdateOptions{
		Channel:    testStrPtr("stable"),
		InstallDir: testStrPtr("/opt/gextto"),
		Restart:    false,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("update options = %#v, want %#v", got, want)
	}

	got = Parse(strings.Fields("--update --release=v1.2.3 --repo=me/gextto --force"))
	want = Command{Kind: CommandUpdate, Options: UpdateOptions{
		Release: testStrPtr("v1.2.3"),
		Repo:    testStrPtr("me/gextto"),
		Restart: true,
		Force:   true,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("update options = %#v, want %#v", got, want)
	}
}

func TestParsesImportSubcommand(t *testing.T) {
	got := Parse(strings.Fields("import --from-copy /srv/extto --data-dir /srv/data"))
	want := Command{Kind: CommandImport, Source: "/srv/extto", DataDir: "/srv/data"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("import = %#v, want %#v", got, want)
	}
}

func TestParsesTuiSubcommand(t *testing.T) {
	got := Parse(strings.Fields("tui --url http://host:5000 --lang en"))
	want := Command{Kind: CommandTUI, TUIURL: "http://host:5000", TUILang: "en"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tui = %#v, want %#v", got, want)
	}
	got = Parse(strings.Fields("tui --url=http://x --lang=it"))
	want = Command{Kind: CommandTUI, TUIURL: "http://x", TUILang: "it"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tui inline = %#v, want %#v", got, want)
	}
	if got := Parse([]string{"tui"}); got.Kind != CommandTUI {
		t.Fatalf("tui bare = %#v", got)
	}
}

func TestUsageMentionsEverySwitchesAndDefaults(t *testing.T) {
	usage := Usage()
	for _, needle := range []string{
		"gexttod 0.1.0 — Gextto (gextto): self-contained Go daemon for media acquisition and archiving",
		"    gexttod [OPTIONS]",
		"    gexttod tui [--url <url>] [--lang it|en]",
		"    gexttod import --from-copy <dir> [--data-dir <dir>]",
		"    gexttod --update [OPTIONS]",
		"    -h, --help            Show this help and exit",
		"    -V, --version         Show the installed version and exit",
		"    --config <file>       Configuration file (default: gextto.json)",
		"    --dry-run             Never start real downloads",
		"    -u, --url <url>       Daemon URL (default: GEXTTO_URL or http://127.0.0.1:5000)",
		"    -l, --lang <it|en>    Interface language (default: daemon language)",
		"    --repo <owner/name>   GitHub repository (default: buzzqw/gextto)",
		"    --release <tag>       Install a specific release tag",
		"    --channel <name>      continuous (default) or stable",
		"    --install-dir <dir>   Installation directory (default: binary directory)",
		"    --archive <file>      Install from a local archive instead of downloading",
		"    --force               Reinstall even if the version is unchanged",
		"    --no-restart          Do not restart the service after updating",
	} {
		if !strings.Contains(usage, needle) {
			t.Errorf("usage is missing %q", needle)
		}
	}
	if !strings.HasSuffix(usage, "after updating\n") {
		t.Errorf("usage should end with a newline: %q", usage)
	}
}
