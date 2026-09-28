// Package tui implements the terminal interface for a running gextto daemon.
//
// It only talks to the daemon's HTTP API (never the databases), mirrors the
// classic web interface, and is fully bilingual (Italian and English).
package tui

import (
	"fmt"
	"strings"
)

// Languages supported by the interface.
const (
	LangIT = "it"
	LangEN = "en"
)

// catalog holds every user-facing string as {Italian, English}. Keeping both
// variants side by side makes it obvious when a translation is missing.
var catalog = map[string][2]string{
	"app.title":     {"Gextto TUI", "Gextto TUI"},
	"tab.status":    {"Stato", "Status"},
	"tab.torrents":  {"Torrent", "Torrents"},
	"tab.logs":      {"Log", "Logs"},
	"tab.health":    {"Salute", "Health"},
	"tab.archive":   {"Archivio", "Archive"},
	"tab.missing":   {"Mancanti", "Missing"},
	"tab.blocklist": {"Blocco", "Blocklist"},

	"mode.dryrun":  {"DRY-RUN", "DRY-RUN"},
	"mode.active":  {"ATTIVA", "ACTIVE"},
	"mode.standby": {"IN ATTESA", "STANDBY"},
	"mode.loading": {"CARICAMENTO", "LOADING"},

	"label.mode":        {"Modalità", "Mode"},
	"label.torrents":    {"Torrent", "Torrents"},
	"label.downloading": {"in scarico", "downloading"},
	"label.queued":      {"in coda", "queued"},
	"label.seeding":     {"in seed", "seeding"},
	"label.stalled":     {"in stallo", "stalled"},
	"label.lastcycle":   {"Ultimo ciclo", "Last cycle"},
	"label.scraped":     {"analizzate", "scraped"},
	"label.candidates":  {"candidate", "candidates"},
	"label.downloads":   {"download", "downloads"},
	"label.gaps":        {"gap", "gaps"},
	"label.errors":      {"errori", "errors"},
	"label.seen":        {"Visti nei feed", "Seen in feeds"},
	"label.groups":      {"gruppi", "groups"},
	"label.movies":      {"film", "movies"},
	"label.series":      {"serie", "series"},
	"label.nextcycle":   {"Prossimo ciclo", "Next cycle"},
	"label.version":     {"versione", "version"},
	"label.hash":        {"Hash", "Hash"},
	"label.state":       {"Stato", "State"},
	"label.progress":    {"Progresso", "Progress"},
	"label.done":        {"Scaricato", "Done"},
	"label.down":        {"↓", "↓"},
	"label.up":          {"↑", "↑"},
	"label.name":        {"Nome", "Name"},
	"label.selected":    {"selezionato", "selected"},
	"label.filter":      {"filtro", "filter"},
	"label.sort":        {"ordina", "sort"},
	"label.lines":       {"righe", "lines"},
	"label.follow":      {"segui", "follow"},
	"label.live":        {"LIVE SSE", "LIVE SSE"},
	"label.health":      {"Salute", "Health"},
	"label.paths":       {"Percorsi", "Paths"},
	"label.disks":       {"Dischi", "Disks"},
	"label.ramdisk":     {"RAM disk", "RAM disk"},
	"label.recenterror": {"Ultimi errori", "Recent errors"},
	"label.size":        {"Dimensione", "Size"},
	"label.downloaded":  {"Scaricato", "Downloaded"},
	"label.alltime":     {"Totale storico", "All-time"},
	"label.rates":       {"Velocità", "Rates"},
	"label.peersseeds":  {"Peer / seed", "Peers / seeds"},
	"label.queuepos":    {"Posizione in coda", "Queue position"},
	"label.seedlimit":   {"Limite seed", "Seed limit"},
	"label.metadata":    {"Metadati", "Metadata"},
	"label.version2":    {"Versione torrent", "Torrent version"},
	"label.automanaged": {"Auto-gestito", "Auto-managed"},
	"label.norename":    {"Senza rinomina", "No rename"},
	"label.archived":    {"Archiviato", "Archived"},
	"label.source":      {"Sorgente", "Source"},
	"label.reason":      {"Motivo", "Reason"},
	"label.savepath":    {"Cartella", "Save path"},
	"label.magnet":      {"Magnet", "Magnet"},
	"label.trackers":    {"Tracker", "Trackers"},
	"label.files":       {"File", "Files"},
	"label.peers":       {"Peer", "Peers"},
	"label.none":        {"nessuno", "none"},
	"label.yes":         {"sì", "yes"},
	"label.no":          {"no", "no"},
	"label.infinite":    {"infinito", "infinite"},
	"label.system":      {"Sistema", "System"},
	"label.daemon":      {"Gextto", "Gextto"},
	"label.uptime":      {"uptime", "uptime"},
	"label.cpu":         {"CPU", "CPU"},
	"label.ram":         {"RAM", "RAM"},
	"label.free":        {"liberi", "free"},
	"label.used":        {"usati", "used"},
	"label.trash":       {"Cestino", "Trash"},
	"label.writable":    {"scrivibile", "writable"},

	"hint.global":    {"q esci · ? aiuto · r aggiorna · a magnet/URL · t file · c ciclo · s cerca · e eventi", "q quit · ? help · r refresh · a magnet/URL · t file · c cycle · s search · e events"},
	"hint.tabs":      {"Tab/1-7", "Tab/1-7"},
	"hint.torrents":  {"↑↓/PgUp/PgDn seleziona · Invio dettagli · p pausa/riprendi · b riavvia · d rimuovi · X pulisci completati · k verifica · R riannuncia · n senza-rinomina · i/u pin · L limiti · o ordina · F filtro", "↑↓/PgUp/PgDn select · Enter details · p pause/resume · b restart · d remove · X clean completed · k recheck · R reannounce · n no-rename · i/u pin · L limits · o sort · F filter"},
	"hint.logs":      {"↑↓/PgUp/PgDn scorri · / filtro · f segui/ferma · Home/End", "↑↓/PgUp/PgDn scroll · / filter · f follow/pause · Home/End"},
	"hint.health":    {"x svuota cestino", "x empty trash"},
	"hint.archive":   {"↑↓ seleziona · Invio accoda · / filtro", "↑↓ select · Enter queue · / filter"},
	"hint.missing":   {"↑↓ seleziona · r aggiorna", "↑↓ select · r refresh"},
	"hint.blocklist": {"↑↓ seleziona · d rimuovi", "↑↓ select · d remove"},
	"hint.details":   {"1 generale · 2 tracker · 3 file · 4 peer · ↑↓ scorri · Invio/Esc indietro", "1 general · 2 trackers · 3 files · 4 peers · ↑↓ scroll · Enter/Esc back"},

	"prompt.cycle":           {"Ciclo [full/series/movies/comics] (full): ", "Cycle [full/series/movies/comics] (full): "},
	"prompt.search":          {"Cerca: ", "Search: "},
	"prompt.magnet":          {"Magnet/URL: ", "Magnet/URL: "},
	"prompt.file":            {"File .torrent: ", "File .torrent: "},
	"prompt.limits":          {"Limiti globali DL UL KiB/s (0 0): ", "Global DL UL limits KiB/s (0 0): "},
	"prompt.logfilter":       {"Filtro log (vuoto=tutti): ", "Log filter (empty=all): "},
	"prompt.torrentfilter":   {"Filtro torrent (vuoto=tutti): ", "Torrent filter (empty=all): "},
	"prompt.archivefilter":   {"Filtro archivio (vuoto=tutto): ", "Archive filter (empty=all): "},
	"prompt.remove":          {"Rimuovo '%s'? [s/N] ", "Remove '%s'? [y/N] "},
	"prompt.removefiles":     {"Rimuovo '%s' e i file scaricati? [s/N] ", "Remove '%s' and its files? [y/N] "},
	"prompt.deletefiles":     {"Elimino anche i file scaricati? [s/N] ", "Also delete downloaded files? [y/N] "},
	"prompt.cleantrash":      {"Svuoto ora il cestino? [s/N] ", "Empty the trash now? [y/N] "},
	"prompt.cleancomp":       {"Rimuovo i completati che hanno raggiunto il limite di seed? [s/N] ", "Remove completed torrents that reached seed limits? [y/N] "},
	"prompt.blocklistremove": {"Rimuovo '%s' dalla blocklist? [s/N] ", "Remove '%s' from the blocklist? [y/N] "},

	"msg.refreshed":             {"aggiornato", "refreshed"},
	"msg.cyclefull":             {"ciclo completo avviato", "full cycle started"},
	"msg.cyclequeued":           {"ciclo %s in coda (un ciclo è in corso)", "%s cycle queued (a cycle is running)"},
	"msg.cyclestarted":          {"ciclo %s avviato", "%s cycle started"},
	"msg.cycleinvalid":          {"dominio ciclo non valido", "invalid cycle domain"},
	"msg.cyclefailed":           {"ciclo non riuscito: %s", "cycle failed: %s"},
	"msg.notorrent":             {"nessun torrent selezionato", "no torrent selected"},
	"msg.paused":                {"in pausa", "paused"},
	"msg.resumed":               {"ripreso", "resumed"},
	"msg.restarted":             {"riavvio richiesto", "restart requested"},
	"msg.pinned":                {"torrent fissato", "torrent pinned"},
	"msg.unpinned":              {"torrent non più fissato", "torrent unpinned"},
	"msg.rechecked":             {"verifica richiesta", "recheck requested"},
	"msg.reannounced":           {"riannuncio richiesto", "reannounce requested"},
	"msg.norenameon":            {"senza-rinomina attivo", "no-rename on"},
	"msg.norenameoff":           {"senza-rinomina disattivo", "no-rename off"},
	"msg.removed":               {"rimosso", "removed"},
	"msg.removedfiles":          {"rimosso con i file", "removed with files"},
	"msg.removedone":            {"completati rimossi: %d, saltati: %d", "completed removed: %d, skipped: %d"},
	"msg.limits":                {"limiti globali impostati: %d/%d KiB/s", "global limits set: %d/%d KiB/s"},
	"msg.limitsinvalid":         {"i limiti devono essere numeri KiB/s", "limits must be KiB/s numbers"},
	"msg.twoValues":             {"inserisci due valori: download upload", "enter two values: download upload"},
	"msg.search":                {"ricerca: %d risultati", "search: %d results"},
	"msg.searchfailed":          {"ricerca non riuscita: %s", "search failed: %s"},
	"msg.queued":                {"risultato accodato", "search result queued"},
	"msg.queuefailed":           {"accodamento non riuscito: %s", "queue failed: %s"},
	"msg.nosearch":              {"nessun risultato selezionato", "no search result selected"},
	"msg.events":                {"eventi: %d", "events: %d"},
	"msg.eventsfailed":          {"eventi non disponibili: %s", "events failed: %s"},
	"msg.magneton":              {"magnet/URL aggiunto", "magnet/URL added"},
	"msg.magnetinvalid":         {"non è un magnet o URL torrent", "not a magnet or torrent URL"},
	"msg.addfailed":             {"aggiunta non riuscita: %s", "add failed: %s"},
	"msg.fileerror":             {"errore file: %s", "file error: %s"},
	"msg.fileempty":             {"file vuoto", "empty file"},
	"msg.torrentadded":          {"torrent aggiunto", "torrent added"},
	"msg.trashcleaned":          {"cestino svuotato (%d file)", "trash cleaned (%d files)"},
	"msg.trashcleanedna":        {"cestino svuotato", "trash cleaned"},
	"msg.trashfailed":           {"svuotamento cestino non riuscito: %s", "trash cleanup failed: %s"},
	"msg.detailsfailed":         {"dettagli non disponibili: %s", "details failed: %s"},
	"msg.detailfailed":          {"%s non disponibili: %s", "%s failed: %s"},
	"msg.actionfailed":          {"%s non riuscito: %s", "%s failed: %s"},
	"msg.offline":               {"impossibile raggiungere il daemon: %s", "cannot reach the daemon: %s"},
	"msg.sethint":               {"imposta GEXTTO_URL / GEXTTO_API_TOKEN", "set GEXTTO_URL / GEXTTO_API_TOKEN"},
	"msg.loading":               {"caricamento…", "loading…"},
	"msg.emptylogs":             {"nessun log da mostrare", "no logs to show"},
	"msg.emptytorrents":         {"nessun torrent nella sessione", "no torrents in the session"},
	"msg.emptyarchive":          {"archivio vuoto", "archive is empty"},
	"msg.emptymissing":          {"nessun elemento mancante", "no missing items"},
	"msg.emptyblocklist":        {"blocklist vuota", "blocklist is empty"},
	"msg.termtoolsmall":         {"terminale troppo piccolo", "terminal too small"},
	"msg.confirmed":             {"confermato", "confirmed"},
	"msg.cancelled":             {"annullato", "cancelled"},
	"msg.busy":                  {"un'azione è già in corso", "an action is already running"},
	"msg.archivefailed":         {"archivio non disponibile: %s", "archive unavailable: %s"},
	"msg.missingfailed":         {"mancanti non disponibili: %s", "missing items unavailable: %s"},
	"msg.blocklistfailed":       {"blocklist non disponibile: %s", "blocklist unavailable: %s"},
	"msg.blocklistremovefailed": {"rimozione blocklist non riuscita: %s", "blocklist removal failed: %s"},
	"msg.blocklistremoved":      {"voce rimossa dalla blocklist", "blocklist entry removed"},

	"help.title":     {"Gextto TUI — comandi da tastiera", "Gextto TUI — keyboard help"},
	"help.close":     {"Premi Esc, Invio o ? per chiudere", "Press Esc, Enter or ? to close"},
	"help.global":    {"Globali:  1-7/Tab schede · r aggiorna · ? chiudi aiuto · q esci", "Global:  1-7/Tab tabs · r refresh · ? close help · q quit"},
	"help.global2":   {"          a magnet/URL · t file .torrent · c ciclo · s cerca · e eventi", "          a magnet/URL · t .torrent file · c cycle · s search · e events"},
	"help.torrents":  {"Torrent:  ↑↓ o PgUp/PgDn · Home/End · Invio dettagli", "Torrents: ↑↓ or PgUp/PgDn · Home/End · Enter details"},
	"help.torrents2": {"          p pausa/riprendi · b riavvia · d rimuovi · X pulisci completati", "          p pause/resume · b restart · d remove · X clean completed"},
	"help.torrents3": {"          k verifica · R riannuncia · n senza-rinomina · i/u pin · L limiti", "          k recheck · R reannounce · n no-rename · i/u pin · L limits"},
	"help.torrents4": {"          o cambia ordinamento · F filtro torrent", "          o cycle sort · F torrent filter"},
	"help.details":   {"Dettagli: 1 generale · 2 tracker · 3 file · 4 peer · ↑↓ scorri", "Details:  1 general · 2 trackers · 3 files · 4 peers · ↑↓ scroll"},
	"help.logs":      {"Log:      ↑↓ o PgUp/PgDn · Home/End · / filtro · f segui/ferma", "Logs:     ↑↓ or PgUp/PgDn · Home/End · / filter · f follow/pause"},
	"help.health":    {"Salute:   x svuota cestino (con conferma)", "Health:   x empty trash (confirmation required)"},
	"help.archive":   {"Archivio: ↑↓ seleziona · Invio accoda · / filtra", "Archive: ↑↓ select · Enter queue · / filter"},
	"help.missing":   {"Mancanti: ↑↓ seleziona · r aggiorna", "Missing: ↑↓ select · r refresh"},
	"help.blocklist": {"Blocco:   ↑↓ seleziona · d rimuovi", "Blocklist: ↑↓ select · d remove"},

	"sort.name":     {"nome", "name"},
	"sort.progress": {"progresso", "progress"},
	"sort.state":    {"stato", "state"},
	"sort.rate":     {"velocità", "rate"},
	"sort.size":     {"dimensione", "size"},

	"state.downloading":          {"In scarico", "Downloading"},
	"state.downloading_metadata": {"Metadati", "Metadata"},
	"state.stalled":              {"In attesa di seed", "Stalled"},
	"state.seeding":              {"In seed", "Seeding"},
	"state.finished":             {"Completato", "Finished"},
	"state.checking_files":       {"Verifica file", "Checking files"},
	"state.checking_resume_data": {"Ripristino", "Restoring"},
	"state.queued":               {"In coda", "Queued"},
	"state.paused":               {"In pausa", "Paused"},
	"state.moving":               {"Spostamento", "Moving"},
	"state.error":                {"Errore", "Error"},
	"state.unknown":              {"Sconosciuto", "Unknown"},
}

// Translator returns localized strings for one language.
type Translator struct {
	lang string
	idx  int
}

// NewTranslator builds a translator. Unknown languages fall back to Italian,
// then to English, matching the daemon default.
func NewTranslator(lang string) *Translator {
	switch normalizeLang(lang) {
	case LangEN:
		return &Translator{lang: LangEN, idx: 1}
	default:
		return &Translator{lang: LangIT, idx: 0}
	}
}

func normalizeLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(lang, "en"):
		return LangEN
	case strings.HasPrefix(lang, "it"):
		return LangIT
	default:
		return lang
	}
}

// Lang is the resolved language code ("it" or "en").
func (t *Translator) Lang() string {
	if t == nil {
		return LangIT
	}
	return t.lang
}

// T returns the string for a key, falling back to English then to the key.
func (t *Translator) T(key string) string {
	entry, ok := catalog[key]
	if !ok {
		return key
	}
	idx := 0
	if t != nil {
		idx = t.idx
	}
	if entry[idx] != "" {
		return entry[idx]
	}
	if entry[1-idx] != "" {
		return entry[1-idx]
	}
	return key
}

// Format returns a formatted string (fmt.Sprintf) for a key.
func (t *Translator) Format(key string, args ...any) string {
	return fmt.Sprintf(t.T(key), args...)
}

// StateLabel maps a daemon torrent state to a localized label.
func (t *Translator) StateLabel(state string) string {
	key := "state." + strings.ToLower(strings.TrimSpace(state))
	if _, ok := catalog[key]; ok {
		return t.T(key)
	}
	return state
}

// CatalogKeys returns every known key (used by tests to check completeness).
func CatalogKeys() []string {
	keys := make([]string, 0, len(catalog))
	for key := range catalog {
		keys = append(keys, key)
	}
	return keys
}

// ResolveLang chooses the interface language: explicit wins, then the
// environment, then the daemon language, then Italian.
func ResolveLang(explicit, env, daemon string) string {
	if value := normalizeLang(explicit); value != "" {
		return value
	}
	if value := normalizeLang(env); value != "" {
		return value
	}
	if value := normalizeLang(daemon); value != "" && value != "unknown" {
		return value
	}
	return LangIT
}
