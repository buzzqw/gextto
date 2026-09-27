package gextto

// torrent_migration.go implements the migration *preparation* described in
// docs/aggiunta-qbittorrent-nox.md §14 (Fase 6) and
// docs/aggiunta-anacrolix.md §12. It never moves files or starts a second
// engine: it exports a persistent manifest of the managed torrents and
// validates, in dry-run, that a switch to the target backend is feasible.
//
// The actual hand-off stays an operator-driven, restart-based operation: the
// daemon only ever runs one transfer engine, so a migration is a clean shutdown
// with the manifest on disk, then a restart with the new `torrent_backend`.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/buzzqw/gextto/internal/logging"
)

// MigrationManifest is the persisted, replayable state of one migration.
type MigrationManifest struct {
	FromBackend string          `json:"from_backend"`
	ToBackend   string          `json:"to_backend"`
	CreatedAt   string          `json:"created_at"`
	Items       []MigrationItem `json:"items"`
	Warnings    []string        `json:"warnings"`
	Ready       bool            `json:"ready"`
	// Imported and CompletedAt are set once the target backend re-imported the
	// manifest at startup, so the import is not repeated.
	Imported    int    `json:"imported,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// MigrationItem is one torrent to preserve across a backend switch.
type MigrationItem struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	TorrentFile string  `json:"torrent_file,omitempty"`
	Magnet      string  `json:"magnet,omitempty"`
	SavePath    string  `json:"save_path"`
	Progress    float64 `json:"progress"`
	State       string  `json:"state"`
	Paused      bool    `json:"paused"`
	TotalSize   int64   `json:"total_size"`
	HasMetadata bool    `json:"has_metadata"`
}

// migrationManifestPath is where the manifest is persisted for crash recovery.
func migrationManifestPath(cfg *Config) string {
	return filepath.Join(cfg.DataDir, "torrent-migration.json")
}

// BuildMigrationPlan snapshots every managed torrent and validates the target
// backend without touching either engine.
func BuildMigrationPlan(s *AppState, cfg *Config, target string) MigrationManifest {
	if cfg == nil {
		cfg = latestConfig(s)
	}
	source := ActiveTorrentBackend(s).Name()
	plan := MigrationManifest{
		FromBackend: source,
		ToBackend:   target,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	engine := s.activeEngine()
	for _, view := range engine.List() {
		item := MigrationItem{
			Hash:        strings.ToLower(view.Hash),
			Name:        view.Name,
			SavePath:    view.SavePath,
			Progress:    view.Progress,
			State:       view.State,
			Paused:      view.State == "paused",
			TotalSize:   view.TotalSize,
			HasMetadata: view.HasMetadata,
		}
		if path, ok := engine.TorrentFilePath(view.Hash); ok {
			item.TorrentFile = path
		}
		if meta, err := s.db.TorrentMeta(view.Hash); err == nil && meta != nil {
			if strings.HasPrefix(meta.Release.Magnet, "magnet:") {
				item.Magnet = meta.Release.Magnet
			}
		}
		if item.TorrentFile == "" {
			if item.Magnet != "" {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("%s: nessun .torrent persistito, verrà reimportato dal magnet", item.Name))
			} else {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("%s: nessun .torrent e nessun magnet, impossibile reimportare", item.Name))
			}
		}
		plan.Items = append(plan.Items, item)
	}
	sort.Slice(plan.Items, func(i, j int) bool { return plan.Items[i].Hash < plan.Items[j].Hash })

	switch target {
	case BackendEmbedded:
		// Always available.
	case BackendQbittorrent:
		settings, err := qbittorrentSettingsFromConfig(cfg)
		if err != nil {
			plan.Warnings = append(plan.Warnings, err.Error())
		} else {
			if settings.Client.BaseURL == "" {
				plan.Warnings = append(plan.Warnings, "qbittorrent_url non configurato")
			}
			if err := validateBackendMappings(settings.Mappings, requiredBackendPaths(cfg)); err != nil {
				plan.Warnings = append(plan.Warnings, err.Error())
			}
		}
	case BackendAnacrolix:
		if newAnacrolixEngine == nil {
			plan.Warnings = append(plan.Warnings, "anacrolix non compilato (serve il build tag `anacrolix`)")
		}
	default:
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("backend target sconosciuto: %s", target))
	}
	plan.Ready = true
	for _, warning := range plan.Warnings {
		if strings.Contains(warning, "impossibile reimportare") || strings.Contains(warning, "non compilato") ||
			strings.Contains(warning, "sconosciuto") || strings.Contains(warning, "non configurato") ||
			strings.Contains(warning, "non coperto") || strings.Contains(warning, "overlapping") {
			plan.Ready = false
		}
	}
	return plan
}

// PersistMigrationManifest writes the manifest atomically so a crash during the
// switch does not lose the recovery information.
func PersistMigrationManifest(cfg *Config, manifest MigrationManifest) (string, error) {
	path := migrationManifestPath(cfg)
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// LoadMigrationManifest reads a previously persisted manifest, if any.
func LoadMigrationManifest(cfg *Config) (*MigrationManifest, error) {
	raw, err := os.ReadFile(migrationManifestPath(cfg))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var manifest MigrationManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// ImportMigrationManifest re-adds the torrents of a pending manifest into the
// active backend. It is safe at startup: it only acts when the manifest targets
// the active backend and is not completed, and re-adding is idempotent. On
// success the manifest is marked completed so a restart does not repeat it.
func ImportMigrationManifest(s *AppState, cfg *Config) (int, []string, error) {
	if s == nil || cfg == nil {
		return 0, nil, nil
	}
	manifest, err := LoadMigrationManifest(cfg)
	if err != nil || manifest == nil {
		return 0, nil, err
	}
	active := ActiveTorrentBackend(s).Name()
	if manifest.ToBackend != active {
		return 0, nil, nil
	}
	if manifest.CompletedAt != "" {
		return 0, nil, nil
	}
	engine := s.activeEngine()
	imported := 0
	var warnings []string
	for _, item := range manifest.Items {
		opts := AddOptions{Paused: item.Paused}
		if item.TorrentFile != "" && fileExists(item.TorrentFile) {
			savePath := item.SavePath
			hash, addErr := engine.AddTorrentFileWithOptions(item.TorrentFile, cfg, &savePath, opts)
			if addErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", item.Name, addErr))
				continue
			}
			if hash != nil {
				imported++
			}
			continue
		}
		if strings.HasPrefix(item.Magnet, "magnet:") {
			savePath := item.SavePath
			added, addErr := engine.AddWithOptions(item.Magnet, cfg, &savePath, opts)
			if addErr != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", item.Name, addErr))
				continue
			}
			if added {
				imported++
			}
			continue
		}
		warnings = append(warnings, fmt.Sprintf("%s: nessuna sorgente reimportabile", item.Name))
	}
	if len(warnings) == 0 {
		manifest.Imported = imported
		manifest.CompletedAt = time.Now().UTC().Format(time.RFC3339)
		if _, err := PersistMigrationManifest(cfg, *manifest); err != nil {
			return imported, warnings, err
		}
		logging.Info("migrazione: torrent reimportati nel backend attivo",
			"backend", active, "imported", imported)
	} else {
		logging.Warn("migrazione: alcuni torrent non sono stati reimportati", "warnings", len(warnings))
	}
	return imported, warnings, nil
}

// migrationPlanInput is the body of POST /api/torrent-migrations/plan.
type migrationPlanInput struct {
	To string `json:"to"`
	// Persist writes the manifest to disk (default true).
	Persist *bool `json:"persist"`
}

// TorrentMigrations implements GET /api/torrent-migrations: it reports the
// active backend and any persisted migration manifest.
func TorrentMigrations(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	manifest, err := LoadMigrationManifest(cfg)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	payload := map[string]any{
		"ok":       true,
		"backend":  ActiveTorrentBackend(s).Name(),
		"manifest": manifest,
	}
	jsonResponse(w, payload)
}

// TorrentMigrationPlan implements POST /api/torrent-migrations/plan: it builds
// (and optionally persists) the dry-run manifest without touching files.
func TorrentMigrationPlan(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	var input migrationPlanInput
	if r.Body != nil {
		_ = decodeJSON(r, &input)
	}
	target := strings.ToLower(strings.TrimSpace(input.To))
	if target == "" {
		target = TorrentBackendName(cfg)
	}
	if target == "" {
		target = BackendEmbedded
	}
	plan := BuildMigrationPlan(s, cfg, target)
	persist := input.Persist == nil || *input.Persist
	path := ""
	if persist {
		written, err := PersistMigrationManifest(cfg, plan)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		path = written
	}
	status := http.StatusOK
	if !plan.Ready {
		status = http.StatusConflict
	}
	jsonStatus(w, status, map[string]any{
		"ok":       plan.Ready,
		"manifest": plan,
		"path":     path,
	})
}

// TorrentMigrationCancel implements POST /api/torrent-migrations/cancel: it
// removes the persisted manifest once the operator completed the switch.
func TorrentMigrationCancel(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	path := migrationManifestPath(cfg)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonResponse(w, map[string]any{"ok": true})
}
