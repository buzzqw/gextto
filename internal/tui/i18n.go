// Package tui implements the terminal interface for a running gextto daemon.
//
// It only talks to the daemon's HTTP API (never the databases), mirrors the
// classic web interface, and is available in six languages (Italian, English,
// German, French, Spanish and Polish).
package tui

import (
	"fmt"
	"strings"
)

// Languages supported by the interface.
const (
	LangIT = "it"
	LangEN = "en"
	LangDE = "de"
	LangFR = "fr"
	LangES = "es"
	LangPL = "pl"
)

// catalog holds every user-facing string as {Italian, English, German, French,
// Spanish, Polish}. Keeping the variants side by side makes it obvious when a
// translation is missing.
var catalog = map[string][6]string{
	"app.title":                   {"Gextto TUI", "Gextto TUI", "Gextto TUI", "Gextto TUI", "Gextto TUI", "Gextto TUI"},
	"tab.status":                  {"Stato", "Status", "Status", "État", "Estado", "Stan"},
	"tab.torrents":                {"Torrent", "Torrents", "Torrent", "Torrent", "Torrent", "Torrent"},
	"tab.logs":                    {"Log", "Logs", "Log", "Log", "Log", "Log"},
	"tab.health":                  {"Salute", "Health", "Gesundheit", "Santé", "Salud", "Zdrowie"},
	"tab.archive":                 {"Archivio", "Archive", "Archiv", "Archive", "Archivo", "Archiwum"},
	"tab.missing":                 {"Mancanti", "Missing", "Fehlend", "Manquant", "Faltantes", "Brakujące"},
	"tab.blocklist":               {"Blocco", "Blocklist", "Block", "Blocage", "Bloqueo", "Blokada"},
	"tab.library":                 {"Libreria", "Library", "Bibliothek", "Bibliothèque", "Biblioteca", "Biblioteka"},
	"tab.maintenance":             {"Manutenzione", "Maintenance", "Wartung", "Maintenance", "Mantenimiento", "Konserwacja"},
	"mode.dryrun":                 {"DRY-RUN", "DRY-RUN", "DRY-RUN", "DRY-RUN", "DRY-RUN", "DRY-RUN"},
	"mode.active":                 {"ATTIVA", "ACTIVE", "AKTIV", "ACTIF", "ACTIVO", "AKTYWNY"},
	"mode.standby":                {"IN ATTESA", "STANDBY", "WARTEND", "EN ATTENTE", "EN ESPERA", "OCZEKUJE"},
	"mode.loading":                {"CARICAMENTO", "LOADING", "LADEVORGANG", "CHARGEMENT", "CARGANDO", "ŁADOWANIE"},
	"label.mode":                  {"Modalità", "Mode", "Modus", "Mode", "Modo", "Tryb"},
	"label.torrents":              {"Torrent", "Torrents", "Torrent", "Torrent", "Torrent", "Torrent"},
	"label.downloading":           {"in scarico", "downloading", "wird geladen", "en téléchargement", "descargando", "pobieranie"},
	"label.queued":                {"in coda", "queued", "in Warteschlange", "en file d'attente", "en cola", "w kolejce"},
	"label.seeding":               {"in seed", "seeding", "im Seed", "en seed", "en seed", "w seed"},
	"label.stalled":               {"in stallo", "stalled", "ins Stocken geraten", "en attente", "estancado", "w zawieszeniu"},
	"label.lastcycle":             {"Ultimo ciclo", "Last cycle", "Letzter Zyklus", "Dernier cycle", "Último ciclo", "Ostatni cykl"},
	"label.scraped":               {"analizzate", "scraped", "analysiert", "analysés", "analizados", "przeanalizowane"},
	"label.candidates":            {"candidate", "candidates", "Kandidaten", "candidats", "candidatos", "kandydaci"},
	"label.downloads":             {"download", "downloads", "Downloads", "téléchargements", "descargas", "pobrania"},
	"label.http":                  {"HTTP", "HTTP", "HTTP", "HTTP", "HTTP", "HTTP"},
	"label.gaps":                  {"gap", "gaps", "Lücke", "écart", "brecha", "luka"},
	"label.errors":                {"errori", "errors", "Fehler", "erreurs", "errores", "błędy"},
	"label.seen":                  {"Visti nei feed", "Seen in feeds", "In Feeds gesehen", "Vus dans les flux", "Vistos en los feeds", "Widziane w kanałach"},
	"label.groups":                {"gruppi", "groups", "Gruppen", "groupes", "grupos", "grupy"},
	"label.movies":                {"film", "movies", "Filme", "films", "películas", "filmy"},
	"label.series":                {"serie", "series", "Serien", "séries", "series", "seriale"},
	"label.nextcycle":             {"Prossimo ciclo", "Next cycle", "Nächster Zyklus", "Prochain cycle", "Próximo ciclo", "Następny cykl"},
	"label.online":                {"ONLINE", "ONLINE", "ONLINE", "ONLINE", "ONLINE", "ONLINE"},
	"label.offline":               {"OFFLINE", "OFFLINE", "OFFLINE", "OFFLINE", "OFFLINE", "OFFLINE"},
	"label.connecting":            {"CONNESSIONE…", "CONNECTING…", "VERBINDUNG…", "CONNEXION…", "CONEXIÓN…", "POŁĄCZENIE…"},
	"label.version":               {"versione", "version", "Version", "version", "versión", "wersja"},
	"label.hash":                  {"Hash", "Hash", "Hash", "Hash", "Hash", "Hash"},
	"label.state":                 {"Stato", "State", "Status", "État", "Estado", "Stan"},
	"label.progress":              {"Progresso", "Progress", "Fortschritt", "Progression", "Progreso", "Postęp"},
	"label.done":                  {"Scaricato", "Done", "Heruntergeladen", "Téléchargé", "Descargado", "Pobrano"},
	"label.down":                  {"↓", "↓", "↓", "↓", "↓", "↓"},
	"label.up":                    {"↑", "↑", "↑", "↑", "↑", "↑"},
	"label.name":                  {"Nome", "Name", "Name", "Nom", "Nombre", "Nazwa"},
	"label.selected":              {"selezionato", "selected", "Ausgewählt", "sélectionné", "seleccionado", "wybrany"},
	"label.filter":                {"filtro", "filter", "Filter", "filtre", "filtro", "filtr"},
	"label.sort":                  {"ordina", "sort", "Sortieren", "trier", "ordenar", "sortuj"},
	"label.page":                  {"pagina", "page", "Seite", "page", "página", "strona"},
	"label.archiveid":             {"ID archivio", "Archive ID", "Archiv-ID", "ID archive", "ID archivo", "ID archiwum"},
	"label.quality":               {"Qualità", "Quality", "Qualität", "Qualité", "Calidad", "Jakość"},
	"label.added":                 {"Aggiunto", "Added", "Hinzugefügt", "Ajouté", "Añadido", "Dodano"},
	"label.reconnect":             {"riconnessione", "reconnecting", "Wiederverbindung", "reconnexion", "reconexión", "ponowne połączenie"},
	"label.lines":                 {"righe", "lines", "Zeilen", "lignes", "líneas", "wiersze"},
	"label.follow":                {"segui", "follow", "Folgen", "suivre", "seguir", "śledź"},
	"label.live":                  {"LIVE SSE", "LIVE SSE", "LIVE SSE", "LIVE SSE", "LIVE SSE", "LIVE SSE"},
	"label.health":                {"Salute", "Health", "Gesundheit", "Santé", "Salud", "Zdrowie"},
	"label.paths":                 {"Percorsi", "Paths", "Pfade", "chemins", "rutas", "ścieżki"},
	"label.disks":                 {"Dischi", "Disks", "Laufwerke", "disques", "discos", "dyski"},
	"label.ramdisk":               {"RAM disk", "RAM disk", "RAM disk", "RAM disk", "RAM disk", "RAM disk"},
	"label.recenterror":           {"Ultimi errori", "Recent errors", "Letzte Fehler", "Dernières erreurs", "Últimos errores", "Ostatnie błędy"},
	"label.size":                  {"Dimensione", "Size", "Größe", "taille", "tamaño", "rozmiar"},
	"label.downloaded":            {"Scaricato", "Downloaded", "Heruntergeladen", "Téléchargé", "Descargado", "Pobrano"},
	"label.alltime":               {"Totale storico", "All-time", "Gesamt historisch", "Total historique", "Total histórico", "Łącznie historycznie"},
	"label.rates":                 {"Velocità", "Rates", "Geschwindigkeit", "vitesse", "velocidad", "prędkość"},
	"label.transfer":              {"Trasferimento", "Transfer", "Übertragung", "transfert", "transferencia", "transfer"},
	"label.consumption":           {"Consumi", "Consumption", "Verbrauch", "Consommation", "Consumo", "Zużycie"},
	"label.total":                 {"totale", "total", "gesamt", "total", "total", "łącznie"},
	"label.trend":                 {"Tendenza", "Trend", "Trend", "tendance", "tendencia", "trend"},
	"label.consumption7d":         {"Consumo 7g", "7d consumption", "Verbrauch 7g", "Consommation 7g", "Consumo 7g", "Zużycie 7g"},
	"label.peersseeds":            {"Peer / seed", "Peers / seeds", "Peer / seed", "Peer / seed", "Peer / seed", "Peer / seed"},
	"label.queuepos":              {"Posizione in coda", "Queue position", "Warteschlangenposition", "Position dans la file", "Posición en cola", "Pozycja w kolejce"},
	"label.seedlimit":             {"Limite seed", "Seed limit", "Seed-Limit", "Limite seed", "Límite seed", "Limit seed"},
	"label.metadata":              {"Metadati", "Metadata", "Metadaten", "Métadonnées", "Metadatos", "Metadane"},
	"label.version2":              {"Versione torrent", "Torrent version", "Torrent-Version", "Version torrent", "Versión torrent", "Wersja torrent"},
	"label.automanaged":           {"Auto-gestito", "Auto-managed", "Selbstverwaltet", "Auto-géré", "Autogestionado", "Samozarządzany"},
	"label.norename":              {"Senza rinomina", "No rename", "Ohne Umbenennen", "Sans renommage", "Sin renombrar", "Bez zmiany nazwy"},
	"label.archived":              {"Archiviato", "Archived", "Archiviert", "Archivé", "Archivado", "Zarchiwizowane"},
	"label.source":                {"Sorgente", "Source", "Quelle", "Source", "Fuente", "Źródło"},
	"label.reason":                {"Motivo", "Reason", "Grund", "Raison", "Motivo", "Powód"},
	"label.savepath":              {"Cartella", "Save path", "Ordner", "Dossier", "Carpeta", "Folder"},
	"label.magnet":                {"Magnet", "Magnet", "Magnet", "Magnet", "Magnet", "Magnet"},
	"label.trackers":              {"Tracker", "Trackers", "Tracker", "Tracker", "Tracker", "Tracker"},
	"label.files":                 {"File", "Files", "File", "File", "File", "File"},
	"label.peers":                 {"Peer", "Peers", "Peer", "Peer", "Peer", "Peer"},
	"label.none":                  {"nessuno", "none", "keine", "aucun", "ninguno", "brak"},
	"label.yes":                   {"sì", "yes", "ja", "oui", "sí", "tak"},
	"label.no":                    {"no", "no", "nein", "non", "no", "nie"},
	"label.infinite":              {"infinito", "infinite", "unendlich", "infini", "infinito", "nieskończone"},
	"label.unlimited":             {"illimitato", "unlimited", "unbegrenzt", "illimité", "ilimitado", "nieograniczone"},
	"label.system":                {"Sistema", "System", "System", "Système", "Sistema", "System"},
	"label.daemon":                {"Gextto", "Gextto", "Gextto", "Gextto", "Gextto", "Gextto"},
	"label.uptime":                {"attivo da", "up", "aktiv seit", "actif depuis", "activo desde", "aktywny od"},
	"label.cpu":                   {"CPU", "CPU", "CPU", "CPU", "CPU", "CPU"},
	"label.ram":                   {"RAM", "RAM", "RAM", "RAM", "RAM", "RAM"},
	"label.free":                  {"liberi", "free", "frei", "libres", "libres", "wolne"},
	"label.used":                  {"usati", "used", "belegt", "utilisés", "usados", "używane"},
	"label.trash":                 {"Cestino", "Trash", "Papierkorb", "Corbeille", "Papelera", "Kosz"},
	"label.writable":              {"scrivibile", "writable", "beschreibbar", "inscriptible", "escribible", "zapisywalny"},
	"label.enabled":               {"ON", "ON", "ON", "ON", "ON", "ON"},
	"label.disabled":              {"OFF", "OFF", "OFF", "OFF", "OFF", "OFF"},
	"hint.global":                 {"? aiuto · q esci", "? help · q quit", "? Hilfe · q beenden", "? aide · q quitter", "? ayuda · q salir", "? pomoc · q wyjście"},
	"hint.actions":                {"a magnet/URL · t file · c ciclo · s cerca · e eventi · g impostazioni · r aggiorna", "a magnet/URL · t file · c cycle · s search · e events · g settings · r refresh", "a magnet/URL · t Datei · c Zyklus · s suchen · e Ereignisse · g Einstellungen · r aktualisieren", "a magnet/URL · t fichier · c cycle · s rechercher · e événements · g paramètres · r actualiser", "a magnet/URL · t archivo · c ciclo · s buscar · e eventos · g ajustes · r actualizar", "a magnet/URL · t plik · c cykl · s szukaj · e zdarzenia · g ustawienia · r odśwież"},
	"hint.tabs":                   {"Tab/1-9", "Tab/1-9", "Tab/1-9", "Tab/1-9", "Tab/1-9", "Tab/1-9"},
	"hint.torrents":               {"↑↓/PgUp/PgDn seleziona · Invio dettagli · p pausa/riprendi · b riavvia · d rimuovi · X pulisci completati · k verifica · R riannuncia · n senza-rinomina · i/u pin · L limiti · T limite temp. · Spazio seleziona · # tag · A auto-rimozione · H storico · o ordina · / filtro", "↑↓/PgUp/PgDn select · Enter details · p pause/resume · b restart · d remove · X clean completed · k recheck · R reannounce · n no-rename · i/u pin · L limits · T temp limit · Space mark · # tag · A auto-remove · H history · o sort · / filter", "↑↓/PgUp/PgDn auswählen · Enter Details · p Pause/Fortsetzen · b neu starten · d entfernen · X erledigte löschen · k prüfen · R erneut ankündigen · n ohne Umbenennen · i/u anheften · L Limits · T Temp.-Limit · Leertaste auswählen · # Tag · A Auto-Entfernung · H Verlauf · o sortieren · / Filter", "↑↓/PgUp/PgDn sélectionner · Entrée détails · p pause/reprendre · b redémarrer · d supprimer · X nettoyer terminés · k vérifier · R réannoncer · n sans-renommage · i/u épingler · L limites · T limite temp. · Espace sélectionner · # tag · A auto-suppression · H historique · o trier · / filtre", "↑↓/PgUp/PgDn seleccionar · Intro detalles · p pausa/reanudar · b reiniciar · d eliminar · X limpiar completados · k verificar · R reanunciar · n sin-renombrar · i/u fijar · L límites · T límite temp. · Espacio seleccionar · # tag · A auto-eliminación · H historial · o ordenar · / filtro", "↑↓/PgUp/PgDn wybierz · Enter szczegóły · p pauza/wznów · b restartuj · d usuń · X wyczyść ukończone · k sprawdź · R ogłoś ponownie · n bez-zmiany-nazwy · i/u przypnij · L limity · T limit temp. · Spacja wybierz · # tag · A auto-usuwanie · H historia · o sortuj · / filtr"},
	"hint.logs":                   {"↑↓/PgUp/PgDn scorri · / filtro · w solo avvisi/errori · f segui/ferma · Home/End", "↑↓/PgUp/PgDn scroll · / filter · w warnings/errors only · f follow/pause · Home/End", "↑↓/PgUp/PgDn scrollen · / Filter · w nur Warnungen/Fehler · f folgen/stoppen · Home/End", "↑↓/PgUp/PgDn défiler · / filtre · w seulement alertes/erreurs · f suivre/arrêter · Home/End", "↑↓/PgUp/PgDn desplazar · / filtro · w solo avisos/errores · f seguir/detener · Home/End", "↑↓/PgUp/PgDn przewiń · / filtr · w tylko ostrzeżenia/błędy · f śledź/zatrzymaj · Home/End"},
	"hint.health":                 {"↑↓ scorri · x svuota cestino", "↑↓ scroll · x empty trash", "↑↓ scrollen · x Papierkorb leeren", "↑↓ défiler · x vider la corbeille", "↑↓ desplazar · x vaciar papelera", "↑↓ przewiń · x opróżnij kosz"},
	"hint.status":                 {"↑↓ scorri · 3 log completo", "↑↓ scroll · 3 full log", "↑↓ scrollen · 3 vollständiges Log", "↑↓ défiler · 3 journal complet", "↑↓ desplazar · 3 registro completo", "↑↓ przewiń · 3 pełny log"},
	"status.daemon":               {"Daemon", "Daemon", "Daemon", "Démon", "Daemon", "Demon"},
	"status.system":               {"Sistema", "System", "System", "Système", "Sistema", "System"},
	"status.cycle":                {"Ricerca", "Search", "Suche", "Recherche", "Búsqueda", "Wyszukiwanie"},
	"status.feeds":                {"Nei feed", "In feeds", "In Feeds", "Dans les flux", "En los feeds", "W kanałach"},
	"status.downloads":            {"Download", "Downloads", "Download", "Téléchargement", "Descarga", "Pobieranie"},
	"status.transfer":             {"Traffico", "Traffic", "Traffic", "Trafic", "Tráfico", "Ruch"},
	"status.consumption":          {"Consumi", "Usage", "Verbrauch", "Consommation", "Consumo", "Zużycie"},
	"status.attention":            {"Attenzione", "Attention", "Achtung", "Attention", "Atención", "Uwaga"},
	"status.active":               {"In corso", "Active", "Laufend", "En cours", "En curso", "W toku"},
	"status.transferring":         {"%d attivi", "%d active", "%d aktiv", "%d actifs", "%d activos", "%d aktywnych"},
	"status.idle":                 {"%d fermi a 0 B/s", "%d stuck at 0 B/s", "%d gestoppt bei 0 B/s", "%d à l'arrêt à 0 B/s", "%d detenidos a 0 B/s", "%d zatrzymanych na 0 B/s"},
	"status.waiting":              {"%d in coda/pausa", "%d queued/paused", "%d in Warteschlange/Pause", "%d en file/pause", "%d en cola/pausa", "%d w kolejce/pauzie"},
	"status.done":                 {"%d completati", "%d completed", "%d abgeschlossen", "%d terminés", "%d completados", "%d ukończonych"},
	"status.idlelist":             {"Bloccati", "Stuck", "Blockiert", "Bloqués", "Bloqueados", "Zablokowane"},
	"status.recentlogs":           {"Log recenti", "Recent log", "Neueste Logs", "Journaux récents", "Registros recientes", "Ostatnie logi"},
	"status.nologs":               {"nessuna riga di log ancora", "no log lines yet", "noch keine Log-Zeile", "aucune ligne de journal pour l'instant", "aún no hay líneas de registro", "brak jeszcze wierszy logu"},
	"status.noactive":             {"nessun trasferimento in corso", "nothing is transferring", "keine laufende Übertragung", "aucun transfert en cours", "ninguna transferencia en curso", "brak transferu w toku"},
	"status.allgood":              {"nessun problema rilevato", "no problems detected", "kein Problem erkannt", "aucun problème détecté", "ningún problema detectado", "nie wykryto problemów"},
	"status.torrenterrs":          {"%d torrent in errore", "%d torrents in error", "%d torrent im Fehler", "%d torrent en erreur", "%d torrent con error", "%d torrentów z błędem"},
	"status.stalled":              {"%d in stallo", "%d stalled", "%d festgefahren", "%d bloqués", "%d atascados", "%d zawieszonych"},
	"status.badpaths":             {"%d percorsi non scrivibili", "%d paths not writable", "%d nicht beschreibbare Pfade", "%d chemins non inscriptibles", "%d rutas no escribibles", "%d ścieżek niezapisywalnych"},
	"status.diskfull":             {"disco pieno al %.0f%%", "disk %.0f%% full", "Festplatte voll zu %.0f%%", "disque plein à %.0f%%", "disco lleno al %.0f%%", "dysk pełny w %.0f%%"},
	"status.cycleerrors":          {"%d errori nell'ultimo ciclo", "%d errors in the last cycle", "%d Fehler im letzten Zyklus", "%d erreurs lors du dernier cycle", "%d errores en el último ciclo", "%d błędów w ostatnim cyklu"},
	"status.lasterror":            {"ultimo errore: %s", "last error: %s", "letzter Fehler: %s", "dernière erreur : %s", "último error: %s", "ostatni błąd: %s"},
	"status.health":               {"salute %s", "health %s", "Zustand %s", "santé %s", "salud %s", "stan %s"},
	"status.offline":              {"daemon non raggiungibile: %s", "daemon unreachable: %s", "Daemon nicht erreichbar: %s", "démon inaccessible : %s", "daemon no accesible: %s", "demon nieosiągalny: %s"},
	"status.next":                 {"prossimo tra %s", "next in %s", "nächstes in %s", "prochain dans %s", "próximo en %s", "następne za %s"},
	"status.running":              {"in esecuzione o in attesa", "running or due", "laufend oder wartend", "en cours ou en attente", "en ejecución o en espera", "w trakcie lub oczekuje"},
	"status.laststart":            {"ultima alle %s (%s fa)", "last at %s (%s ago)", "zuletzt um %s (vor %s)", "dernière à %s (il y a %s)", "última a las %s (hace %s)", "ostatnia o %s (%s temu)"},
	"status.uptime":               {"attivo da %s", "up %s", "aktiv seit %s", "actif depuis %s", "activo desde %s", "aktywny od %s"},
	"status.pid":                  {"PID %d", "PID %d", "PID %d", "PID %d", "PID %d", "PID %d"},
	"status.load":                 {"carico %.2f", "load %.2f", "Last %.2f", "charge %.2f", "carga %.2f", "obciążenie %.2f"},
	"status.ramfree":              {"RAM %s liberi / %s", "RAM %s free / %s", "RAM %s frei / %s", "RAM %s libre / %s", "RAM %s libre / %s", "RAM %s wolne / %s"},
	"status.disk":                 {"disco %s liberi / %s (%.0f%% usato)", "disk %s free / %s (%.0f%% used)", "Speicher %s frei / %s (%.0f%% belegt)", "disque %s libre / %s (%.0f%% utilisé)", "disco %s libre / %s (%.0f%% usado)", "dysk %s wolne / %s (%.0f%% zajęte)"},
	"status.trash":                {"cestino %d file (%s)", "trash %d files (%s)", "Papierkorb %d Dateien (%s)", "corbeille %d fichiers (%s)", "papelera %d archivos (%s)", "kosz %d plików (%s)"},
	"status.torrentcount":         {"%d torrent", "%d torrents", "%d torrent", "%d torrent", "%d torrent", "%d torrent"},
	"status.eta":                  {"ETA %s", "ETA %s", "ETA %s", "ETA %s", "ETA %s", "ETA %s"},
	"status.more":                 {"↓ altre %d righe · ↑↓ scorri", "↓ %d more lines · ↑↓ scroll", "↓ weitere %d Zeilen · ↑↓ scrollen", "↓ %d autres lignes · ↑↓ défiler", "↓ %d líneas más · ↑↓ desplazar", "↓ %d więcej wierszy · ↑↓ przewijaj"},
	"hint.archive":                {"↑↓ seleziona · PgUp/PgDn pagina · Invio accoda · d dettagli · o/O ordina · s cerca", "↑↓ select · PgUp/PgDn page · Enter queue · d details · o/O sort · s search", "↑↓ auswählen · PgUp/PgDn Seite · Eingabe einreihen · d Details · o/O sortieren · s suchen", "↑↓ sélectionner · PgUp/PgDn page · Entrée mettre en file · d détails · o/O trier · s rechercher", "↑↓ seleccionar · PgUp/PgDn página · Intro encolar · d detalles · o/O ordenar · s buscar", "↑↓ wybierz · PgUp/PgDn strona · Enter zakolejkuj · d szczegóły · o/O sortuj · s szukaj"},
	"hint.archivedetail":          {"↑↓ scorri · a accoda · Invio/Esc indietro", "↑↓ scroll · a queue · Enter/Esc back", "↑↓ scrollen · a einreihen · Eingabe/Esc zurück", "↑↓ défiler · a mettre en file · Entrée/Échap retour", "↑↓ desplazar · a encolar · Intro/Esc atrás", "↑↓ przewijaj · a zakolejkuj · Enter/Esc wstecz"},
	"hint.missing":                {"↑↓ seleziona · Invio apri serie · s cerca · i ignora · r aggiorna", "↑↓ select · Enter open series · s search · i ignore · r refresh", "↑↓ auswählen · Eingabe Serie öffnen · s suchen · i ignorieren · r aktualisieren", "↑↓ sélectionner · Entrée ouvrir la série · s rechercher · i ignorer · r actualiser", "↑↓ seleccionar · Intro abrir serie · s buscar · i ignorar · r actualizar", "↑↓ wybierz · Enter otwórz serial · s szukaj · i ignoruj · r odśwież"},
	"hint.blocklist":              {"↑↓ seleziona · d rimuovi", "↑↓ select · d remove", "↑↓ auswählen · d entfernen", "↑↓ sélectionner · d supprimer", "↑↓ seleccionar · d eliminar", "↑↓ wybierz · d usuń"},
	"hint.library":                {"←→ serie/film/fumetti · ↑↓ seleziona · s filtra · Esc indietro", "←→ series/movies/comics · ↑↓ select · s filter · Esc back", "←→ Serien/Filme/Comics · ↑↓ auswählen · s filtern · Esc zurück", "←→ séries/films/BD · ↑↓ sélectionner · s filtrer · Échap retour", "←→ series/películas/cómics · ↑↓ seleccionar · s filtrar · Esc atrás", "←→ seriale/filmy/komiksy · ↑↓ wybierz · s filtruj · Esc wstecz"},
	"library.series":              {"Serie TV", "TV series", "TV-Serien", "Séries TV", "Series TV", "Seriale TV"},
	"library.movies":              {"Film", "Movies", "Filme", "Films", "Películas", "Filmy"},
	"library.comics":              {"Fumetti", "Comics", "Comics", "BD", "Cómics", "Komiksy"},
	"bulk.pause":                  {"pausa", "pause", "Pause", "pause", "pausa", "pauza"},
	"bulk.resume":                 {"ripresa", "resume", "Fortsetzen", "reprise", "reanudar", "wznowienie"},
	"bulk.recheck":                {"verifica", "recheck", "Überprüfen", "vérifier", "verificar", "weryfikuj"},
	"bulk.remove":                 {"rimozione", "remove", "Entfernen", "suppression", "eliminación", "usuwanie"},
	"bulk.removefiles":            {"rimozione con file", "remove with files", "Entfernen mit Dateien", "suppression avec fichiers", "eliminación con archivos", "usuwanie z plikami"},
	"hint.torrentsmarked":         {"selezionati: p pausa · P riprendi · k verifica · d rimuovi · D rimuovi con file · # tag · * tutti/nessuno · Esc deseleziona", "marked: p pause · P resume · k recheck · d remove · D remove with files · # tag · * all/none · Esc clear", "ausgewählt: p Pause · P fortsetzen · k überprüfen · d entfernen · D mit Dateien entfernen · # Tag · * alle/keine · Esc abwählen", "sélectionnés : p pause · P reprendre · k vérifier · d supprimer · D supprimer avec fichiers · # tag · * tous/aucun · Échap désélectionner", "seleccionados: p pausar · P reanudar · k verificar · d eliminar · D eliminar con archivos · # etiqueta · * todos/ninguno · Esc deseleccionar", "wybrane: p pauza · P wznów · k weryfikuj · d usuń · D usuń z plikami · # tag · * wszystkie/żadne · Esc odznacz"},
	"hint.detailfiles":            {"↑↓ file · Spazio salta/scarica · +/- priorità · 1-4 viste · Esc indietro", "↑↓ file · Space skip/download · +/- priority · 1-4 views · Esc back", "↑↓ Datei · Leertaste überspringen/laden · +/- Priorität · 1-4 Ansichten · Esc zurück", "↑↓ fichier · Espace ignorer/télécharger · +/- priorité · 1-4 vues · Échap retour", "↑↓ archivo · Espacio omitir/descargar · +/- prioridad · 1-4 vistas · Esc atrás", "↑↓ plik · Spacja pomiń/pobierz · +/- priorytet · 1-4 widoki · Esc wstecz"},
	"hint.detailtrackers":         {"↑↓ tracker · a aggiungi · d rimuovi · 1-4 viste · Esc indietro", "↑↓ tracker · a add · d remove · 1-4 views · Esc back", "↑↓ Tracker · a hinzufügen · d entfernen · 1-4 Ansichten · Esc zurück", "↑↓ tracker · a ajouter · d supprimer · 1-4 vues · Échap retour", "↑↓ tracker · a añadir · d eliminar · 1-4 vistas · Esc atrás", "↑↓ tracker · a dodaj · d usuń · 1-4 widoki · Esc wstecz"},
	"hint.history":                {"↑↓ scorri · / filtra · r aggiorna · Esc chiudi", "↑↓ scroll · / filter · r refresh · Esc close", "↑↓ scrollen · / filtern · r aktualisieren · Esc schließen", "↑↓ défiler · / filtrer · r actualiser · Échap fermer", "↑↓ desplazar · / filtrar · r actualizar · Esc cerrar", "↑↓ przewijaj · / filtruj · r odśwież · Esc zamknij"},
	"hint.maintenance":            {"↑↓ seleziona · Invio esegui (con conferma per le operazioni pesanti)", "↑↓ select · Enter run (confirmation for heavy operations)", "↑↓ auswählen · Eingabe ausführen (mit Bestätigung für schwere Vorgänge)", "↑↓ sélectionner · Entrée exécuter (avec confirmation pour les opérations lourdes)", "↑↓ seleccionar · Intro ejecutar (con confirmación para las operaciones pesadas)", "↑↓ wybierz · Enter wykonaj (z potwierdzeniem dla ciężkich operacji)"},
	"hint.report":                 {"↑↓ scorri · Spazio includi/escludi · x applica · Esc chiudi", "↑↓ scroll · Space include/exclude · x apply · Esc close", "↑↓ scrollen · Leertaste einschließen/ausschließen · x anwenden · Esc schließen", "↑↓ défiler · Espace inclure/exclure · x appliquer · Échap fermer", "↑↓ desplazar · Espacio incluir/excluir · x aplicar · Esc cerrar", "↑↓ przewijaj · Spacja uwzględnij/wyklucz · x zastosuj · Esc zamknij"},
	"history.title":               {"Storico download: %d di %d", "Download history: %d of %d", "Download-Verlauf: %d von %d", "Historique des téléchargements : %d sur %d", "Historial de descargas: %d de %d", "Historia pobierania: %d z %d"},
	"history.empty":               {"nessun download nello storico", "no download in the history", "kein Download im Verlauf", "aucun téléchargement dans l'historique", "ninguna descarga en el historial", "brak pobrań w historii"},
	"label.autoremove":            {"auto-rimozione completati", "auto-remove completed", "automatisches Entfernen abgeschlossener", "suppression automatique des terminés", "eliminación automática de completados", "automatyczne usuwanie ukończonych"},
	"label.marked":                {"%d selezionati", "%d marked", "%d ausgewählt", "%d sélectionnés", "%d seleccionados", "%d wybranych"},
	"label.superseeding":          {"Super-seeding", "Super-seeding", "Super-Seeding", "Super-seeding", "Super-seeding", "Super-seeding"},
	"label.tag":                   {"Tag", "Tag", "Tag", "Tag", "Etiqueta", "Tag"},
	"label.torrentlimits":         {"Limiti torrent", "Torrent limits", "Torrent-Limits", "Limites des torrents", "Límites de los torrents", "Limity torrentów"},
	"maint.title":                 {"Manutenzione", "Maintenance", "Wartung", "Maintenance", "Mantenimiento", "Konserwacja"},
	"maint.job":                   {"Job in corso: %s (Invio annulla)", "Running job: %s (Enter cancels)", "Laufender Job: %s (Eingabe abbrechen)", "Tâche en cours : %s (Entrée annuler)", "Tarea en curso: %s (Intro cancelar)", "Zadanie w toku: %s (Enter anuluj)"},
	"maint.backup":                {"Crea backup", "Create backup", "Backup erstellen", "Créer une sauvegarde", "Crear copia de seguridad", "Utwórz kopię zapasową"},
	"maint.dbcheck":               {"Verifica database", "Check databases", "Datenbank überprüfen", "Vérifier la base de données", "Verificar base de datos", "Weryfikuj bazę danych"},
	"maint.vacuum":                {"Compatta database (VACUUM)", "Compact databases (VACUUM)", "Datenbank komprimieren (VACUUM)", "Compacter la base de données (VACUUM)", "Compactar base de datos (VACUUM)", "Kompaktuj bazę danych (VACUUM)"},
	"maint.analyze":               {"Aggiorna statistiche (ANALYZE)", "Update statistics (ANALYZE)", "Statistiken aktualisieren (ANALYZE)", "Actualiser les statistiques (ANALYZE)", "Actualizar estadísticas (ANALYZE)", "Zaktualizuj statystyki (ANALYZE)"},
	"maint.duplicates":            {"Duplicati inferiori (anteprima)", "Inferior duplicates (preview)", "Geringere Duplikate (Vorschau)", "Doublons inférieurs (aperçu)", "Duplicados inferiores (vista previa)", "Niższe duplikaty (podgląd)"},
	"maint.renameall":             {"Rinomina tutte le serie", "Rename all series", "Alle Serien umbenennen", "Renommer toutes les séries", "Renombrar todas las series", "Zmień nazwy wszystkich seriali"},
	"maint.folderrename":          {"Rinomina una cartella…", "Rename a folder…", "Einen Ordner umbenennen…", "Renommer un dossier…", "Renombrar una carpeta…", "Zmień nazwę folderu…"},
	"maint.scanarchives":          {"Scansiona tutti gli archivi", "Scan all archives", "Alle Archive scannen", "Analyser toutes les archives", "Escanear todos los archivos", "Skanuj wszystkie archiwa"},
	"maint.backfill":              {"Aggiorna MediaInfo", "Refresh MediaInfo", "MediaInfo aktualisieren", "Actualiser MediaInfo", "Actualizar MediaInfo", "Zaktualizuj MediaInfo"},
	"maint.housekeeping":          {"Pulizia database ora", "Database housekeeping now", "Datenbank jetzt bereinigen", "Nettoyer la base de données maintenant", "Limpiar base de datos ahora", "Wyczyść bazę danych teraz"},
	"maint.ramdisk":               {"RAM disk", "RAM disk", "RAM disk", "RAM disk", "RAM disk", "RAM disk"},
	"maint.trash":                 {"Svuota cestino", "Empty trash", "Papierkorb leeren", "Vider la corbeille", "Vaciar papelera", "Opróżnij kosz"},
	"maint.lastbackup":            {"ultimo %s · %d backup", "last %s · %d backups", "letztes %s · %d Backup", "dernier %s · %d sauvegardes", "último %s · %d copias", "ostatni %s · %d kopii"},
	"maint.nobackup":              {"nessun backup", "no backup", "kein Backup", "aucune sauvegarde", "ninguna copia", "brak kopii"},
	"maint.dbsize":                {"%d file · %s", "%d files · %s", "%d Dateien · %s", "%d fichiers · %s", "%d archivos · %s", "%d plików · %s"},
	"maint.ramdiskoff":            {"non configurato", "not configured", "nicht konfiguriert", "non configuré", "no configurado", "nieskonfigurowany"},
	"maint.dbresult":              {"prima %s, ora %s", "before %s, now %s", "vorher %s, jetzt %s", "avant %s, maintenant %s", "antes %s, ahora %s", "wcześniej %s, teraz %s"},
	"maint.done":                  {"completato", "done", "abgeschlossen", "terminé", "completado", "ukończono"},
	"maint.duptitle":              {"Duplicati inferiori: %d (x elimina)", "Inferior duplicates: %d (x deletes)", "Geringere Duplikate: %d (x löschen)", "Doublons inférieurs : %d (x supprimer)", "Duplicados inferiores: %d (x eliminar)", "Niższe duplikaty: %d (x usuń)"},
	"maint.noduplicates":          {"nessun duplicato inferiore", "no inferior duplicate", "keine geringeren Duplikate", "aucun doublon inférieur", "ningún duplicado inferior", "brak niższych duplikatów"},
	"maint.foldertitle":           {"Rinomina %s: %d file (Spazio includi, x applica)", "Rename %s: %d files (Space include, x apply)", "%s umbenennen: %d Dateien (Leertaste einschließen, x anwenden)", "Renommer %s : %d fichiers (Espace inclure, x appliquer)", "Renombrar %s: %d archivos (Espacio incluir, x aplicar)", "Zmień nazwę %s: %d plików (Spacja uwzględnij, x zastosuj)"},
	"maint.folderempty":           {"nessun file da rinominare", "no file to rename", "keine Dateien umzubenennen", "aucun fichier à renommer", "ningún archivo que renombrar", "brak plików do zmiany nazwy"},
	"msg.autoremoveon":            {"auto-rimozione dei completati attiva", "auto-removal of completed torrents on", "automatisches Entfernen der Abgeschlossenen aktiv", "suppression automatique des terminés activée", "eliminación automática de los completados activada", "automatyczne usuwanie ukończonych włączone"},
	"msg.autoremoveoff":           {"auto-rimozione dei completati disattiva", "auto-removal of completed torrents off", "automatisches Entfernen der Abgeschlossenen inaktiv", "suppression automatique des terminés désactivée", "eliminación automática de los completados desactivada", "automatyczne usuwanie ukończonych wyłączone"},
	"msg.backupdone":              {"backup creato: %s (%s)", "backup created: %s (%s)", "Backup erstellt: %s (%s)", "sauvegarde créée : %s (%s)", "copia creada: %s (%s)", "kopia utworzona: %s (%s)"},
	"msg.bulkdone":                {"%s: %d/%d torrent", "%s: %d/%d torrents", "%s: %d/%d torrent", "%s: %d/%d torrent", "%s: %d/%d torrent", "%s: %d/%d torrent"},
	"msg.duplicatesremoved":       {"duplicati eliminati: %d", "duplicates removed: %d", "Duplikate gelöscht: %d", "doublons supprimés : %d", "duplicados eliminados: %d", "usunięte duplikaty: %d"},
	"msg.folderrenamed":           {"file rinominati: %d/%d", "files renamed: %d/%d", "umbenannte Dateien: %d/%d", "fichiers renommés : %d/%d", "archivos renombrados: %d/%d", "zmienione nazwy plików: %d/%d"},
	"msg.jobcancelled":            {"annullamento richiesto", "cancellation requested", "Abbruch angefordert", "annulation demandée", "cancelación solicitada", "zażądano anulowania"},
	"msg.jobstarted":              {"avviato: %s", "started: %s", "gestartet: %s", "démarré: %s", "iniciado: %s", "uruchomiony: %s"},
	"msg.loadingsettings":         {"impostazioni non ancora caricate, riprova", "settings not loaded yet, try again", "Einstellungen noch nicht geladen, versuche es erneut", "paramètres pas encore chargés, réessayez", "ajustes aún no cargados, inténtalo de nuevo", "ustawienia nie zostały jeszcze wczytane, spróbuj ponownie"},
	"msg.maintfailed":             {"%s non riuscito: %s", "%s failed: %s", "%s fehlgeschlagen: %s", "%s échoué : %s", "%s falló: %s", "%s nie powiodło się: %s"},
	"msg.markedfailed":            {"scartato e rimosso: al prossimo ciclo si cerca un'altra versione", "discarded and removed: the next cycle looks for another version", "verworfen und entfernt: beim nächsten Durchlauf wird eine andere Version gesucht", "écarté et supprimé: au prochain cycle, une autre version sera recherchée", "descartado y eliminado: en el próximo ciclo se buscará otra versión", "odrzucono i usunięto: w następnym cyklu zostanie wyszukana inna wersja"},
	"msg.marksclear":              {"selezione annullata", "marks cleared", "Auswahl abgebrochen", "sélection annulée", "selección cancelada", "anulowano wybór"},
	"msg.moving":                  {"spostamento verso %s", "moving to %s", "Verschieben nach %s", "déplacement vers %s", "movimiento hacia %s", "przenoszenie do %s"},
	"msg.nothingselected":         {"nessun elemento selezionato", "nothing selected", "kein Element ausgewählt", "aucun élément sélectionné", "ningún elemento seleccionado", "nie wybrano żadnego elementu"},
	"msg.prioritysaved":           {"priorità salvata", "priority saved", "Priorität gespeichert", "priorité enregistrée", "prioridad guardada", "zapisano priorytet"},
	"msg.ramdiskset":              {"RAM disk: %s", "RAM disk: %s", "RAM disk: %s", "RAM disk: %s", "RAM disk: %s", "RAM disk: %s"},
	"msg.superseedingon":          {"super-seeding attivo", "super-seeding on", "super-seeding aktiv", "super-seeding actif", "super-seeding activo", "super-seeding aktywny"},
	"msg.superseedingoff":         {"super-seeding disattivo", "super-seeding off", "super-seeding inaktiv", "super-seeding inactif", "super-seeding inactivo", "super-seeding nieaktywny"},
	"msg.tagremoved":              {"tag rimosso da %d torrent", "tag removed from %d torrents", "Tag von %d torrent entfernt", "tag retiré de %d torrent", "etiqueta eliminada de %d torrent", "usunięto tag z %d torrent"},
	"msg.tagset":                  {"tag '%s' su %d torrent", "tag '%s' on %d torrents", "Tag '%s' auf %d torrent", "tag '%s' sur %d torrent", "etiqueta '%s' en %d torrent", "tag '%s' w %d torrent"},
	"msg.torrentlimits":           {"limiti torrent ↓%s ↑%s", "torrent limits ↓%s ↑%s", "torrent-Limits ↓%s ↑%s", "limites torrent ↓%s ↑%s", "límites torrent ↓%s ↑%s", "limity torrent ↓%s ↑%s"},
	"msg.torrentlimitsinvalid":    {"formato: DL UL [ratio] [giorni], KiB/s, 0 = illimitato", "format: DL UL [ratio] [days], KiB/s, 0 = unlimited", "Format: DL UL [ratio] [Tage], KiB/s, 0 = unbegrenzt", "format: DL UL [ratio] [jours], KiB/s, 0 = illimité", "formato: DL UL [ratio] [días], KiB/s, 0 = ilimitado", "format: DL UL [ratio] [dni], KiB/s, 0 = bez limitu"},
	"msg.trackerexists":           {"tracker già presente", "tracker already present", "Tracker bereits vorhanden", "tracker déjà présent", "tracker ya presente", "tracker już istnieje"},
	"msg.trackerinvalid":          {"URL tracker non valido", "invalid tracker URL", "URL tracker ungültig", "URL tracker invalide", "URL tracker no válido", "nieprawidłowy URL tracker"},
	"msg.trackerssaved":           {"tracker salvati: %d", "trackers saved: %d", "Tracker gespeichert: %d", "trackers enregistrés: %d", "trackers guardados: %d", "zapisano trackery: %d"},
	"priority.skip":               {"salta", "skip", "überspringen", "ignorer", "omitir", "pomiń"},
	"priority.low":                {"bassa", "low", "niedrig", "basse", "baja", "niski"},
	"priority.normal":             {"normale", "normal", "normal", "normale", "normal", "normalny"},
	"priority.high":               {"alta", "high", "hoch", "haute", "alta", "wysoki"},
	"priority.top":                {"massima", "top", "maximal", "maximale", "máxima", "maksymalny"},
	"prompt.addtracker":           {"URL tracker: ", "Tracker URL: ", "URL tracker: ", "URL tracker: ", "URL tracker: ", "URL tracker: "},
	"prompt.bulkremove":           {"Rimuovo %d torrent (i file restano)? [s/N] ", "Remove %d torrents (files kept)? [y/N] ", "%d torrent entfernen (Dateien bleiben erhalten)? [j/N] ", "supprimer %d torrent (les fichiers restent)? [o/N] ", "¿eliminar %d torrent (los archivos permanecen)? [s/N] ", "usunąć %d torrent (pliki pozostaną)? [t/N] "},
	"prompt.bulkremovefiles":      {"Rimuovo %d torrent E i loro file? [s/N] ", "Remove %d torrents AND their files? [y/N] ", "%d torrent UND ihre Dateien entfernen? [j/N] ", "supprimer %d torrent ET leurs fichiers? [o/N] ", "¿eliminar %d torrent Y sus archivos? [s/N] ", "usunąć %d torrent ORAZ ich pliki? [t/N] "},
	"prompt.canceljob":            {"Annullo il job %s? [s/N] ", "Cancel job %s? [y/N] ", "Job %s abbrechen? [j/N] ", "annuler le job %s? [o/N] ", "¿cancelar el job %s? [s/N] ", "anulować zadanie %s? [t/N] "},
	"prompt.duplicates":           {"Elimino %d file duplicati? [s/N] ", "Delete %d duplicate files? [y/N] ", "%d doppelte Dateien löschen? [j/N] ", "supprimer %d fichiers en double? [o/N] ", "¿eliminar %d archivos duplicados? [s/N] ", "usunąć %d zduplikowanych plików? [t/N] "},
	"prompt.folderrename":         {"Cartella da rinominare: ", "Folder to rename: ", "Umzubenennender Ordner: ", "dossier à renommer: ", "carpeta a renombrar: ", "folder do zmiany nazwy: "},
	"prompt.historyfilter":        {"Filtro storico: ", "History filter: ", "Verlaufsfilter: ", "filtre historique: ", "filtro historial: ", "filtr historii: "},
	"prompt.maint.housekeeping":   {"Eseguo ora la pulizia del database? [s/N] ", "Run database housekeeping now? [y/N] ", "Datenbankbereinigung jetzt ausführen? [j/N] ", "effectuer maintenant le nettoyage de la base de données? [o/N] ", "¿ejecutar ahora la limpieza de la base de datos? [s/N] ", "wykonać teraz czyszczenie bazy danych? [t/N] "},
	"prompt.maint.renameall":      {"Rinomino i file di tutte le serie? [s/N] ", "Rename the files of all series? [y/N] ", "Dateien aller Serien umbenennen? [j/N] ", "renommer les fichiers de toutes les séries? [o/N] ", "¿renombrar los archivos de todas las series? [s/N] ", "zmienić nazwy plików wszystkich serii? [t/N] "},
	"prompt.maint.scanarchives":   {"Scansiono tutti gli archivi? [s/N] ", "Scan all archives? [y/N] ", "Alle Archive scannen? [j/N] ", "scanner toutes les archives? [o/N] ", "¿escanear todos los archivos? [s/N] ", "zeskanować wszystkie archiwa? [t/N] "},
	"prompt.maint.vacuum":         {"Compatto i database (VACUUM, può richiedere tempo)? [s/N] ", "Compact the databases (VACUUM, may take a while)? [y/N] ", "Datenbanken komprimieren (VACUUM, kann Zeit dauern)? [j/N] ", "compacter les bases de données (VACUUM, cela peut prendre du temps)? [o/N] ", "¿compactar las bases de datos (VACUUM, puede tardar)? [s/N] ", "skompaktować bazy danych (VACUUM, może zająć trochę czasu)? [t/N] "},
	"prompt.markfailed":           {"Scarto '%s' (blocklist + rimozione) e cerco un'altra versione al prossimo ciclo? [s/N] ", "Discard '%s' (blocklist + remove) and look for another version at the next cycle? [y/N] ", "'%s' verwerfen (Blocklist + Entfernung) und beim nächsten Durchlauf eine andere Version suchen? [j/N] ", "écarter '%s' (blocklist + suppression) et chercher une autre version au prochain cycle? [o/N] ", "¿descartar '%s' (blocklist + eliminación) y buscar otra versión en el próximo ciclo? [s/N] ", "odrzucić '%s' (blocklist + usunięcie) i w następnym cyklu wyszukać inną wersję? [t/N] "},
	"prompt.movestorage":          {"Sposta in: ", "Move to: ", "Verschieben nach: ", "déplacer vers: ", "mover a: ", "przenieś do: "},
	"prompt.ramdisk":              {"Cartella RAM disk: ", "RAM disk folder: ", "RAM disk-Ordner: ", "dossier RAM disk: ", "carpeta RAM disk: ", "folder RAM disk: "},
	"prompt.tag":                  {"Tag (esistenti: %s; vuoto = nessuno): ", "Tag (existing: %s; empty = none): ", "Tag (vorhanden: %s; leer = keine): ", "tag (existants: %s; vide = aucun): ", "etiqueta (existentes: %s; vacío = ninguna): ", "tag (istniejące: %s; puste = brak): "},
	"prompt.tagbulk":              {"Tag per %d torrent (esistenti: %s; vuoto = nessuno): ", "Tag for %d torrents (existing: %s; empty = none): ", "Tag für %d torrent (vorhanden: %s; leer = keine): ", "tag pour %d torrent (existants: %s; vide = aucun): ", "etiqueta para %d torrent (existentes: %s; vacío = ninguna): ", "tag dla %d torrent (istniejące: %s; puste = brak): "},
	"prompt.torrentlimits":        {"Limiti torrent DL UL KiB/s [ratio] [giorni] (0 = illimitato): ", "Torrent limits DL UL KiB/s [ratio] [days] (0 = unlimited): ", "torrent-Limits DL UL KiB/s [ratio] [Tage] (0 = unbegrenzt): ", "limites torrent DL UL KiB/s [ratio] [jours] (0 = illimité): ", "límites torrent DL UL KiB/s [ratio] [días] (0 = ilimitado): ", "limity torrent DL UL KiB/s [ratio] [dni] (0 = bez limitu): "},
	"prompt.trackerremove":        {"Rimuovo il tracker %s? [s/N] ", "Remove tracker %s? [y/N] ", "Tracker %s entfernen? [j/N] ", "supprimer le tracker %s? [o/N] ", "¿eliminar el tracker %s? [s/N] ", "usunąć tracker %s? [t/N] "},
	"prompt.folderapply":          {"Rinomino %d file? [s/N] ", "Rename %d files? [y/N] ", "%d Dateien umbenennen? [j/N] ", "renommer %d fichiers? [o/N] ", "¿renombrar %d archivos? [s/N] ", "zmienić nazwy %d plików? [t/N] "},
	"library.switch":              {"(←→ per cambiare)", "(←→ to switch)", "(←→ zum Ändern)", "(←→ pour changer)", "(←→ para cambiar)", "(←→ aby zmienić)"},
	"hint.librarymanage":          {"Invio dettagli · a aggiungi (TMDB) · e modifica · p pausa/riprendi · d elimina · m cerca · s filtra · ←→ serie/film/fumetti · Esc indietro", "Enter details · a add (TMDB) · e edit · p pause/resume · d delete · m search · s filter · ←→ series/movies/comics · Esc back", "Enter Details · a hinzufügen (TMDB) · e bearbeiten · p Pause/Fortsetzen · d löschen · m suchen · s filtern · ←→ Serien/Filme/Comics · Esc zurück", "Entrée détails · a ajouter (TMDB) · e modifier · p pause/reprendre · d supprimer · m rechercher · s filtrer · ←→ séries/films/comics · Esc retour", "Intro detalles · a añadir (TMDB) · e modificar · p pausa/reanudar · d eliminar · m buscar · s filtrar · ←→ series/películas/cómics · Esc atrás", "Enter szczegóły · a dodaj (TMDB) · e edytuj · p pauza/wznów · d usuń · m szukaj · s filtruj · ←→ serie/filmy/komiksy · Esc wstecz"},
	"hint.series":                 {"←→ stagione · Spazio attiva/disattiva stagione · Invio sorgenti · s cerca online · i ignora · R riscarica · m cerca mancanti · e modifica · n rinomina · M metadati · p pausa · d elimina · Esc indietro", "←→ season · Space season on/off · Enter sources · s search online · i ignore · R redownload · m search missing · e edit · n rename · M metadata · p pause · d delete · Esc back", "←→ Staffel · Leertaste Staffel aktivieren/deaktivieren · Enter Quellen · s online suchen · i ignorieren · R erneut herunterladen · m fehlende suchen · e bearbeiten · n umbenennen · M Metadaten · p Pause · d löschen · Esc zurück", "←→ saison · Espace activer/désactiver la saison · Entrée sources · s rechercher en ligne · i ignorer · R retélécharger · m rechercher manquants · e modifier · n renommer · M métadonnées · p pause · d supprimer · Esc retour", "←→ temporada · Espacio activar/desactivar temporada · Intro fuentes · s buscar en línea · i ignorar · R volver a descargar · m buscar faltantes · e modificar · n renombrar · M metadatos · p pausa · d eliminar · Esc atrás", "←→ sezon · Spacja włącz/wyłącz sezon · Enter źródła · s szukaj online · i ignoruj · R pobierz ponownie · m szukaj brakujących · e edytuj · n zmień nazwę · M metadane · p pauza · d usuń · Esc wstecz"},
	"hint.movie":                  {"↑↓ archivio · Invio accoda · s cerca online · e modifica · R riscarica · p pausa · d elimina · y copia magnet · Esc indietro", "↑↓ archive · Enter queue · s search online · e edit · R redownload · p pause · d delete · y copy magnet · Esc back", "↑↓ Archiv · Enter in Warteschlange · s online suchen · e bearbeiten · R erneut herunterladen · p Pause · d löschen · y magnet kopieren · Esc zurück", "↑↓ archive · Entrée mettre en file · s rechercher en ligne · e modifier · R retélécharger · p pause · d supprimer · y copier magnet · Esc retour", "↑↓ archivo · Intro en cola · s buscar en línea · e modificar · R volver a descargar · p pausa · d eliminar · y copiar magnet · Esc atrás", "↑↓ archiwum · Enter do kolejki · s szukaj online · e edytuj · R pobierz ponownie · p pauza · d usuń · y kopiuj magnet · Esc wstecz"},
	"hint.form":                   {"↑↓ campo · Invio modifica (sì/no: Invio o Spazio) · s salva · Esc annulla", "↑↓ field · Enter edit (yes/no: Enter or Space) · s save · Esc cancel", "↑↓ Feld · Enter bearbeiten (Ja/Nein: Enter oder Leertaste) · s speichern · Esc abbrechen", "↑↓ champ · Entrée modifier (oui/non: Entrée ou Espace) · s enregistrer · Esc annuler", "↑↓ campo · Intro modificar (sí/no: Intro o Espacio) · s guardar · Esc cancelar", "↑↓ pole · Enter edytuj (tak/nie: Enter lub Spacja) · s zapisz · Esc anuluj"},
	"hint.tmdb":                   {"↑↓ seleziona · Invio aggiungi alla libreria · Esc chiudi", "↑↓ select · Enter add to library · Esc close", "↑↓ auswählen · Enter zur Bibliothek hinzufügen · Esc schließen", "↑↓ sélectionner · Entrée ajouter à la bibliothèque · Esc fermer", "↑↓ seleccionar · Intro añadir a la biblioteca · Esc cerrar", "↑↓ wybierz · Enter dodaj do biblioteki · Esc zamknij"},
	"hint.search":                 {"↑↓ seleziona · Invio accoda · y copia magnet · Esc chiudi", "↑↓ select · Enter queue · y copy magnet · Esc close", "↑↓ auswählen · Enter in Warteschlange · y magnet kopieren · Esc schließen", "↑↓ sélectionner · Entrée mettre en file · y copier magnet · Esc fermer", "↑↓ seleccionar · Intro en cola · y copiar magnet · Esc cerrar", "↑↓ wybierz · Enter do kolejki · y kopiuj magnet · Esc zamknij"},
	"library.active":              {"attiva", "active", "aktiv", "active", "activa", "aktywna"},
	"library.paused":              {"in pausa", "paused", "pausiert", "en pause", "en pausa", "wstrzymano"},
	"library.episodes":            {"%d/%d ep.", "%d/%d ep.", "%d/%d ep.", "%d/%d ep.", "%d/%d ep.", "%d/%d ep."},
	"library.last":                {"ultimo %s", "last %s", "letzter %s", "dernier %s", "último %s", "ostatni %s"},
	"label.score":                 {"punti %d", "score %d", "Punkte %d", "points %d", "puntos %d", "punkty %d"},
	"label.seeders":               {"%d seed", "%d seeds", "%d seed", "%d seed", "%d seed", "%d seed"},
	"label.vote":                  {"voto %.1f", "rating %.1f", "Bewertung %.1f", "note %.1f", "voto %.1f", "ocena %.1f"},
	"label.inlibrary":             {"già in libreria", "already in library", "bereits in der Bibliothek", "déjà dans la bibliothèque", "ya en la biblioteca", "już w bibliotece"},
	"label.seasonsfield":          {"stagioni %s", "seasons %s", "Staffeln %s", "saisons %s", "temporadas %s", "sezony %s"},
	"series.seasons":              {"Stagioni", "Seasons", "Staffeln", "Saisons", "Temporadas", "Sezony"},
	"series.off":                  {"off", "off", "aus", "désactivé", "desactivado", "wyłączona"},
	"series.noseasons":            {"nessuna stagione nota: M aggiorna i metadati da TMDB", "no known seasons: M refreshes the metadata from TMDB", "keine Staffel bekannt: M aktualisiert die Metadaten von TMDB", "aucune saison connue: M met à jour les métadonnées depuis TMDB", "ninguna temporada conocida: M actualiza los metadatos desde TMDB", "brak znanego sezonu: M aktualizuje metadane z TMDB"},
	"series.noepisodes":           {"nessun episodio noto per questa stagione", "no known episodes in this season", "keine Episode für diese Staffel bekannt", "aucun épisode connu pour cette saison", "ningún episodio conocido para esta temporada", "brak znanego odcinka w tym sezonie"},
	"series.seasonoff":            {"Stagione %d disattivata: Spazio per riattivarla", "Season %d is switched off: Space switches it on", "Staffel %d deaktiviert: Leertaste zum Reaktivieren", "saison %d désactivée: Espace pour la réactiver", "temporada %d desactivada: Espacio para reactivarla", "sezon %d wyłączony: Spacja, aby go ponownie włączyć"},
	"series.seasonexcluded":       {"Stagione %d esclusa dal campo Stagioni (%s): e per modificarlo", "Season %d is outside the Seasons field (%s): e to edit it", "Staffel %d aus dem Feld Staffeln ausgeschlossen (%s): e zum Bearbeiten", "saison %d exclue du champ Saisons (%s): e pour la modifier", "temporada %d excluida del campo Temporadas (%s): e para modificarlo", "sezon %d wykluczony z pola Sezony (%s): e aby edytować"},
	"episode.missing":             {"mancante", "missing", "fehlend", "manquant", "faltante", "brakujący"},
	"episode.ignored":             {"ignorato", "ignored", "ignoriert", "ignoré", "ignorado", "zignorowany"},
	"episode.upcoming":            {"in uscita", "upcoming", "erscheint", "à venir", "próximamente", "wkrótce"},
	"movie.history":               {"Scaricato", "Downloaded", "Heruntergeladen", "Téléchargé", "Descargado", "Pobrano"},
	"movie.nohistory":             {"non ancora scaricato", "not downloaded yet", "noch nicht heruntergeladen", "pas encore téléchargé", "aún no descargado", "jeszcze nie pobrano"},
	"movie.matches":               {"In archivio: %d (Invio accoda)", "In the archive: %d (Enter queues)", "Im Archiv: %d (Enter reiht ein)", "Dans l'archive : %d (Entrée met en file)", "En archivo: %d (Intro encola)", "W archiwum: %d (Enter dodaje do kolejki)"},
	"movie.nomatches":             {"nessuna release in archivio: s cerca online", "no release in the archive: s searches online", "keine release im Archiv: s sucht online", "aucune release en archive : s recherche en ligne", "ninguna release en archivo: s busca en línea", "brak release w archiwum: s szuka online"},
	"form.seriesedit":             {"Modifica serie: %s", "Edit series: %s", "Serie bearbeiten: %s", "Modifier la série : %s", "Editar serie: %s", "Edytuj serial: %s"},
	"form.movieedit":              {"Modifica film: %s", "Edit movie: %s", "Film bearbeiten: %s", "Modifier le film : %s", "Editar película: %s", "Edytuj film: %s"},
	"form.seriesadd":              {"Aggiungi serie: %s", "Add series: %s", "Serie hinzufügen: %s", "Ajouter une série : %s", "Añadir serie: %s", "Dodaj serial: %s"},
	"form.movieadd":               {"Aggiungi film: %s", "Add movie: %s", "Film hinzufügen: %s", "Ajouter un film : %s", "Añadir película: %s", "Dodaj film: %s"},
	"field.name":                  {"Nome", "Name", "Name", "Nom", "Nombre", "Nazwa"},
	"field.year":                  {"Anno", "Year", "Jahr", "Année", "Año", "Rok"},
	"field.seasons":               {"Stagioni", "Seasons", "Staffeln", "Saisons", "Temporadas", "Sezony"},
	"field.quality":               {"Qualità", "Quality", "Qualität", "Qualité", "Calidad", "Jakość"},
	"field.language":              {"Lingua", "Language", "Sprache", "Langue", "Idioma", "Język"},
	"field.subtitle":              {"Sottotitoli", "Subtitles", "Untertitel", "Sous-titres", "Subtítulos", "Napisy"},
	"field.exclude":               {"Escludi", "Exclude", "Ausschließen", "Exclure", "Excluir", "Wyklucz"},
	"field.aliases":               {"Alias", "Aliases", "Alias", "Alias", "Alias", "Alias"},
	"field.archive_path":          {"Cartella archivio", "Archive folder", "Archivordner", "Dossier d'archive", "Carpeta de archivo", "Folder archiwum"},
	"field.tmdb_id":               {"TMDB ID", "TMDB ID", "TMDB ID", "TMDB ID", "TMDB ID", "TMDB ID"},
	"field.tvdb_id":               {"TVDB ID", "TVDB ID", "TVDB ID", "TVDB ID", "TVDB ID", "TVDB ID"},
	"field.season_subfolders":     {"Sottocartelle stagione", "Season subfolders", "Unterordner pro Staffel", "Sous-dossiers par saison", "Subcarpetas por temporada", "Podfoldery sezonu"},
	"field.disable_upgrades":      {"Niente upgrade", "No upgrades", "Kein Upgrade", "Aucune mise à niveau", "Sin actualización", "Bez aktualizacji"},
	"field.anime":                 {"Anime (numerazione assoluta)", "Anime (absolute numbering)", "Anime (absolute Nummerierung)", "Anime (numérotation absolue)", "Anime (numeración absoluta)", "Anime (numeracja absolutna)"},
	"fieldhint.name":              {"Titolo usato per riconoscere le release.", "Title used to match releases.", "Titel zum Erkennen der release.", "Titre utilisé pour reconnaître les release.", "Título usado para reconocer las release.", "Tytuł używany do rozpoznawania release."},
	"fieldhint.year":              {"Anno di uscita, aiuta a distinguere titoli omonimi.", "Release year, tells apart titles with the same name.", "Erscheinungsjahr, hilft gleichnamige Titel zu unterscheiden.", "Année de sortie, aide à distinguer les titres homonymes.", "Año de estreno, ayuda a distinguir títulos homónimos.", "Rok wydania, pomaga odróżnić tytuły o tej samej nazwie."},
	"fieldhint.seasons":           {"Stagioni da seguire: 1+ (dalla 1 in poi), 2-4, 1,3 oppure * per tutte.", "Seasons to follow: 1+ (from 1 on), 2-4, 1,3 or * for all.", "Zu verfolgende Staffeln: 1+ (ab 1), 2-4, 1,3 oder * für alle.", "Saisons à suivre : 1+ (à partir de 1), 2-4, 1,3 ou * pour toutes.", "Temporadas a seguir: 1+ (desde la 1), 2-4, 1,3 o * para todas.", "Sezony do śledzenia: 1+ (od 1), 2-4, 1,3 lub * dla wszystkich."},
	"fieldhint.quality":           {"Risoluzione: 1080p (minima), 720p-1080p (intervallo), <2160p (massima); vuoto = qualsiasi.", "Resolution: 1080p (minimum), 720p-1080p (range), <2160p (maximum); empty = any.", "Auflösung: 1080p (Minimum), 720p-1080p (Bereich), <2160p (Maximum); leer = beliebig.", "Résolution : 1080p (minimale), 720p-1080p (plage), <2160p (maximale) ; vide = n'importe laquelle.", "Resolución: 1080p (mínima), 720p-1080p (intervalo), <2160p (máxima); vacío = cualquiera.", "Rozdzielczość: 1080p (minimalna), 720p-1080p (zakres), <2160p (maksymalna); puste = dowolna."},
	"fieldhint.language":          {"Lingua audio richiesta, es. ita oppure ita,eng.", "Required audio language, e.g. ita or ita,eng.", "Gewünschte Audiosprache, z. B. ita oder ita,eng.", "Langue audio souhaitée, ex. ita ou ita,eng.", "Idioma de audio solicitado, p. ej. ita o ita,eng.", "Żądany język audio, np. ita lub ita,eng."},
	"fieldhint.subtitle":          {"Sottotitoli richiesti, separati da virgola; vuoto = non richiesti.", "Required subtitles, comma separated; empty = not required.", "Gewünschte Untertitel, durch Komma getrennt; leer = keine.", "Sous-titres souhaités, séparés par une virgule ; vide = aucun.", "Subtítulos solicitados, separados por coma; vacío = ninguno.", "Żądane napisy, oddzielone przecinkiem; puste = brak."},
	"fieldhint.exclude":           {"Parole che scartano una release se compaiono nel titolo.", "Words that reject a release when they appear in its title.", "Wörter, die eine release verwerfen, wenn sie im Titel vorkommen.", "Mots qui rejettent une release s'ils apparaissent dans le titre.", "Palabras que descartan una release si aparecen en el título.", "Słowa, które odrzucają release, jeśli pojawią się w tytule."},
	"fieldhint.aliases":           {"Altri titoli con cui la serie compare nelle release, separati da virgola.", "Other titles the series appears under in releases, comma separated.", "Andere Titel, unter denen die Serie in release vorkommt, durch Komma getrennt.", "Autres titres sous lesquels la série apparaît dans les release, séparés par une virgule.", "Otros títulos con los que la serie aparece en las release, separados por coma.", "Inne tytuły, pod którymi serial występuje w release, oddzielone przecinkiem."},
	"fieldhint.archive_path":      {"Cartella della serie sul NAS; vuoto = sotto la radice dell'archivio.", "Series folder on the NAS; empty = under the archive root.", "Ordner der Serie auf dem NAS; leer = unter der Archivwurzel.", "Dossier de la série sur le NAS ; vide = sous la racine de l'archive.", "Carpeta de la serie en el NAS; vacío = bajo la raíz del archivo.", "Folder serialu na NAS; puste = pod katalogiem głównym archiwum."},
	"fieldhint.tmdb_id":           {"Identificativo TMDB: metadati, stagioni ed episodi attesi.", "TMDB id: metadata, seasons and expected episodes.", "TMDB-ID: Metadaten, erwartete Staffeln und Episoden.", "Identifiant TMDB : métadonnées, saisons et épisodes attendus.", "Identificador TMDB: metadatos, temporadas y episodios esperados.", "Identyfikator TMDB: metadane, oczekiwane sezony i odcinki."},
	"fieldhint.tvdb_id":           {"Identificativo TVDB, alternativo a TMDB.", "TVDB id, alternative to TMDB.", "TVDB-ID, Alternative zu TMDB.", "Identifiant TVDB, alternative à TMDB.", "Identificador TVDB, alternativo a TMDB.", "Identyfikator TVDB, alternatywa dla TMDB."},
	"fieldhint.season_subfolders": {"Archivia gli episodi in una sottocartella per stagione.", "Archive episodes in one subfolder per season.", "Speichert die Episoden in einem Unterordner pro Staffel.", "Archive les épisodes dans un sous-dossier par saison.", "Archiva los episodios en una subcarpeta por temporada.", "Archiwizuje odcinki w podfolderze dla każdego sezonu."},
	"fieldhint.disable_upgrades":  {"Sì = una release migliore non sostituisce più un file già archiviato.", "Yes = a better release no longer replaces an archived file.", "Ja = eine bessere release ersetzt eine bereits archivierte Datei nicht mehr.", "Oui = une meilleure release ne remplace plus un fichier déjà archivé.", "Sí = una release mejor ya no sustituye un archivo ya archivado.", "Tak = lepszy release nie zastępuje już zarchiwizowanego pliku."},
	"fieldhint.anime":             {"Sì = le release «Titolo - 1071» vengono convertite in stagione ed episodio.", "Yes = “Title - 1071” releases are mapped to season and episode.", "Ja = release «Titel - 1071» werden in Staffel und Episode umgewandelt.", "Oui = les release «Titre - 1071» sont converties en saison et épisode.", "Sí = las release «Título - 1071» se convierten en temporada y episodio.", "Tak = release «Tytuł - 1071» są konwertowane na sezon i odcinek."},
	"prompt.tmdbseries":           {"Cerca serie su TMDB: ", "Search series on TMDB: ", "Serie auf TMDB suchen: ", "Rechercher une série sur TMDB : ", "Buscar serie en TMDB: ", "Szukaj serialu w TMDB: "},
	"prompt.tmdbmovie":            {"Cerca film su TMDB: ", "Search movie on TMDB: ", "Film auf TMDB suchen: ", "Rechercher un film sur TMDB : ", "Buscar película en TMDB: ", "Szukaj filmu w TMDB: "},
	"prompt.seriesremove":         {"Elimino la serie '%s' dalla libreria (i file restano)? [s/N] ", "Remove series '%s' from the library (files are kept)? [y/N] ", "Serie '%s' aus der Bibliothek löschen (die Dateien bleiben)? [s/N] ", "Supprimer la série '%s' de la bibliothèque (les fichiers restent) ? [s/N] ", "¿Elimino la serie '%s' de la biblioteca (los archivos permanecen)? [s/N] ", "Usunąć serial '%s' z biblioteki (pliki pozostaną)? [s/N] "},
	"prompt.movieremove":          {"Elimino il film '%s' dalla libreria (i file restano)? [s/N] ", "Remove movie '%s' from the library (files are kept)? [y/N] ", "Film '%s' aus der Bibliothek löschen (die Dateien bleiben)? [s/N] ", "Supprimer le film '%s' de la bibliothèque (les fichiers restent) ? [s/N] ", "¿Elimino la película '%s' de la biblioteca (los archivos permanecen)? [s/N] ", "Usunąć film '%s' z biblioteki (pliki pozostaną)? [s/N] "},
	"prompt.episoderedownload":    {"Riscarico S%02dE%02d al prossimo ciclo? [s/N] ", "Download S%02dE%02d again at the next cycle? [y/N] ", "S%02dE%02d beim nächsten Durchlauf erneut herunterladen? [s/N] ", "Retélécharger S%02dE%02d au prochain cycle ? [s/N] ", "¿Volver a descargar S%02dE%02d en el próximo ciclo? [s/N] ", "Ponownie pobrać S%02dE%02d w następnym cyklu? [s/N] "},
	"prompt.movieredownload":      {"Riscarico '%s' al prossimo ciclo? [s/N] ", "Download '%s' again at the next cycle? [y/N] ", "'%s' beim nächsten Durchlauf erneut herunterladen? [s/N] ", "Retélécharger '%s' au prochain cycle ? [s/N] ", "¿Volver a descargar '%s' en el próximo ciclo? [s/N] ", "Ponownie pobrać '%s' w następnym cyklu? [s/N] "},
	"prompt.episodeignore":        {"Ignoro %s S%02dE%02d nella ricerca dei mancanti? [s/N] ", "Ignore %s S%02dE%02d in the missing search? [y/N] ", "%s S%02dE%02d bei der Suche nach fehlenden Episoden ignorieren? [s/N] ", "Ignorer %s S%02dE%02d dans la recherche des manquants ? [s/N] ", "¿Ignorar %s S%02dE%02d en la búsqueda de los faltantes? [s/N] ", "Zignorować %s S%02dE%02d przy wyszukiwaniu brakujących? [s/N] "},
	"prompt.rename":               {"Rinomino %d file di '%s'? [s/N] ", "Rename %d files of '%s'? [y/N] ", "%d Dateien von '%s' umbenennen? [s/N] ", "Renommer %d fichiers de '%s' ? [s/N] ", "¿Renombrar %d archivos de '%s'? [s/N] ", "Zmienić nazwę %d plików z '%s'? [s/N] "},
	"search.missing":              {"mancanti di %s (%d episodi)", "missing from %s (%d episodes)", "fehlend für %s (%d Episoden)", "manquants de %s (%d épisodes)", "faltantes de %s (%d episodios)", "brakujące dla %s (%d odcinków)"},
	"msg.libraryfailed":           {"operazione non riuscita: %s", "operation failed: %s", "Vorgang fehlgeschlagen: %s", "opération échouée : %s", "operación fallida: %s", "operacja nie powiodła się: %s"},
	"msg.seriessaved":             {"serie salvata", "series saved", "Serie gespeichert", "série enregistrée", "serie guardada", "serial zapisany"},
	"msg.seriespaused":            {"'%s' in pausa", "'%s' paused", "'%s' pausiert", "'%s' en pause", "'%s' en pausa", "'%s' wstrzymany"},
	"msg.seriesresumed":           {"'%s' di nuovo attiva", "'%s' active again", "'%s' wieder aktiv", "'%s' de nouveau active", "'%s' de nuevo activa", "'%s' ponownie aktywny"},
	"msg.seriesremoved":           {"'%s' eliminata dalla libreria", "'%s' removed from the library", "'%s' aus der Bibliothek gelöscht", "'%s' supprimée de la bibliothèque", "'%s' eliminada de la biblioteca", "'%s' usunięty z biblioteki"},
	"msg.moviesaved":              {"'%s' salvato", "'%s' saved", "'%s' gespeichert", "'%s' enregistré", "'%s' guardado", "'%s' zapisany"},
	"msg.movieremoved":            {"film eliminato dalla libreria", "movie removed from the library", "Film aus der Bibliothek gelöscht", "film supprimé de la bibliothèque", "película eliminada de la biblioteca", "film usunięty z biblioteki"},
	"msg.libraryadded":            {"'%s' aggiunto alla libreria", "'%s' added to the library", "'%s' zur Bibliothek hinzugefügt", "'%s' ajouté à la bibliothèque", "'%s' añadido a la biblioteca", "'%s' dodany do biblioteki"},
	"msg.tmdbresults":             {"TMDB: %d risultati", "TMDB: %d results", "TMDB: %d Ergebnisse", "TMDB : %d résultats", "TMDB: %d resultados", "TMDB: %d wyników"},
	"msg.inlibrary":               {"già in libreria", "already in the library", "bereits in der Bibliothek", "déjà dans la bibliothèque", "ya en la biblioteca", "już w bibliotece"},
	"msg.seasonon":                {"stagione %d attivata", "season %d switched on", "Staffel %d aktiviert", "saison %d activée", "temporada %d activada", "sezon %d aktywowany"},
	"msg.seasonoff":               {"stagione %d disattivata", "season %d switched off", "Staffel %d deaktiviert", "saison %d désactivée", "temporada %d desactivada", "sezon %d dezaktywowany"},
	"msg.seasonexcluded":          {"la stagione %d è fuori dal campo Stagioni: e per modificarlo", "season %d is outside the Seasons field: e to edit it", "Staffel %d liegt außerhalb des Felds Staffeln: e zum Bearbeiten", "la saison %d est hors du champ Saisons : e pour le modifier", "la temporada %d está fuera del campo Temporadas: e para modificarlo", "sezon %d jest poza polem Sezony: e aby edytować"},
	"msg.nomissing":               {"nessun episodio mancante da cercare", "no missing episode to search", "keine fehlenden Episoden zu suchen", "aucun épisode manquant à rechercher", "ningún episodio faltante que buscar", "brak brakujących odcinków do wyszukania"},
	"msg.metadataupdated":         {"metadati aggiornati da TMDB", "metadata refreshed from TMDB", "Metadaten von TMDB aktualisiert", "métadonnées mises à jour depuis TMDB", "metadatos actualizados desde TMDB", "metadane zaktualizowane z TMDB"},
	"msg.renamenone":              {"i nomi dei file sono già corretti", "file names are already correct", "die Dateinamen sind bereits korrekt", "les noms des fichiers sont déjà corrects", "los nombres de los archivos ya son correctos", "nazwy plików są już poprawne"},
	"msg.renamed":                 {"%d file rinominati", "%d files renamed", "%d Dateien umbenannt", "%d fichiers renommés", "%d archivos renombrados", "%d plików zmieniono nazwę"},
	"msg.nosources":               {"nessuna sorgente in archivio per %s: s cerca online", "no archived source for %s: s searches online", "keine Quelle im Archiv für %s: s sucht online", "aucune source en archive pour %s : s recherche en ligne", "ninguna fuente en archivo para %s: s busca en línea", "brak źródła w archiwum dla %s: s szuka online"},
	"msg.episodeignored":          {"%s ignorato", "%s ignored", "%s ignoriert", "%s ignoré", "%s ignorado", "%s zignorowany"},
	"msg.episodeunignored":        {"%s non più ignorato", "%s no longer ignored", "%s nicht mehr ignoriert", "%s n'est plus ignoré", "%s ya no ignorado", "%s nie jest już ignorowany"},
	"msg.episoderedownload":       {"%s sarà riscaricato al prossimo ciclo", "%s will be downloaded again at the next cycle", "%s wird beim nächsten Durchlauf erneut heruntergeladen", "%s sera retéléchargé au prochain cycle", "%s se volverá a descargar en el próximo ciclo", "%s zostanie ponownie pobrany w następnym cyklu"},
	"msg.movieredownload":         {"film rimesso in coda al prossimo ciclo", "movie queued again for the next cycle", "Film beim nächsten Durchlauf erneut in die Warteschlange gestellt", "film remis en file d'attente au prochain cycle", "película vuelta a poner en cola para el próximo ciclo", "film ponownie dodany do kolejki w następnym cyklu"},
	"msg.nochanges":               {"nessuna modifica", "no changes", "Keine Änderung", "Aucune modification", "Sin cambios", "Brak zmian"},
	"msg.namerequired":            {"il nome è obbligatorio", "the name is required", "Der Name ist erforderlich", "Le nom est obligatoire", "El nombre es obligatorio", "Nazwa jest wymagana"},
	"msg.tmdbrequired":            {"serve il TMDB ID", "the TMDB id is required", "TMDB ID erforderlich", "TMDB ID requis", "Se requiere TMDB ID", "Wymagany TMDB ID"},
	"msg.seasonsrequired":         {"indica le stagioni da seguire (es. 1+)", "set the seasons to follow (e.g. 1+)", "Gib die zu verfolgenden Staffeln an (z. B. 1+)", "Indiquez les saisons à suivre (ex. 1+)", "Indica las temporadas a seguir (ej. 1+)", "Podaj sezony do śledzenia (np. 1+)"},
	"help.series":                 {"Serie:    ←→ stagione · Spazio stagione on/off · Invio sorgenti · s cerca · i ignora · R riscarica · m mancanti · e modifica · n rinomina · M metadati", "Series:   ←→ season · Space season on/off · Enter sources · s search · i ignore · R redownload · m missing · e edit · n rename · M metadata", "Serie:    ←→ Staffel · Leertaste Staffel an/aus · Eingabe Quellen · s suchen · i ignorieren · R erneut laden · m fehlend · e bearbeiten · n umbenennen · M Metadaten", "Séries :   ←→ saison · Espace saison on/off · Entrée sources · s chercher · i ignorer · R retélécharger · m manquants · e modifier · n renommer · M métadonnées", "Serie:    ←→ temporada · Espacio temporada on/off · Intro fuentes · s buscar · i ignorar · R redescargar · m faltantes · e modificar · n renombrar · M metadatos", "Serie:    ←→ sezon · Spacja sezon wł/wył · Enter źródła · s szukaj · i ignoruj · R pobierz ponownie · m brakujące · e edytuj · n zmień nazwę · M metadane"},
	"help.movie":                  {"Film:     Invio accoda dall'archivio · s cerca online · e modifica · R riscarica · p pausa · d elimina", "Movie:    Enter queue from archive · s search online · e edit · R redownload · p pause · d delete", "Film:     Eingabe hängt aus dem Archiv an · s online suchen · e bearbeiten · R erneut laden · p Pause · d löschen", "Film :     Entrée mettre en file depuis l'archive · s chercher en ligne · e modifier · R retélécharger · p pause · d supprimer", "Film:     Intro encolar desde el archivo · s buscar en línea · e modificar · R redescargar · p pausa · d eliminar", "Film:     Enter zakolejkuj z archiwum · s szukaj online · e edytuj · R pobierz ponownie · p pauza · d usuń"},
	"help.form":                   {"Moduli:   ↑↓ campo · Invio modifica · s salva · Esc annulla (* = campo modificato)", "Forms:    ↑↓ field · Enter edit · s save · Esc cancel (* = changed field)", "Module:   ↑↓ Feld · Eingabe bearbeiten · s speichern · Esc abbrechen (* = geändertes Feld)", "Modules :   ↑↓ champ · Entrée modifier · s enregistrer · Esc annuler (* = champ modifié)", "Módulos:   ↑↓ campo · Intro modificar · s guardar · Esc cancelar (* = campo modificado)", "Moduły:   ↑↓ pole · Enter edytuj · s zapisz · Esc anuluj (* = zmienione pole)"},
	"hint.details":                {"1 generale · 2 tracker · 3 file · 4 peer · ↑↓ scorri · F scarta e cerca un'altra versione · Invio/Esc indietro", "1 general · 2 trackers · 3 files · 4 peers · ↑↓ scroll · F discard and find another version · Enter/Esc back", "1 allgemein · 2 Tracker · 3 Dateien · 4 Peers · ↑↓ scrollen · F verwerfen und andere Version suchen · Eingabe/Esc zurück", "1 général · 2 trackers · 3 fichiers · 4 pairs · ↑↓ faire défiler · F rejeter et chercher une autre version · Entrée/Esc retour", "1 general · 2 trackers · 3 archivos · 4 pares · ↑↓ desplazar · F descartar y buscar otra versión · Intro/Esc atrás", "1 ogólne · 2 trackery · 3 pliki · 4 peery · ↑↓ przewijaj · F odrzuć i szukaj innej wersji · Enter/Esc wstecz"},
	"hint.settings":               {"↑↓ seleziona · Invio modifica · c colori · h contrasto · Esc chiudi", "↑↓ select · Enter edit · c colors · h contrast · Esc close", "↑↓ auswählen · Eingabe bearbeiten · c Farben · h Kontrast · Esc schließen", "↑↓ sélectionner · Entrée modifier · c couleurs · h contraste · Esc fermer", "↑↓ seleccionar · Intro modificar · c colores · h contraste · Esc cerrar", "↑↓ wybierz · Enter edytuj · c kolory · h kontrast · Esc zamknij"},
	"settings.title":              {"Impostazioni", "Settings", "Einstellungen", "Paramètres", "Ajustes", "Ustawienia"},
	"settings.language":           {"Lingua dell'interfaccia", "Interface language", "Sprache der Oberfläche", "Langue de l'interface", "Idioma de la interfaz", "Język interfejsu"},
	"settings.refresh":            {"Intervallo tra le ricerche", "Time between searches", "Intervall zwischen den Suchen", "Intervalle entre les recherches", "Intervalo entre búsquedas", "Interwał między wyszukiwaniami"},
	"settings.limits":             {"Limiti download/upload", "Download/upload limits", "Download-/Upload-Limits", "Limites de téléchargement/upload", "Límites de descarga/subida", "Limity pobierania/wysyłania"},
	"settings.dryrun":             {"Modalità prova (dry-run)", "Test mode (dry-run)", "Testmodus (Dry-Run)", "Mode essai (dry-run)", "Modo de prueba (dry-run)", "Tryb próbny (dry-run)"},
	"settings.colors":             {"Colori", "Colors", "Farben", "Couleurs", "Colores", "Kolory"},
	"settings.contrast":           {"Alto contrasto", "High contrast", "Hoher Kontrast", "Contraste élevé", "Alto contraste", "Wysoki kontrast"},
	"prompt.cycle":                {"Avvia ricerca — Invio tutto · s serie · f film · c fumetti: ", "Start search — Enter all · s series · m movies · c comics: ", "Suche starten — Eingabe alles · s Serien · f Filme · c Comics: ", "Lancer la recherche — Entrée tout · s séries · f films · c bandes dessinées : ", "Iniciar búsqueda — Intro todo · s series · f películas · c cómics: ", "Rozpocznij wyszukiwanie — Enter wszystko · s serie · f filmy · c komiksy: "},
	"prompt.search":               {"Cerca: ", "Search: ", "Suchen: ", "Chercher : ", "Buscar: ", "Szukaj: "},
	"prompt.magnet":               {"Magnet/URL: ", "Magnet/URL: ", "Magnet/URL: ", "Magnet/URL : ", "Magnet/URL: ", "Magnet/URL: "},
	"prompt.file":                 {"File .torrent: ", "File .torrent: ", "Datei .torrent: ", "Fichier .torrent : ", "Archivo .torrent: ", "Plik .torrent: "},
	"prompt.limits":               {"Limiti globali DL UL KiB/s (0 0): ", "Global DL UL limits KiB/s (0 0): ", "Globale Limits DL UL KiB/s (0 0): ", "Limites globaux DL UL KiB/s (0 0) : ", "Límites globales DL UL KiB/s (0 0): ", "Limity globalne DL UL KiB/s (0 0): "},
	"prompt.templimits":           {"Limite temporaneo DL UL KiB/s [minuti] (0=illimitato · senza minuti=finché non rimosso · off=rimuovi): ", "Temporary limit DL UL KiB/s [minutes] (0=unlimited · no minutes=until removed · off=remove): ", "Temporäres Limit DL UL KiB/s [Minuten] (0=unbegrenzt · ohne Minuten=bis zur Entfernung · off=entfernen): ", "Limite temporaire DL UL KiB/s [minutes] (0=illimité · sans minutes=jusqu'à retrait · off=retirer) : ", "Límite temporal DL UL KiB/s [minutos] (0=ilimitado · sin minutos=hasta que se elimine · off=eliminar): ", "Limit tymczasowy DL UL KiB/s [minuty] (0=bez limitu · bez minut=do usunięcia · off=usuń): "},
	"prompt.logfilter":            {"Filtro log (vuoto=tutti): ", "Log filter (empty=all): ", "Log-Filter (leer=alle): ", "Filtre de journal (vide=tous) : ", "Filtro de log (vacío=todos): ", "Filtr logów (puste=wszystkie): "},
	"prompt.torrentfilter":        {"Filtro torrent (vuoto=tutti): ", "Torrent filter (empty=all): ", "Torrent-Filter (leer=alle): ", "Filtre torrent (vide=tous) : ", "Filtro de torrent (vacío=todos): ", "Filtr torrentów (puste=wszystkie): "},
	"prompt.archivefilter":        {"Filtro archivio (vuoto=tutto): ", "Archive filter (empty=all): ", "Archivfilter (leer=alles): ", "Filtre archive (vide=tout) : ", "Filtro de archivo (vacío=todo): ", "Filtr archiwum (puste=wszystko): "},
	"prompt.language":             {"Lingua [it/en]: ", "Language [it/en]: ", "Sprache [it/en]: ", "Langue [it/en] : ", "Idioma [it/en]: ", "Język [it/en]: "},
	"prompt.refresh":              {"Refresh in secondi: ", "Refresh seconds: ", "Aktualisierung in Sekunden: ", "Rafraîchissement en secondes : ", "Actualización en segundos: ", "Odświeżanie w sekundach: "},
	"prompt.remove":               {"Rimuovo '%s'? [s/N] ", "Remove '%s'? [y/N] ", "'%s' entfernen? [s/N] ", "Supprimer '%s' ? [s/N] ", "¿Elimino '%s'? [s/N] ", "Usunąć '%s'? [s/N] "},
	"prompt.removefiles":          {"Rimuovo '%s' e i file scaricati? [s/N] ", "Remove '%s' and its files? [y/N] ", "'%s' und die heruntergeladenen Dateien entfernen? [s/N] ", "Supprimer '%s' et les fichiers téléchargés ? [s/N] ", "¿Elimino '%s' y los archivos descargados? [s/N] ", "Usunąć '%s' i pobrane pliki? [s/N] "},
	"prompt.deletefiles":          {"Elimino anche i file scaricati? [s/N] ", "Also delete downloaded files? [y/N] ", "Auch die heruntergeladenen Dateien löschen? [s/N] ", "Supprimer aussi les fichiers téléchargés ? [s/N] ", "¿Elimino también los archivos descargados? [s/N] ", "Usunąć także pobrane pliki? [s/N] "},
	"prompt.cleantrash":           {"Svuoto ora il cestino? [s/N] ", "Empty the trash now? [y/N] ", "Papierkorb jetzt leeren? [s/N] ", "Vider la corbeille maintenant ? [s/N] ", "¿Vacío la papelera ahora? [s/N] ", "Opróżnić teraz kosz? [s/N] "},
	"prompt.cleancomp":            {"Rimuovo i completati che hanno raggiunto il limite di seed? [s/N] ", "Remove completed torrents that reached seed limits? [y/N] ", "Die abgeschlossenen entfernen, die das Seed-Limit erreicht haben? [s/N] ", "Supprimer les terminés ayant atteint la limite de seed ? [s/N] ", "¿Elimino los completados que alcanzaron el límite de seed? [s/N] ", "Usunąć ukończone, które osiągnęły limit seed? [s/N] "},
	"prompt.blocklistremove":      {"Rimuovo '%s' dalla blocklist? [s/N] ", "Remove '%s' from the blocklist? [y/N] ", "'%s' aus der Blocklist entfernen? [s/N] ", "Supprimer '%s' de la blocklist ? [s/N] ", "¿Elimino '%s' de la blocklist? [s/N] ", "Usunąć '%s' z blocklisty? [s/N] "},
	"prompt.httpremove":           {"Rimuovo il download HTTP '%s'? [s/N] ", "Remove HTTP download '%s'? [y/N] ", "HTTP-Download '%s' entfernen? [s/N] ", "Supprimer le téléchargement HTTP '%s' ? [s/N] ", "¿Elimino la descarga HTTP '%s'? [s/N] ", "Usunąć pobieranie HTTP '%s'? [s/N] "},
	"msg.refreshed":               {"aggiornato", "refreshed", "aktualisiert", "actualisé", "actualizado", "odświeżono"},
	"msg.cyclefull":               {"ciclo completo avviato", "full cycle started", "Vollständiger Zyklus gestartet", "Cycle complet démarré", "Ciclo completo iniciado", "Rozpoczęto pełny cykl"},
	"msg.cyclequeued":             {"ciclo %s in coda (un ciclo è in corso)", "%s cycle queued (a cycle is running)", "Zyklus %s in Warteschlange (ein Zyklus läuft)", "Cycle %s en file d'attente (un cycle est en cours)", "Ciclo %s en cola (un ciclo está en curso)", "Cykl %s w kolejce (cykl jest w toku)"},
	"msg.cyclestarted":            {"ciclo %s avviato", "%s cycle started", "Zyklus %s gestartet", "Cycle %s démarré", "Ciclo %s iniciado", "Rozpoczęto cykl %s"},
	"msg.cycleinvalid":            {"dominio ciclo non valido", "invalid cycle domain", "Ungültige Zyklusdomäne", "Domaine de cycle non valide", "Dominio de ciclo no válido", "Nieprawidłowa domena cyklu"},
	"msg.cyclefailed":             {"ciclo non riuscito: %s", "cycle failed: %s", "Zyklus fehlgeschlagen: %s", "Échec du cycle : %s", "Ciclo fallido: %s", "Cykl nie powiódł się: %s"},
	"msg.notorrent":               {"nessun torrent selezionato", "no torrent selected", "Kein Torrent ausgewählt", "Aucun torrent sélectionné", "Ningún torrent seleccionado", "Nie wybrano torrentu"},
	"msg.paused":                  {"in pausa", "paused", "pausiert", "en pause", "en pausa", "wstrzymano"},
	"msg.resumed":                 {"ripreso", "resumed", "fortgesetzt", "repris", "reanudado", "wznowiono"},
	"msg.restarted":               {"riavvio richiesto", "restart requested", "Neustart angefordert", "Redémarrage demandé", "Reinicio solicitado", "Zażądano restartu"},
	"msg.pinned":                  {"torrent fissato", "torrent pinned", "Torrent angeheftet", "torrent épinglé", "torrent fijado", "torrent przypięty"},
	"msg.unpinned":                {"torrent non più fissato", "torrent unpinned", "Torrent nicht mehr angeheftet", "torrent désépinglé", "torrent ya no fijado", "torrent odpięty"},
	"msg.rechecked":               {"verifica richiesta", "recheck requested", "Prüfung angefordert", "Vérification demandée", "Verificación solicitada", "Zażądano weryfikacji"},
	"msg.reannounced":             {"riannuncio richiesto", "reannounce requested", "Reannounce angefordert", "Réannonce demandée", "Reanuncio solicitado", "Zażądano ponownego ogłoszenia"},
	"msg.norenameon":              {"senza-rinomina attivo", "no-rename on", "Kein-Umbenennen an", "sans-renommage activé", "sin-renombrar activo", "bez zmiany nazwy włączone"},
	"msg.norenameoff":             {"senza-rinomina disattivo", "no-rename off", "Kein-Umbenennen aus", "sans-renommage désactivé", "sin-renombrar desactivado", "bez zmiany nazwy wyłączone"},
	"msg.removed":                 {"rimosso", "removed", "entfernt", "supprimé", "eliminado", "usunięto"},
	"msg.removedfiles":            {"rimosso con i file", "removed with files", "mit Dateien entfernt", "supprimé avec les fichiers", "eliminado con los archivos", "usunięto wraz z plikami"},
	"msg.removedone":              {"completati rimossi: %d, saltati: %d", "completed removed: %d, skipped: %d", "Abgeschlossene entfernt: %d, übersprungen: %d", "terminés supprimés : %d, ignorés : %d", "completados eliminados: %d, omitidos: %d", "ukończone usunięte: %d, pominięte: %d"},
	"msg.limits":                  {"limiti globali impostati: %d/%d KiB/s", "global limits set: %d/%d KiB/s", "globale Limits gesetzt: %d/%d KiB/s", "limites globaux définis : %d/%d KiB/s", "límites globales establecidos: %d/%d KiB/s", "ustawiono limity globalne: %d/%d KiB/s"},
	"msg.limitsinvalid":           {"i limiti devono essere numeri KiB/s", "limits must be KiB/s numbers", "Limits müssen KiB/s-Zahlen sein", "les limites doivent être des nombres en KiB/s", "los límites deben ser números en KiB/s", "limity muszą być liczbami KiB/s"},
	"msg.tempset":                 {"limite temporaneo ↓%s ↑%s per %d min", "temporary limit ↓%s ↑%s for %d min", "temporäres Limit ↓%s ↑%s für %d min", "limite temporaire ↓%s ↑%s pendant %d min", "límite temporal ↓%s ↑%s durante %d min", "limit tymczasowy ↓%s ↑%s przez %d min"},
	"msg.tempsetkeep":             {"limite temporaneo ↓%s ↑%s fino a rimozione", "temporary limit ↓%s ↑%s until removed", "temporäres Limit ↓%s ↑%s bis zur Entfernung", "limite temporaire ↓%s ↑%s jusqu'au retrait", "límite temporal ↓%s ↑%s hasta su eliminación", "limit tymczasowy ↓%s ↑%s do usunięcia"},
	"msg.tempcleared":             {"limite temporaneo rimosso: tornano i limiti normali", "temporary limit removed: normal limits restored", "temporäres Limit entfernt: normale Limits wieder aktiv", "limite temporaire retiré : les limites normales reviennent", "límite temporal eliminado: vuelven los límites normales", "usunięto limit tymczasowy: przywrócono normalne limity"},
	"msg.tempinvalid":             {"formato: DL UL [minuti] in KiB/s, oppure off", "format: DL UL [minutes] in KiB/s, or off", "Format: DL UL [Minuten] in KiB/s, oder off", "format : DL UL [minutes] en KiB/s, ou off", "formato: DL UL [minutos] en KiB/s, o off", "format: DL UL [minuty] w KiB/s lub off"},
	"msg.tempminutes":             {"durata massima %d minuti", "maximum duration %d minutes", "maximale Dauer %d Minuten", "durée maximale %d minutes", "duración máxima %d minutos", "maksymalny czas trwania %d minut"},
	"policy.temp":                 {"limite temporaneo ↓%s ↑%s · ancora %d min", "temporary limit ↓%s ↑%s · %d min left", "temporäres Limit ↓%s ↑%s · noch %d min", "limite temporaire ↓%s ↑%s · encore %d min", "límite temporal ↓%s ↑%s · aún %d min", "limit tymczasowy ↓%s ↑%s · jeszcze %d min"},
	"policy.tempkeep":             {"limite temporaneo ↓%s ↑%s · fino a rimozione", "temporary limit ↓%s ↑%s · until removed", "temporäres Limit ↓%s ↑%s · bis zur Entfernung", "limite temporaire ↓%s ↑%s · jusqu'au retrait", "límite temporal ↓%s ↑%s · hasta su eliminación", "limit tymczasowy ↓%s ↑%s · do usunięcia"},
	"policy.sched":                {"banda programmata ↓%s ↑%s", "scheduled bandwidth ↓%s ↑%s", "geplante Bandbreite ↓%s ↑%s", "bande passante programmée ↓%s ↑%s", "ancho de banda programado ↓%s ↑%s", "zaplanowana przepustowość ↓%s ↑%s"},
	"policy.base":                 {"limiti ↓%s ↑%s", "limits ↓%s ↑%s", "Limits ↓%s ↑%s", "limites ↓%s ↑%s", "límites ↓%s ↑%s", "limity ↓%s ↑%s"},
	"msg.search":                  {"ricerca: %d risultati", "search: %d results", "Suche: %d Ergebnisse", "recherche : %d résultats", "búsqueda: %d resultados", "wyszukiwanie: %d wyników"},
	"msg.searchfailed":            {"ricerca non riuscita: %s", "search failed: %s", "Suche fehlgeschlagen: %s", "recherche échouée : %s", "búsqueda fallida: %s", "wyszukiwanie nie powiodło się: %s"},
	"msg.queued":                  {"risultato accodato", "search result queued", "Ergebnis in Warteschlange", "résultat mis en file d'attente", "resultado encolado", "wynik zakolejkowany"},
	"msg.queuefailed":             {"accodamento non riuscito: %s", "queue failed: %s", "Einreihung fehlgeschlagen: %s", "Échec de la mise en file d'attente : %s", "no se pudo poner en cola: %s", "kolejkowanie nie powiodło się: %s"},
	"msg.nosearch":                {"nessun risultato selezionato", "no search result selected", "kein Ergebnis ausgewählt", "aucun résultat sélectionné", "ningún resultado seleccionado", "nie wybrano żadnego wyniku"},
	"msg.events":                  {"eventi: %d", "events: %d", "Ereignisse: %d", "événements : %d", "eventos: %d", "zdarzenia: %d"},
	"msg.eventsfailed":            {"eventi non disponibili: %s", "events failed: %s", "Ereignisse nicht verfügbar: %s", "événements non disponibles : %s", "eventos no disponibles: %s", "zdarzenia niedostępne: %s"},
	"msg.magneton":                {"magnet/URL aggiunto", "magnet/URL added", "magnet/URL hinzugefügt", "magnet/URL ajouté", "magnet/URL añadido", "dodano magnet/URL"},
	"msg.magnetinvalid":           {"non è un magnet o URL torrent", "not a magnet or torrent URL", "kein magnet oder torrent-URL", "ce n'est pas un magnet ou une URL torrent", "no es un magnet o URL de torrent", "to nie jest magnet ani URL torrent"},
	"msg.addfailed":               {"aggiunta non riuscita: %s", "add failed: %s", "Hinzufügen fehlgeschlagen: %s", "ajout échoué : %s", "no se pudo añadir: %s", "dodawanie nie powiodło się: %s"},
	"msg.fileerror":               {"errore file: %s", "file error: %s", "Dateifehler: %s", "erreur de fichier : %s", "error de archivo: %s", "błąd pliku: %s"},
	"msg.fileempty":               {"file vuoto", "empty file", "leere Datei", "fichier vide", "archivo vacío", "pusty plik"},
	"msg.torrentadded":            {"torrent aggiunto", "torrent added", "torrent hinzugefügt", "torrent ajouté", "torrent añadido", "dodano torrent"},
	"msg.trashcleaned":            {"cestino svuotato (%d file)", "trash cleaned (%d files)", "Papierkorb geleert (%d Dateien)", "corbeille vidée (%d fichiers)", "papelera vaciada (%d archivos)", "kosz opróżniony (%d plików)"},
	"msg.trashcleanedna":          {"cestino svuotato", "trash cleaned", "Papierkorb geleert", "corbeille vidée", "papelera vaciada", "kosz opróżniony"},
	"msg.trashfailed":             {"svuotamento cestino non riuscito: %s", "trash cleanup failed: %s", "Leeren des Papierkorbs fehlgeschlagen: %s", "vidage de la corbeille échoué : %s", "no se pudo vaciar la papelera: %s", "opróżnianie kosza nie powiodło się: %s"},
	"msg.detailsfailed":           {"dettagli non disponibili: %s", "details failed: %s", "Details nicht verfügbar: %s", "détails non disponibles : %s", "detalles no disponibles: %s", "szczegóły niedostępne: %s"},
	"msg.detailfailed":            {"%s non disponibili: %s", "%s failed: %s", "%s nicht verfügbar: %s", "%s non disponibles : %s", "%s no disponibles: %s", "%s niedostępne: %s"},
	"msg.actionfailed":            {"%s non riuscito: %s", "%s failed: %s", "%s fehlgeschlagen: %s", "%s échoué : %s", "%s falló: %s", "%s nie powiodło się: %s"},
	"msg.offline":                 {"impossibile raggiungere il daemon: %s", "cannot reach the daemon: %s", "Daemon nicht erreichbar: %s", "impossible d'atteindre le daemon : %s", "no se puede alcanzar el daemon: %s", "nie można dotrzeć do daemona: %s"},
	"msg.sethint":                 {"imposta GEXTTO_URL", "set GEXTTO_URL", "GEXTTO_URL setzen", "définir GEXTTO_URL", "configura GEXTTO_URL", "ustaw GEXTTO_URL"},
	"msg.loading":                 {"caricamento…", "loading…", "Lädt…", "chargement…", "cargando…", "ładowanie…"},
	"msg.emptylogs":               {"nessun log da mostrare", "no logs to show", "keine Logs anzuzeigen", "aucun log à afficher", "no hay logs que mostrar", "brak logów do wyświetlenia"},
	"msg.emptytorrents":           {"nessun torrent nella sessione", "no torrents in the session", "kein torrent in der Sitzung", "aucun torrent dans la session", "ningún torrent en la sesión", "brak torrentów w sesji"},
	"msg.emptyarchive":            {"archivio vuoto", "archive is empty", "Archiv leer", "archive vide", "archivo vacío", "archiwum puste"},
	"msg.emptymissing":            {"nessun elemento mancante", "no missing items", "kein fehlendes Element", "aucun élément manquant", "ningún elemento faltante", "brak brakujących elementów"},
	"msg.emptyblocklist":          {"blocklist vuota", "blocklist is empty", "Blocklist leer", "blocklist vide", "blocklist vacía", "blocklist pusty"},
	"msg.emptylibrary":            {"nessun titolo monitorato", "no monitored titles", "kein überwachter Titel", "aucun titre surveillé", "ningún título monitorizado", "brak monitorowanych tytułów"},
	"msg.termtoolsmall":           {"terminale troppo piccolo", "terminal too small", "Terminal zu klein", "terminal trop petit", "terminal demasiado pequeño", "terminal zbyt mały"},
	"msg.confirmed":               {"confermato", "confirmed", "bestätigt", "confirmé", "confirmado", "potwierdzono"},
	"msg.cancelled":               {"annullato", "cancelled", "abgebrochen", "annulé", "cancelado", "anulowano"},
	"msg.busy":                    {"un'azione è già in corso", "an action is already running", "eine Aktion läuft bereits", "une action est déjà en cours", "ya hay una acción en curso", "akcja jest już w toku"},
	"msg.copied":                  {"copia inviata al terminale (OSC52; in tmux serve set-clipboard on)", "copy sent to the terminal (OSC52; tmux needs set-clipboard on)", "Kopie an das Terminal gesendet (OSC52; in tmux ist set-clipboard on nötig)", "copie envoyée au terminal (OSC52 ; dans tmux, set-clipboard on est nécessaire)", "copia enviada al terminal (OSC52; en tmux se necesita set-clipboard on)", "kopia wysłana do terminala (OSC52; w tmux wymagane set-clipboard on)"},
	"msg.notty":                   {"la TUI richiede un terminale interattivo: da remoto usa 'ssh -t host …'", "the TUI needs an interactive terminal: remotely, use 'ssh -t host …'", "die TUI benötigt ein interaktives Terminal: aus der Ferne 'ssh -t host …' verwenden", "la TUI nécessite un terminal interactif : à distance, utilisez 'ssh -t host …'", "la TUI requiere un terminal interactivo: en remoto usa 'ssh -t host …'", "TUI wymaga interaktywnego terminala: zdalnie użyj 'ssh -t host …'"},
	"msg.pasteignored":            {"testo incollato ignorato: incolla un magnet/URL o apri prima un campo", "pasted text ignored: paste a magnet/URL or open a field first", "eingefügter Text ignoriert: füge ein magnet/URL ein oder öffne zuerst ein Feld", "texte collé ignoré : collez un magnet/URL ou ouvrez d'abord un champ", "texto pegado ignorado: pega un magnet/URL o abre primero un campo", "wklejony tekst zignorowany: wklej magnet/URL lub najpierw otwórz pole"},
	"msg.copyfailed":              {"copia non riuscita: %s", "copy failed: %s", "Kopieren fehlgeschlagen: %s", "copie échouée : %s", "no se pudo copiar: %s", "kopiowanie nie powiodło się: %s"},
	"msg.notifyfinished":          {"completato: %s", "finished: %s", "abgeschlossen: %s", "terminé : %s", "completado: %s", "ukończono: %s"},
	"msg.notifyerror":             {"errore torrent %s: %s", "torrent error %s: %s", "torrent-Fehler %s: %s", "erreur torrent %s : %s", "error de torrent %s: %s", "błąd torrent %s: %s"},
	"msg.notifystalled":           {"torrent in stallo: %s", "torrent stalled: %s", "torrent hängt: %s", "torrent bloqué : %s", "torrent bloqueado: %s", "torrent zawieszony: %s"},
	"msg.notifyarchived":          {"archiviato: %s", "archived: %s", "archiviert: %s", "archivé : %s", "archivado: %s", "zarchiwizowano: %s"},
	"msg.notifyhttp":              {"download HTTP %s: %s", "HTTP download %s: %s", "HTTP-Download %s: %s", "téléchargement HTTP %s : %s", "descarga HTTP %s: %s", "pobieranie HTTP %s: %s"},
	"msg.daemononline":            {"daemon nuovamente online", "daemon is online again", "Daemon wieder online", "daemon de nouveau en ligne", "daemon de nuevo en línea", "daemon ponownie online"},
	"msg.daemonoffline":           {"daemon offline", "daemon is offline", "Daemon offline", "daemon hors ligne", "daemon sin conexión", "daemon offline"},
	"msg.unknownerror":            {"errore sconosciuto", "unknown error", "unbekannter Fehler", "erreur inconnue", "error desconocido", "nieznany błąd"},
	"msg.languageinvalid":         {"lingua non valida: usare it o en", "invalid language: use it or en", "ungültige Sprache: it oder en verwenden", "langue non valide : utilisez it ou en", "idioma no válido: usa it o en", "nieprawidłowy język: użyj it lub en"},
	"msg.refreshinvalid":          {"refresh tra 1 e 86400 secondi", "refresh must be 1-86400 seconds", "refresh zwischen 1 und 86400 Sekunden", "refresh entre 1 et 86400 secondes", "refresh entre 1 y 86400 segundos", "refresh między 1 a 86400 sekund"},
	"msg.settingsfailed":          {"impostazioni non salvate: %s", "settings not saved: %s", "Einstellungen nicht gespeichert: %s", "paramètres non enregistrés : %s", "ajustes no guardados: %s", "ustawienia nie zostały zapisane: %s"},
	"msg.settingssaved":           {"impostazione salvata", "setting saved", "Einstellung gespeichert", "paramètre enregistré", "ajuste guardado", "ustawienie zapisane"},
	"msg.settingsrestart":         {"dry-run salvato: riavviare il daemon", "dry-run saved: restart the daemon", "dry-run gespeichert: Daemon neu starten", "dry-run enregistré : redémarrer le daemon", "dry-run guardado: reinicia el daemon", "dry-run zapisany: zrestartuj daemona"},
	"msg.languagesaved":           {"lingua aggiornata", "language updated", "Sprache aktualisiert", "langue mise à jour", "idioma actualizado", "język zaktualizowany"},
	"msg.archivefailed":           {"archivio non disponibile: %s", "archive unavailable: %s", "Archiv nicht verfügbar: %s", "archive non disponible : %s", "archivo no disponible: %s", "archiwum niedostępne: %s"},
	"msg.missingfailed":           {"mancanti non disponibili: %s", "missing items unavailable: %s", "Fehlende nicht verfügbar: %s", "manquants non disponibles : %s", "faltantes no disponibles: %s", "brakujące niedostępne: %s"},
	"msg.blocklistfailed":         {"blocklist non disponibile: %s", "blocklist unavailable: %s", "Blocklist nicht verfügbar: %s", "blocklist non disponible : %s", "blocklist no disponible: %s", "blocklist niedostępny: %s"},
	"msg.blocklistremovefailed":   {"rimozione blocklist non riuscita: %s", "blocklist removal failed: %s", "Entfernen aus der Blocklist fehlgeschlagen: %s", "suppression de la blocklist échouée : %s", "no se pudo eliminar de la blocklist: %s", "usuwanie z blocklisty nie powiodło się: %s"},
	"msg.blocklistremoved":        {"voce rimossa dalla blocklist", "blocklist entry removed", "Eintrag aus der Blocklist entfernt", "entrée supprimée de la blocklist", "entrada eliminada de la blocklist", "wpis usunięty z blocklisty"},
	"help.title":                  {"Gextto TUI — comandi da tastiera", "Gextto TUI — keyboard help", "Gextto TUI — Tastaturbefehle", "Gextto TUI — commandes clavier", "Gextto TUI — comandos de teclado", "Gextto TUI — skróty klawiszowe"},
	"help.close":                  {"Premi Esc, Invio o ? per chiudere", "Press Esc, Enter or ? to close", "Esc, Enter oder ? zum Schließen drücken", "Appuyez sur Esc, Entrée ou ? pour fermer", "Pulsa Esc, Intro o ? para cerrar", "Naciśnij Esc, Enter lub ?, aby zamknąć"},
	"label.problemsonly":          {"solo avvisi/errori", "warnings/errors only", "nur Warnungen/Fehler", "uniquement avertissements/erreurs", "solo avisos/errores", "tylko ostrzeżenia/błędy"},
	"label.etaratio":              {"tempo/ratio", "ETA/ratio", "Zeit/ratio", "temps/ratio", "tiempo/ratio", "czas/ratio"},
	"label.seedspeers":            {"seed/peer", "seeds/peers", "seed/peer", "seed/peer", "seed/peer", "seed/peer"},
	"label.diagnosis":             {"Situazione", "Situation", "Situation", "Situation", "Situación", "Sytuacja"},
	"label.stalledsince":          {"Bloccato da", "Stuck since", "Blockiert von", "Bloqué par", "Bloqueado por", "Zablokowany przez"},
	"label.nextretry":             {"Prossimo tentativo", "Next attempt", "Nächster Versuch", "Prochaine tentative", "Próximo intento", "Następna próba"},
	"label.stallaction":           {"Cosa fare", "What to do", "Was zu tun", "Que faire", "Qué hacer", "Co robić"},
	"label.unseenproblems":        {"⚠ %d nuovi avvisi (3 Log)", "⚠ %d new warnings (3 Log)", "⚠ %d neue Warnungen (3 Log)", "⚠ %d nouveaux avertissements (3 Log)", "⚠ %d avisos nuevos (3 Log)", "⚠ %d nowych ostrzeżeń (3 Log)"},
	"status.nocycle":              {"ancora nessuna ricerca", "no search yet", "noch keine Suche", "encore aucune recherche", "todavía ninguna búsqueda", "jeszcze brak wyszukiwania"},
	"status.cyclechecked":         {"%s release controllate, %s corrispondenti ai tuoi titoli", "%s releases checked, %s matching your titles", "%s release geprüft, %s passend zu deinen Titeln", "%s releases vérifiées, %s correspondant à vos titres", "%s releases comprobadas, %s coincidentes con tus títulos", "%s sprawdzonych release, %s pasujących do twoich tytułów"},
	"status.onedownload":          {"1 download avviato", "1 download started", "1 Download gestartet", "1 téléchargement démarré", "1 descarga iniciada", "1 pobieranie rozpoczęte"},
	"status.ndownloads":           {"%d download avviati", "%d downloads started", "%d Downloads gestartet", "%d téléchargements démarrés", "%d descargas iniciadas", "%d pobrań rozpoczętych"},
	"status.nothingnew":           {"niente di nuovo", "nothing new", "nichts Neues", "rien de nouveau", "nada nuevo", "nic nowego"},
	"status.onegapfilled":         {"1 episodio mancante trovato", "1 missing episode found", "1 fehlende Episode gefunden", "1 épisode manquant trouvé", "1 episodio faltante encontrado", "Znaleziono 1 brakujący odcinek"},
	"status.gapsfilled":           {"%d episodi mancanti trovati", "%d missing episodes found", "%d fehlende Episoden gefunden", "%d épisodes manquants trouvés", "%d episodios faltantes encontrados", "Znaleziono %d brakujących odcinków"},
	"status.seen":                 {"%s serie · %s film · %s gruppi visti", "%s series · %s movies · %s groups seen", "%s Serien · %s Filme · %s gesehene Gruppen", "%s séries · %s films · %s groupes vus", "%s series · %s películas · %s grupos vistos", "%s seriali · %s filmów · %s widzianych grup"},
	"stall.action":                {"F per scartarlo e cercare un'altra versione", "F to discard it and look for another version", "F zum Verwerfen und Suchen einer anderen Version", "F pour l'écarter et chercher une autre version", "F para descartarlo y buscar otra versión", "F, aby go odrzucić i poszukać innej wersji"},
	"stall.nextin":                {"prossimo tentativo tra %s", "next attempt in %s", "Nächster Versuch in %s", "Prochaine tentative dans %s", "Próximo intento en %s", "Następna próba za %s"},
	"stall.nextsoon":              {"nuovo tentativo a breve", "next attempt soon", "Neuer Versuch in Kürze", "Nouvelle tentative bientôt", "Nuevo intento en breve", "Nowa próba wkrótce"},
	"stall.since":                 {"%s (%s fa)", "%s (%s ago)", "%s (vor %s)", "%s (il y a %s)", "%s (hace %s)", "%s (%s temu)"},
	"diag.dead_swarm":             {"nessuno di chi condivide ha il file completo: probabilmente non finirà, meglio un'altra versione", "nobody sharing it has the complete file: it will probably never finish, another version is better", "Keiner der Teilenden hat die vollständige Datei: Sie wird wahrscheinlich nicht fertig, besser eine andere Version", "Aucun de ceux qui partagent n'a le fichier complet : elle ne se terminera probablement pas, mieux vaut une autre version", "Ninguno de los que comparten tiene el archivo completo: probablemente no terminará, mejor otra versión", "Żaden z udostępniających nie ma pełnego pliku: prawdopodobnie nie zostanie ukończony, lepiej inna wersja"},
	"diag.no_connected_seed":      {"ci sono utenti con il file completo ma non si riesce a collegarsi (tracker, porta, firewall)", "users with the complete file exist but cannot be reached (tracker, port, firewall)", "Es gibt Nutzer mit der vollständigen Datei, aber keine Verbindung möglich (Tracker, Port, Firewall)", "Il y a des utilisateurs avec le fichier complet mais impossible de se connecter (tracker, port, pare-feu)", "Hay usuarios con el archivo completo pero no se logra conectar (tracker, puerto, firewall)", "Są użytkownicy z pełnym plikiem, ale nie można się połączyć (tracker, port, zapora)"},
	"diag.stalled":                {"nessun progresso nonostante gli utenti collegati; verrà ritentato", "no progress despite connected users; it will be retried", "Kein Fortschritt trotz verbundener Nutzer; es wird erneut versucht", "Aucun progrès malgré les utilisateurs connectés ; une nouvelle tentative aura lieu", "Sin progreso a pesar de los usuarios conectados; se reintentará", "Brak postępu mimo połączonych użytkowników; zostanie ponowione"},
	"diag.no_peers":               {"nessun utente collegato, si attende che tracker e DHT ne trovino", "no connected users, waiting for trackers and DHT to find some", "Keine Nutzer verbunden, es wird gewartet, dass tracker und DHT welche finden", "Aucun utilisateur connecté, en attente que tracker et DHT en trouvent", "Ningún usuario conectado, se espera que tracker y DHT encuentren algunos", "Brak połączonych użytkowników, czekamy aż tracker i DHT kogoś znajdą"},
	"diag.metadata":               {"in attesa dell'elenco dei file", "waiting for the list of files", "Warte auf Dateiliste", "En attente de la liste des fichiers", "Esperando la lista de archivos", "Oczekiwanie na listę plików"},
	"diag.error":                  {"il motore torrent ha segnalato un errore", "the torrent engine reported an error", "Die torrent-Engine hat einen Fehler gemeldet", "Le moteur torrent a signalé une erreur", "El motor torrent ha notificado un error", "Silnik torrent zgłosił błąd"},
	"diag.downloading":            {"download in corso", "downloading", "Download läuft", "Téléchargement en cours", "Descarga en curso", "Pobieranie w toku"},
	"hint.help":                   {"↑↓/PgUp/PgDn scorri · Esc chiudi", "↑↓/PgUp/PgDn scroll · Esc close", "↑↓/PgUp/PgDn scrollen · Esc schließen", "↑↓/PgUp/PgDn défiler · Esc fermer", "↑↓/PgUp/PgDn desplazar · Esc cerrar", "↑↓/PgUp/PgDn przewijaj · Esc zamknij"},
	"hint.events":                 {"↑↓/PgUp/PgDn scorri · Home/End · r aggiorna · Esc chiudi", "↑↓/PgUp/PgDn scroll · Home/End · r refresh · Esc close", "↑↓/PgUp/PgDn scrollen · Home/End · r aktualisieren · Esc schließen", "↑↓/PgUp/PgDn défiler · Home/End · r actualiser · Esc fermer", "↑↓/PgUp/PgDn desplazar · Home/End · r actualizar · Esc cerrar", "↑↓/PgUp/PgDn przewijaj · Home/End · r odśwież · Esc zamknij"},
	"settings.hint":               {"Invio modifica la voce selezionata · c colori · h contrasto · Esc chiude", "Enter edits the selected item · c colors · h contrast · Esc closes", "Enter bearbeitet den ausgewählten Eintrag · c Farben · h Kontrast · Esc schließt", "Entrée modifie l'entrée sélectionnée · c couleurs · h contraste · Esc ferme", "Intro modifica la entrada seleccionada · c colores · h contraste · Esc cierra", "Enter edytuje wybrany wpis · c kolory · h kontrast · Esc zamyka"},
	"settings.uilang":             {"italiano", "English", "Italienisch", "italien", "italiano", "włoski"},
	"path.data":                   {"Dati", "Data", "Daten", "Données", "Datos", "Dane"},
	"path.download":               {"Download", "Download", "Download", "Téléchargement", "Descarga", "Pobieranie"},
	"path.trash":                  {"Cestino", "Trash", "Papierkorb", "Corbeille", "Papelera", "Kosz"},
	"help.global":                 {"Globali:  1-9/Tab schede · r aggiorna · ? chiudi aiuto · q esci", "Global:  1-9/Tab tabs · r refresh · ? close help · q quit", "Global:  1-9/Tab Tabs · r aktualisieren · ? Hilfe schließen · q beenden", "Global :  1-9/Tab onglets · r actualiser · ? fermer l'aide · q quitter", "Global:  1-9/Tab pestañas · r actualizar · ? cerrar ayuda · q salir", "Globalne:  1-9/Tab zakładki · r odśwież · ? zamknij pomoc · q wyjdź"},
	"help.maintenance":            {"Manutenzione: ↑↓ seleziona · Invio esegui (le operazioni pesanti chiedono conferma)", "Maintenance: ↑↓ select · Enter run (heavy operations ask for confirmation)", "Wartung: ↑↓ auswählen · Enter ausführen (schwere Vorgänge erfordern Bestätigung)", "Maintenance : ↑↓ sélectionner · Entrée exécuter (les opérations lourdes demandent confirmation)", "Mantenimiento: ↑↓ seleccionar · Intro ejecutar (las operaciones pesadas piden confirmación)", "Konserwacja: ↑↓ wybierz · Enter wykonaj (ciężkie operacje wymagają potwierdzenia)"},
	"help.scroll":                 {"↑↓/PgUp/PgDn scorri l'aiuto", "↑↓/PgUp/PgDn scroll the help", "↑↓/PgUp/PgDn Hilfe scrollen", "↑↓/PgUp/PgDn défiler l'aide", "↑↓/PgUp/PgDn desplazar la ayuda", "↑↓/PgUp/PgDn przewijaj pomoc"},
	"help.global2":                {"          a magnet/URL · t file .torrent · c ciclo · s cerca · l libreria · e eventi · g impostazioni", "          a magnet/URL · t .torrent file · c cycle · s search · l library · e events · g settings", "          a magnet/URL · t .torrent-Datei · c Zyklus · s suchen · l Bibliothek · e Ereignisse · g Einstellungen", "          a magnet/URL · t fichier .torrent · c cycle · s rechercher · l bibliothèque · e événements · g paramètres", "          a magnet/URL · t archivo .torrent · c ciclo · s buscar · l biblioteca · e eventos · g ajustes", "          a magnet/URL · t plik .torrent · c cykl · s szukaj · l biblioteka · e zdarzenia · g ustawienia"},
	"help.torrents":               {"Torrent:  ↑↓ o PgUp/PgDn · Home/End · Invio dettagli", "Torrents: ↑↓ or PgUp/PgDn · Home/End · Enter details", "Torrent:  ↑↓ oder PgUp/PgDn · Home/End · Enter Details", "Torrent :  ↑↓ ou PgUp/PgDn · Home/End · Entrée détails", "Torrent:  ↑↓ o PgUp/PgDn · Home/End · Intro detalles", "Torrent:  ↑↓ lub PgUp/PgDn · Home/End · Enter szczegóły"},
	"help.torrents2":              {"          p pausa/riprendi · b riavvia · d rimuovi · X pulisci completati", "          p pause/resume · b restart · d remove · X clean completed", "          p Pause/Fortsetzen · b neu starten · d entfernen · X abgeschlossene bereinigen", "          p pause/reprendre · b redémarrer · d supprimer · X nettoyer les terminés", "          p pausa/reanudar · b reiniciar · d eliminar · X limpiar completados", "          p pauza/wznów · b zrestartuj · d usuń · X wyczyść ukończone"},
	"help.torrents3":              {"          k verifica · R riannuncia · n senza-rinomina · i/u pin · L limiti globali · T limite temporaneo", "          k recheck · R reannounce · n no-rename · i/u pin · L global limits · T temporary limit", "          k überprüfen · R erneut ankündigen · n ohne Umbenennen · i/u anheften · L globale Limits · T temporäres Limit", "          k vérifier · R réannoncer · n sans renommage · i/u épingler · L limites globales · T limite temporaire", "          k verificar · R reanunciar · n sin-renombrado · i/u fijar · L límites globales · T límite temporal", "          k sprawdź · R ogłoś ponownie · n bez-zmiany-nazwy · i/u przypnij · L globalne limity · T limit tymczasowy"},
	"help.torrents4":              {"          o cambia ordinamento · / (o F) filtro torrent", "          o cycle sort · / (or F) torrent filter", "          o Sortierung ändern · / (oder F) torrent-Filter", "          o changer le tri · / (ou F) filtre torrent", "          o cambiar orden · / (o F) filtro torrent", "          o zmień sortowanie · / (lub F) filtr torrent"},
	"help.details":                {"Dettagli: 1 generale · 2 tracker · 3 file · 4 peer · ↑↓ scorri · F scarta e cerca un'altra versione", "Details:  1 general · 2 trackers · 3 files · 4 peers · ↑↓ scroll · F discard and find another version", "Details: 1 Allgemein · 2 Tracker · 3 Dateien · 4 Peers · ↑↓ scrollen · F verwerfen und andere Version suchen", "Détails : 1 général · 2 tracker · 3 fichiers · 4 pairs · ↑↓ défiler · F écarter et chercher une autre version", "Detalles: 1 general · 2 tracker · 3 archivos · 4 pares · ↑↓ desplazar · F descartar y buscar otra versión", "Szczegóły: 1 ogólne · 2 tracker · 3 pliki · 4 peerzy · ↑↓ przewijaj · F odrzuć i poszukaj innej wersji"},
	"help.logs":                   {"Log:      ↑↓ o PgUp/PgDn · Home/End · / filtro · w solo avvisi/errori · f segui/ferma", "Logs:     ↑↓ or PgUp/PgDn · Home/End · / filter · w warnings/errors only · f follow/pause", "Log:      ↑↓ oder PgUp/PgDn · Home/End · / Filter · w nur Warnungen/Fehler · f folgen/stoppen", "Log :      ↑↓ ou PgUp/PgDn · Home/End · / filtre · w seulement avertissements/erreurs · f suivre/arrêter", "Log:      ↑↓ o PgUp/PgDn · Home/End · / filtro · w solo avisos/errores · f seguir/detener", "Log:      ↑↓ lub PgUp/PgDn · Home/End · / filtr · w tylko ostrzeżenia/błędy · f śledź/zatrzymaj"},
	"help.health":                 {"Salute:   ↑↓/PgUp/PgDn scorri · x svuota cestino (con conferma)", "Health:   ↑↓/PgUp/PgDn scroll · x empty trash (confirmation required)", "Gesundheit:   ↑↓/PgUp/PgDn scrollen · x Papierkorb leeren (mit Bestätigung)", "Santé :   ↑↓/PgUp/PgDn défiler · x vider la corbeille (avec confirmation)", "Salud:   ↑↓/PgUp/PgDn desplazar · x vaciar papelera (con confirmación)", "Zdrowie:   ↑↓/PgUp/PgDn przewijaj · x opróżnij kosz (z potwierdzeniem)"},
	"help.status":                 {"Stato:    ↑↓/PgUp/PgDn scorri · le ultime righe di log restano in fondo", "Status:   ↑↓/PgUp/PgDn scroll · the latest log lines stay at the bottom", "Status:    ↑↓/PgUp/PgDn scrollen · die letzten Log-Zeilen bleiben unten", "État :    ↑↓/PgUp/PgDn défiler · les dernières lignes de log restent en bas", "Estado:    ↑↓/PgUp/PgDn desplazar · las últimas líneas de log quedan abajo", "Status:    ↑↓/PgUp/PgDn przewijaj · ostatnie wiersze logu pozostają na dole"},
	"help.terminal":               {"Terminale: Ctrl-L ridisegna · incolla un magnet per aggiungerlo · conferme solo con s/y", "Terminal: Ctrl-L redraw · paste a magnet to add it · confirm only with y", "Terminal: Ctrl-L neu zeichnen · ein magnet zum Hinzufügen einfügen · Bestätigungen nur mit s/y", "Terminal : Ctrl-L redessiner · coller un magnet pour l'ajouter · confirmations seulement avec s/y", "Terminal: Ctrl-L redibujar · pega un magnet para añadirlo · confirmaciones solo con s/y", "Terminal: Ctrl-L przerysuj · wklej magnet, aby go dodać · potwierdzenia tylko przez s/y"},
	"help.bandwidth":              {"Banda:    in basso a destra la banda usata dalla TUI (↓ ricevuti, ↑ inviati) e le richieste al daemon al secondo", "Bandwidth: bottom right shows the bandwidth the TUI uses (↓ received, ↑ sent) and daemon requests per second", "Bandbreite:    unten rechts die von der TUI genutzte Bandbreite (↓ empfangen, ↑ gesendet) und die Anfragen an den Daemon pro Sekunde", "Bande passante :    en bas à droite la bande passante utilisée par la TUI (↓ reçus, ↑ envoyés) et les requêtes au daemon par seconde", "Ancho de banda:    abajo a la derecha el ancho de banda usado por la TUI (↓ recibidos, ↑ enviados) y las solicitudes al daemon por segundo", "Przepustowość:    w prawym dolnym rogu przepustowość używana przez TUI (↓ odebrane, ↑ wysłane) oraz żądania do daemona na sekundę"},
	"help.settings":               {"Impostazioni: g apre il pannello · ↑↓ seleziona · Invio modifica · c colori · h contrasto", "Settings: g opens panel · ↑↓ select · Enter edit · c colors · h contrast", "Einstellungen: g öffnet das Panel · ↑↓ auswählen · Enter bearbeiten · c Farben · h Kontrast", "Paramètres : g ouvre le panneau · ↑↓ sélectionner · Entrée modifier · c couleurs · h contraste", "Ajustes: g abre el panel · ↑↓ seleccionar · Intro modificar · c colores · h contraste", "Ustawienia: g otwiera panel · ↑↓ wybierz · Enter edytuj · c kolory · h kontrast"},
	"help.archive":                {"Archivio: ↑↓ seleziona · PgUp/PgDn pagina · Invio accoda · d dettagli · o/O ordina · s cerca", "Archive: ↑↓ select · PgUp/PgDn page · Enter queue · d details · o/O sort · s search", "Archiv: ↑↓ auswählen · PgUp/PgDn Seite · Enter in Warteschlange · d Details · o/O sortieren · s suchen", "Archive : ↑↓ sélectionner · PgUp/PgDn page · Entrée mettre en file · d détails · o/O trier · s rechercher", "Archivo: ↑↓ seleccionar · PgUp/PgDn página · Intro encolar · d detalles · o/O ordenar · s buscar", "Archiwum: ↑↓ wybierz · PgUp/PgDn strona · Enter dodaj do kolejki · d szczegóły · o/O sortuj · s szukaj"},
	"help.missing":                {"Mancanti: ↑↓ seleziona · Invio apri la serie · s cerca l'episodio · i ignora · r aggiorna", "Missing: ↑↓ select · Enter open the series · s search the episode · i ignore · r refresh", "Fehlend: ↑↓ auswählen · Enter Serie öffnen · s Episode suchen · i ignorieren · r aktualisieren", "Manquants : ↑↓ sélectionner · Entrée ouvrir la série · s chercher l'épisode · i ignorer · r actualiser", "Faltantes: ↑↓ seleccionar · Intro abrir la serie · s buscar el episodio · i ignorar · r actualizar", "Brakujące: ↑↓ wybierz · Enter otwórz serial · s szukaj odcinka · i ignoruj · r odśwież"},
	"help.blocklist":              {"Blocco:   ↑↓ seleziona · d rimuovi", "Blocklist: ↑↓ select · d remove", "Block:   ↑↓ auswählen · d entfernen", "Blocage :   ↑↓ sélectionner · d supprimer", "Bloqueo:   ↑↓ seleccionar · d eliminar", "Blokada:   ↑↓ wybierz · d usuń"},
	"help.library":                {"Libreria: ←→ serie/film/fumetti · Invio dettagli · a aggiungi da TMDB · e modifica · p pausa · d elimina · m cerca · s filtra", "Library: ←→ series/movies/comics · Enter details · a add from TMDB · e edit · p pause · d delete · m search · s filter", "Bibliothek: ←→ Serien/Filme/Comics · Enter Details · a aus TMDB hinzufügen · e bearbeiten · p Pause · d löschen · m suchen · s filtern", "Bibliothèque : ←→ séries/films/comics · Entrée détails · a ajouter depuis TMDB · e modifier · p pause · d supprimer · m rechercher · s filtrer", "Biblioteca: ←→ series/películas/cómics · Intro detalles · a añadir desde TMDB · e modificar · p pausa · d eliminar · m buscar · s filtrar", "Biblioteka: ←→ seriale/filmy/komiksy · Enter szczegóły · a dodaj z TMDB · e edytuj · p pauza · d usuń · m szukaj · s filtruj"},
	"sort.name":                   {"nome", "name", "Name", "nom", "nombre", "nazwa"},
	"sort.progress":               {"progresso", "progress", "Fortschritt", "progression", "progreso", "postęp"},
	"sort.state":                  {"stato", "state", "Status", "état", "estado", "status"},
	"sort.rate":                   {"velocità", "rate", "Geschwindigkeit", "vitesse", "velocidad", "prędkość"},
	"sort.size":                   {"dimensione", "size", "Größe", "taille", "tamaño", "rozmiar"},
	"sort.archive_title":          {"titolo", "title", "Titel", "titre", "título", "tytuł"},
	"sort.archive_source":         {"sorgente", "source", "Quelle", "source", "fuente", "źródło"},
	"sort.archive_quality":        {"qualità", "quality", "Qualität", "qualité", "calidad", "jakość"},
	"sort.archive_added":          {"data", "date", "Datum", "date", "fecha", "data"},
	"state.downloading":           {"In scarico", "Downloading", "Im Download", "En téléchargement", "En descarga", "W pobieraniu"},
	"state.idle":                  {"Fermo 0 B/s", "Stuck 0 B/s", "Angehalten 0 B/s", "Arrêté 0 B/s", "Detenido 0 B/s", "Zatrzymane 0 B/s"},
	"state.downloading_metadata":  {"Metadati", "Metadata", "Metadaten", "Métadonnées", "Metadatos", "Metadane"},
	"state.stalled":               {"In attesa di seed", "Stalled", "Warte auf seed", "En attente de seed", "Esperando seed", "Oczekiwanie na seed"},
	"state.seeding":               {"In seed", "Seeding", "Im seed", "En seed", "En seed", "W seed"},
	"state.finished":              {"Completato", "Finished", "Abgeschlossen", "Terminé", "Completado", "Ukończone"},
	"state.completed":             {"Completato", "Completed", "Abgeschlossen", "Terminé", "Completado", "Ukończone"},
	"state.checking_files":        {"Verifica file", "Checking files", "Dateien prüfen", "Vérification des fichiers", "Verificando archivos", "Sprawdzanie plików"},
	"state.checking_resume_data":  {"Ripristino", "Restoring", "Wiederherstellung", "Restauration", "Restauración", "Przywracanie"},
	"state.queued":                {"In coda", "Queued", "In Warteschlange", "En file", "En cola", "W kolejce"},
	"state.paused":                {"In pausa", "Paused", "Pausiert", "En pause", "En pausa", "Wstrzymane"},
	"state.moving":                {"Spostamento", "Moving", "Verschieben", "Déplacement", "Movimiento", "Przenoszenie"},
	"state.error":                 {"Errore", "Error", "Fehler", "Erreur", "Error", "Błąd"},
	"state.unknown":               {"Sconosciuto", "Unknown", "Unbekannt", "Inconnu", "Desconocido", "Nieznane"},
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
	case LangDE:
		return &Translator{lang: LangDE, idx: 2}
	case LangFR:
		return &Translator{lang: LangFR, idx: 3}
	case LangES:
		return &Translator{lang: LangES, idx: 4}
	case LangPL:
		return &Translator{lang: LangPL, idx: 5}
	default:
		return &Translator{lang: LangIT, idx: 0}
	}
}

func normalizeLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(lang, "en"):
		return LangEN
	case strings.HasPrefix(lang, "de"):
		return LangDE
	case strings.HasPrefix(lang, "fr"):
		return LangFR
	case strings.HasPrefix(lang, "es") || lang == "spa":
		return LangES
	case strings.HasPrefix(lang, "pl") || lang == "pol":
		return LangPL
	case strings.HasPrefix(lang, "it"):
		return LangIT
	default:
		return lang
	}
}

// Lang is the resolved language code (it, en, de, fr, es, pl).
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
	if idx >= 0 && idx < len(entry) && entry[idx] != "" {
		return entry[idx]
	}
	if entry[1] != "" {
		return entry[1]
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
