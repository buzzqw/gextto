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
	"app.title":       {"Gextto TUI", "Gextto TUI"},
	"tab.status":      {"Stato", "Status"},
	"tab.torrents":    {"Torrent", "Torrents"},
	"tab.logs":        {"Log", "Logs"},
	"tab.health":      {"Salute", "Health"},
	"tab.archive":     {"Archivio", "Archive"},
	"tab.missing":     {"Mancanti", "Missing"},
	"tab.blocklist":   {"Blocco", "Blocklist"},
	"tab.library":     {"Libreria", "Library"},
	"tab.maintenance": {"Manutenzione", "Maintenance"},

	"mode.dryrun":  {"DRY-RUN", "DRY-RUN"},
	"mode.active":  {"ATTIVA", "ACTIVE"},
	"mode.standby": {"IN ATTESA", "STANDBY"},
	"mode.loading": {"CARICAMENTO", "LOADING"},

	"label.mode":          {"Modalità", "Mode"},
	"label.torrents":      {"Torrent", "Torrents"},
	"label.downloading":   {"in scarico", "downloading"},
	"label.queued":        {"in coda", "queued"},
	"label.seeding":       {"in seed", "seeding"},
	"label.stalled":       {"in stallo", "stalled"},
	"label.lastcycle":     {"Ultimo ciclo", "Last cycle"},
	"label.scraped":       {"analizzate", "scraped"},
	"label.candidates":    {"candidate", "candidates"},
	"label.downloads":     {"download", "downloads"},
	"label.http":          {"HTTP", "HTTP"},
	"label.gaps":          {"gap", "gaps"},
	"label.errors":        {"errori", "errors"},
	"label.seen":          {"Visti nei feed", "Seen in feeds"},
	"label.groups":        {"gruppi", "groups"},
	"label.movies":        {"film", "movies"},
	"label.series":        {"serie", "series"},
	"label.nextcycle":     {"Prossimo ciclo", "Next cycle"},
	"label.online":        {"ONLINE", "ONLINE"},
	"label.offline":       {"OFFLINE", "OFFLINE"},
	"label.connecting":    {"CONNESSIONE…", "CONNECTING…"},
	"label.version":       {"versione", "version"},
	"label.hash":          {"Hash", "Hash"},
	"label.state":         {"Stato", "State"},
	"label.progress":      {"Progresso", "Progress"},
	"label.done":          {"Scaricato", "Done"},
	"label.down":          {"↓", "↓"},
	"label.up":            {"↑", "↑"},
	"label.name":          {"Nome", "Name"},
	"label.selected":      {"selezionato", "selected"},
	"label.filter":        {"filtro", "filter"},
	"label.sort":          {"ordina", "sort"},
	"label.page":          {"pagina", "page"},
	"label.archiveid":     {"ID archivio", "Archive ID"},
	"label.quality":       {"Qualità", "Quality"},
	"label.added":         {"Aggiunto", "Added"},
	"label.reconnect":     {"riconnessione", "reconnecting"},
	"label.lines":         {"righe", "lines"},
	"label.follow":        {"segui", "follow"},
	"label.live":          {"LIVE SSE", "LIVE SSE"},
	"label.health":        {"Salute", "Health"},
	"label.paths":         {"Percorsi", "Paths"},
	"label.disks":         {"Dischi", "Disks"},
	"label.ramdisk":       {"RAM disk", "RAM disk"},
	"label.recenterror":   {"Ultimi errori", "Recent errors"},
	"label.size":          {"Dimensione", "Size"},
	"label.downloaded":    {"Scaricato", "Downloaded"},
	"label.alltime":       {"Totale storico", "All-time"},
	"label.rates":         {"Velocità", "Rates"},
	"label.transfer":      {"Trasferimento", "Transfer"},
	"label.consumption":   {"Consumi", "Consumption"},
	"label.total":         {"totale", "total"},
	"label.trend":         {"Tendenza", "Trend"},
	"label.consumption7d": {"Consumo 7g", "7d consumption"},
	"label.peersseeds":    {"Peer / seed", "Peers / seeds"},
	"label.queuepos":      {"Posizione in coda", "Queue position"},
	"label.seedlimit":     {"Limite seed", "Seed limit"},
	"label.metadata":      {"Metadati", "Metadata"},
	"label.version2":      {"Versione torrent", "Torrent version"},
	"label.automanaged":   {"Auto-gestito", "Auto-managed"},
	"label.norename":      {"Senza rinomina", "No rename"},
	"label.archived":      {"Archiviato", "Archived"},
	"label.source":        {"Sorgente", "Source"},
	"label.reason":        {"Motivo", "Reason"},
	"label.savepath":      {"Cartella", "Save path"},
	"label.magnet":        {"Magnet", "Magnet"},
	"label.trackers":      {"Tracker", "Trackers"},
	"label.files":         {"File", "Files"},
	"label.peers":         {"Peer", "Peers"},
	"label.none":          {"nessuno", "none"},
	"label.yes":           {"sì", "yes"},
	"label.no":            {"no", "no"},
	"label.infinite":      {"infinito", "infinite"},
	"label.unlimited":     {"illimitato", "unlimited"},
	"label.system":        {"Sistema", "System"},
	"label.daemon":        {"Gextto", "Gextto"},
	"label.uptime":        {"uptime", "uptime"},
	"label.cpu":           {"CPU", "CPU"},
	"label.ram":           {"RAM", "RAM"},
	"label.free":          {"liberi", "free"},
	"label.used":          {"usati", "used"},
	"label.trash":         {"Cestino", "Trash"},
	"label.writable":      {"scrivibile", "writable"},
	"label.enabled":       {"ON", "ON"},
	"label.disabled":      {"OFF", "OFF"},

	"hint.global":   {"q esci · ? aiuto · r aggiorna · a magnet/URL · t file · c ciclo · s cerca · l libreria · e eventi · g impostazioni", "q quit · ? help · r refresh · a magnet/URL · t file · c cycle · s search · l library · e events · g settings"},
	"hint.tabs":     {"Tab/1-9", "Tab/1-9"},
	"hint.torrents": {"↑↓/PgUp/PgDn seleziona · Invio dettagli · p pausa/riprendi · b riavvia · d rimuovi · X pulisci completati · k verifica · R riannuncia · n senza-rinomina · i/u pin · L limiti · T limite temp. · Spazio seleziona · # tag · A auto-rimozione · H storico · o ordina · F filtro", "↑↓/PgUp/PgDn select · Enter details · p pause/resume · b restart · d remove · X clean completed · k recheck · R reannounce · n no-rename · i/u pin · L limits · T temp limit · Space mark · # tag · A auto-remove · H history · o sort · F filter"},
	"hint.logs":     {"↑↓/PgUp/PgDn scorri · / filtro · f segui/ferma · Home/End", "↑↓/PgUp/PgDn scroll · / filter · f follow/pause · Home/End"},
	"hint.health":   {"↑↓ scorri · x svuota cestino", "↑↓ scroll · x empty trash"},
	"hint.status":   {"↑↓ scorri · 3 log completo", "↑↓ scroll · 3 full log"},

	"status.daemon":               {"Daemon", "Daemon"},
	"status.system":               {"Sistema", "System"},
	"status.cycle":                {"Ciclo", "Cycle"},
	"status.feeds":                {"Feed", "Feeds"},
	"status.downloads":            {"Download", "Downloads"},
	"status.transfer":             {"Traffico", "Traffic"},
	"status.consumption":          {"Consumi", "Usage"},
	"status.attention":            {"Attenzione", "Attention"},
	"status.active":               {"In corso", "Active"},
	"status.transferring":         {"%d attivi", "%d active"},
	"status.idle":                 {"%d fermi a 0 B/s", "%d stuck at 0 B/s"},
	"status.waiting":              {"%d in coda/pausa", "%d queued/paused"},
	"status.done":                 {"%d completati", "%d completed"},
	"status.idlelist":             {"Bloccati", "Stuck"},
	"status.recentlogs":           {"Log recenti", "Recent log"},
	"status.nologs":               {"nessuna riga di log ancora", "no log lines yet"},
	"status.noactive":             {"nessun trasferimento in corso", "nothing is transferring"},
	"status.allgood":              {"nessun problema rilevato", "no problems detected"},
	"status.torrenterrs":          {"%d torrent in errore", "%d torrents in error"},
	"status.stalled":              {"%d in stallo", "%d stalled"},
	"status.badpaths":             {"%d percorsi non scrivibili", "%d paths not writable"},
	"status.diskfull":             {"disco pieno al %.0f%%", "disk %.0f%% full"},
	"status.cycleerrors":          {"%d errori nell'ultimo ciclo", "%d errors in the last cycle"},
	"status.lasterror":            {"ultimo errore: %s", "last error: %s"},
	"status.health":               {"salute %s", "health %s"},
	"status.offline":              {"daemon non raggiungibile: %s", "daemon unreachable: %s"},
	"status.next":                 {"prossimo tra %s", "next in %s"},
	"status.running":              {"in esecuzione o in attesa", "running or due"},
	"status.laststart":            {"ultimo avvio %s (%s fa)", "last run %s (%s ago)"},
	"status.uptime":               {"avviato da %s", "up %s"},
	"status.pid":                  {"PID %d", "PID %d"},
	"status.load":                 {"carico %.2f", "load %.2f"},
	"status.ramfree":              {"RAM %s liberi / %s", "RAM %s free / %s"},
	"status.disk":                 {"disco %s liberi / %s (%.0f%% usato)", "disk %s free / %s (%.0f%% used)"},
	"status.trash":                {"cestino %d file (%s)", "trash %d files (%s)"},
	"status.torrentcount":         {"%d torrent", "%d torrents"},
	"status.eta":                  {"ETA %s", "ETA %s"},
	"status.more":                 {"↓ altre %d righe · ↑↓ scorri", "↓ %d more lines · ↑↓ scroll"},
	"hint.archive":                {"↑↓ seleziona · PgUp/PgDn pagina · Invio accoda · d dettagli · o/O ordina · s cerca", "↑↓ select · PgUp/PgDn page · Enter queue · d details · o/O sort · s search"},
	"hint.archivedetail":          {"↑↓ scorri · a accoda · Invio/Esc indietro", "↑↓ scroll · a queue · Enter/Esc back"},
	"hint.missing":                {"↑↓ seleziona · Invio apri serie · s cerca · i ignora · r aggiorna", "↑↓ select · Enter open series · s search · i ignore · r refresh"},
	"hint.blocklist":              {"↑↓ seleziona · d rimuovi", "↑↓ select · d remove"},
	"hint.library":                {"1 serie · 2 film · 3 fumetti · ↑↓ seleziona · s filtra · Esc indietro", "1 series · 2 movies · 3 comics · ↑↓ select · s filter · Esc back"},
	"library.series":              {"Serie TV", "TV series"},
	"library.movies":              {"Film", "Movies"},
	"library.comics":              {"Fumetti", "Comics"},
	"bulk.pause":                  {"pausa", "pause"},
	"bulk.resume":                 {"ripresa", "resume"},
	"bulk.recheck":                {"verifica", "recheck"},
	"bulk.remove":                 {"rimozione", "remove"},
	"bulk.removefiles":            {"rimozione con file", "remove with files"},
	"hint.torrentsmarked":         {"selezionati: p pausa · P riprendi · k verifica · d rimuovi · D rimuovi con file · # tag · * tutti/nessuno · Esc deseleziona", "marked: p pause · P resume · k recheck · d remove · D remove with files · # tag · * all/none · Esc clear"},
	"hint.detailfiles":            {"↑↓ file · Spazio salta/scarica · +/- priorità · 1-4 viste · Esc indietro", "↑↓ file · Space skip/download · +/- priority · 1-4 views · Esc back"},
	"hint.detailtrackers":         {"↑↓ tracker · a aggiungi · d rimuovi · 1-4 viste · Esc indietro", "↑↓ tracker · a add · d remove · 1-4 views · Esc back"},
	"hint.history":                {"↑↓ scorri · / filtra · r aggiorna · Esc chiudi", "↑↓ scroll · / filter · r refresh · Esc close"},
	"hint.maintenance":            {"↑↓ seleziona · Invio esegui (con conferma per le operazioni pesanti)", "↑↓ select · Enter run (confirmation for heavy operations)"},
	"hint.report":                 {"↑↓ scorri · Spazio includi/escludi · x applica · Esc chiudi", "↑↓ scroll · Space include/exclude · x apply · Esc close"},
	"history.title":               {"Storico download: %d di %d", "Download history: %d of %d"},
	"history.empty":               {"nessun download nello storico", "no download in the history"},
	"label.autoremove":            {"auto-rimozione completati", "auto-remove completed"},
	"label.marked":                {"%d selezionati", "%d marked"},
	"label.superseeding":          {"Super-seeding", "Super-seeding"},
	"label.tag":                   {"Tag", "Tag"},
	"label.torrentlimits":         {"Limiti torrent", "Torrent limits"},
	"maint.title":                 {"Manutenzione", "Maintenance"},
	"maint.job":                   {"Job in corso: %s (Invio annulla)", "Running job: %s (Enter cancels)"},
	"maint.backup":                {"Crea backup", "Create backup"},
	"maint.dbcheck":               {"Verifica database", "Check databases"},
	"maint.vacuum":                {"Compatta database (VACUUM)", "Compact databases (VACUUM)"},
	"maint.analyze":               {"Aggiorna statistiche (ANALYZE)", "Update statistics (ANALYZE)"},
	"maint.duplicates":            {"Duplicati inferiori (anteprima)", "Inferior duplicates (preview)"},
	"maint.renameall":             {"Rinomina tutte le serie", "Rename all series"},
	"maint.folderrename":          {"Rinomina una cartella…", "Rename a folder…"},
	"maint.scanarchives":          {"Scansiona tutti gli archivi", "Scan all archives"},
	"maint.backfill":              {"Aggiorna MediaInfo", "Refresh MediaInfo"},
	"maint.housekeeping":          {"Pulizia database ora", "Database housekeeping now"},
	"maint.ramdisk":               {"RAM disk", "RAM disk"},
	"maint.trash":                 {"Svuota cestino", "Empty trash"},
	"maint.lastbackup":            {"ultimo %s · %d backup", "last %s · %d backups"},
	"maint.nobackup":              {"nessun backup", "no backup"},
	"maint.dbsize":                {"%d file · %s", "%d files · %s"},
	"maint.ramdiskoff":            {"non configurato", "not configured"},
	"maint.dbresult":              {"prima %s, ora %s", "before %s, now %s"},
	"maint.done":                  {"completato", "done"},
	"maint.duptitle":              {"Duplicati inferiori: %d (x elimina)", "Inferior duplicates: %d (x deletes)"},
	"maint.noduplicates":          {"nessun duplicato inferiore", "no inferior duplicate"},
	"maint.foldertitle":           {"Rinomina %s: %d file (Spazio includi, x applica)", "Rename %s: %d files (Space include, x apply)"},
	"maint.folderempty":           {"nessun file da rinominare", "no file to rename"},
	"msg.autoremoveon":            {"auto-rimozione dei completati attiva", "auto-removal of completed torrents on"},
	"msg.autoremoveoff":           {"auto-rimozione dei completati disattiva", "auto-removal of completed torrents off"},
	"msg.backupdone":              {"backup creato: %s (%s)", "backup created: %s (%s)"},
	"msg.bulkdone":                {"%s: %d/%d torrent", "%s: %d/%d torrents"},
	"msg.duplicatesremoved":       {"duplicati eliminati: %d", "duplicates removed: %d"},
	"msg.folderrenamed":           {"file rinominati: %d/%d", "files renamed: %d/%d"},
	"msg.jobcancelled":            {"annullamento richiesto", "cancellation requested"},
	"msg.jobstarted":              {"avviato: %s", "started: %s"},
	"msg.loadingsettings":         {"impostazioni non ancora caricate, riprova", "settings not loaded yet, try again"},
	"msg.maintfailed":             {"%s non riuscito: %s", "%s failed: %s"},
	"msg.markedfailed":            {"torrent segnato come fallito e rimosso", "torrent marked as failed and removed"},
	"msg.marksclear":              {"selezione annullata", "marks cleared"},
	"msg.moving":                  {"spostamento verso %s", "moving to %s"},
	"msg.nothingselected":         {"nessun elemento selezionato", "nothing selected"},
	"msg.prioritysaved":           {"priorità salvata", "priority saved"},
	"msg.ramdiskset":              {"RAM disk: %s", "RAM disk: %s"},
	"msg.superseedingon":          {"super-seeding attivo", "super-seeding on"},
	"msg.superseedingoff":         {"super-seeding disattivo", "super-seeding off"},
	"msg.tagremoved":              {"tag rimosso da %d torrent", "tag removed from %d torrents"},
	"msg.tagset":                  {"tag '%s' su %d torrent", "tag '%s' on %d torrents"},
	"msg.torrentlimits":           {"limiti torrent ↓%s ↑%s", "torrent limits ↓%s ↑%s"},
	"msg.torrentlimitsinvalid":    {"formato: DL UL [ratio] [giorni], KiB/s, 0 = illimitato", "format: DL UL [ratio] [days], KiB/s, 0 = unlimited"},
	"msg.trackerexists":           {"tracker già presente", "tracker already present"},
	"msg.trackerinvalid":          {"URL tracker non valido", "invalid tracker URL"},
	"msg.trackerssaved":           {"tracker salvati: %d", "trackers saved: %d"},
	"priority.skip":               {"salta", "skip"},
	"priority.low":                {"bassa", "low"},
	"priority.normal":             {"normale", "normal"},
	"priority.high":               {"alta", "high"},
	"priority.top":                {"massima", "top"},
	"prompt.addtracker":           {"URL tracker: ", "Tracker URL: "},
	"prompt.bulkremove":           {"Rimuovo %d torrent (i file restano)? [s/N] ", "Remove %d torrents (files kept)? [y/N] "},
	"prompt.bulkremovefiles":      {"Rimuovo %d torrent E i loro file? [s/N] ", "Remove %d torrents AND their files? [y/N] "},
	"prompt.canceljob":            {"Annullo il job %s? [s/N] ", "Cancel job %s? [y/N] "},
	"prompt.duplicates":           {"Elimino %d file duplicati? [s/N] ", "Delete %d duplicate files? [y/N] "},
	"prompt.folderrename":         {"Cartella da rinominare: ", "Folder to rename: "},
	"prompt.historyfilter":        {"Filtro storico: ", "History filter: "},
	"prompt.maint.housekeeping":   {"Eseguo ora la pulizia del database? [s/N] ", "Run database housekeeping now? [y/N] "},
	"prompt.maint.renameall":      {"Rinomino i file di tutte le serie? [s/N] ", "Rename the files of all series? [y/N] "},
	"prompt.maint.scanarchives":   {"Scansiono tutti gli archivi? [s/N] ", "Scan all archives? [y/N] "},
	"prompt.maint.vacuum":         {"Compatto i database (VACUUM, può richiedere tempo)? [s/N] ", "Compact the databases (VACUUM, may take a while)? [y/N] "},
	"prompt.markfailed":           {"Segno '%s' come fallito (blocklist + rimozione)? [s/N] ", "Mark '%s' as failed (blocklist + remove)? [y/N] "},
	"prompt.movestorage":          {"Sposta in: ", "Move to: "},
	"prompt.ramdisk":              {"Cartella RAM disk: ", "RAM disk folder: "},
	"prompt.tag":                  {"Tag (esistenti: %s; vuoto = nessuno): ", "Tag (existing: %s; empty = none): "},
	"prompt.tagbulk":              {"Tag per %d torrent (esistenti: %s; vuoto = nessuno): ", "Tag for %d torrents (existing: %s; empty = none): "},
	"prompt.torrentlimits":        {"Limiti torrent DL UL KiB/s [ratio] [giorni] (0 = illimitato): ", "Torrent limits DL UL KiB/s [ratio] [days] (0 = unlimited): "},
	"prompt.trackerremove":        {"Rimuovo il tracker %s? [s/N] ", "Remove tracker %s? [y/N] "},
	"prompt.folderapply":          {"Rinomino %d file? [s/N] ", "Rename %d files? [y/N] "},
	"library.switch":              {"(1-3 o ←→ per cambiare)", "(1-3 or ←→ to switch)"},
	"hint.librarymanage":          {"Invio dettagli · a aggiungi (TMDB) · e modifica · p pausa/riprendi · d elimina · m cerca · s filtra · 1 serie · 2 film · 3 fumetti · Esc indietro", "Enter details · a add (TMDB) · e edit · p pause/resume · d delete · m search · s filter · 1 series · 2 movies · 3 comics · Esc back"},
	"hint.series":                 {"←→ stagione · Spazio attiva/disattiva stagione · Invio sorgenti · s cerca online · i ignora · R riscarica · m cerca mancanti · e modifica · n rinomina · M metadati · p pausa · d elimina · Esc indietro", "←→ season · Space season on/off · Enter sources · s search online · i ignore · R redownload · m search missing · e edit · n rename · M metadata · p pause · d delete · Esc back"},
	"hint.movie":                  {"↑↓ archivio · Invio accoda · s cerca online · e modifica · R riscarica · p pausa · d elimina · y copia magnet · Esc indietro", "↑↓ archive · Enter queue · s search online · e edit · R redownload · p pause · d delete · y copy magnet · Esc back"},
	"hint.form":                   {"↑↓ campo · Invio modifica (sì/no: Invio o Spazio) · s salva · Esc annulla", "↑↓ field · Enter edit (yes/no: Enter or Space) · s save · Esc cancel"},
	"hint.tmdb":                   {"↑↓ seleziona · Invio aggiungi alla libreria · Esc chiudi", "↑↓ select · Enter add to library · Esc close"},
	"hint.search":                 {"↑↓ seleziona · Invio accoda · y copia magnet · Esc chiudi", "↑↓ select · Enter queue · y copy magnet · Esc close"},
	"library.active":              {"attiva", "active"},
	"library.paused":              {"in pausa", "paused"},
	"library.episodes":            {"%d/%d ep.", "%d/%d ep."},
	"library.last":                {"ultimo %s", "last %s"},
	"label.score":                 {"punti %d", "score %d"},
	"label.seeders":               {"%d seed", "%d seeds"},
	"label.vote":                  {"voto %.1f", "rating %.1f"},
	"label.inlibrary":             {"già in libreria", "already in library"},
	"label.seasonsfield":          {"stagioni %s", "seasons %s"},
	"series.seasons":              {"Stagioni", "Seasons"},
	"series.off":                  {"off", "off"},
	"series.noseasons":            {"nessuna stagione nota: M aggiorna i metadati da TMDB", "no known seasons: M refreshes the metadata from TMDB"},
	"series.noepisodes":           {"nessun episodio noto per questa stagione", "no known episodes in this season"},
	"series.seasonoff":            {"Stagione %d disattivata: Spazio per riattivarla", "Season %d is switched off: Space switches it on"},
	"series.seasonexcluded":       {"Stagione %d esclusa dal campo Stagioni (%s): e per modificarlo", "Season %d is outside the Seasons field (%s): e to edit it"},
	"episode.missing":             {"mancante", "missing"},
	"episode.ignored":             {"ignorato", "ignored"},
	"episode.upcoming":            {"in uscita", "upcoming"},
	"movie.history":               {"Scaricato", "Downloaded"},
	"movie.nohistory":             {"non ancora scaricato", "not downloaded yet"},
	"movie.matches":               {"In archivio: %d (Invio accoda)", "In the archive: %d (Enter queues)"},
	"movie.nomatches":             {"nessuna release in archivio: s cerca online", "no release in the archive: s searches online"},
	"form.seriesedit":             {"Modifica serie: %s", "Edit series: %s"},
	"form.movieedit":              {"Modifica film: %s", "Edit movie: %s"},
	"form.seriesadd":              {"Aggiungi serie: %s", "Add series: %s"},
	"form.movieadd":               {"Aggiungi film: %s", "Add movie: %s"},
	"field.name":                  {"Nome", "Name"},
	"field.year":                  {"Anno", "Year"},
	"field.seasons":               {"Stagioni", "Seasons"},
	"field.quality":               {"Qualità", "Quality"},
	"field.language":              {"Lingua", "Language"},
	"field.subtitle":              {"Sottotitoli", "Subtitles"},
	"field.exclude":               {"Escludi", "Exclude"},
	"field.aliases":               {"Alias", "Aliases"},
	"field.archive_path":          {"Cartella archivio", "Archive folder"},
	"field.tmdb_id":               {"TMDB ID", "TMDB ID"},
	"field.tvdb_id":               {"TVDB ID", "TVDB ID"},
	"field.season_subfolders":     {"Sottocartelle stagione", "Season subfolders"},
	"field.disable_upgrades":      {"Niente upgrade", "No upgrades"},
	"fieldhint.name":              {"Titolo usato per riconoscere le release.", "Title used to match releases."},
	"fieldhint.year":              {"Anno di uscita, aiuta a distinguere titoli omonimi.", "Release year, tells apart titles with the same name."},
	"fieldhint.seasons":           {"Stagioni da seguire: 1+ (dalla 1 in poi), 2-4, 1,3 oppure * per tutte.", "Seasons to follow: 1+ (from 1 on), 2-4, 1,3 or * for all."},
	"fieldhint.quality":           {"Risoluzione: 1080p (minima), 720p-1080p (intervallo), <2160p (massima); vuoto = qualsiasi.", "Resolution: 1080p (minimum), 720p-1080p (range), <2160p (maximum); empty = any."},
	"fieldhint.language":          {"Lingua audio richiesta, es. ita oppure ita,eng.", "Required audio language, e.g. ita or ita,eng."},
	"fieldhint.subtitle":          {"Sottotitoli richiesti, separati da virgola; vuoto = non richiesti.", "Required subtitles, comma separated; empty = not required."},
	"fieldhint.exclude":           {"Parole che scartano una release se compaiono nel titolo.", "Words that reject a release when they appear in its title."},
	"fieldhint.aliases":           {"Altri titoli con cui la serie compare nelle release, separati da virgola.", "Other titles the series appears under in releases, comma separated."},
	"fieldhint.archive_path":      {"Cartella della serie sul NAS; vuoto = sotto la radice dell'archivio.", "Series folder on the NAS; empty = under the archive root."},
	"fieldhint.tmdb_id":           {"Identificativo TMDB: metadati, stagioni ed episodi attesi.", "TMDB id: metadata, seasons and expected episodes."},
	"fieldhint.tvdb_id":           {"Identificativo TVDB, alternativo a TMDB.", "TVDB id, alternative to TMDB."},
	"fieldhint.season_subfolders": {"Archivia gli episodi in una sottocartella per stagione.", "Archive episodes in one subfolder per season."},
	"fieldhint.disable_upgrades":  {"Sì = una release migliore non sostituisce più un file già archiviato.", "Yes = a better release no longer replaces an archived file."},
	"prompt.tmdbseries":           {"Cerca serie su TMDB: ", "Search series on TMDB: "},
	"prompt.tmdbmovie":            {"Cerca film su TMDB: ", "Search movie on TMDB: "},
	"prompt.seriesremove":         {"Elimino la serie '%s' dalla libreria (i file restano)? [s/N] ", "Remove series '%s' from the library (files are kept)? [y/N] "},
	"prompt.movieremove":          {"Elimino il film '%s' dalla libreria (i file restano)? [s/N] ", "Remove movie '%s' from the library (files are kept)? [y/N] "},
	"prompt.episoderedownload":    {"Riscarico S%02dE%02d al prossimo ciclo? [s/N] ", "Download S%02dE%02d again at the next cycle? [y/N] "},
	"prompt.movieredownload":      {"Riscarico '%s' al prossimo ciclo? [s/N] ", "Download '%s' again at the next cycle? [y/N] "},
	"prompt.episodeignore":        {"Ignoro %s S%02dE%02d nella ricerca dei mancanti? [s/N] ", "Ignore %s S%02dE%02d in the missing search? [y/N] "},
	"prompt.rename":               {"Rinomino %d file di '%s'? [s/N] ", "Rename %d files of '%s'? [y/N] "},
	"search.missing":              {"mancanti di %s (%d episodi)", "missing from %s (%d episodes)"},
	"msg.libraryfailed":           {"operazione non riuscita: %s", "operation failed: %s"},
	"msg.seriessaved":             {"serie salvata", "series saved"},
	"msg.seriespaused":            {"'%s' in pausa", "'%s' paused"},
	"msg.seriesresumed":           {"'%s' di nuovo attiva", "'%s' active again"},
	"msg.seriesremoved":           {"'%s' eliminata dalla libreria", "'%s' removed from the library"},
	"msg.moviesaved":              {"'%s' salvato", "'%s' saved"},
	"msg.movieremoved":            {"film eliminato dalla libreria", "movie removed from the library"},
	"msg.libraryadded":            {"'%s' aggiunto alla libreria", "'%s' added to the library"},
	"msg.tmdbresults":             {"TMDB: %d risultati", "TMDB: %d results"},
	"msg.inlibrary":               {"già in libreria", "already in the library"},
	"msg.seasonon":                {"stagione %d attivata", "season %d switched on"},
	"msg.seasonoff":               {"stagione %d disattivata", "season %d switched off"},
	"msg.seasonexcluded":          {"la stagione %d è fuori dal campo Stagioni: e per modificarlo", "season %d is outside the Seasons field: e to edit it"},
	"msg.nomissing":               {"nessun episodio mancante da cercare", "no missing episode to search"},
	"msg.metadataupdated":         {"metadati aggiornati da TMDB", "metadata refreshed from TMDB"},
	"msg.renamenone":              {"i nomi dei file sono già corretti", "file names are already correct"},
	"msg.renamed":                 {"%d file rinominati", "%d files renamed"},
	"msg.nosources":               {"nessuna sorgente in archivio per %s: s cerca online", "no archived source for %s: s searches online"},
	"msg.episodeignored":          {"%s ignorato", "%s ignored"},
	"msg.episodeunignored":        {"%s non più ignorato", "%s no longer ignored"},
	"msg.episoderedownload":       {"%s sarà riscaricato al prossimo ciclo", "%s will be downloaded again at the next cycle"},
	"msg.movieredownload":         {"film rimesso in coda al prossimo ciclo", "movie queued again for the next cycle"},
	"msg.nochanges":               {"nessuna modifica", "no changes"},
	"msg.namerequired":            {"il nome è obbligatorio", "the name is required"},
	"msg.tmdbrequired":            {"serve il TMDB ID", "the TMDB id is required"},
	"msg.seasonsrequired":         {"indica le stagioni da seguire (es. 1+)", "set the seasons to follow (e.g. 1+)"},
	"help.series":                 {"Serie:    ←→ stagione · Spazio stagione on/off · Invio sorgenti · s cerca · i ignora · R riscarica · m mancanti · e modifica · n rinomina · M metadati", "Series:   ←→ season · Space season on/off · Enter sources · s search · i ignore · R redownload · m missing · e edit · n rename · M metadata"},
	"help.movie":                  {"Film:     Invio accoda dall'archivio · s cerca online · e modifica · R riscarica · p pausa · d elimina", "Movie:    Enter queue from archive · s search online · e edit · R redownload · p pause · d delete"},
	"help.form":                   {"Moduli:   ↑↓ campo · Invio modifica · s salva · Esc annulla (* = campo modificato)", "Forms:    ↑↓ field · Enter edit · s save · Esc cancel (* = changed field)"},
	"hint.details":                {"1 generale · 2 tracker · 3 file · 4 peer · ↑↓ scorri · Invio/Esc indietro", "1 general · 2 trackers · 3 files · 4 peers · ↑↓ scroll · Enter/Esc back"},
	"hint.settings":               {"↑↓ seleziona · Invio modifica · c colori · h contrasto · Esc chiudi", "↑↓ select · Enter edit · c colors · h contrast · Esc close"},
	"settings.title":              {"Impostazioni TUI", "TUI settings"},
	"settings.language":           {"Lingua", "Language"},
	"settings.refresh":            {"Intervallo refresh", "Refresh interval"},
	"settings.limits":             {"Limiti DL/UL", "DL/UL limits"},
	"settings.dryrun":             {"Dry-run", "Dry-run"},
	"settings.colors":             {"Colori", "Colors"},
	"settings.contrast":           {"Alto contrasto", "High contrast"},

	"prompt.cycle":           {"Ciclo [full/series/movies/comics] (full): ", "Cycle [full/series/movies/comics] (full): "},
	"prompt.search":          {"Cerca: ", "Search: "},
	"prompt.magnet":          {"Magnet/URL: ", "Magnet/URL: "},
	"prompt.file":            {"File .torrent: ", "File .torrent: "},
	"prompt.limits":          {"Limiti globali DL UL KiB/s (0 0): ", "Global DL UL limits KiB/s (0 0): "},
	"prompt.templimits":      {"Limite temporaneo DL UL KiB/s [minuti] (0=illimitato · senza minuti=finché non rimosso · off=rimuovi): ", "Temporary limit DL UL KiB/s [minutes] (0=unlimited · no minutes=until removed · off=remove): "},
	"prompt.logfilter":       {"Filtro log (vuoto=tutti): ", "Log filter (empty=all): "},
	"prompt.torrentfilter":   {"Filtro torrent (vuoto=tutti): ", "Torrent filter (empty=all): "},
	"prompt.archivefilter":   {"Filtro archivio (vuoto=tutto): ", "Archive filter (empty=all): "},
	"prompt.language":        {"Lingua [it/en]: ", "Language [it/en]: "},
	"prompt.refresh":         {"Refresh in secondi: ", "Refresh seconds: "},
	"prompt.remove":          {"Rimuovo '%s'? [s/N] ", "Remove '%s'? [y/N] "},
	"prompt.removefiles":     {"Rimuovo '%s' e i file scaricati? [s/N] ", "Remove '%s' and its files? [y/N] "},
	"prompt.deletefiles":     {"Elimino anche i file scaricati? [s/N] ", "Also delete downloaded files? [y/N] "},
	"prompt.cleantrash":      {"Svuoto ora il cestino? [s/N] ", "Empty the trash now? [y/N] "},
	"prompt.cleancomp":       {"Rimuovo i completati che hanno raggiunto il limite di seed? [s/N] ", "Remove completed torrents that reached seed limits? [y/N] "},
	"prompt.blocklistremove": {"Rimuovo '%s' dalla blocklist? [s/N] ", "Remove '%s' from the blocklist? [y/N] "},
	"prompt.httpremove":      {"Rimuovo il download HTTP '%s'? [s/N] ", "Remove HTTP download '%s'? [y/N] "},

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
	"msg.tempset":               {"limite temporaneo ↓%s ↑%s per %d min", "temporary limit ↓%s ↑%s for %d min"},
	"msg.tempsetkeep":           {"limite temporaneo ↓%s ↑%s fino a rimozione", "temporary limit ↓%s ↑%s until removed"},
	"msg.tempcleared":           {"limite temporaneo rimosso: tornano i limiti normali", "temporary limit removed: normal limits restored"},
	"msg.tempinvalid":           {"formato: DL UL [minuti] in KiB/s, oppure off", "format: DL UL [minutes] in KiB/s, or off"},
	"msg.tempminutes":           {"durata massima %d minuti", "maximum duration %d minutes"},
	"policy.temp":               {"limite temporaneo ↓%s ↑%s · ancora %d min", "temporary limit ↓%s ↑%s · %d min left"},
	"policy.tempkeep":           {"limite temporaneo ↓%s ↑%s · fino a rimozione", "temporary limit ↓%s ↑%s · until removed"},
	"policy.sched":              {"banda programmata ↓%s ↑%s", "scheduled bandwidth ↓%s ↑%s"},
	"policy.base":               {"limiti ↓%s ↑%s", "limits ↓%s ↑%s"},
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
	"msg.sethint":               {"imposta GEXTTO_URL", "set GEXTTO_URL"},
	"msg.loading":               {"caricamento…", "loading…"},
	"msg.emptylogs":             {"nessun log da mostrare", "no logs to show"},
	"msg.emptytorrents":         {"nessun torrent nella sessione", "no torrents in the session"},
	"msg.emptyarchive":          {"archivio vuoto", "archive is empty"},
	"msg.emptymissing":          {"nessun elemento mancante", "no missing items"},
	"msg.emptyblocklist":        {"blocklist vuota", "blocklist is empty"},
	"msg.emptylibrary":          {"nessun titolo monitorato", "no monitored titles"},
	"msg.termtoolsmall":         {"terminale troppo piccolo", "terminal too small"},
	"msg.confirmed":             {"confermato", "confirmed"},
	"msg.cancelled":             {"annullato", "cancelled"},
	"msg.busy":                  {"un'azione è già in corso", "an action is already running"},
	"msg.copied":                {"copia inviata al terminale (OSC52; in tmux serve set-clipboard on)", "copy sent to the terminal (OSC52; tmux needs set-clipboard on)"},
	"msg.notty":                 {"la TUI richiede un terminale interattivo: da remoto usa 'ssh -t host …'", "the TUI needs an interactive terminal: remotely, use 'ssh -t host …'"},
	"msg.pasteignored":          {"testo incollato ignorato: incolla un magnet/URL o apri prima un campo", "pasted text ignored: paste a magnet/URL or open a field first"},
	"msg.copyfailed":            {"copia non riuscita: %s", "copy failed: %s"},
	"msg.notifyfinished":        {"completato: %s", "finished: %s"},
	"msg.notifyerror":           {"errore torrent %s: %s", "torrent error %s: %s"},
	"msg.notifystalled":         {"torrent in stallo: %s", "torrent stalled: %s"},
	"msg.notifyarchived":        {"archiviato: %s", "archived: %s"},
	"msg.notifyhttp":            {"download HTTP %s: %s", "HTTP download %s: %s"},
	"msg.daemononline":          {"daemon nuovamente online", "daemon is online again"},
	"msg.daemonoffline":         {"daemon offline", "daemon is offline"},
	"msg.unknownerror":          {"errore sconosciuto", "unknown error"},
	"msg.languageinvalid":       {"lingua non valida: usare it o en", "invalid language: use it or en"},
	"msg.refreshinvalid":        {"refresh tra 1 e 86400 secondi", "refresh must be 1-86400 seconds"},
	"msg.settingsfailed":        {"impostazioni non salvate: %s", "settings not saved: %s"},
	"msg.settingssaved":         {"impostazione salvata", "setting saved"},
	"msg.settingsrestart":       {"dry-run salvato: riavviare il daemon", "dry-run saved: restart the daemon"},
	"msg.languagesaved":         {"lingua aggiornata", "language updated"},
	"msg.archivefailed":         {"archivio non disponibile: %s", "archive unavailable: %s"},
	"msg.missingfailed":         {"mancanti non disponibili: %s", "missing items unavailable: %s"},
	"msg.blocklistfailed":       {"blocklist non disponibile: %s", "blocklist unavailable: %s"},
	"msg.blocklistremovefailed": {"rimozione blocklist non riuscita: %s", "blocklist removal failed: %s"},
	"msg.blocklistremoved":      {"voce rimossa dalla blocklist", "blocklist entry removed"},

	"help.title":     {"Gextto TUI — comandi da tastiera", "Gextto TUI — keyboard help"},
	"help.close":     {"Premi Esc, Invio o ? per chiudere", "Press Esc, Enter or ? to close"},
	"help.global":    {"Globali:  1-8/Tab schede · r aggiorna · ? chiudi aiuto · q esci", "Global:  1-8/Tab tabs · r refresh · ? close help · q quit"},
	"help.global2":   {"          a magnet/URL · t file .torrent · c ciclo · s cerca · l libreria · e eventi · g impostazioni", "          a magnet/URL · t .torrent file · c cycle · s search · l library · e events · g settings"},
	"help.torrents":  {"Torrent:  ↑↓ o PgUp/PgDn · Home/End · Invio dettagli", "Torrents: ↑↓ or PgUp/PgDn · Home/End · Enter details"},
	"help.torrents2": {"          p pausa/riprendi · b riavvia · d rimuovi · X pulisci completati", "          p pause/resume · b restart · d remove · X clean completed"},
	"help.torrents3": {"          k verifica · R riannuncia · n senza-rinomina · i/u pin · L limiti globali · T limite temporaneo", "          k recheck · R reannounce · n no-rename · i/u pin · L global limits · T temporary limit"},
	"help.torrents4": {"          o cambia ordinamento · F filtro torrent", "          o cycle sort · F torrent filter"},
	"help.details":   {"Dettagli: 1 generale · 2 tracker · 3 file · 4 peer · ↑↓ scorri", "Details:  1 general · 2 trackers · 3 files · 4 peers · ↑↓ scroll"},
	"help.logs":      {"Log:      ↑↓ o PgUp/PgDn · Home/End · / filtro · f segui/ferma", "Logs:     ↑↓ or PgUp/PgDn · Home/End · / filter · f follow/pause"},
	"help.health":    {"Salute:   ↑↓/PgUp/PgDn scorri · x svuota cestino (con conferma)", "Health:   ↑↓/PgUp/PgDn scroll · x empty trash (confirmation required)"},
	"help.status":    {"Stato:    ↑↓/PgUp/PgDn scorri · le ultime righe di log restano in fondo", "Status:   ↑↓/PgUp/PgDn scroll · the latest log lines stay at the bottom"},
	"help.terminal":  {"Terminale: Ctrl-L ridisegna · incolla un magnet per aggiungerlo · conferme solo con s/y", "Terminal: Ctrl-L redraw · paste a magnet to add it · confirm only with y"},
	"help.bandwidth": {"Banda:    in basso a destra la banda usata dalla TUI (↓ ricevuti, ↑ inviati) e le richieste al daemon al secondo", "Bandwidth: bottom right shows the bandwidth the TUI uses (↓ received, ↑ sent) and daemon requests per second"},
	"help.settings":  {"Impostazioni: g apre il pannello · ↑↓ seleziona · Invio modifica · c colori · h contrasto", "Settings: g opens panel · ↑↓ select · Enter edit · c colors · h contrast"},
	"help.archive":   {"Archivio: ↑↓ seleziona · PgUp/PgDn pagina · Invio accoda · d dettagli · o/O ordina · s cerca", "Archive: ↑↓ select · PgUp/PgDn page · Enter queue · d details · o/O sort · s search"},
	"help.missing":   {"Mancanti: ↑↓ seleziona · Invio apri la serie · s cerca l'episodio · i ignora · r aggiorna", "Missing: ↑↓ select · Enter open the series · s search the episode · i ignore · r refresh"},
	"help.blocklist": {"Blocco:   ↑↓ seleziona · d rimuovi", "Blocklist: ↑↓ select · d remove"},
	"help.library":   {"Libreria: 1 serie · 2 film · 3 fumetti · Invio dettagli · a aggiungi da TMDB · e modifica · p pausa · d elimina · m cerca · s filtra", "Library: 1 series · 2 movies · 3 comics · Enter details · a add from TMDB · e edit · p pause · d delete · m search · s filter"},

	"sort.name":            {"nome", "name"},
	"sort.progress":        {"progresso", "progress"},
	"sort.state":           {"stato", "state"},
	"sort.rate":            {"velocità", "rate"},
	"sort.size":            {"dimensione", "size"},
	"sort.archive_title":   {"titolo", "title"},
	"sort.archive_source":  {"sorgente", "source"},
	"sort.archive_quality": {"qualità", "quality"},
	"sort.archive_added":   {"data", "date"},

	"state.downloading":          {"In scarico", "Downloading"},
	"state.idle":                 {"Fermo 0 B/s", "Stuck 0 B/s"},
	"state.downloading_metadata": {"Metadati", "Metadata"},
	"state.stalled":              {"In attesa di seed", "Stalled"},
	"state.seeding":              {"In seed", "Seeding"},
	"state.finished":             {"Completato", "Finished"},
	"state.completed":            {"Completato", "Completed"},
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
