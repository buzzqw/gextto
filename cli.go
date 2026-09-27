package gextto

import (
	"fmt"
	"os"
	"strings"

	"github.com/buzzqw/gextto/internal/constants"
)

// UpdateOptions holds the options accepted by `gexttod --update`.
type UpdateOptions struct {
	// Repo is the GitHub repository (`owner/name`). Defaults to DefaultRepo.
	Repo *string `json:"repo"`
	// Release is an explicit release tag, for example `v0.2.0`.
	Release *string `json:"release"`
	// Channel is the release channel (`continuous` or `stable`). Ignored when
	// Release is set.
	Channel *string `json:"channel"`
	// InstallDir is the installation directory. Defaults to the directory of the
	// running binary.
	InstallDir *string `json:"install_dir"`
	// Archive is a local archive to install instead of downloading one
	// (offline/testing).
	Archive *string `json:"archive"`
	// Restart restarts the service after a successful update (default: true).
	Restart bool `json:"restart"`
	// Force reinstalls even when the installed marker already matches the
	// release.
	Force bool `json:"force"`
}

// CommandKind identifies which Command a parsed line represents.
type CommandKind int

const (
	// CommandServe runs the daemon (default command).
	CommandServe CommandKind = iota
	// CommandImport imports data from a legacy `Extto` installation.
	CommandImport
	// CommandVersion prints the installed version and exits.
	CommandVersion
	// CommandHelp prints usage and exits.
	CommandHelp
	// CommandUpdate downloads and installs the latest components, then exits.
	CommandUpdate
	// CommandMigrate copies and renames a legacy data directory into the
	// current gextto layout.
	CommandMigrate
	// CommandTUI opens the interactive terminal interface.
	CommandTUI
)

// Command is a parsed command line.
type Command struct {
	Kind CommandKind

	// Serve fields.
	DryRun bool
	Config *string

	// Import fields.
	Source  string
	DataDir string

	// Migrate fields.
	From string
	To   string

	// TUI fields.
	TUIURL   string
	TUIToken string
	TUILang  string

	// Update fields.
	Options UpdateOptions
}

// DefaultCommand returns the command used when no arguments are given.
func DefaultCommand() Command {
	return Command{Kind: CommandServe}
}

// Parse parses the process arguments, excluding `argv[0]`.
func Parse(args []string) Command {
	// `import` is a sub-command and owns the rest of the line.
	if len(args) > 0 && args[0] == "import" {
		return parseImport(args[1:])
	}
	if len(args) > 0 && args[0] == "migrate" {
		return parseMigrate(args[1:])
	}
	if len(args) > 0 && args[0] == "tui" {
		return parseTUI(args[1:])
	}

	if hasFlag(args, "--help", "-h") {
		return Command{Kind: CommandHelp}
	}
	if hasFlag(args, "--version", "-V") {
		return Command{Kind: CommandVersion}
	}
	if hasFlag(args, "--update") {
		return Command{Kind: CommandUpdate, Options: parseUpdate(args)}
	}

	dryRun := false
	var config *string
	for index := 0; index < len(args); index++ {
		switch value := args[index]; {
		case value == "--dry-run":
			dryRun = true
		case value == "--config":
			if index+1 < len(args) {
				next := args[index+1]
				config = &next
			} else {
				config = nil
			}
			index++
		case strings.HasPrefix(value, "--config="):
			trimmed := value[len("--config="):]
			config = &trimmed
		}
	}
	return Command{Kind: CommandServe, DryRun: dryRun, Config: config}
}

func parseImport(args []string) Command {
	source := "/path/to/legacy"
	if value, ok := os.LookupEnv("GEXTTO_IMPORT_SOURCE"); ok {
		source = value
	}
	dataDir := "data"
	for index := 0; index < len(args); index++ {
		value := args[index]
		switch {
		case value == "--from-copy":
			if index+1 < len(args) {
				source = args[index+1]
				index++
			}
		case value == "--data-dir":
			if index+1 < len(args) {
				dataDir = args[index+1]
				index++
			}
		case strings.HasPrefix(value, "--from-copy="):
			source = value[len("--from-copy="):]
		case strings.HasPrefix(value, "--data-dir="):
			dataDir = value[len("--data-dir="):]
		}
	}
	return Command{Kind: CommandImport, Source: source, DataDir: dataDir}
}

func parseMigrate(args []string) Command {
	from := ""
	if value, ok := os.LookupEnv("GEXTTO_MIGRATE_FROM"); ok {
		from = value
	}
	to := "data"
	for index := 0; index < len(args); index++ {
		value := args[index]
		switch {
		case value == "--from":
			if index+1 < len(args) {
				from = args[index+1]
				index++
			}
		case value == "--to":
			if index+1 < len(args) {
				to = args[index+1]
				index++
			}
		case strings.HasPrefix(value, "--from="):
			from = value[len("--from="):]
		case strings.HasPrefix(value, "--to="):
			to = value[len("--to="):]
		}
	}
	return Command{Kind: CommandMigrate, From: from, To: to}
}

func parseTUI(args []string) Command {
	command := Command{Kind: CommandTUI}
	for index := 0; index < len(args); index++ {
		value := args[index]
		switch {
		case (value == "--url" || value == "-u") && index+1 < len(args):
			command.TUIURL = args[index+1]
			index++
		case strings.HasPrefix(value, "--url="):
			command.TUIURL = value[len("--url="):]
		case value == "--token" && index+1 < len(args):
			command.TUIToken = args[index+1]
			index++
		case strings.HasPrefix(value, "--token="):
			command.TUIToken = value[len("--token="):]
		case (value == "--lang" || value == "-l") && index+1 < len(args):
			command.TUILang = args[index+1]
			index++
		case strings.HasPrefix(value, "--lang="):
			command.TUILang = value[len("--lang="):]
		}
	}
	return command
}

func parseUpdate(args []string) UpdateOptions {
	options := UpdateOptions{Restart: true}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		key := arg
		var inline *string
		if before, after, found := strings.Cut(arg, "="); found {
			key = before
			value := after
			inline = &value
		}
		takeValue := func() *string {
			if inline != nil {
				return inline
			}
			if index+1 < len(args) {
				value := args[index+1]
				index++
				return &value
			}
			return nil
		}
		switch key {
		case "--repo":
			options.Repo = takeValue()
		case "--release":
			options.Release = takeValue()
		case "--channel":
			options.Channel = takeValue()
		case "--install-dir":
			options.InstallDir = takeValue()
		case "--archive":
			options.Archive = takeValue()
		case "--no-restart":
			options.Restart = false
		case "--force":
			options.Force = true
		}
	}
	return options
}

func hasFlag(args []string, flags ...string) bool {
	for _, arg := range args {
		for _, flag := range flags {
			if arg == flag {
				return true
			}
		}
	}
	return false
}

// Usage is the text shown by `gexttod --help`.
func Usage() string {
	return fmt.Sprintf(`%s %s — %s

USAGE:
    %s [OPTIONS]
    %s tui [--url <url>] [--token <token>] [--lang it|en]
    %s import --from-copy <dir> [--data-dir <dir>]
    %s migrate --from <dir> [--to <dir>]
    %s --update [OPTIONS]

OPTIONS:
    -h, --help            Show this help and exit
    -V, --version         Show the installed version and exit
    --config <file>       Configuration file (default: gextto.json)
    --dry-run             Never start real downloads

TUI OPTIONS:
    -u, --url <url>       Daemon URL (default: GEXTTO_URL or http://127.0.0.1:5000)
    --token <token>       API token (default: GEXTTO_API_TOKEN)
    -l, --lang <it|en>    Interface language (default: daemon language)

MIGRATE OPTIONS:
    --from <dir>          Source data directory to migrate (required)
    --to <dir>            Destination data directory (default: data)

UPDATE OPTIONS:
    --repo <owner/name>   GitHub repository (default: %s)
    --release <tag>       Install a specific release tag
    --channel <name>      continuous (default) or stable
    --install-dir <dir>   Installation directory (default: binary directory)
    --archive <file>      Install from a local archive instead of downloading
    --force               Reinstall even if the version is unchanged
    --no-restart          Do not restart the service after updating
`,
		constants.AppName, constants.Version, appAbout,
		constants.AppName, constants.AppName, constants.AppName, constants.AppName, constants.AppName,
		DefaultRepo,
	)
}

// appAbout mirrors the project description.
const appAbout = "Gextto (gextto): self-contained Go daemon for media acquisition and archiving"
