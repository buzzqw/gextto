package main

// i18n_peer.go holds the strings of the peer policies (trading share, client
// filter, tracker list, country database), the per-torrent peer options and
// the DHT panel, merged into uiCatalog at start-up: {it, de, fr, es, pl}.

var uiCatalogPeer = map[string][5]string{
	// A credit the DB-IP licence asks for, kept as written in every language.
	"IP Geolocation by DB-IP": {"IP Geolocation by DB-IP", "IP Geolocation by DB-IP", "IP Geolocation by DB-IP", "IP Geolocation by DB-IP", "IP Geolocation by DB-IP"},
	"Add the trackers of a public list to new public torrents, never to private ones; the list is downloaded every day": {"Aggiunge ai nuovi torrent pubblici i tracker di una lista pubblica, mai ai privati; la lista viene scaricata ogni giorno", "Fügt neuen öffentlichen Torrents die Tracker einer öffentlichen Liste hinzu, nie privaten; die Liste wird täglich heruntergeladen", "Ajoute aux nouveaux torrents publics les trackers d'une liste publique, jamais aux privés ; la liste est téléchargée chaque jour", "Añade a los nuevos torrents públicos los trackers de una lista pública, nunca a los privados; la lista se descarga cada día", "Dodaje do nowych publicznych torrentów trackery z publicznej listy, nigdy do prywatnych; lista jest pobierana codziennie"},
	"Add trackers from a public list":                {"Aggiungi i tracker di una lista pubblica", "Tracker aus einer öffentlichen Liste hinzufügen", "Ajouter les trackers d'une liste publique", "Añadir trackers de una lista pública", "Dodawaj trackery z publicznej listy"},
	"Country database (CSV file, empty = automatic)": {"Database dei paesi (file CSV, vuoto = automatico)", "Länderdatenbank (CSV-Datei, leer = automatisch)", "Base des pays (fichier CSV, vide = automatique)", "Base de datos de países (archivo CSV, vacío = automático)", "Baza krajów (plik CSV, puste = automatycznie)"},
	"Show the country of each peer":                  {"Mostra il paese di ogni peer", "Land jedes Peers anzeigen", "Afficher le pays de chaque pair", "Mostrar el país de cada par", "Pokazuj kraj każdego peera"},
	"Show the country of each peer; the free DB-IP database is downloaded once a month and every lookup stays on this machine": {"Mostra il paese di ogni peer; il database gratuito DB-IP viene scaricato una volta al mese e ogni ricerca resta su questa macchina", "Zeigt das Land jedes Peers; die kostenlose DB-IP-Datenbank wird einmal im Monat geladen und jede Abfrage bleibt auf diesem Rechner", "Affiche le pays de chaque pair ; la base gratuite DB-IP est téléchargée une fois par mois et chaque recherche reste sur cette machine", "Muestra el país de cada par; la base gratuita DB-IP se descarga una vez al mes y cada consulta se queda en esta máquina", "Pokazuje kraj każdego peera; darmowa baza DB-IP jest pobierana raz w miesiącu, a każde wyszukiwanie zostaje na tej maszynie"},
	"Tracker list (URL, empty = the default list)": {"Lista di tracker (URL, vuoto = lista predefinita)", "Trackerliste (URL, leer = Standardliste)", "Liste de trackers (URL, vide = liste par défaut)", "Lista de trackers (URL, vacío = lista predeterminada)", "Lista trackerów (URL, puste = lista domyślna)"},
	"Your own IP-to-country file (DB-IP lite or IP2Location LITE CSV); empty uses the DB-IP database gx-torrent downloads every month": {"Un tuo file indirizzo → paese (CSV DB-IP lite o IP2Location LITE); vuoto usa il database DB-IP che gx-torrent scarica ogni mese", "Eigene Datei IP → Land (CSV von DB-IP lite oder IP2Location LITE); leer nutzt die DB-IP-Datenbank, die gx-torrent monatlich lädt", "Votre propre fichier IP → pays (CSV DB-IP lite ou IP2Location LITE) ; vide utilise la base DB-IP que gx-torrent télécharge chaque mois", "Un archivo propio IP → país (CSV de DB-IP lite o IP2Location LITE); vacío usa la base DB-IP que gx-torrent descarga cada mes", "Własny plik IP → kraj (CSV DB-IP lite lub IP2Location LITE); puste używa bazy DB-IP pobieranej przez gx-torrent co miesiąc"},
	"Countries:": {"Paesi:", "Länder:", "Pays :", "Países:", "Kraje:"},
	"A line starting with - is a peer ID prefix (for example -XL); any other line is looked for in the client name the peer announces. Empty restores the suggested list": {
		"Una riga che inizia con - è un prefisso del peer ID (per esempio -XL); ogni altra riga viene cercata nel nome del client che il peer dichiara. Vuoto ripristina la lista suggerita",
		"Eine Zeile mit - am Anfang ist ein Peer-ID-Präfix (zum Beispiel -XL); jede andere Zeile wird im Clientnamen gesucht, den der Peer meldet. Leer stellt die vorgeschlagene Liste wieder her",
		"Une ligne commençant par - est un préfixe d'ID de pair (par exemple -XL) ; toute autre ligne est cherchée dans le nom de client annoncé par le pair. Vide rétablit la liste suggérée",
		"Una línea que empieza por - es un prefijo de ID de par (por ejemplo -XL); cualquier otra línea se busca en el nombre de cliente que anuncia el par. Vacío restaura la lista sugerida",
		"Wiersz zaczynający się od - to prefiks ID peera (na przykład -XL); każdy inny wiersz jest szukany w nazwie klienta ogłaszanej przez peera. Puste przywraca sugerowaną listę"},
	"Address of a public list of trackers, one per line: it is downloaded every day and its trackers are added to the new public torrents, never to private ones": {
		"Indirizzo di una lista pubblica di tracker, uno per riga: viene scaricata ogni giorno e i suoi tracker vengono aggiunti ai nuovi torrent pubblici, mai a quelli privati",
		"Adresse einer öffentlichen Trackerliste, einer pro Zeile: Sie wird täglich heruntergeladen und ihre Tracker werden neuen öffentlichen Torrents hinzugefügt, nie privaten",
		"Adresse d'une liste publique de trackers, un par ligne : elle est téléchargée chaque jour et ses trackers sont ajoutés aux nouveaux torrents publics, jamais aux privés",
		"Dirección de una lista pública de trackers, uno por línea: se descarga cada día y sus trackers se añaden a los nuevos torrents públicos, nunca a los privados",
		"Adres publicznej listy trackerów, jeden na wiersz: pobierana codziennie, a jej trackery są dodawane do nowych publicznych torrentów, nigdy do prywatnych"},
	"Clients to refuse (one per line)":         {"Client da rifiutare (uno per riga)", "Abzulehnende Clients (einer pro Zeile)", "Clients à refuser (un par ligne)", "Clientes que rechazar (uno por línea)", "Klienci do odrzucenia (jeden na wiersz)"},
	"Country database (CSV file, empty = off)": {"Database dei paesi (file CSV, vuoto = spento)", "Länderdatenbank (CSV-Datei, leer = aus)", "Base des pays (fichier CSV, vide = désactivé)", "Base de datos de países (archivo CSV, vacío = desactivado)", "Baza krajów (plik CSV, puste = wyłączone)"},
	"Offline IP-to-country file (DB-IP lite or IP2Location LITE CSV) used to show the country of each peer; nothing is downloaded": {
		"File offline indirizzo → paese (CSV DB-IP lite o IP2Location LITE) per mostrare il paese di ogni peer; non viene scaricato nulla",
		"Offline-Datei IP → Land (CSV von DB-IP lite oder IP2Location LITE), um das Land jedes Peers zu zeigen; es wird nichts heruntergeladen",
		"Fichier hors ligne IP → pays (CSV DB-IP lite ou IP2Location LITE) pour afficher le pays de chaque pair ; rien n'est téléchargé",
		"Archivo sin conexión IP → país (CSV de DB-IP lite o IP2Location LITE) para mostrar el país de cada par; no se descarga nada",
		"Plik offline IP → kraj (CSV DB-IP lite lub IP2Location LITE) do pokazania kraju każdego peera; nic nie jest pobierane"},
	"Peer sources and clients":       {"Fonti di peer e client", "Peer-Quellen und Clients", "Sources de pairs et clients", "Fuentes de pares y clientes", "Źródła peerów i klienci"},
	"Refuse the clients on the list": {"Rifiuta i client della lista", "Clients auf der Liste ablehnen", "Refuser les clients de la liste", "Rechazar los clientes de la lista", "Odrzucaj klientów z listy"},
	"Refuse the peers whose client is on the list below: clients known to download without ever uploading back": {
		"Rifiuta i peer il cui client è nella lista qui sotto: client noti per scaricare senza restituire mai nulla",
		"Peers ablehnen, deren Client auf der Liste unten steht: Clients, die bekanntlich herunterladen, ohne je etwas zurückzugeben",
		"Refuser les pairs dont le client figure dans la liste ci-dessous : des clients connus pour télécharger sans jamais rien renvoyer",
		"Rechazar los pares cuyo cliente está en la lista de abajo: clientes conocidos por descargar sin devolver nunca nada",
		"Odrzucaj peerów, których klient jest na liście poniżej: klienci znani z pobierania bez oddawania czegokolwiek"},
	"Tracker list (URL, empty = off)":             {"Lista di tracker (URL, vuoto = spento)", "Trackerliste (URL, leer = aus)", "Liste de trackers (URL, vide = désactivé)", "Lista de trackers (URL, vacío = desactivado)", "Lista trackerów (URL, puste = wyłączone)"},
	"Upload share for trading peers (%, 0 = off)": {"Quota di upload per i peer che restituiscono (%, 0 = spento)", "Upload-Anteil für tauschende Peers (%, 0 = aus)", "Part d'envoi pour les pairs qui échangent (%, 0 = désactivé)", "Cuota de subida para los pares que intercambian (%, 0 = desactivado)", "Udział wysyłania dla wymieniających się peerów (%, 0 = wyłączone)"},
	"When the upload is limited, this share goes first to the peers that send data back to a torrent you are still downloading; the rest goes to seeding. 0 gives every peer the same share": {
		"Quando l'upload è limitato, questa quota va prima ai peer che ti rimandano dati per un torrent che stai ancora scaricando; il resto va al seed. 0 dà a tutti i peer la stessa quota",
		"Wenn der Upload begrenzt ist, geht dieser Anteil zuerst an die Peers, die dir Daten für einen noch ladenden Torrent zurückschicken; der Rest geht ans Seeden. 0 gibt jedem Peer denselben Anteil",
		"Quand l'envoi est limité, cette part va d'abord aux pairs qui vous renvoient des données pour un torrent encore en téléchargement ; le reste va au partage. 0 donne la même part à chaque pair",
		"Cuando la subida está limitada, esta cuota va primero a los pares que te devuelven datos de un torrent que aún descargas; el resto va a la siembra. 0 da a todos los pares la misma cuota",
		"Gdy wysyłanie jest ograniczone, ten udział trafia najpierw do peerów, którzy odsyłają dane torrenta, który wciąż pobierasz; reszta idzie na seedowanie. 0 daje każdemu peerowi ten sam udział"},
	"Clients refused":          {"Client rifiutati", "Abgelehnte Clients", "Clients refusés", "Clientes rechazados", "Odrzuceni klienci"},
	"DHT and peer sources":     {"DHT e fonti di peer", "DHT und Peer-Quellen", "DHT et sources de pairs", "DHT y fuentes de pares", "DHT i źródła peerów"},
	"DHT nodes (good / known)": {"Nodi DHT (buoni / noti)", "DHT-Knoten (gut / bekannt)", "Nœuds DHT (bons / connus)", "Nodos DHT (buenos / conocidos)", "Węzły DHT (dobre / znane)"},
	"Good":                     {"Buoni", "Gut", "Bons", "Buenos", "Dobre"},
	"IPv4 / IPv6 nodes":        {"Nodi IPv4 / IPv6", "IPv4-/IPv6-Knoten", "Nœuds IPv4 / IPv6", "Nodos IPv4 / IPv6", "Węzły IPv4 / IPv6"},
	"Incoming DHT queries":     {"Query DHT in entrata", "Eingehende DHT-Anfragen", "Requêtes DHT entrantes", "Consultas DHT entrantes", "Przychodzące zapytania DHT"},
	"Leading bits shared with this node's ID: higher is closer": {"Bit iniziali in comune con l'ID di questo nodo: più alto è più vicino", "Führende Bits, die mit der ID dieses Knotens übereinstimmen: höher ist näher", "Bits de tête communs avec l'ID de ce nœud : plus c'est élevé, plus c'est proche", "Bits iniciales compartidos con el ID de este nodo: más alto es más cercano", "Początkowe bity wspólne z ID tego węzła: wyżej znaczy bliżej"},
	"Lookups in progress": {"Ricerche in corso", "Laufende Suchen", "Recherches en cours", "Búsquedas en curso", "Wyszukiwania w toku"},
	"Node ID":             {"ID del nodo", "Knoten-ID", "ID du nœud", "ID del nodo", "ID węzła"},
	"Nodes":               {"Nodi", "Knoten", "Nœuds", "Nodos", "Węzły"},
	"Peers refused because their client is on the client filter list": {"Peer rifiutati perché il loro client è nella lista del filtro client", "Peers, die abgelehnt wurden, weil ihr Client auf der Liste des Clientfilters steht", "Pairs refusés parce que leur client est dans la liste du filtre de clients", "Pares rechazados porque su cliente está en la lista del filtro de clientes", "Peerzy odrzuceni, bo ich klient jest na liście filtra klientów"},
	"Public tracker list appended to new public torrents":             {"Lista pubblica di tracker aggiunta ai nuovi torrent pubblici", "Öffentliche Trackerliste, die neuen öffentlichen Torrents hinzugefügt wird", "Liste publique de trackers ajoutée aux nouveaux torrents publics", "Lista pública de trackers añadida a los nuevos torrents públicos", "Publiczna lista trackerów dodawana do nowych publicznych torrentów"},
	"Routing table by distance from this node":                        {"Tabella di routing per distanza da questo nodo", "Routingtabelle nach Abstand zu diesem Knoten", "Table de routage par distance à ce nœud", "Tabla de enrutamiento por distancia a este nodo", "Tablica routingu według odległości od tego węzła"},
	"Shared bits":  {"Bit in comune", "Gemeinsame Bits", "Bits communs", "Bits compartidos", "Wspólne bity"},
	"Tracker list": {"Lista di tracker", "Trackerliste", "Liste de trackers", "Lista de trackers", "Lista trackerów"},
	"Whether other DHT nodes reach this daemon: yes means the port is open from outside": {"Se altri nodi DHT raggiungono questo demone: sì vuol dire che la porta è aperta dall'esterno", "Ob andere DHT-Knoten diesen Daemon erreichen: ja heißt, der Port ist von außen offen", "Si d'autres nœuds DHT atteignent ce démon : oui signifie que le port est ouvert depuis l'extérieur", "Si otros nodos DHT llegan a este demonio: sí significa que el puerto está abierto desde fuera", "Czy inne węzły DHT docierają do tego demona: tak znaczy, że port jest otwarty z zewnątrz"},
	"Bandwidth share %": {"Quota di banda %", "Bandbreitenanteil %", "Part de bande passante %", "Cuota de ancho de banda %", "Udział pasma %"},
	"Country of the peer, from the offline country database":   {"Paese del peer, dal database dei paesi offline", "Land des Peers, aus der Offline-Länderdatenbank", "Pays du pair, d'après la base des pays hors ligne", "País del par, según la base de datos de países sin conexión", "Kraj peera z offline bazy krajów"},
	"Exchange peer lists with the peers of this torrent (PEX)": {"Scambia elenchi di peer con i peer di questo torrent (PEX)", "Peer-Listen mit den Peers dieses Torrents austauschen (PEX)", "Échanger des listes de pairs avec les pairs de ce torrent (PEX)", "Intercambiar listas de pares con los pares de este torrent (PEX)", "Wymieniaj listy peerów z peerami tego torrenta (PEX)"},
	"Find peers for this torrent on the local network (LSD)":   {"Cerca peer per questo torrent nella rete locale (LSD)", "Peers für diesen Torrent im lokalen Netzwerk finden (LSD)", "Trouver des pairs pour ce torrent sur le réseau local (LSD)", "Buscar pares para este torrent en la red local (LSD)", "Szukaj peerów dla tego torrenta w sieci lokalnej (LSD)"},
	"Find peers for this torrent through the DHT":              {"Cerca peer per questo torrent attraverso la DHT", "Peers für diesen Torrent über die DHT finden", "Trouver des pairs pour ce torrent via la DHT", "Buscar pares para este torrent a través de la DHT", "Szukaj peerów dla tego torrenta przez DHT"},
	"Local discovery":           {"Scoperta locale", "Lokale Erkennung", "Découverte locale", "Descubrimiento local", "Wykrywanie lokalne"},
	"PEX":                       {"PEX", "PEX", "PEX", "PEX", "PEX"},
	"Peers and bandwidth share": {"Peer e quota di banda", "Peers und Bandbreitenanteil", "Pairs et part de bande passante", "Pares y cuota de ancho de banda", "Peerzy i udział pasma"},
	"Private torrent: DHT, PEX and local discovery stay off, as the tracker requires.": {"Torrent privato: DHT, PEX e scoperta locale restano spenti, come richiede il tracker.", "Privater Torrent: DHT, PEX und lokale Erkennung bleiben aus, wie der Tracker es verlangt.", "Torrent privé : DHT, PEX et découverte locale restent désactivés, comme l'exige le tracker.", "Torrent privado: DHT, PEX y el descubrimiento local siguen desactivados, como exige el tracker.", "Prywatny torrent: DHT, PEX i wykrywanie lokalne pozostają wyłączone, jak wymaga tracker."},
	"Save the bandwidth share and the peer sources of this torrent":                    {"Salva la quota di banda e le fonti di peer di questo torrent", "Bandbreitenanteil und Peer-Quellen dieses Torrents speichern", "Enregistrer la part de bande passante et les sources de pairs de ce torrent", "Guardar la cuota de ancho de banda y las fuentes de pares de este torrent", "Zapisz udział pasma i źródła peerów tego torrenta"},
	"Share of a limited line for this torrent, in percent: 100 is normal, 200 takes twice as much as a normal torrent, 50 half (10 to 1000). It only matters while a speed limit is in place": {
		"Quota di una linea limitata per questo torrent, in percentuale: 100 è normale, 200 prende il doppio di un torrent normale, 50 la metà (da 10 a 1000). Conta solo mentre c'è un limite di velocità",
		"Anteil einer begrenzten Leitung für diesen Torrent in Prozent: 100 ist normal, 200 nimmt doppelt so viel wie ein normaler Torrent, 50 die Hälfte (10 bis 1000). Zählt nur, solange ein Geschwindigkeitslimit gilt",
		"Part d'une ligne limitée pour ce torrent, en pourcentage : 100 est normal, 200 prend deux fois plus qu'un torrent normal, 50 la moitié (de 10 à 1000). Ne compte que tant qu'une limite de vitesse s'applique",
		"Cuota de una línea limitada para este torrent, en porcentaje: 100 es normal, 200 toma el doble que un torrent normal, 50 la mitad (de 10 a 1000). Solo cuenta mientras hay un límite de velocidad",
		"Udział ograniczonego łącza dla tego torrenta w procentach: 100 to normalnie, 200 bierze dwa razy więcej niż zwykły torrent, 50 połowę (od 10 do 1000). Liczy się tylko przy limicie prędkości"},
	"Download failed": {"Download non riuscito", "Download fehlgeschlagen", "Échec du téléchargement", "Descarga fallida", "Pobieranie nieudane"},
	"trackers":        {"tracker", "Tracker", "trackers", "trackers", "trackerów"},
	"Starting…":       {"Avvio…", "Startet…", "Démarrage…", "Iniciando…", "Uruchamianie…"},
	"No: the port does not seem reachable from outside": {"No: la porta non sembra raggiungibile dall'esterno", "Nein: Der Port scheint von außen nicht erreichbar", "Non : le port ne semble pas joignable depuis l'extérieur", "No: el puerto no parece accesible desde fuera", "Nie: port nie wydaje się osiągalny z zewnątrz"},
	"Not known yet":                  {"Non ancora noto", "Noch nicht bekannt", "Pas encore connu", "Aún no se sabe", "Jeszcze nieznane"},
	"Read-only DHT":                  {"DHT in sola lettura", "Nur-Lese-DHT", "DHT en lecture seule", "DHT de solo lectura", "DHT tylko do odczytu"},
	"waiting for the first download": {"in attesa del primo download", "wartet auf den ersten Download", "en attente du premier téléchargement", "esperando la primera descarga", "czeka na pierwsze pobranie"},
	"Country database":               {"Database dei paesi", "Länderdatenbank", "Base des pays", "Base de datos de países", "Baza krajów"},
	"address ranges":                 {"intervalli di indirizzi", "Adressbereiche", "plages d'adresses", "rangos de direcciones", "zakresów adresów"},
	"Peer options saved":             {"Opzioni dei peer salvate", "Peer-Optionen gespeichert", "Options des pairs enregistrées", "Opciones de pares guardadas", "Opcje peerów zapisane"},
}

func init() {
	for key, value := range uiCatalogPeer {
		if _, ok := uiCatalog[key]; !ok {
			uiCatalog[key] = value
		}
	}
	uiClientKeys = append(uiClientKeys, "Download failed", "trackers", "Starting…",
		"No: the port does not seem reachable from outside", "Not known yet", "Read-only DHT",
		"Off", "Yes")
}
