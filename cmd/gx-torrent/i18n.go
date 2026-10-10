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
	"downloading":                                          {"in download", "Lädt", "en téléchargement", "descargando", "pobieranie"},
	"first/last":                                           {"primo/ultimo", "erste/letzte", "premier/dernier", "primero/último", "pierwszy/ostatni"},
	"incoming":                                             {"in ingresso", "eingehend", "entrant", "entrante", "przychodzące"},
	"moving":                                               {"spostamento", "Wird verschoben", "déplacement", "moviendo", "przenoszenie"},
	"none":                                                 {"nessuno", "keine", "aucun", "ninguno", "brak"},
	"paste magnet:… or https://…/file.torrent": {"incolla magnet:… o https://…/file.torrent", "magnet:… oder https://…/file.torrent einfügen", "coller magnet:… ou https://…/file.torrent", "pega magnet:… o https://…/file.torrent", "wklej magnet:… lub https://…/file.torrent"},
	"paused":                    {"in pausa", "Pausiert", "en pause", "en pausa", "wstrzymany"},
	"peer port":                 {"porta peer", "peer Port", "port peer", "puerto peer", "port peer"},
	"seeding":                   {"in seed", "Seeding", "en seed", "en seed", "seedowanie"},
	"sequential":                {"sequenziale", "sequenziell", "séquentiel", "secuencial", "sekwencyjnie"},
	"stalled":                   {"in stallo", "Blockiert", "bloqué", "estancado", "zablokowany"},
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

	// Page chrome and status bar.
	"up for":   {"attivo da", "läuft seit", "actif depuis", "activo desde", "działa od"},
	"or":       {"o", "oder", "ou", "o", "lub"},
	"MB":       {"MB", "MB", "MB", "MB", "MB"},
	"of":       {"di", "von", "sur", "de", "z"},
	"nodes":    {"nodi", "Knoten", "nœuds", "nodos", "węzłów"},
	"off":      {"spento", "aus", "désactivé", "desactivado", "wyłączone"},
	"open":     {"aperta", "offen", "ouvert", "abierta", "otwarty"},
	"not open": {"chiusa", "geschlossen", "fermé", "cerrada", "zamknięty"},
	"lines":    {"righe", "Zeilen", "lignes", "líneas", "wiersze"},
	"Lines":    {"Righe", "Zeilen", "Lignes", "Líneas", "Wiersze"},
	"Peer/tracker errors filtered from the log, by category (since start)": {"Errori peer/tracker filtrati dal log, per categoria (dall'avvio)", "Vom Peer/Tracker aus dem Log gefilterte Fehler, nach Kategorie (seit dem Start)", "Erreurs peer/tracker filtrées du journal, par catégorie (depuis le démarrage)", "Errores de peer/tracker filtrados del registro, por categoría (desde el inicio)", "Błędy peer/tracker odfiltrowane z dziennika, według kategorii (od uruchomienia)"},

	// Tabs and Gextto log pane.
	"Torrents":      {"Torrent", "Torrents", "Torrents", "Torrents", "Torrenty"},
	"Gextto log":    {"Log di Gextto", "Gextto-Protokoll", "Journal de Gextto", "Registro de Gextto", "Dziennik Gextto"},
	"Filter lines…": {"Filtra righe…", "Zeilen filtern…", "Filtrer les lignes…", "Filtrar líneas…", "Filtruj wiersze…"},
	"hide DEBUG":    {"nascondi DEBUG", "DEBUG ausblenden", "masquer DEBUG", "ocultar DEBUG", "ukryj DEBUG"},
	"↻ Reload":      {"↻ Ricarica", "↻ Neu laden", "↻ Recharger", "↻ Recargar", "↻ Odśwież"},
	"Loading…":      {"Caricamento…", "Lädt…", "Chargement…", "Cargando…", "Ładowanie…"},

	// Torrent detail.
	"Close":                    {"Chiudi", "Schließen", "Fermer", "Cerrar", "Zamknij"},
	"General":                  {"Generale", "Allgemein", "Général", "General", "Ogólne"},
	"Files":                    {"File", "Dateien", "Fichiers", "Archivos", "Pliki"},
	"Peers":                    {"Peer", "Peers", "Pairs", "Pares", "Peery"},
	"Trackers":                 {"Tracker", "Tracker", "Trackers", "Trackers", "Trackery"},
	"Pieces":                   {"Pezzi", "Teile", "Pièces", "Piezas", "Części"},
	"State":                    {"Stato", "Status", "État", "Estado", "Stan"},
	"Progress":                 {"Avanzamento", "Fortschritt", "Progression", "Progreso", "Postęp"},
	"Size":                     {"Dimensione", "Größe", "Taille", "Tamaño", "Rozmiar"},
	"Done":                     {"Completato", "Fertig", "Terminé", "Completado", "Ukończono"},
	"Downloaded":               {"Scaricato", "Heruntergeladen", "Téléchargé", "Descargado", "Pobrano"},
	"Uploaded":                 {"Caricato", "Hochgeladen", "Envoyé", "Subido", "Wysłano"},
	"Ratio":                    {"Rapporto", "Verhältnis", "Ratio", "Ratio", "Współczynnik"},
	"Peer / Seed":              {"Peer / Seed", "Peers / Seeds", "Pairs / Seeds", "Pares / Seeds", "Peery / Seedy"},
	"Swarm (seeds / peers)":    {"Sciame (seed / peer)", "Swarm (Seeds / Peers)", "Swarm (seeds / pairs)", "Enjambre (seeds / pares)", "Swarm (seedy / peery)"},
	"ETA":                      {"ETA", "ETA", "ETA", "ETA", "ETA"},
	"Folder":                   {"Cartella", "Ordner", "Dossier", "Carpeta", "Folder"},
	"Seed ratio set":           {"Rapporto di seed impostato", "Gesetztes Seed-Verhältnis", "Ratio de seed défini", "Ratio de seed definido", "Ustawiony współczynnik seedowania"},
	"days":                     {"giorni", "Tage", "jours", "días", "dni"},
	"Pin":                      {"Fissa", "Anheften", "Épingler", "Fijar", "Przypnij"},
	"Unpin":                    {"Sblocca", "Lösen", "Détacher", "Soltar", "Odepnij"},
	"Pin (outside the queue)":  {"Fissa (fuori coda)", "Anheften (außerhalb der Warteschlange)", "Épingler (hors file)", "Fijar (fuera de la cola)", "Przypnij (poza kolejką)"},
	"yes":                      {"sì", "ja", "oui", "sí", "tak"},
	"no":                       {"no", "nein", "non", "no", "nie"},
	"Private":                  {"Privato", "Privat", "Privé", "Privado", "Prywatny"},
	"File":                     {"File", "Datei", "Fichier", "Archivo", "Plik"},
	"Available / total pieces": {"Pezzi disponibili / totali", "Verfügbare / gesamte Teile", "Pièces disponibles / totales", "Piezas disponibles / totales", "Części dostępne / wszystkie"},
	"Completed pieces":         {"Pezzi completati", "Abgeschlossene Teile", "Pièces terminées", "Piezas completadas", "Ukończone części"},
	"Piece size":               {"Dimensione pezzo", "Teilgröße", "Taille des pièces", "Tamaño de pieza", "Rozmiar części"},
	"Wasted":                   {"Sprecato", "Verschwendet", "Gaspillé", "Desperdiciado", "Zmarnowane"},
	"Allocated on disk":        {"Allocato su disco", "Auf der Festplatte belegt", "Alloué sur disque", "Asignado en disco", "Zajęte na dysku"},
	"Added":                    {"Aggiunto", "Hinzugefügt", "Ajouté", "Añadido", "Dodano"},
	"Completed":                {"Completato", "Abgeschlossen", "Terminé", "Completado", "Ukończono"},
	"Copy magnet":              {"Copia magnet", "Magnet kopieren", "Copier le magnet", "Copiar magnet", "Kopiuj magnet"},
	"Export .torrent":          {"Esporta .torrent", ".torrent exportieren", "Exporter le .torrent", "Exportar .torrent", "Eksportuj .torrent"},
	"Limits & seeding (-1 global, 0 unlimited).": {"Limiti e seeding (-1 globale, 0 illimitato).", "Limits & Seeding (-1 global, 0 unbegrenzt).", "Limites et seeding (-1 global, 0 illimité).", "Límites y seeding (-1 global, 0 ilimitado).", "Limity i seedowanie (-1 globalne, 0 bez limitu)."},
	"Download limit KiB/s":                       {"Limite download KiB/s", "Download-Limit KiB/s", "Limite de téléchargement KiB/s", "Límite de descarga KiB/s", "Limit pobierania KiB/s"},
	"Upload limit KiB/s":                         {"Limite upload KiB/s", "Upload-Limit KiB/s", "Limite d'envoi KiB/s", "Límite de subida KiB/s", "Limit wysyłania KiB/s"},
	"Max connections":                            {"Connessioni massime", "Max. Verbindungen", "Connexions max.", "Conexiones máx.", "Maks. połączeń"},
	"Max uploads":                                {"Upload massimi", "Max. Uploads", "Envois max.", "Subidas máx.", "Maks. wysyłań"},
	"Seed ratio":                                 {"Rapporto di seed", "Seed-Verhältnis", "Ratio de seed", "Ratio de seed", "Współczynnik seedowania"},
	"Seed days":                                  {"Giorni di seed", "Seed-Tage", "Jours de seed", "Días de seed", "Dni seedowania"},
	"Save limits":                                {"Salva limiti", "Limits speichern", "Enregistrer les limites", "Guardar límites", "Zapisz limity"},
	"Super-seeding (BEP 16)":                     {"Super-seeding (BEP 16)", "Super-Seeding (BEP 16)", "Super-seeding (BEP 16)", "Super-seeding (BEP 16)", "Super-seeding (BEP 16)"},
	"Save":                                       {"Salva", "Speichern", "Enregistrer", "Guardar", "Zapisz"},
	"Web seed (one URL per line)":                {"Web seed (un URL per riga)", "Web seed (eine URL pro Zeile)", "Web seed (une URL par ligne)", "Web seed (una URL por línea)", "Web seed (jeden URL w wierszu)"},
	"Remove":                                     {"Rimuovi", "Entfernen", "Supprimer", "Eliminar", "Usuń"},
	"Move data to":                               {"Sposta i dati in", "Daten verschieben nach", "Déplacer les données vers", "Mover datos a", "Przenieś dane do"},
	"Move":                                       {"Sposta", "Verschieben", "Déplacer", "Mover", "Przenieś"},

	// Categories and tags (standalone).
	"Categories":                {"Categorie", "Kategorien", "Catégories", "Categorías", "Kategorie"},
	"Category":                  {"Categoria", "Kategorie", "Catégorie", "Categoría", "Kategoria"},
	"Tags":                      {"Tag", "Tags", "Étiquettes", "Etiquetas", "Tagi"},
	"tag, tag":                  {"tag, tag", "Tag, Tag", "étiquette, étiquette", "etiqueta, etiqueta", "tag, tag"},
	"Category and tags updated": {"Categoria e tag aggiornati", "Kategorie und Tags aktualisiert", "Catégorie et étiquettes mises à jour", "Categoría y etiquetas actualizadas", "Zaktualizowano kategorię i tagi"},

	// Indexer search (standalone).
	"Search":           {"Cerca", "Suchen", "Rechercher", "Buscar", "Szukaj"},
	"Search indexers…": {"Cerca negli indexer…", "Indexer durchsuchen…", "Rechercher dans les indexeurs…", "Buscar en los indexadores…", "Szukaj w indekserach…"},
	"Searching…":       {"Ricerca…", "Suche…", "Recherche…", "Buscando…", "Szukam…"},
	"No results":       {"Nessun risultato", "Keine Ergebnisse", "Aucun résultat", "Sin resultados", "Brak wyników"},

	// RSS feeds (standalone).
	"Feeds":       {"Feed", "Feeds", "Flux", "Feeds", "Kanały"},
	"Check feeds": {"Controlla i feed", "Feeds prüfen", "Vérifier les flux", "Comprobar feeds", "Sprawdź kanały"},
	"Checking…":   {"Controllo…", "Prüfe…", "Vérification…", "Comprobando…", "Sprawdzam…"},

	// Files tab.
	"Priority": {"Priorità", "Priorität", "Priorité", "Prioridad", "Priorytet"},
	"Play":     {"Riproduci", "Abspielen", "Lire", "Reproducir", "Odtwórz"},
	"Skip":     {"Salta", "Überspringen", "Ignorer", "Omitir", "Pomiń"},
	"Download": {"Scarica", "Herunterladen", "Télécharger", "Descargar", "Pobierz"},
	"Stream this file with HTTP Range; the pieces of the window are downloaded first": {"Trasmetti questo file con HTTP Range; i pezzi della finestra vengono scaricati per primi", "Diese Datei per HTTP Range streamen; die Teile des Fensters werden zuerst geladen", "Diffuser ce fichier en HTTP Range ; les pièces de la fenêtre sont téléchargées en premier", "Transmitir este archivo con HTTP Range; las piezas de la ventana se descargan primero", "Przesyłaj strumieniowo ten plik przez HTTP Range; fragmenty okna są pobierane najpierw"},
	"Metadata not available yet.": {"Metadati non ancora disponibili.", "Metadaten noch nicht verfügbar.", "Métadonnées pas encore disponibles.", "Metadatos aún no disponibles.", "Metadane jeszcze niedostępne."},

	// Peers tab.
	"Peer":                {"Peer", "Peer", "Pair", "Par", "Peer"},
	"Client":              {"Client", "Client", "Client", "Cliente", "Klient"},
	"Source":              {"Origine", "Quelle", "Source", "Origen", "Źródło"},
	"Prog.":               {"Prog.", "Fortschr.", "Prog.", "Prog.", "Post."},
	"Seed":                {"Seed", "Seed", "Seed", "Seed", "Seed"},
	"Flag":                {"Flag", "Flag", "Drapeau", "Indicador", "Flaga"},
	"For":                 {"Da", "Seit", "Depuis", "Desde", "Od"},
	"outgoing":            {"in uscita", "ausgehend", "sortant", "saliente", "wychodzące"},
	"encrypted":           {"cifrato", "verschlüsselt", "chiffré", "cifrado", "zaszyfrowane"},
	"encrypted HS":        {"HS cifrato", "verschlüsselt HS", "HS chiffré", "HS cifrado", "zaszyfrowane HS"},
	"snubbed":             {"snobbato", "ignoriert", "ignoré", "ignorado", "pomijany"},
	"optimistic":          {"ottimistico", "optimistisch", "optimiste", "optimista", "optymistyczny"},
	"choking us":          {"ci blocca", "blockiert uns", "nous bloque", "nos bloquea", "blokuje nas"},
	"we choke":            {"blocchiamo", "wir blockieren", "nous bloquons", "bloqueamos", "blokujemy"},
	"interested":          {"interessato", "interessiert", "intéressé", "interesado", "zainteresowany"},
	"interested in us":    {"interessato a noi", "an uns interessiert", "intéressé par nous", "interesado en nosotros", "zainteresowany nami"},
	"No peers connected.": {"Nessun peer connesso.", "Keine Peers verbunden.", "Aucun pair connecté.", "Ningún par conectado.", "Brak połączonych peerów."},

	// Trackers tab.
	"URL":                         {"URL", "URL", "URL", "URL", "URL"},
	"Message":                     {"Messaggio", "Nachricht", "Message", "Mensaje", "Komunikat"},
	"Seeds":                       {"Seed", "Seeds", "Seeds", "Seeds", "Seedy"},
	"Next":                        {"Prossimo", "Nächster", "Prochain", "Próximo", "Następny"},
	"Remove this tracker":         {"Rimuovi questo tracker", "Diesen Tracker entfernen", "Supprimer ce tracker", "Eliminar este tracker", "Usuń ten tracker"},
	"No trackers.":                {"Nessun tracker.", "Keine Tracker.", "Aucun tracker.", "Ningún tracker.", "Brak trackerów."},
	"Add trackers (one per line)": {"Aggiungi tracker (uno per riga)", "Tracker hinzufügen (einer pro Zeile)", "Ajouter des trackers (un par ligne)", "Añadir trackers (uno por línea)", "Dodaj trackery (jeden w wierszu)"},
	"Add trackers":                {"Aggiungi tracker", "Tracker hinzufügen", "Ajouter des trackers", "Añadir trackers", "Dodaj trackery"},

	// Pieces tab.
	"Piece map": {"Mappa dei pezzi", "Teile-Karte", "Carte des pièces", "Mapa de piezas", "Mapa części"},
	"pieces downloaded. Green: have, yellow: downloading, grey: missing, dimmed: skipped.": {"pezzi scaricati. Verde: disponibile, giallo: in download, grigio: mancante, attenuato: escluso.", "Teile heruntergeladen. Grün: vorhanden, gelb: wird geladen, grau: fehlt, gedimmt: übersprungen.", "pièces téléchargées. Vert : présent, jaune : en téléchargement, gris : manquant, atténué : ignoré.", "piezas descargadas. Verde: disponible, amarillo: descargando, gris: faltante, atenuado: omitido.", "fragmentów pobranych. Zielony: dostępne, żółty: pobierane, szary: brakujące, przygaszony: pominięte."},
	"have":    {"disponibile", "vorhanden", "présent", "disponible", "dostępne"},
	"skipped": {"escluso", "übersprungen", "ignoré", "omitido", "pominięte"},
	"missing": {"mancante", "fehlt", "manquant", "faltante", "brakujące"},
	"Piece map is not available yet (metadata missing).": {"Mappa dei pezzi non ancora disponibile (metadati mancanti).", "Teile-Karte noch nicht verfügbar (Metadaten fehlen).", "Carte des pièces pas encore disponible (métadonnées manquantes).", "Mapa de piezas aún no disponible (faltan metadatos).", "Mapa części jeszcze niedostępna (brak metadanych)."},

	// Filters and table headers.
	"Filters":                              {"Filtri", "Filter", "Filtres", "Filtros", "Filtry"},
	"All":                                  {"Tutti", "Alle", "Tous", "Todos", "Wszystkie"},
	"Downloading":                          {"In download", "Lädt", "En téléchargement", "Descargando", "Pobieranie"},
	"Seeding":                              {"In seed", "Seeding", "En seed", "Sembrando", "Seedowanie"},
	"Paused":                               {"In pausa", "Pausiert", "En pause", "En pausa", "Wstrzymane"},
	"Stalled":                              {"In stallo", "Blockiert", "Bloqués", "Estancados", "Zablokowane"},
	"Moving":                               {"Spostamento", "Verschieben", "Déplacement", "Moviendo", "Przenoszenie"},
	"Error":                                {"Errore", "Fehler", "Erreur", "Error", "Błąd"},
	"Name":                                 {"Nome", "Name", "Nom", "Nombre", "Nazwa"},
	"Actions":                              {"Azioni", "Aktionen", "Actions", "Acciones", "Akcje"},
	"Done / Size":                          {"Completato / Dimensione", "Fertig / Größe", "Terminé / Taille", "Completado / Tamaño", "Ukończono / Rozmiar"},
	"Select all":                           {"Seleziona tutto", "Alle auswählen", "Tout sélectionner", "Seleccionar todo", "Zaznacz wszystko"},
	"Details: files, peers, trackers":      {"Dettagli: file, peer, tracker", "Details: Dateien, Peers, Tracker", "Détails : fichiers, pairs, trackers", "Detalles: archivos, pares, trackers", "Szczegóły: pliki, peery, trackery"},
	"Resume":                               {"Riprendi", "Fortsetzen", "Reprendre", "Reanudar", "Wznów"},
	"Pause":                                {"Pausa", "Pausieren", "Pause", "Pausar", "Pauza"},
	"Recheck data on disk":                 {"Riverifica i dati su disco", "Daten auf der Festplatte erneut prüfen", "Revérifier les données sur disque", "Revalidar los datos en disco", "Sprawdź ponownie dane na dysku"},
	"Reannounce to trackers":               {"Riannuncia ai tracker", "Bei Trackern erneut ankündigen", "Réannoncer aux trackers", "Reanunciar a los trackers", "Ogłoś ponownie trackerom"},
	"Move to the top of the queue":         {"Sposta in cima alla coda", "An den Anfang der Warteschlange", "Déplacer en haut de la file", "Mover al principio de la cola", "Przenieś na początek kolejki"},
	"Remove from the session (files stay)": {"Rimuovi dalla sessione (i file restano)", "Aus der Sitzung entfernen (Dateien bleiben)", "Retirer de la session (les fichiers restent)", "Quitar de la sesión (los archivos permanecen)", "Usuń z sesji (pliki pozostają)"},
	"Remove and delete the files":          {"Rimuovi ed elimina i file", "Entfernen und Dateien löschen", "Supprimer et effacer les fichiers", "Eliminar y borrar archivos", "Usuń i usuń pliki"},
	"✕ file":                               {"✕ file", "✕ Datei", "✕ fichier", "✕ archivo", "✕ plik"},

	// Torrent states reported by the backend.
	"checking_files":       {"verifica in corso", "Prüft Dateien", "vérification des fichiers", "comprobando archivos", "sprawdzanie plików"},
	"downloading_metadata": {"download metadati", "Lädt Metadaten", "téléchargement des métadonnées", "descargando metadatos", "pobieranie metadanych"},
	"error":                {"errore", "Fehler", "erreur", "error", "błąd"},

	// Standalone sign-in page.
	"Sign in":                         {"Accedi", "Anmelden", "Se connecter", "Iniciar sesión", "Zaloguj się"},
	"Sign in to the gx-torrent page.": {"Accedi alla pagina di gx-torrent.", "Bei der gx-torrent-Seite anmelden.", "Connectez-vous à la page gx-torrent.", "Inicia sesión en la página de gx-torrent.", "Zaloguj się do strony gx-torrent."},
	"user":                            {"utente", "Benutzer", "utilisateur", "usuario", "użytkownik"},
	"password":                        {"password", "Passwort", "mot de passe", "contraseña", "hasło"},
	"Wrong username or password.":     {"Nome utente o password errati.", "Benutzername oder Passwort falsch.", "Nom d'utilisateur ou mot de passe incorrect.", "Usuario o contraseña incorrectos.", "Nieprawidłowa nazwa użytkownika lub hasło."},

	// Standalone first-run wizard.
	"gx-torrent setup": {"Configurazione di gx-torrent", "gx-torrent Einrichtung", "Configuration de gx-torrent", "Configuración de gx-torrent", "Konfiguracja gx-torrent"},
	"First run: choose how the daemon works. The settings are saved in settings.json.": {"Primo avvio: scegli come deve funzionare il demone. Le impostazioni sono salvate in settings.json.", "Erster Start: lege fest, wie der Daemon arbeitet. Die Einstellungen werden in settings.json gespeichert.", "Premier lancement : choisissez le fonctionnement du démon. Les réglages sont enregistrés dans settings.json.", "Primer inicio: elige cómo funciona el demonio. Los ajustes se guardan en settings.json.", "Pierwsze uruchomienie: wybierz, jak ma działać demon. Ustawienia są zapisywane w settings.json."},
	"Language":                         {"Lingua", "Sprache", "Langue", "Idioma", "Język"},
	"Download folder":                  {"Cartella di download", "Download-Ordner", "Dossier de téléchargement", "Carpeta de descarga", "Folder pobierania"},
	"Access":                           {"Accesso", "Zugang", "Accès", "Acceso", "Dostęp"},
	"Username":                         {"Nome utente", "Benutzername", "Nom d'utilisateur", "Usuario", "Nazwa użytkownika"},
	"Password":                         {"Password", "Passwort", "Mot de passe", "Contraseña", "Hasło"},
	"Confirm password":                 {"Conferma password", "Passwort bestätigen", "Confirmer le mot de passe", "Confirmar contraseña", "Potwierdź hasło"},
	"Allow the LAN without a password": {"Consenti la LAN senza password", "LAN ohne Passwort zulassen", "Autoriser le LAN sans mot de passe", "Permitir la LAN sin contraseña", "Zezwól na LAN bez hasła"},
	"Network":                          {"Rete", "Netzwerk", "Réseau", "Red", "Sieć"},
	"Peer port":                        {"Porta peer", "Peer-Port", "Port peer", "Puerto peer", "Port peer"},
	"Finish setup":                     {"Termina configurazione", "Einrichtung abschließen", "Terminer la configuration", "Finalizar configuración", "Zakończ konfigurację"},
	"The download folder and the peer port apply at the next start.": {"La cartella di download e la porta peer valgono dal prossimo avvio.", "Download-Ordner und Peer-Port gelten ab dem nächsten Start.", "Le dossier de téléchargement et le port peer s'appliquent au prochain démarrage.", "La carpeta de descarga y el puerto peer se aplican en el próximo inicio.", "Folder pobierania i port peer działają od następnego uruchomienia."},
	"The download folder must be an absolute path.":                  {"La cartella di download deve essere un percorso assoluto.", "Der Download-Ordner muss ein absoluter Pfad sein.", "Le dossier de téléchargement doit être un chemin absolu.", "La carpeta de descarga debe ser una ruta absoluta.", "Folder pobierania musi być ścieżką bezwzględną."},
	"The password and the confirmation do not match.":                {"Le password non coincidono.", "Passwort und Bestätigung stimmen nicht überein.", "Le mot de passe et la confirmation ne correspondent pas.", "La contraseña y su confirmación no coinciden.", "Hasło i potwierdzenie nie są zgodne."},
	"Setup failed.": {"Configurazione non riuscita.", "Einrichtung fehlgeschlagen.", "Échec de la configuration.", "Error en la configuración.", "Konfiguracja nie powiodła się."},

	// Strings rendered by the page's JavaScript through t().
	"Select at least one torrent":                                      {"Seleziona almeno un torrent", "Wähle mindestens einen Torrent", "Sélectionne au moins un torrent", "Selecciona al menos un torrent", "Wybierz co najmniej jeden torrent"},
	"Remove the torrent? Files stay on disk.":                          {"Rimuovere il torrent? I file restano su disco.", "Torrent entfernen? Die Dateien bleiben auf der Festplatte.", "Retirer le torrent ? Les fichiers restent sur le disque.", "¿Quitar el torrent? Los archivos permanecen en disco.", "Usunąć torrent? Pliki pozostaną na dysku."},
	"Remove the torrent AND DELETE the files? irreversible.":           {"Rimuovere il torrent ED ELIMINARE i file? irreversibile.", "Torrent entfernen UND die Dateien löschen? unwiderruflich.", "Retirer le torrent ET SUPPRIMER les fichiers ? irréversible.", "¿Quitar el torrent Y BORRAR los archivos? irreversible.", "Usunąć torrent I USUNĄĆ pliki? nieodwracalne."},
	"Remove the selected torrents AND delete the files? Irreversible.": {"Rimuovere i torrent selezionati ED eliminare i file? Irreversibile.", "Die ausgewählten Torrents entfernen UND die Dateien löschen? Unwiderruflich.", "Retirer les torrents sélectionnés ET supprimer les fichiers ? Irréversible.", "¿Quitar los torrents seleccionados Y borrar los archivos? Irreversible.", "Usunąć wybrane torrenty I usunąć pliki? Nieodwracalne."},
	"Magnet copied":               {"Magnet copiato", "Magnet kopiert", "Magnet copié", "Magnet copiado", "Magnet skopiowany"},
	"Copy failed":                 {"Copia non riuscita", "Kopieren fehlgeschlagen", "Échec de la copie", "Copia fallida", "Kopiowanie nie powiodło się"},
	"Copy unavailable":            {"Copia non disponibile", "Kopieren nicht verfügbar", "Copie indisponible", "Copia no disponible", "Kopiowanie niedostępne"},
	"checking…":                   {"verifica…", "prüfe…", "vérification…", "comprobando…", "sprawdzanie…"},
	"test failed:":                {"test non riuscito:", "Test fehlgeschlagen:", "test échoué :", "prueba fallida:", "test nie powiódł się:"},
	"loading…":                    {"caricamento…", "lädt…", "chargement…", "cargando…", "ładowanie…"},
	"Cannot read the Gextto log:": {"Impossibile leggere il log di Gextto:", "Gextto-Protokoll kann nicht gelesen werden:", "Impossible de lire le journal de Gextto :", "No se puede leer el registro de Gextto:", "Nie można odczytać dziennika Gextto:"},
}

// uiClientKeys are the catalog keys the page's JavaScript looks up at runtime
// through the t() helper, using the dictionary injected into the page. Every
// one must be a uiCatalog key (checked by the coverage test) or the client
// would silently fall back to English.
var uiClientKeys = []string{
	"Select at least one torrent",
	"Remove the torrent? Files stay on disk.",
	"Remove the torrent AND DELETE the files? irreversible.",
	"Remove the selected torrents AND delete the files? Irreversible.",
	"Magnet copied",
	"Copy failed",
	"Copy unavailable",
	"checking…",
	"test failed:",
	"loading…",
	"Cannot read the Gextto log:",
	"lines",
	"Search",
	"Searching…",
	"No results",
	"Checking…",
	"Seeds",
	"Add",
}

// uiLangIndex maps a language code to its column in uiCatalog.
var uiLangIndex = map[string]int{"it": 0, "de": 1, "fr": 2, "es": 3, "pl": 4}

// uiDicts holds one ready dictionary per language, built once at startup: the
// catalog is static, so there is no reason to rebuild it per request.
var uiDicts = func() map[string]map[string]string {
	dicts := make(map[string]map[string]string, len(uiLangIndex))
	for lang, idx := range uiLangIndex {
		dict := make(map[string]string, len(uiCatalog))
		for key, value := range uiCatalog {
			dict[key] = value[idx]
		}
		dicts[lang] = dict
	}
	return dicts
}()

// uiDictionary returns the English-to-language map for lang, or nil when
// lang is English or unknown (the page then stays as it is).
func uiDictionary(lang string) map[string]string {
	return uiDicts[lang]
}
