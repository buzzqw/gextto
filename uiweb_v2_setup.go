package gextto

// uiweb_v2_setup.go is the first-run setup wizard (/?view=setup). A fresh
// installation opens it instead of the dashboard until it is completed or
// skipped; it stays reachable afterwards. Each step saves through the same
// validation as the settings page, so the wizard cannot store anything the
// settings page would refuse:
//
//  1. Accesso    interface language, login on/off, user and password
//  2. Cartelle   library (NAS), downloads, temporary files, trash, each checked
//                live: exists, writable by the service user, filesystem, space
//  3. Fonti      a Prowlarr/Jackett indexer, the TMDB key (recommended) and,
//                as a complete alternative, the TVDB key and PIN
//  4. Primo titolo  the usual TMDB search (TVDB without a TMDB key) with
//                its "add" buttons
//  5. Attiva     leave dry-run and start the automatic cycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const v2SetupSteps = 5

type v2SetupStep struct {
	Number  int
	Title   string
	Current bool
	Done    bool
}

// v2SetupPath is one folder of step 2 with its live check.
type v2SetupPath struct {
	Key         string
	Label       string
	Hint        string
	Value       string
	Checked     bool
	Exists      bool
	Writable    bool
	WillCreate  bool
	Filesystem  string
	Network     bool
	FreeBytes   uint64
	SameAsLib   bool
	Problem     string
	Placeholder string
}

// OK is true when the folder can be used as it is (or will be created).
func (p v2SetupPath) OK() bool { return p.Checked && p.Problem == "" }

type v2SetupView struct {
	Step        int
	Steps       []v2SetupStep
	Message     string
	Error       bool
	ServiceUser string
	Language    string
	AuthEnabled bool
	AuthUser    string
	AuthBypass  bool
	AuthHasPass bool
	Paths       []v2SetupPath
	Mounts      []string
	Indexers    []IndexerConfig
	TmdbSet     bool
	TvdbSet     bool
	SeriesCount int
	MoviesCount int
	Active      bool
	DryRun      bool
	HasIndexer  bool
	ArchiveSet  bool
	Completed   bool
}

// Prev and Next are the neighbouring step numbers.
func (v v2SetupView) Prev() int { return v.Step - 1 }
func (v v2SetupView) Next() int { return v.Step + 1 }

// Last is true on the final step.
func (v v2SetupView) Last() bool { return v.Step >= v2SetupSteps }

// v2SetupNeeded is true on a fresh installation: no setup marker and no
// series, movies or archived files yet. An existing installation upgraded to
// a version with the wizard never sees it (the marker is written for it).
func v2SetupNeeded(s *AppState) bool {
	cfg := latestConfig(s)
	if SetupComplete(cfg) {
		return false
	}
	hasData := false
	if value, err := s.db.HasData(); err == nil && value {
		hasData = true
	}
	if count, err := s.archive.Count(); err == nil && count > 0 {
		hasData = true
	}
	if len(cfg.Series) > 0 || len(cfg.Movies) > 0 {
		hasData = true
	}
	if hasData {
		_ = CompleteSetup(cfg)
		return false
	}
	return true
}

func v2SetupViewFrom(s *AppState, r *http.Request) v2SetupView {
	cfg := latestConfig(s)
	step, _ := strconv.Atoi(r.FormValue("step"))
	if step < 1 || step > v2SetupSteps {
		step = 1
	}
	view := v2SetupView{
		Step:        step,
		Message:     strings.TrimSpace(r.FormValue("msg")),
		Error:       r.FormValue("msg_err") == "1",
		Language:    v2Language(s),
		AuthEnabled: settingsBool(cfg, authEnabledSetting, false),
		AuthUser:    strings.TrimSpace(cfg.Settings[authUsernameSetting]),
		AuthBypass:  settingsBool(cfg, authLocalBypassSetting, true),
		AuthHasPass: strings.TrimSpace(cfg.Settings[authPasswordSetting]) != "",
		Indexers:    cfg.Indexers,
		TmdbSet:     cfg.TmdbAPIKey != nil && strings.TrimSpace(*cfg.TmdbAPIKey) != "",
		TvdbSet:     tvdbClientFor(cfg).Configured(),
		SeriesCount: len(cfg.Series),
		MoviesCount: len(cfg.Movies),
		Active:      cfg.Active,
		DryRun:      cfg.DryRun,
		ArchiveSet:  cfg.ArchiveRoot != nil && strings.TrimSpace(*cfg.ArchiveRoot) != "",
		Completed:   SetupComplete(cfg),
	}
	if current, err := user.Current(); err == nil {
		view.ServiceUser = current.Username
	}
	for _, indexer := range cfg.Indexers {
		if indexer.Enabled {
			view.HasIndexer = true
		}
	}
	titles := []string{"Accesso", "Cartelle", "Fonti", "Primo titolo", "Attiva"}
	for index, title := range titles {
		number := index + 1
		view.Steps = append(view.Steps, v2SetupStep{Number: number, Title: title, Current: number == step, Done: number < step})
	}
	if step == 2 {
		values := v2SetupPathValues(cfg)
		check := r.FormValue("checked") == "1"
		for key := range values {
			if value := r.FormValue(key); value != "" {
				values[key] = value
			}
		}
		view.Paths = v2SetupCheckPaths(values, check)
		view.Mounts = v2SetupNetworkMounts()
	}
	return view
}

func v2SetupPathValues(cfg *Config) map[string]string {
	values := map[string]string{
		"archive_root":        "",
		"libtorrent_dir":      cfg.LibtorrentDir,
		"libtorrent_temp_dir": "",
		"trash_path":          "",
	}
	if cfg.ArchiveRoot != nil {
		values["archive_root"] = *cfg.ArchiveRoot
	}
	if cfg.LibtorrentTempDir != nil {
		values["libtorrent_temp_dir"] = *cfg.LibtorrentTempDir
	}
	if cfg.TrashPath != nil {
		values["trash_path"] = *cfg.TrashPath
	}
	return values
}

var v2SetupPathOrder = []struct{ key, label, hint, placeholder string }{
	{"archive_root", "Libreria (serie TV e film)", "La cartella finale, di solito sul NAS: ogni serie o film vi ha la sua sottocartella.", "/mnt/nas/media"},
	{"libtorrent_dir", "Download", "Dove restano i file mentre vengono condivisi (seed).", "/var/lib/gextto/downloads"},
	{"libtorrent_temp_dir", "Download temporanei", "I file incompleti. Meglio su un disco locale veloce, non sul NAS.", "/var/lib/gextto/incomplete"},
	{"trash_path", "Cestino", "I file sostituiti da un upgrade, conservati prima della cancellazione.", "/var/lib/gextto/trash"},
}

// v2SetupCheckPaths returns the folders in display order; with check set, each
// one is inspected.
func v2SetupCheckPaths(values map[string]string, check bool) []v2SetupPath {
	var library syscall.Stat_t
	libraryOK := false
	if root := strings.TrimSpace(values["archive_root"]); root != "" {
		libraryOK = syscall.Stat(root, &library) == nil
	}
	out := make([]v2SetupPath, 0, len(v2SetupPathOrder))
	for _, item := range v2SetupPathOrder {
		path := v2SetupPath{Key: item.key, Label: item.label, Hint: item.hint, Placeholder: item.placeholder, Value: strings.TrimSpace(values[item.key])}
		if check {
			v2SetupInspect(&path)
			if libraryOK && path.Exists && item.key != "archive_root" {
				var stat syscall.Stat_t
				if syscall.Stat(path.Value, &stat) == nil {
					path.SameAsLib = stat.Dev == library.Dev
				}
			}
		}
		out = append(out, path)
	}
	return out
}

func v2SetupInspect(path *v2SetupPath) {
	path.Checked = true
	if path.Value == "" {
		if path.Key == "archive_root" {
			path.Problem = "Indica la cartella della libreria."
		}
		return
	}
	if !filepath.IsAbs(path.Value) {
		path.Problem = "Serve un percorso assoluto (che inizia con /)."
		return
	}
	info, err := os.Stat(path.Value)
	target := path.Value
	switch {
	case err == nil && !info.IsDir():
		path.Problem = "Esiste ma non è una cartella."
		return
	case err == nil:
		path.Exists = true
	case errors.Is(err, os.ErrNotExist):
		if path.Key == "archive_root" {
			// The library is usually a NAS mount: creating it would hide a
			// missing mount behind an empty local folder.
			path.Problem = "La cartella non esiste: se è sul NAS, controlla che sia montata."
			return
		}
		path.WillCreate = true
		target = v2SetupExistingParent(path.Value)
	default:
		path.Problem = err.Error()
		return
	}
	path.Writable = v2SetupWritable(target)
	if !path.Writable {
		path.Problem = "L'utente del servizio non può scrivere qui."
	}
	var stat syscall.Statfs_t
	if syscall.Statfs(target, &stat) == nil {
		path.Filesystem, path.Network = v2SetupFilesystemName(int64(stat.Type))
		path.FreeBytes = stat.Bavail * uint64(stat.Bsize)
	}
}

func v2SetupExistingParent(path string) string {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		if info, err := os.Stat(current); err == nil && info.IsDir() {
			return current
		}
		if current == filepath.Dir(current) {
			return current
		}
	}
}

// v2SetupWritable creates and removes a probe file: the permission bits alone
// do not tell what NFS/SMB mounts (root squash, uid mapping) allow.
func v2SetupWritable(dir string) bool {
	file, err := os.CreateTemp(dir, ".gextto-write-test-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

// v2SetupFilesystemName names the common filesystems by their statfs magic.
func v2SetupFilesystemName(magic int64) (string, bool) {
	switch uint32(magic) {
	case 0x6969:
		return "NFS", true
	case 0xFF534D42, 0xFE534D42, 0x517B:
		return "SMB/CIFS", true
	case 0x65735546:
		return "FUSE", true
	case 0xEF53:
		return "ext4", false
	case 0x9123683E:
		return "btrfs", false
	case 0x58465342:
		return "xfs", false
	case 0x2FC12FC1:
		return "zfs", false
	case 0x01021994:
		return "tmpfs (RAM)", false
	case 0x794C7630:
		return "overlay", false
	case 0x4D44:
		return "FAT", false
	case 0x5346544E:
		return "NTFS", false
	}
	return fmt.Sprintf("0x%x", uint32(magic)), false
}

// v2SetupNetworkMounts lists NAS mounts (NFS, SMB, FUSE) and the folders
// mounted under /mnt, /media and /srv: the likely library locations.
func v2SetupNetworkMounts() []string {
	raw, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		point := strings.ReplaceAll(fields[1], `\040`, " ")
		kind := fields[2]
		network := strings.HasPrefix(kind, "nfs") || kind == "cifs" || kind == "smb3" || strings.HasPrefix(kind, "fuse.")
		local := strings.HasPrefix(point, "/mnt/") || strings.HasPrefix(point, "/media/") || strings.HasPrefix(point, "/srv/")
		if !network && !local || kind == "fuse.portal" || kind == "fuse.gvfsd-fuse" || seen[point] {
			continue
		}
		seen[point] = true
		out = append(out, point)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func v2SetupRedirect(w http.ResponseWriter, r *http.Request, step int, message string, isErr bool) {
	target := "/?view=setup&step=" + strconv.Itoa(step)
	if message != "" {
		target += "&msg=" + url.QueryEscape(message)
		if isErr {
			target += "&msg_err=1"
		}
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// v2SetupSave stores one setting with the same checks as the settings API.
func v2SetupSave(s *AppState, key, value string) error {
	if !gh7_setting_key_allowed(key) {
		return fmt.Errorf("%s is not writable", key)
	}
	if err := validatePathSetting(key, value, latestConfig(s)); err != nil {
		return err
	}
	return saveConfigSetting(s.cfg.DataDir, key, value)
}

// V2SetupAuth saves step 1.
func V2SetupAuth(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.FormValue("auth") != "on" {
		if err := v2SetupSave(s, authEnabledSetting, "false"); err != nil {
			v2SetupRedirect(w, r, 1, err.Error(), true)
			return
		}
		v2SetupRedirect(w, r, 2, "", false)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	cfg := latestConfig(s)
	hasPassword := strings.TrimSpace(cfg.Settings[authPasswordSetting]) != ""
	switch {
	case username == "":
		v2SetupRedirect(w, r, 1, "Indica il nome utente.", true)
		return
	case password == "" && !hasPassword:
		v2SetupRedirect(w, r, 1, "Indica una password.", true)
		return
	case password != "" && len(password) < 8:
		v2SetupRedirect(w, r, 1, "La password deve avere almeno 8 caratteri.", true)
		return
	case password != r.FormValue("password2"):
		v2SetupRedirect(w, r, 1, "Le due password non coincidono.", true)
		return
	}
	bypass := "false"
	if r.FormValue("local_bypass") == "on" {
		bypass = "true"
	}
	steps := [][2]string{{authUsernameSetting, username}, {authLocalBypassSetting, bypass}}
	if password != "" {
		steps = append(steps, [2]string{authPasswordSetting, password})
	}
	// Enabled last: the credentials exist before the login is required.
	steps = append(steps, [2]string{authEnabledSetting, "true"})
	for _, step := range steps {
		if err := v2SetupSave(s, step[0], step[1]); err != nil {
			v2SetupRedirect(w, r, 1, err.Error(), true)
			return
		}
	}
	v2SetupRedirect(w, r, 2, "Accesso protetto da password.", false)
}

// V2SetupPaths checks (action=check) or saves (action=save) step 2.
func V2SetupPaths(w http.ResponseWriter, r *http.Request, s *AppState) {
	values := map[string]string{}
	for _, item := range v2SetupPathOrder {
		values[item.key] = strings.TrimSpace(r.FormValue(item.key))
	}
	if r.FormValue("action") != "save" {
		query := url.Values{"view": {"setup"}, "step": {"2"}, "checked": {"1"}}
		for key, value := range values {
			query.Set(key, value)
		}
		http.Redirect(w, r, "/?"+query.Encode(), http.StatusSeeOther)
		return
	}
	paths := v2SetupCheckPaths(values, true)
	for _, path := range paths {
		if path.Problem != "" {
			query := url.Values{"view": {"setup"}, "step": {"2"}, "checked": {"1"}, "msg": {path.Label + ": " + path.Problem}, "msg_err": {"1"}}
			for key, value := range values {
				query.Set(key, value)
			}
			http.Redirect(w, r, "/?"+query.Encode(), http.StatusSeeOther)
			return
		}
	}
	current := v2SetupPathValues(latestConfig(s))
	for _, path := range paths {
		// Unchanged values are not saved again: the path validation refuses a
		// folder that contains the current download folder, itself included.
		if path.Value == "" || filepath.Clean(path.Value) == filepath.Clean(current[path.Key]) {
			continue
		}
		if path.WillCreate {
			if err := os.MkdirAll(path.Value, 0o755); err != nil {
				v2SetupRedirect(w, r, 2, path.Label+": "+err.Error(), true)
				return
			}
		}
		if err := v2SetupSave(s, path.Key, path.Value); err != nil {
			v2SetupRedirect(w, r, 2, path.Label+": "+err.Error(), true)
			return
		}
	}
	v2SetupRedirect(w, r, 3, "Cartelle salvate.", false)
}

// V2SetupSources saves step 3: an optional indexer and the TMDB/TVDB keys.
func V2SetupSources(w http.ResponseWriter, r *http.Request, s *AppState) {
	cfg := latestConfig(s)
	for _, field := range []string{"tmdb_api_key", "tvdb_api_key", "tvdb_pin"} {
		if key := strings.TrimSpace(r.FormValue(field)); key != "" {
			if err := v2SetupSave(s, field, key); err != nil {
				v2SetupRedirect(w, r, 3, err.Error(), true)
				return
			}
		}
	}
	indexerURL := strings.TrimSpace(r.FormValue("indexer_url"))
	if indexerURL != "" {
		parsed, err := url.Parse(indexerURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			v2SetupRedirect(w, r, 3, "Indirizzo dell'indexer non valido: usa http://host:porta.", true)
			return
		}
		manager := r.FormValue("indexer_manager")
		if manager != "jackett" {
			manager = "prowlarr"
		}
		name := strings.TrimSpace(r.FormValue("indexer_name"))
		if name == "" {
			name = manager
		}
		indexers := append([]IndexerConfig{}, cfg.Indexers...)
		indexers = append(indexers, IndexerConfig{Name: name, URL: strings.TrimRight(indexerURL, "/"), APIKey: strings.TrimSpace(r.FormValue("indexer_api_key")), Enabled: true, Manager: manager})
		encoded, err := json.Marshal(indexers)
		if err == nil {
			err = v2SetupSave(s, "indexers", string(encoded))
		}
		if err != nil {
			v2SetupRedirect(w, r, 3, err.Error(), true)
			return
		}
	}
	v2SetupRedirect(w, r, 4, "", false)
}

// V2SetupFinish saves step 5 (start the automatic cycle, leave dry-run) and
// marks the setup as completed; action=skip only marks it.
func V2SetupFinish(w http.ResponseWriter, r *http.Request, s *AppState) {
	if r.FormValue("action") != "skip" {
		active, dryRun := "false", "true"
		if r.FormValue("active") == "on" {
			active = "true"
		}
		if r.FormValue("real_downloads") == "on" {
			dryRun = "false"
		}
		for _, step := range [][2]string{{"dry_run", dryRun}, {"active", active}} {
			if err := v2SetupSave(s, step[0], step[1]); err != nil {
				v2SetupRedirect(w, r, 5, err.Error(), true)
				return
			}
		}
	}
	if err := CompleteSetup(latestConfig(s)); err != nil {
		v2SetupRedirect(w, r, 5, err.Error(), true)
		return
	}
	http.Redirect(w, r, "/?view=dashboard&toast="+url.QueryEscape("Configurazione iniziale completata."), http.StatusSeeOther)
}
