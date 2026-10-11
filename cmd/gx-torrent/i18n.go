package main

// i18n.go holds the page strings and their translations.
// The page source is English; uiDictionary returns, for a language, the
// map used by translateHTML to localize the rendered page (the same
// post-processing approach used by Gextto's web UI).

// uiCatalog maps an English page string to {it, de, fr, es, pl}.
var uiCatalog = map[string][5]string{
	".torrent file to add":         {"File .torrent da aggiungere", ".torrent-Datei zum Hinzufügen", "Fichier .torrent à ajouter", "Archivo .torrent para añadir", "Plik .torrent do dodania"},
	"Add":                          {"Aggiungi", "Hinzufügen", "Ajouter", "Añadir", "Dodaj"},
	"Create .torrent":              {"Crea .torrent", "Torrent erstellen", "Créer un .torrent", "Crear .torrent", "Utwórz .torrent"},
	"Folder or file on the server": {"Cartella o file sul server", "Ordner oder Datei auf dem Server", "Dossier ou fichier sur le serveur", "Carpeta o archivo en el servidor", "Folder lub plik na serwerze"},
	"Name (optional)":              {"Nome (facoltativo)", "Name (optional)", "Nom (facultatif)", "Nombre (opcional)", "Nazwa (opcjonalnie)"},
	"Trackers, one per line":       {"Tracker, uno per riga", "Tracker, einer pro Zeile", "Trackers, un par ligne", "Trackers, uno por línea", "Trackery, jeden w wierszu"},
	"BEP 16 super-seeding: advertise one piece at a time so the swarm spreads the data. For initial seeding only; it reduces the seed's upload throughput": {"BEP 16 super-seeding: annuncia un pezzo alla volta così lo swarm distribuisce i dati. Solo per il seeding iniziale; riduce il throughput di upload del seed", "BEP 16 super-seeding: jeweils ein Stück ankündigen, damit der swarm die Daten verteilt. Nur für initiales seeding; reduziert den Upload-Durchsatz des seed", "BEP 16 super-seeding : annoncer un morceau à la fois pour que le swarm diffuse les données. Pour le seeding initial uniquement ; réduit le débit d'envoi du seed", "BEP 16 super-seeding: anunciar una pieza a la vez para que el swarm distribuya los datos. Solo para el seeding inicial; reduce el rendimiento de subida del seed", "BEP 16 super-seeding: ogłaszaj jeden fragment naraz, aby swarm rozgłaszał dane. Tylko do początkowego seedowania; zmniejsza przepustowość wysyłania seeda"},
	"Check that the peer port is listening and forwarded by the router":                                                                                    {"Verifica se la porta è in ascolto e inoltrata dal router", "Prüft, ob der Port lauscht und vom Router weitergeleitet wird", "Vérifie si le port est en écoute et redirigé par le routeur", "Comprueba si el puerto está a la escucha y redirigido por el router", "Sprawdza, czy port nasłuchuje i jest przekierowany przez router"},
	"DHT":                          {"DHT", "DHT", "DHT", "DHT", "DHT"},
	"DHT swarm":                    {"Sciame DHT", "DHT-Schwarm", "Essaim DHT", "Enjambre DHT", "Rój DHT"},
	"DHT swarm (seeders / peers):": {"Sciame DHT (seed/peer):", "DHT-Schwarm (Seeder/Peers):", "Essaim DHT (sources/pairs) :", "Enjambre DHT (seeders/peers):", "Rój DHT (seed/peer):"},
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
	"Showing":     {"Mostrati", "Angezeigt", "Affichés", "Mostrando", "Wyświetlono"},
	"Show more":   {"Mostra altri", "Mehr anzeigen", "Afficher plus", "Mostrar más", "Pokaż więcej"},

	// RSS management page (standalone).
	"Rules":      {"Regole", "Regeln", "Règles", "Reglas", "Reguły"},
	"Indexers":   {"Indexer", "Indexer", "Indexeurs", "Indexadores", "Indeksery"},
	"Feed items": {"Elementi del feed", "Feed-Elemente", "Éléments du flux", "Elementos del feed", "Elementy kanału"},
	"Title":      {"Titolo", "Titel", "Titre", "Título", "Tytuł"},
	"Rule":       {"Regola", "Regel", "Règle", "Regla", "Reguła"},
	"Feeds, rules and indexers are saved in settings.json. The rules are evaluated in order; the first match wins.": {"Feed, regole e indexer sono salvati in settings.json. Le regole sono valutate in ordine; vince la prima che corrisponde.", "Feeds, Regeln und Indexer werden in settings.json gespeichert. Die Regeln werden der Reihe nach geprüft; die erste passende gewinnt.", "Les flux, règles et indexeurs sont enregistrés dans settings.json. Les règles sont évaluées dans l'ordre ; la première correspondance gagne.", "Los feeds, reglas e indexadores se guardan en settings.json. Las reglas se evalúan en orden; gana la primera coincidencia.", "Kanały, reguły i indeksery są zapisywane w settings.json. Reguły są sprawdzane po kolei; wygrywa pierwsza pasująca."},

	// Standalone navigation and the friendly indexer/feed editors.
	"RSS & indexers": {"RSS e indexer", "RSS & Indexer", "RSS et indexeurs", "RSS e indexadores", "RSS i indeksery"},
	"Type":           {"Tipo", "Typ", "Type", "Tipo", "Typ"},
	"API key":        {"Chiave API", "API-Schlüssel", "Clé API", "Clave API", "Klucz API"},
	"Add indexer":    {"Aggiungi indexer", "Indexer hinzufügen", "Ajouter un indexeur", "Añadir indexador", "Dodaj indekser"},
	"Add feed":       {"Aggiungi feed", "Feed hinzufügen", "Ajouter un flux", "Añadir feed", "Dodaj kanał"},
	"Feed URL":       {"URL del feed", "Feed-URL", "URL du flux", "URL del feed", "URL kanału"},
	"Search query":   {"Query di ricerca", "Suchbegriff", "Requête de recherche", "Consulta de búsqueda", "Zapytanie wyszukiwania"},
	"Include":        {"Includi", "Einschließen", "Inclure", "Incluir", "Dołącz"},
	"Exclude":        {"Escludi", "Ausschließen", "Exclure", "Excluir", "Wyklucz"},
	"Save path":      {"Percorso di salvataggio", "Speicherpfad", "Chemin d'enregistrement", "Ruta de guardado", "Ścieżka zapisu"},
	"Advanced JSON":  {"JSON avanzato", "Erweitertes JSON", "JSON avancé", "JSON avanzado", "Zaawansowany JSON"},
	"Other":          {"Altro", "Andere", "Autre", "Otro", "Inny"},
	"None":           {"Nessuno", "Keine", "Aucun", "Ninguno", "Brak"},
	"active":         {"attivo", "aktiv", "actif", "activo", "aktywny"},
	"A feed is an RSS URL, or a Torznab search query (Search query) across the configured indexers. Empty = the global default.": {"Un feed è un URL RSS, oppure una query di ricerca Torznab (Search query) su tutti gli indexer configurati. Vuoto = impostazione globale.", "Ein Feed ist eine RSS-URL oder eine Torznab-Suchanfrage (Search query) über die konfigurierten Indexer. Leer = globale Voreinstellung.", "Un flux est une URL RSS, ou une requête de recherche Torznab (Search query) sur les indexeurs configurés. Vide = valeur globale.", "Un feed es una URL RSS, o una consulta de búsqueda Torznab (Search query) en los indexadores configurados. Vacío = valor global.", "Kanał to URL RSS lub zapytanie wyszukiwania Torznab (Search query) we wszystkich skonfigurowanych indekserach. Puste = ustawienie globalne."},

	// Ordered rules editor.
	"Scope":               {"Ambito", "Umfang", "Portée", "Ámbito", "Zakres"},
	"Match":               {"Corrispondenza", "Übereinstimmung", "Correspondance", "Coincidencia", "Dopasowanie"},
	"Rule name":           {"Nome regola", "Regelname", "Nom de la règle", "Nombre de la regla", "Nazwa reguły"},
	"Feeds (comma)":       {"Feed (virgola)", "Feeds (Komma)", "Flux (virgule)", "Feeds (coma)", "Kanały (przecinek)"},
	"Skip matching items": {"Salta gli elementi corrispondenti", "Passende Elemente überspringen", "Ignorer les éléments correspondants", "Omitir los elementos coincidentes", "Pomiń pasujące elementy"},
	"Regex":               {"Regex", "Regex", "Regex", "Regex", "Regex"},
	"Not regex":           {"Non regex", "Nicht Regex", "Pas regex", "No regex", "Nie regex"},
	"Min size (MiB)":      {"Dimensione minima (MiB)", "Mindestgröße (MiB)", "Taille min. (MiB)", "Tamaño mín. (MiB)", "Min. rozmiar (MiB)"},
	"Max size (MiB)":      {"Dimensione massima (MiB)", "Maximale Größe (MiB)", "Taille max. (MiB)", "Tamaño máx. (MiB)", "Maks. rozmiar (MiB)"},
	"Min seeders":         {"Seed minimi", "Min. Seeder", "Seeds min.", "Seeds mín.", "Min. seedy"},
	"Max seeders":         {"Seed massimi", "Max. Seeder", "Seeds max.", "Seeds máx.", "Maks. seedy"},
	"Min peers":           {"Peer minimi", "Min. Peers", "Pairs min.", "Pares mín.", "Min. peery"},
	"Max age (days)":      {"Età massima (giorni)", "Max. Alter (Tage)", "Âge max. (jours)", "Antigüedad máx. (días)", "Maks. wiek (dni)"},
	"Require episode":     {"Richiedi episodio", "Folge erforderlich", "Exiger un épisode", "Requerir episodio", "Wymagaj odcinka"},
	"Smart episode":       {"Smart episode", "Smart Episode", "Smart episode", "Smart episode", "Smart episode"},
	"Episode filter":      {"Filtro episodi", "Episodenfilter", "Filtre d'épisodes", "Filtro de episodios", "Filtr odcinków"},
	"Enabled":             {"Attiva", "Aktiviert", "Activée", "Activada", "Włączona"},
	"Disabled":            {"Disattivata", "Deaktiviert", "Désactivée", "Desactivada", "Wyłączona"},
	"Add rule":            {"Aggiungi regola", "Regel hinzufügen", "Ajouter une règle", "Añadir regla", "Dodaj regułę"},
	"A rule filters what to download and how. With at least one rule, only items matching a rule are added: a Skip rule rejects, an Add rule accepts with its save path, category, tags and flags. Smart episode adds each episode of a series once.": {"Una regola filtra cosa scaricare e come. Con almeno una regola vengono aggiunti solo gli elementi che corrispondono a una regola: una regola Skip rifiuta, una regola Add accetta con percorso, categoria, tag e flag. Smart episode aggiunge ogni episodio di una serie una volta sola.", "Eine Regel filtert, was und wie heruntergeladen wird. Mit mindestens einer Regel werden nur Elemente hinzugefügt, die zu einer Regel passen: eine Skip-Regel lehnt ab, eine Add-Regel nimmt mit Speicherpfad, Kategorie, Tags und Flags an. Smart Episode fügt jede Folge einer Serie nur einmal hinzu.", "Une règle filtre ce qui est téléchargé et comment. Avec au moins une règle, seuls les éléments correspondant à une règle sont ajoutés : une règle Skip refuse, une règle Add accepte avec chemin, catégorie, tags et options. Smart episode ajoute chaque épisode d'une série une seule fois.", "Una regla filtra qué se descarga y cómo. Con al menos una regla, solo se añaden los elementos que coinciden con una regla: una regla Skip rechaza, una regla Add acepta con ruta, categoría, etiquetas y opciones. Smart episode añade cada episodio de una serie una sola vez.", "Reguła filtruje, co i jak pobierać. Przy co najmniej jednej regule dodawane są tylko elementy pasujące do reguły: reguła Skip odrzuca, reguła Add przyjmuje z ścieżką, kategorią, tagami i flagami. Smart episode dodaje każdy odcinek serialu tylko raz."},

	// Follow a title (search feed + scoped rule).
	"Follow a title": {"Segui un titolo", "Einen Titel verfolgen", "Suivre un titre", "Seguir un título", "Obserwuj tytuł"},
	"Series":         {"Serie", "Serie", "Série", "Serie", "Serial"},
	"Movie":          {"Film", "Film", "Film", "Película", "Film"},
	"Follow":         {"Segui", "Verfolgen", "Suivre", "Seguir", "Obserwuj"},
	"Follow creates a search feed for the title and a rule scoped to it (Smart episode for series). Once any rule exists it applies to every feed.": {"Segui crea un feed di ricerca per il titolo e una regola limitata a esso (Smart episode per le serie). Quando esiste una regola, vale per tutti i feed.", "Verfolgen erstellt einen Such-Feed für den Titel und eine darauf beschränkte Regel (Smart Episode für Serien). Sobald eine Regel existiert, gilt sie für alle Feeds.", "Suivre crée un flux de recherche pour le titre et une règle limitée à celui-ci (Smart episode pour les séries). Dès qu'une règle existe, elle s'applique à tous les flux.", "Seguir crea un feed de búsqueda para el título y una regla limitada a él (Smart episode para series). Cuando existe una regla, se aplica a todos los feeds.", "Obserwuj tworzy kanał wyszukiwania dla tytułu i ograniczoną do niego regułę (Smart episode dla seriali). Gdy istnieje reguła, obowiązuje dla wszystkich kanałów."},

	// Standalone settings page (bandwidth and scheduler).
	"Settings":             {"Impostazioni", "Einstellungen", "Paramètres", "Ajustes", "Ustawienia"},
	"Settings saved":       {"Impostazioni salvate", "Einstellungen gespeichert", "Paramètres enregistrés", "Ajustes guardados", "Ustawienia zapisane"},
	"Bandwidth scheduler":  {"Scheduler di banda", "Bandbreiten-Zeitplan", "Planificateur de bande passante", "Planificador de ancho de banda", "Harmonogram przepustowości"},
	"Enable the scheduler": {"Attiva lo scheduler", "Zeitplan aktivieren", "Activer le planificateur", "Activar el planificador", "Włącz harmonogram"},
	"Start (HH:MM)":        {"Inizio (HH:MM)", "Start (HH:MM)", "Début (HH:MM)", "Inicio (HH:MM)", "Początek (HH:MM)"},
	"End (HH:MM)":          {"Fine (HH:MM)", "Ende (HH:MM)", "Fin (HH:MM)", "Fin (HH:MM)", "Koniec (HH:MM)"},
	"Scheduled download (KiB/s, 0 = unlimited)": {"Download programmato (KiB/s, 0 = illimitato)", "Geplanter Download (KiB/s, 0 = unbegrenzt)", "Téléchargement planifié (Kio/s, 0 = illimité)", "Descarga programada (KiB/s, 0 = ilimitado)", "Zaplanowane pobieranie (KiB/s, 0 = bez limitu)"},
	"Scheduled upload (KiB/s, 0 = unlimited)":   {"Upload programmato (KiB/s, 0 = illimitato)", "Geplanter Upload (KiB/s, 0 = unbegrenzt)", "Envoi planifié (Kio/s, 0 = illimité)", "Subida programada (KiB/s, 0 = ilimitado)", "Zaplanowane wysyłanie (KiB/s, 0 = bez limitu)"},
	"Inside the window the scheduled limits apply; outside, the global limits above (0 = unlimited). The window may cross midnight.": {"Dentro la finestra valgono i limiti programmati; fuori, i limiti globali qui sopra (0 = illimitato). La finestra può attraversare la mezzanotte.", "Innerhalb des Zeitfensters gelten die geplanten Limits; außerhalb die globalen Limits oben (0 = unbegrenzt). Das Fenster kann über Mitternacht gehen.", "Dans la fenêtre, les limites planifiées s'appliquent ; en dehors, les limites globales ci-dessus (0 = illimité). La fenêtre peut passer minuit.", "Dentro de la ventana se aplican los límites programados; fuera, los límites globales de arriba (0 = ilimitado). La ventana puede cruzar la medianoche.", "W oknie obowiązują zaplanowane limity; poza nim globalne limity powyżej (0 = bez limitu). Okno może przechodzić przez północ."},

	// First-run wizard: folder picker and indexer presets.
	"Choose an existing folder on the server": {"Scegli una cartella esistente sul server", "Wähle einen vorhandenen Ordner auf dem Server", "Choisir un dossier existant sur le serveur", "Elige una carpeta existente en el servidor", "Wybierz istniejący folder na serwerze"},
	"Browse…":            {"Sfoglia…", "Durchsuchen…", "Parcourir…", "Examinar…", "Przeglądaj…"},
	"Cancel":             {"Annulla", "Abbrechen", "Annuler", "Cancelar", "Anuluj"},
	"↑ Parent":           {"↑ Cartella superiore", "↑ Übergeordnet", "↑ Dossier parent", "↑ Carpeta superior", "↑ Nadrzędny"},
	"Choose this folder": {"Scegli questa cartella", "Diesen Ordner wählen", "Choisir ce dossier", "Elegir esta carpeta", "Wybierz ten folder"},
	"Pick an indexer to enable the search; leave it empty to add one later from the RSS page. Prowlarr and Jackett fill the URL with their usual local address and port. For any other Torznab service (a MirCrew indexer, a remote manager) choose Other and type its URL.": {"Scegli un indexer per abilitare la ricerca; lascialo vuoto per aggiungerlo dopo dalla pagina RSS. Prowlarr e Jackett riempiono l'URL con il loro solito indirizzo locale e porta. Per qualsiasi altro servizio Torznab (un indexer MirCrew, un manager remoto) scegli Altro e digita il suo URL.", "Wähle einen Indexer, um die Suche zu aktivieren; lasse ihn leer, um ihn später auf der RSS-Seite hinzuzufügen. Prowlarr und Jackett füllen die URL mit ihrer üblichen lokalen Adresse und Port. Für jeden anderen Torznab-Dienst (ein MirCrew-Indexer, ein Remote-Manager) wähle Andere und gib seine URL ein.", "Choisissez un indexeur pour activer la recherche ; laissez-le vide pour l'ajouter plus tard depuis la page RSS. Prowlarr et Jackett remplissent l'URL avec leur adresse locale et port habituels. Pour tout autre service Torznab (un indexeur MirCrew, un manager distant) choisissez Autre et saisissez son URL.", "Elige un indexador para habilitar la búsqueda; déjalo vacío para añadirlo luego desde la página RSS. Prowlarr y Jackett rellenan la URL con su dirección local y puerto habituales. Para cualquier otro servicio Torznab (un indexador MirCrew, un gestor remoto) elige Otro y escribe su URL.", "Wybierz indekser, aby włączyć wyszukiwanie; pozostaw go puste, aby dodać go później ze strony RSS. Prowlarr i Jackett wypełniają adres URL swoim zwykłym lokalnym adresem i portem. Dla każdej innej usługi Torznab (indekser MirCrew, zdalny manager) wybierz Inny i wpisz jego URL."},

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
	"Language":                              {"Lingua", "Sprache", "Langue", "Idioma", "Język"},
	"Download folder":                       {"Cartella di download", "Download-Ordner", "Dossier de téléchargement", "Carpeta de descarga", "Folder pobierania"},
	"Access":                                {"Accesso", "Zugang", "Accès", "Acceso", "Dostęp"},
	"Username":                              {"Nome utente", "Benutzername", "Nom d'utilisateur", "Usuario", "Nazwa użytkownika"},
	"Password":                              {"Password", "Passwort", "Mot de passe", "Contraseña", "Hasło"},
	"Confirm password":                      {"Conferma password", "Passwort bestätigen", "Confirmer le mot de passe", "Confirmar contraseña", "Potwierdź hasło"},
	"Allow the LAN without a password":      {"Consenti la LAN senza password", "LAN ohne Passwort zulassen", "Autoriser le LAN sans mot de passe", "Permitir la LAN sin contraseña", "Zezwól na LAN bez hasła"},
	"Network":                               {"Rete", "Netzwerk", "Réseau", "Red", "Sieć"},
	"Peer port":                             {"Porta peer", "Peer-Port", "Port peer", "Puerto peer", "Port peer"},
	"Test port":                             {"Testa porta", "Port testen", "Tester le port", "Probar puerto", "Testuj port"},
	"Bandwidth":                             {"Banda", "Bandbreite", "Bande passante", "Ancho de banda", "Przepustowość"},
	"Download limit (KiB/s, 0 = unlimited)": {"Limite download (KiB/s, 0 = illimitato)", "Download-Limit (KiB/s, 0 = unbegrenzt)", "Limite de téléchargement (Kio/s, 0 = illimité)", "Límite de descarga (KiB/s, 0 = ilimitado)", "Limit pobierania (KiB/s, 0 = bez limitu)"},
	"Upload limit (KiB/s, 0 = unlimited)":   {"Limite upload (KiB/s, 0 = illimitato)", "Upload-Limit (KiB/s, 0 = unbegrenzt)", "Limite d'envoi (Kio/s, 0 = illimité)", "Límite de subida (KiB/s, 0 = ilimitado)", "Limit wysyłania (KiB/s, 0 = bez limitu)"},
	"The bandwidth limits apply immediately.":                                            {"I limiti di banda valgono subito.", "Die Bandbreitenbegrenzungen gelten sofort.", "Les limites de bande passante s'appliquent immédiatement.", "Los límites de ancho de banda se aplican de inmediato.", "Limity przepustowości działają natychmiast."},
	"The bandwidth limits must be positive numbers (0 = unlimited).":                     {"I limiti di banda devono essere numeri positivi (0 = illimitato).", "Die Bandbreitenbegrenzungen müssen positive Zahlen sein (0 = unbegrenzt).", "Les limites de bande passante doivent être des nombres positifs (0 = illimité).", "Los límites de ancho de banda deben ser números positivos (0 = ilimitado).", "Limity przepustowości muszą być liczbami dodatnimi (0 = bez limitu)."},
	"The bandwidth limits could not be applied; check that the data folder is writable.": {"Non è stato possibile applicare i limiti di banda; controlla che la cartella dati sia scrivibile.", "Die Bandbreitenbegrenzungen konnten nicht angewendet werden; prüfe, ob der Datenordner beschreibbar ist.", "Les limites de bande passante n'ont pas pu être appliquées ; vérifiez que le dossier de données soit accessible en écriture.", "No se pudieron aplicar los límites de ancho de banda; comprueba que la carpeta de datos sea escribible.", "Nie udało się zastosować limitów przepustowości; sprawdź, czy folder danych jest zapisywalny."},
	"Finish setup": {"Termina configurazione", "Einrichtung abschließen", "Terminer la configuration", "Finalizar configuración", "Zakończ konfigurację"},
	"The download folder and the peer port apply at the next start.":                                                                 {"La cartella di download e la porta peer valgono dal prossimo avvio.", "Download-Ordner und Peer-Port gelten ab dem nächsten Start.", "Le dossier de téléchargement et le port peer s'appliquent au prochain démarrage.", "La carpeta de descarga y el puerto peer se aplican en el próximo inicio.", "Folder pobierania i port peer działają od następnego uruchomienia."},
	"The test checks the port the daemon is already listening on, not the value typed above (a new port applies at the next start).": {"Il test controlla la porta su cui il demone è già in ascolto, non il valore digitato sopra (una nuova porta vale dal prossimo avvio).", "Der Test prüft den Port, auf dem der Daemon bereits lauscht, nicht den oben eingegebenen Wert (ein neuer Port gilt ab dem nächsten Start).", "Le test vérifie le port sur lequel le démon écoute déjà, pas la valeur saisie ci-dessus (un nouveau port s'applique au prochain démarrage).", "La prueba comprueba el puerto en el que el demonio ya está a la escucha, no el valor escrito arriba (un puerto nuevo se aplica en el próximo inicio).", "Test sprawdza port, na którym demon już nasłuchuje, a nie wartość wpisaną powyżej (nowy port działa od następnego uruchomienia)."},
	"The download folder must be an absolute path.":                                                                                  {"La cartella di download deve essere un percorso assoluto.", "Der Download-Ordner muss ein absoluter Pfad sein.", "Le dossier de téléchargement doit être un chemin absolu.", "La carpeta de descarga debe ser una ruta absoluta.", "Folder pobierania musi być ścieżką bezwzględną."},
	"The password and the confirmation do not match.":                                                                                {"Le password non coincidono.", "Passwort und Bestätigung stimmen nicht überein.", "Le mot de passe et la confirmation ne correspondent pas.", "La contraseña y su confirmación no coinciden.", "Hasło i potwierdzenie nie są zgodne."},
	"Setup failed.":    {"Configurazione non riuscita.", "Einrichtung fehlgeschlagen.", "Échec de la configuration.", "Error en la configuración.", "Konfiguracja nie powiodła się."},
	"Temporary folder": {"Cartella temporanea", "Temporärer Ordner", "Dossier temporaire", "Carpeta temporal", "Folder tymczasowy"},
	"Indexer":          {"Indexer", "Indexer", "Indexeur", "Indexador", "Indekser"},
	"Indexer name":     {"Nome indexer", "Indexer-Name", "Nom de l'indexeur", "Nombre del indexador", "Nazwa indeksera"},
	"Indexer URL":      {"URL indexer", "Indexer-URL", "URL de l'indexeur", "URL del indexador", "URL indeksera"},
	"Indexer API key":  {"Chiave API indexer", "Indexer-API-Schlüssel", "Clé API de l'indexeur", "Clave API del indexador", "Klucz API indeksera"},
	"The temporary folder must be an absolute path.":       {"La cartella temporanea deve essere un percorso assoluto.", "Der temporäre Ordner muss ein absoluter Pfad sein.", "Le dossier temporaire doit être un chemin absolu.", "La carpeta temporal debe ser una ruta absoluta.", "Folder tymczasowy musi być ścieżką bezwzględną."},
	"The indexer URL must start with http:// or https://.": {"L'URL dell'indexer deve iniziare con http:// o https://.", "Die Indexer-URL muss mit http:// oder https:// beginnen.", "L'URL de l'indexeur doit commencer par http:// ou https://.", "La URL del indexador debe empezar por http:// o https://.", "URL indeksera musi zaczynać się od http:// lub https://."},

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
	"DHT swarm",
	"DHT swarm (seeders / peers):",
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
	"Title",
	"Size",
	"Rule",
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
