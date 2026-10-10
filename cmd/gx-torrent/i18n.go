package main

// i18n.go holds the page strings and their translations.
// The page source is English; uiDictionary returns, for a language, the
// map used by translateHTML to localize the rendered page (the same
// post-processing approach used by Gextto's web UI).

// uiCatalog maps an English page string to {it, de, fr, es, pl}.
var uiCatalog = map[string][5]string{
	".torrent file to add": {"File .torrent da aggiungere", ".torrent-Datei zum Hinzufügen", "Fichier .torrent à ajouter", "Archivo .torrent para añadir", "Plik .torrent do dodania"},
	"Add":                  {"Aggiungi", "Hinzufügen", "Ajouter", "Añadir", "Dodaj"},
	"BEP 16 super-seeding: advertise one piece at a time so the swarm spreads the data. For initial seeding only; it reduces the seed's upload throughput": {"BEP 16 super-seeding: annuncia un pezzo alla volta così lo swarm distribuisce i dati. Solo per il seeding iniziale; riduce il throughput di upload del seed", "BEP 16 super-seeding: jeweils ein Stück ankündigen, damit der swarm die Daten verteilt. Nur für initiales seeding; reduziert den Upload-Durchsatz des seed", "BEP 16 super-seeding : annoncer un morceau à la fois pour que le swarm diffuse les données. Pour le seeding initial uniquement ; réduit le débit d'envoi du seed", "BEP 16 super-seeding: anunciar una pieza a la vez para que el swarm distribuya los datos. Solo para el seeding inicial; reduce el rendimiento de subida del seed", "BEP 16 super-seeding: ogłaszaj jeden fragment naraz, aby swarm rozgłaszał dane. Tylko do początkowego seedowania; zmniejsza przepustowość wysyłania seeda"},
	"Check that the peer port is listening and forwarded by the router":                                                                                    {"Verifica se la porta è in ascolto e inoltrata dal router", "Prüft, ob der Port lauscht und vom Router weitergeleitet wird", "Vérifie si le port est en écoute et redirigé par le routeur", "Comprueba si el puerto está a la escucha y redirigido por el router", "Sprawdza, czy port nasłuchuje i jest przekierowany przez router"},
	"DHT": {"DHT", "DHT", "DHT", "DHT", "DHT"},
	"Disk read / write operations since start":             {"Operazioni di lettura / scrittura su disco dall'avvio", "Lese-/Schreibvorgänge auf der Festplatte seit dem Start", "Opérations de lecture / écriture sur disque depuis le démarrage", "Operaciones de lectura / escritura en disco desde el inicio", "Operacje odczytu / zapisu na dysku od uruchomienia"},
	"Download (if a URL) and apply the IP filter now":      {"Scarica (se è un URL) e applica ora l'IP filter", "Herunterladen (falls eine URL) und den IP filter jetzt anwenden", "Télécharger (s'il s'agit d'une URL) et appliquer maintenant l'IP filter", "Descargar (si es una URL) y aplicar ahora el IP filter", "Pobierz (jeśli to URL) i zastosuj teraz IP filter"},
	"Download pieces in order (streaming); slower overall": {"Scarica i pezzi in ordine (streaming); più lento nel complesso", "Teile der Reihe nach herunterladen (streaming); insgesamt langsamer", "Télécharger les morceaux dans l'ordre (streaming) ; plus lent dans l'ensemble", "Descargar las piezas en orden (streaming); más lento en general", "Pobieraj fragmenty po kolei (streaming); ogólnie wolniejsze"},
	"Download the ends of every file first":                {"Scarica prima le estremità di ogni file", "Zuerst die Enden jeder Datei herunterladen", "Télécharger d'abord les extrémités de chaque fichier", "Descargar primero los extremos de cada archivo", "Najpierw pobierz końce każdego pliku"},
	"Encryption":                                           {"Crittografia", "Verschlüsselung", "Chiffrement", "Cifrado", "Szyfrowanie"},
	"Filter torrents…":                                     {"Filtra torrent…", "Torrents filtern…", "Filtrer les torrents…", "Filtrar torrents…", "Filtruj torrenty…"},
	"Free space":                                           {"Spazio libero", "Freier Speicherplatz", "Espace libre", "Espacio libre", "Wolne miejsce"},
	"I/O ops r/w":                                          {"Operazioni I/O r/w", "I/O-Vorgänge r/w", "Opérations I/O r/w", "Operaciones I/O r/w", "Operacje I/O r/w"},
	"IP filter":                                            {"IP filter", "IP filter", "IP filter", "IP filter", "IP filter"},
	"IP filter rules":                                      {"Regole IP filter", "IP filter Regeln", "Règles IP filter", "Reglas IP filter", "Reguły IP filter"},
	"Incoming peer connections since start":                {"Connessioni peer in ingresso dall'avvio", "Eingehende peer Verbindungen seit dem Start", "Connexions peer entrantes depuis le démarrage", "Conexiones peer entrantes desde el inicio", "Połączenia peer przychodzące od uruchomienia"},
	"Load filter":                                          {"Carica filtro", "Filter laden", "Charger le filtre", "Cargar filtro", "Wczytaj filtr"},
	"No torrents in the session.":                          {"Nessun torrent nella sessione.", "Keine Torrents in der Sitzung.", "Aucun torrent dans la session.", "Ningún torrent en la sesión.", "Brak torrentów w sesji."},
	"Port":                                                 {"Porta", "Port", "Port", "Puerto", "Port"},
	"Prefilled with the IP filter configured in Gextto":    {"Precompilato con l'IP filter configurato in Gextto", "Vorbefüllt mit dem in Gextto konfigurierten IP filter", "Prérempli avec l'IP filter configuré dans Gextto", "Prellenado con el IP filter configurado en Gextto", "Wstępnie wypełnione IP filter skonfigurowanym w Gextto"},
	"Read / write cache":                                   {"Cache di lettura / scrittura", "Lese-/Schreib-cache", "cache de lecture / écriture", "cache de lectura / escritura", "cache odczytu / zapisu"},
	"Selected:":                                            {"Selezionati:", "Ausgewählt:", "Sélectionnés :", "Seleccionados:", "Wybrane:"},
	"Test ports":                                           {"Test porte", "Port testen", "Tester le port", "Probar puerto", "Testuj port"},
	"Totals":                                               {"Totali", "Gesamt", "Totaux", "Totales", "Suma"},
	"URL or local file":                                    {"URL o file locale", "URL oder lokale Datei", "URL ou fichier local", "URL o archivo local", "URL lub plik lokalny"},
	"cache r/w":                                            {"cache r/w", "cache r/w", "cache r/w", "cache r/w", "cache r/w"},
	"destination (empty = default)":                        {"destinazione (vuoto = predefinita)", "Ziel (leer = Standard)", "destination (vide = par défaut)", "destino (vacío = predeterminado)", "miejsce docelowe (puste = domyślne)"},
	"downloading":                                          {"downloading", "downloading", "downloading", "downloading", "downloading"},
	"first/last":                                           {"primo/ultimo", "erste/letzte", "premier/dernier", "primero/último", "pierwszy/ostatni"},
	"incoming":                                             {"in ingresso", "eingehend", "entrant", "entrante", "przychodzące"},
	"moving":                                               {"moving", "moving", "moving", "moving", "moving"},
	"none":                                                 {"nessuno", "keine", "aucun", "ninguno", "brak"},
	"paste magnet:… or https://…/file.torrent": {"incolla magnet:… o https://…/file.torrent", "magnet:… oder https://…/file.torrent einfügen", "coller magnet:… ou https://…/file.torrent", "pega magnet:… o https://…/file.torrent", "wklej magnet:… lub https://…/file.torrent"},
	"paused":                    {"paused", "paused", "paused", "paused", "paused"},
	"peer port":                 {"porta peer", "peer Port", "port peer", "puerto peer", "port peer"},
	"seeding":                   {"seeding", "seeding", "seeding", "seeding", "seeding"},
	"sequential":                {"sequenziale", "sequenziell", "séquentiel", "secuencial", "sekwencyjnie"},
	"stalled":                   {"stalled", "stalled", "stalled", "stalled", "stalled"},
	"super-seeding":             {"super-seeding", "Super-Seeding", "super-seeding", "super-seeding", "super-seeding"},
	"top":                       {"in cima", "nach oben", "en haut", "arriba", "na górę"},
	"torrent":                   {"torrent", "torrent", "torrent", "torrent", "torrent"},
	"↻ Reannounce":              {"↻ Riannuncia", "↻ Erneut ankündigen", "↻ Réannoncer", "↻ Reanunciar", "↻ Ogłoś ponownie"},
	"⏸ Pause":                   {"⏸ Pausa", "⏸ Pausieren", "⏸ Pause", "⏸ Pausar", "⏸ Pauza"},
	"▶ Resume":                  {"▶ Riprendi", "▶ Fortsetzen", "▶ Reprendre", "▶ Reanudar", "▶ Wznów"},
	"✓ Recheck":                 {"✓ Riverifica", "✓ Erneut prüfen", "✓ Revérifier", "✓ Revalidar", "✓ Sprawdź ponownie"},
	"✕ Remove":                  {"✕ Rimuovi", "✕ Entfernen", "✕ Supprimer", "✕ Eliminar", "✕ Usuń"},
	"✕ Remove and delete files": {"✕ Rimuovi ed elimina i file", "✕ Entfernen und Dateien löschen", "✕ Supprimer et effacer les fichiers", "✕ Eliminar y borrar archivos", "✕ Usuń i usuń pliki"},
	"⤒ Top":                     {"⤒ In cima", "⤒ Nach oben", "⤒ En haut", "⤒ Arriba", "⤒ Na górę"},
}

// uiLangIndex maps a language code to its column in uiCatalog.
var uiLangIndex = map[string]int{"it": 0, "de": 1, "fr": 2, "es": 3, "pl": 4}

// uiDictionary returns the English-to-language map for lang, or nil when
// lang is English or unknown (the page then stays as it is).
func uiDictionary(lang string) map[string]string {
	idx, ok := uiLangIndex[lang]
	if !ok {
		return nil
	}
	dict := make(map[string]string, len(uiCatalog))
	for key, value := range uiCatalog {
		dict[key] = value[idx]
	}
	return dict
}
