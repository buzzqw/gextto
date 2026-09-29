// Command gexttod is the gextto daemon: a self-contained media acquisition and
// archiving service (Go implementation of gextto).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	gextto "github.com/buzzqw/gextto"
	"github.com/buzzqw/gextto/internal/cache"
	"github.com/buzzqw/gextto/internal/constants"
	"github.com/buzzqw/gextto/internal/logging"
	"github.com/buzzqw/gextto/internal/messages"
	"github.com/buzzqw/gextto/internal/tui"
)

func main() {
	command := gextto.Parse(os.Args[1:])
	switch command.Kind {
	case gextto.CommandVersion:
		fmt.Println(gextto.VersionString())
		return
	case gextto.CommandHelp:
		fmt.Print(gextto.Usage())
		return
	case gextto.CommandUpdate:
		if err := gextto.Run(context.Background(), command.Options); err != nil {
			fmt.Fprintln(os.Stderr, "update failed:", err)
			os.Exit(1)
		}
		return
	case gextto.CommandImport:
		report, err := gextto.ImportExtto(command.Source, command.DataDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "import failed:", err)
			os.Exit(1)
		}
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(encoded))
		return
	case gextto.CommandMigrate:
		report, err := gextto.MigrateDataDir(command.From, command.To)
		encoded, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(encoded))
		if err != nil {
			fmt.Fprintln(os.Stderr, "migrate failed:", err)
			os.Exit(1)
		}
		return
	case gextto.CommandTUI:
		err := tui.Run(context.Background(), tui.Options{
			URL:  command.TUIURL,
			Lang: command.TUILang,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "tui:", err)
			os.Exit(1)
		}
		return
	default:
		if err := runDaemon(command.DryRun, command.Config); err != nil {
			fmt.Fprintln(os.Stderr, "gexttod:", err)
			os.Exit(1)
		}
	}
}

func runDaemon(dryRun bool, configOption *string) error {
	configPath := "gextto.json"
	if configOption != nil {
		configPath = *configOption
	}

	cfg, err := gextto.LoadConfig(configPath)
	if err != nil {
		return err
	}
	if dryRun {
		cfg.DryRun = true
	}
	if err := cfg.PrepareDirs(); err != nil {
		return err
	}
	if err := gextto.CleanupMovieRequirements(cfg.DataDir); err != nil {
		logging.Warn("movie requirements cleanup failed", "error", err)
	}

	cache.Init(cfg.DataDir)

	closeLog := logging.Init(cfg.DataDir, "gextto.log", 5*1024*1024, 4)
	defer closeLog()
	filter := "info"
	if cfg.DebugEnabled() {
		filter = "debug"
	}
	if env := os.Getenv("GEXTTO_LOG"); env != "" {
		filter = env
	}
	logging.SetLevel(filter)

	db, err := gextto.OpenDatabase(filepath.Join(cfg.DataDir, constants.DefaultDBFile))
	if err != nil {
		return err
	}
	defer db.Close()
	archive, err := gextto.OpenArchive(filepath.Join(cfg.DataDir, constants.DefaultArchiveFile))
	if err != nil {
		return err
	}
	defer archive.Close()
	comics, err := gextto.OpenComicsDb(filepath.Join(cfg.DataDir, "gextto_comics.db"))
	if err != nil {
		return err
	}
	defer comics.Close()
	engine := gextto.NewEngine().WithDB(db)
	torrents, err := gextto.NewLibtorrentClient(&cfg)
	if err != nil {
		return err
	}
	i18n, err := gextto.OpenI18nDb(filepath.Join(cfg.DataDir, constants.DefaultConfigFile))
	if err != nil {
		return err
	}
	defer i18n.Close()
	if _, err := i18n.SeedDefaultTranslations(); err != nil {
		logging.Warn("translation seeding failed", "error", err)
	}
	if language, err := i18n.Language(); err == nil {
		messages.SetLanguage(language)
	}

	// Reclaim an oversized WAL and verify integrity, as the daemon does at
	// startup.
	logCheckpoint("series", db.Checkpoint)
	logCheckpoint("archive", archive.Checkpoint)
	logCheckpoint("comics", comics.Checkpoint)
	logCheckpoint("config", i18n.Checkpoint)

	for _, check := range []struct {
		name string
		run  func() ([]string, error)
	}{
		{constants.DefaultDBFile, db.QuickCheck},
		{constants.DefaultArchiveFile, archive.QuickCheck},
		{"gextto_comics.db", comics.QuickCheck},
		{constants.DefaultConfigFile, i18n.QuickCheck},
	} {
		rows, checkErr := check.run()
		switch {
		case checkErr != nil:
			logging.Warn("integrity check not executed", "database", check.name, "error", checkErr)
		case len(rows) == 1 && rows[0] == "ok":
			logging.Info("integrity check: ok", "database", check.name)
		default:
			logging.Error("integrity check: problems found", "database", check.name, "detail", joinRows(rows))
		}
	}

	if conflicts := gextto.ListenConflicts(&cfg); len(conflicts) > 0 {
		logging.Warn("listen address already in use: stop the other instance (legacy extto/rextto or a previous Gextto) before starting",
			"addresses", conflicts)
	}

	state := gextto.NewAppState(
		&cfg,
		configPath,
		i18n,
		db,
		archive,
		comics,
		engine,
		torrents,
		gextto.FromConfig(&cfg),
		gextto.NewTmdbClientWithLanguage(cfg.TmdbAPIKey, cfg.TmdbLanguage()),
	)

	serveErr := gextto.Serve(state)
	if shutdownErr := gextto.ShutdownTorrentEngine(state); shutdownErr != nil {
		logging.Error("torrent engine shutdown failed", "error", shutdownErr)
	}
	if shutdownErr := gextto.ShutdownEmbedded(state, &cfg); shutdownErr != nil {
		logging.Error("libtorrent shutdown did not save all fastresume data", "error", shutdownErr)
	}
	logCheckpoint("series", db.Checkpoint)
	logCheckpoint("archive", archive.Checkpoint)
	logCheckpoint("comics", comics.Checkpoint)
	logCheckpoint("config", i18n.Checkpoint)
	if serveErr != nil {
		logging.Error("web server stopped with error", "error", serveErr)
	}
	closeLog()
	return serveErr
}

// logCheckpoint runs a SQLite checkpoint and logs a warning on failure instead
// of silently ignoring it: a failed checkpoint (disk full, read-only database)
// is worth surfacing but must not prevent a clean shutdown.
func logCheckpoint(database string, checkpoint func() error) {
	if err := checkpoint(); err != nil {
		logging.Warn("SQLite checkpoint failed", "database", database, "error", err)
	}
}

func joinRows(rows []string) string {
	out := ""
	for index, row := range rows {
		if index > 0 {
			out += " | "
		}
		out += row
	}
	return out
}
