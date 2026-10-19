package main

// uiCatalog holds every user-facing string of the gx-ed2k page. The key is a
// stable identifier; the five columns are Italian, German, French, Spanish and
// Polish. English is the key's default and is written in page.tmpl.
//
// Keeping the catalog semantic (instead of keying it by the English text, as
// Gextto does) makes it obvious when a string is added: the i18n test walks
// page.tmpl and clientKeys and fails on a key that is missing here.
var uiCatalog = map[string][5]string{
	"title":             {"gx-ed2k", "gx-ed2k", "gx-ed2k", "gx-ed2k", "gx-ed2k"},
	"subtitle":          {"Ricerca eD2k/Kad", "eD2k/Kad-Suche", "Recherche eD2k/Kad", "Búsqueda eD2k/Kad", "Wyszukiwanie eD2k/Kad"},
	"status":            {"Stato", "Status", "État", "Estado", "Stan"},
	"nodes":             {"Nodi", "Knoten", "Nœuds", "Nodos", "Węzły"},
	"udp_port":          {"Porta UDP", "UDP-Port", "Port UDP", "Puerto UDP", "Port UDP"},
	"mode":              {"Modalità", "Modus", "Mode", "Modo", "Tryb"},
	"version":           {"Versione", "Version", "Version", "Versión", "Wersja"},
	"search_only":       {"Solo ricerca", "Nur Suche", "Recherche seule", "Solo búsqueda", "Tylko wyszukiwanie"},
	"reachability":      {"Connessione", "Verbindung", "Connexion", "Conexión", "Połączenie"},
	"reach_open":        {"Aperta", "Offen", "Ouverte", "Abierta", "Otwarte"},
	"reach_limited":     {"Limitata (firewall/NAT)", "Eingeschränkt (Firewall/NAT)", "Limitée (pare-feu/NAT)", "Limitada (cortafuegos/NAT)", "Ograniczone (firewall/NAT)"},
	"reach_unknown":     {"Sconosciuta", "Unbekannt", "Inconnue", "Desconocida", "Nieznane"},
	"reach_help":        {"Indica se Kad è raggiungibile dall'esterno. Limitata significa NAT o firewall: apri la porta UDP 4672 o abilita UPnP.", "Zeigt, ob Kad von außen erreichbar ist. Eingeschränkt bedeutet NAT oder Firewall: UDP-Port 4672 öffnen oder UPnP aktivieren.", "Indique si Kad est joignable de l'extérieur. Limitée signifie NAT ou pare-feu : ouvrez le port UDP 4672 ou activez UPnP.", "Indica si Kad es accesible desde el exterior. Limitada significa NAT o cortafuegos: abre el puerto UDP 4672 o activa UPnP.", "Wskazuje, czy Kad jest osiągalny z zewnątrz. Ograniczone oznacza NAT lub firewall: otwórz port UDP 4672 albo włącz UPnP."},
	"search_only_note":  {"Il trasferimento eD2k non è ancora disponibile: qui si cercano i file, non si scaricano.", "eD2k-Übertragung ist noch nicht verfügbar: hier werden Dateien gesucht, nicht heruntergeladen.", "Le transfert eD2k n'est pas encore disponible : ici on cherche les fichiers, on ne les télécharge pas.", "La transferencia eD2k aún no está disponible: aquí se buscan archivos, no se descargan.", "Transfer eD2k nie jest jeszcze dostępny: tutaj wyszukuje się pliki, nie pobiera ich."},
	"search":            {"Cerca", "Suchen", "Rechercher", "Buscar", "Szukaj"},
	"search_query":      {"Parola chiave o nome del file", "Stichwort oder Dateiname", "Mot-clé ou nom de fichier", "Palabra clave o nombre del archivo", "Słowo kluczowe lub nazwa pliku"},
	"search_tooltip":    {"La parola chiave viene cercata su Kad e sui server eD2k.", "Das Stichwort wird in Kad und auf den eD2k-Servern gesucht.", "Le mot-clé est recherché sur Kad et sur les serveurs eD2k.", "La palabra clave se busca en Kad y en los servidores eD2k.", "Słowo kluczowe jest wyszukiwane w Kad i na serwerach eD2k."},
	"searching":         {"Ricerca in corso…", "Suche läuft…", "Recherche en cours…", "Buscando…", "Wyszukiwanie…"},
	"search_failed":     {"Ricerca non riuscita", "Suche fehlgeschlagen", "Échec de la recherche", "Error en la búsqueda", "Wyszukiwanie nie powiodło się"},
	"results":           {"Risultati", "Ergebnisse", "Résultats", "Resultados", "Wyniki"},
	"results_empty":     {"Nessun risultato", "Keine Ergebnisse", "Aucun résultat", "Sin resultados", "Brak wyników"},
	"results_count":     {"risultati", "Ergebnisse", "résultats", "resultados", "wyników"},
	"name":              {"Nome", "Name", "Nom", "Nombre", "Nazwa"},
	"size":              {"Dimensione", "Größe", "Taille", "Tamaño", "Rozmiar"},
	"sources":           {"Fonti", "Quellen", "Sources", "Fuentes", "Źródła"},
	"link":              {"Link", "Link", "Lien", "Enlace", "Link"},
	"origin":            {"Origine", "Herkunft", "Origine", "Origen", "Źródło"},
	"copy":              {"Copia", "Kopieren", "Copier", "Copiar", "Kopiuj"},
	"copied":            {"Link copiato", "Link kopiert", "Lien copié", "Enlace copiado", "Link skopiowany"},
	"copy_failed":       {"Copia non riuscita", "Kopieren fehlgeschlagen", "Échec de la copie", "Error al copiar", "Kopiowanie nie powiodło się"},
	"link_title":        {"Incolla un link ed2k://", "ed2k://-Link einfügen", "Coller un lien ed2k://", "Pega un enlace ed2k://", "Wklej link ed2k://"},
	"link_hint":         {"Controlla un link eD2k di file o di server prima di usarlo.", "Prüft einen eD2k-Datei- oder Serverlink, bevor er verwendet wird.", "Vérifie un lien eD2k de fichier ou de serveur avant de l'utiliser.", "Comprueba un enlace eD2k de archivo o servidor antes de usarlo.", "Sprawdza link eD2k do pliku lub serwera przed użyciem."},
	"validate":          {"Verifica", "Prüfen", "Vérifier", "Comprobar", "Sprawdź"},
	"link_invalid":      {"Link non valido", "Ungültiger Link", "Lien non valide", "Enlace no válido", "Nieprawidłowy link"},
	"link_file":         {"File", "Datei", "Fichier", "Archivo", "Plik"},
	"link_server":       {"Server", "Server", "Serveur", "Servidor", "Serwer"},
	"link_download_off": {"Il link è valido, ma il download eD2k non è disponibile in questa versione.", "Der Link ist gültig, aber der eD2k-Download ist in dieser Version nicht verfügbar.", "Le lien est valide, mais le téléchargement eD2k n'est pas disponible dans cette version.", "El enlace es válido, pero la descarga eD2k no está disponible en esta versión.", "Link jest prawidłowy, ale pobieranie eD2k nie jest dostępne w tej wersji."},
	"footer":            {"gx-ed2k è un client di sola ricerca: non scarica, non carica e non condivide file.", "gx-ed2k ist ein reiner Suchclient: er lädt weder herunter noch hoch und teilt keine Dateien.", "gx-ed2k est un client de recherche uniquement : il ne télécharge, ne partage et n'envoie aucun fichier.", "gx-ed2k es un cliente de solo búsqueda: no descarga, no sube ni comparte archivos.", "gx-ed2k to klient wyłącznie wyszukiwania: nie pobiera, nie wysyła i nie udostępnia plików."},
	"invalid_query":     {"Inserisci una parola chiave", "Bitte ein Stichwort eingeben", "Saisissez un mot-clé", "Introduce una palabra clave", "Wpisz słowo kluczowe"},
	"token_title":       {"Token richiesto", "Token erforderlich", "Jeton requis", "Token requerido", "Wymagany token"},
	"token_label":       {"Token API", "API-Token", "Jeton API", "Token de API", "Token API"},
	"token_send":        {"Entra", "Anmelden", "Se connecter", "Entrar", "Zaloguj"},
	"token_tooltip":     {"Il token configurato con -token o GX_ED2K_TOKEN.", "Das mit -token oder GX_ED2K_TOKEN konfigurierte Token.", "Le jeton configuré avec -token ou GX_ED2K_TOKEN.", "El token configurado con -token o GX_ED2K_TOKEN.", "Token skonfigurowany przez -token lub GX_ED2K_TOKEN."},
	"language":          {"Lingua", "Sprache", "Langue", "Idioma", "Język"},
	"theme_auto":        {"Tema: automatico (segue il sistema)", "Design: automatisch (System folgend)", "Thème : automatique (suit le système)", "Tema: automático (sigue el sistema)", "Motyw: automatyczny (zgodny z systemem)"},
	"theme_light":       {"Tema: chiaro", "Design: hell", "Thème : clair", "Tema: claro", "Motyw: jasny"},
	"theme_dark":        {"Tema: scuro", "Design: dunkel", "Thème : sombre", "Tema: oscuro", "Motyw: ciemny"},
	"theme_switch":      {"Cambia tema (automatico, chiaro, scuro)", "Design wechseln (automatisch, hell, dunkel)", "Changer de thème (automatique, clair, sombre)", "Cambiar tema (automático, claro, oscuro)", "Zmień motyw (automatyczny, jasny, ciemny)"},
	"update_check":      {"Controlla aggiornamenti", "Nach Updates suchen", "Rechercher des mises à jour", "Buscar actualizaciones", "Sprawdź aktualizacje"},
	"update_checking":   {"Controllo…", "Prüfe…", "Vérification…", "Comprobando…", "Sprawdzanie…"},
	"update_available":  {"Aggiornamento disponibile", "Update verfügbar", "Mise à jour disponible", "Actualización disponible", "Dostępna aktualizacja"},
	"update_current":    {"Aggiornato", "Aktuell", "À jour", "Actualizado", "Aktualna"},
	"update_local":      {"Build locale: aggiorna dal codice sorgente", "Lokaler Build: aus dem Quellcode aktualisieren", "Build local : mettre à jour depuis le code source", "Compilación local: actualiza desde el código", "Kompilacja lokalna: zaktualizuj z kodu źródłowego"},
	"update_error":      {"Controllo non riuscito", "Prüfung fehlgeschlagen", "Échec de la vérification", "Error al comprobar", "Sprawdzanie nie powiodło się"},
}

// clientKeys are the strings the page JavaScript needs; they are injected as
// window.__ed2kI18n. Keeping them in the same catalog gives them the same
// translation guard as the server-rendered page.
var clientKeys = []string{
	"searching", "search_failed", "results_empty", "results_count",
	"name", "size", "sources", "origin", "copy", "copied", "copy_failed",
	"link_invalid", "link_file", "link_server", "link_download_off", "invalid_query",
}

var uiLangColumn = map[string]int{"it": 0, "de": 1, "fr": 2, "es": 3, "pl": 4}

// uiEnglish is the English dictionary. It is not one of the catalog columns
// because English is the template's source language; keeping it explicit lets
// the renderer always produce a complete dictionary.
var uiEnglish = map[string]string{
	"title":             "gx-ed2k",
	"subtitle":          "eD2k/Kad search",
	"status":            "Status",
	"nodes":             "Nodes",
	"udp_port":          "UDP port",
	"mode":              "Mode",
	"version":           "Version",
	"search_only":       "Search only",
	"reachability":      "Connection",
	"reach_open":        "Open",
	"reach_limited":     "Limited (firewall/NAT)",
	"reach_unknown":     "Unknown",
	"reach_help":        "Shows whether Kad is reachable from outside. Limited means NAT or firewall: open UDP port 4672 or enable UPnP.",
	"search_only_note":  "eD2k transfers are not available yet: this page searches for files, it does not download them.",
	"search":            "Search",
	"search_query":      "Keyword or file name",
	"search_tooltip":    "The keyword is looked up on Kad and on the eD2k servers.",
	"searching":         "Searching…",
	"search_failed":     "Search failed",
	"results":           "Results",
	"results_empty":     "No results",
	"results_count":     "results",
	"name":              "Name",
	"size":              "Size",
	"sources":           "Sources",
	"link":              "Link",
	"origin":            "Origin",
	"copy":              "Copy",
	"copied":            "Link copied",
	"copy_failed":       "Copy failed",
	"link_title":        "Paste an ed2k:// link",
	"link_hint":         "Check an eD2k file or server link before using it.",
	"validate":          "Check",
	"link_invalid":      "Invalid link",
	"link_file":         "File",
	"link_server":       "Server",
	"link_download_off": "The link is valid, but the eD2k download is not available in this version.",
	"footer":            "gx-ed2k is a search-only client: it does not download, upload or share files.",
	"invalid_query":     "Enter a keyword",
	"token_title":       "Token required",
	"token_label":       "API token",
	"token_send":        "Sign in",
	"token_tooltip":     "The token configured with -token or GX_ED2K_TOKEN.",
	"language":          "Language",
	"theme_auto":        "Theme: automatic (follows the system)",
	"theme_light":       "Theme: light",
	"theme_dark":        "Theme: dark",
	"theme_switch":      "Switch theme (automatic, light, dark)",
	"update_check":      "Check for updates",
	"update_checking":   "Checking…",
	"update_available":  "Update available",
	"update_current":    "Up to date",
	"update_local":      "Local build: update from the source code",
	"update_error":      "Update check failed",
}

// uiStrings returns the page dictionary for lang. English (or an unknown
// language) returns the English dictionary.
func uiStrings(lang string) map[string]string {
	column, ok := uiLangColumn[lang]
	if !ok {
		return uiEnglish
	}
	out := make(map[string]string, len(uiCatalog))
	for key, values := range uiCatalog {
		out[key] = values[column]
	}
	return out
}

func validLang(lang string) bool {
	_, ok := uiLangColumn[lang]
	return ok
}
