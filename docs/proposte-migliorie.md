# Proposte di migliorie

Aggiornato: 2026-10-07. Stato: **in attesa dei commenti del proprietario**.

Confronto con i programmi simili per uso e tradizione: Sonarr e Radarr (serie e
film), Mylar3 e Kapowarr (fumetti), Bazarr (sottotitoli), Prowlarr (indexer).
Ogni punto è stato verificato nel sorgente; i riferimenti a file e righe sono
quelli del commit `17392b4`.

Per ogni punto: cosa manca, cosa fanno gli altri, cosa si propone, quanto costa.
Lo spazio **Commento** è per la decisione (sì / no / modifiche).

---

## Priorità alta — costano poco e rendono molto

### 1. Hardlink invece della copia durante il seed
- **Oggi**: l'archiviazione copia i file (`postprocess.go:399`,
  `copyFileAtomically`) e il torrent continua il seed dalla propria copia
  (`web_torrent_events.go:1223`, "download copy removed, the library copy
  stays…"). Per tutta la durata del seed un file da 60 GB ne occupa 120.
- **Altri**: Sonarr/Radarr, opzione "Use hardlinks instead of copy".
- **Proposta**: `os.Link` quando sorgente e archivio stanno sullo stesso
  filesystem; se fallisce (`EXDEV`, filesystem diversi, o FS senza hardlink)
  si torna alla copia attuale. Opzione in configurazione, attiva di default
  solo se si vuole. Il caso NAS su mount diverso resta identico a oggi.
- **Costo**: basso. Un punto di intervento più test su stesso FS / FS diversi.
- **Commento**:

### 2. Aggiornamento mirato di Jellyfin e Plex
- **Oggi**: a ogni import si aggiorna tutta la libreria
  (`web_torrent_events.go:224` `/Library/Refresh`, `:246`
  `/library/sections/all/refresh`; stesse chiamate nei pulsanti di prova
  `web_handlers_g0.go:1644`, `web_handlers_g3.go:442`). Con una libreria
  grande su NAS è una scansione completa per ogni episodio.
- **Altri**: i *arr aggiornano solo la cartella toccata.
- **Proposta**: Jellyfin `POST /Library/Media/Updated` con il percorso; Plex
  `/library/sections/{id}/refresh?path=…` (sezione ricavata dal percorso).
  Se la chiamata mirata fallisce, si ricade sull'aggiornamento completo.
  Eventuale mappatura dei percorsi se Jellyfin/Plex vedono i file sotto un
  mount diverso.
- **Costo**: basso-medio (serve la mappatura percorsi per i casi Docker/NAS).
- **Commento**:

### 3. Soglia "upgrade until"
- **Oggi**: gli upgrade sono limitati solo da `upgrade_min_score_diff`
  (`config.go:321`, usato in `decision.go:185/215/242/562`) e dall'interruttore
  per serie `DisableUpgrades` (`decision.go:287`), che però li spegne del
  tutto. Non c'è un livello di qualità raggiunto il quale smettere di cercare.
  Un episodio già in 1080p WEB-DL può continuare a generare ricerche e
  download di miglioramento.
- **Altri**: Sonarr/Radarr "Upgrade Until" nel profilo di qualità.
- **Proposta**: soglia globale (risoluzione e/o punteggio), con override per
  serie e film. Raggiunta la soglia l'episodio è "chiuso" e non viene più
  cercato per upgrade.
- **Costo**: medio-basso. Logica in `decision.go`, campo in config e nelle
  schede serie/film, voce nel manuale.
- **Commento**:

### 4. Autenticazione facoltativa
- **Oggi**: README e `docs/API.md` dichiarano che non c'è autenticazione
  (scelta consapevole: LAN fidata). Con la UI installabile sul telefono cresce
  la tentazione di esporla fuori casa.
- **Altri**: i *arr hanno chiave API (`X-Api-Key`) e "Forms authentication".
- **Proposta**, tutto spento di default:
  - chiave API in header `X-Api-Key` (o `?apikey=`) per script e integrazioni;
  - login con cookie di sessione per la UI web, password salvata come hash;
  - eccezione configurabile per gli indirizzi LAN.
  Chi resta in LAN senza attivarla non vede differenze.
- **Costo**: medio. Middleware unico, pagina di login, TUI che passa la chiave.
- **Commento**:

---

## Priorità media — allineano gextto alle abitudini degli utenti *arr

### 5. Calendario in formato iCal
- **Oggi**: `/api/calendar` esiste (JSON, usato dalla UI), nessuna uscita
  `text/calendar`.
- **Altri**: Sonarr/Radarr espongono un feed `.ics`, molto usato.
- **Proposta**: `GET /feed/calendar.ics` con le uscite dei prossimi N giorni
  (serie e film), da aggiungere a Thunderbird, Google Calendar o al telefono.
  Se si fa il punto 4, il feed accetta la chiave API nell'URL.
- **Costo**: basso.
- **Commento**:

### 6. Fumetti: `ComicInfo.xml`
- **Oggi**: nessuna traccia di `ComicInfo.xml` nel codice; unica sorgente
  fumetti GetComics.
- **Altri**: Mylar3 e Kapowarr scrivono nel CBZ un `ComicInfo.xml` (serie,
  numero, anno, autori) preso da ComicVine o Metron. Komga, Kavita e i lettori
  su tablet lo usano per ordinare la libreria.
- **Proposta**: all'archiviazione, se il file è CBZ, aggiungere
  `ComicInfo.xml` con i dati già noti a gextto (serie, numero, anno);
  in un secondo passo, arricchimento facoltativo da ComicVine/Metron con chiave
  API dell'utente. CBR: lasciati come sono (o conversione opzionale in CBZ).
- **Costo**: medio (basso per il primo passo).
- **Commento**:

### 7. Anime con numerazione assoluta
- **Oggi**: Nyaa è tra le sorgenti, ma il parser non gestisce
  `[SubsPlease] Titolo - 1071`; "anime" compare nel codice solo come sinonimo
  di "serie" (`web_handlers_g7.go:452`). Gli alias dei titoli esistono già.
- **Altri**: Sonarr ha il tipo serie "Anime" e la mappatura assoluto →
  stagione/episodio (dati TheXEM / TVDB).
- **Proposta**: flag "anime" per serie; parser per il numero assoluto;
  conversione assoluto ↔ SxxEyy con i dati episodi già scaricati da TMDB.
- **Costo**: medio-alto. Ha senso solo se usi gextto per gli anime.
- **Commento**:

---

## Proposte minori (emerse nelle sessioni precedenti)

### 8. Ricerca dei mancanti senza gli episodi già in download
- **Proposta**: escludere dalla ricerca dei mancanti gli episodi che hanno già
  un torrent attivo, così i log e i riepiloghi non li contano come "trovati
  ma non scaricati".
- **Costo**: basso.
- **Commento**:

### 9. Badge dei download attivi nella barra (web e telefono)
- **Proposta**: numero dei download in corso sulla voce "Scarico" della barra
  di navigazione, come il contatore degli avvisi nel Log.
- **Costo**: basso.
- **Commento**:

---

## Escluse perché già presenti o tolte di proposito

Webhook firmato HMAC, event hook, alias dei titoli, blocklist, ritardi per
serie e film (delay profile), pack di stagione, sottotitoli nel punteggio,
Discover/Popular da TMDB: già presenti. Integrazione Trakt: rimossa di
proposito (`TestTraktIntegrationRoutesRemoved`).

## Ordine suggerito

**1 + 2 + 3**: interventi circoscritti, si testano bene e l'effetto si vede
subito su disco e sul NAS. Poi 5 e 8–9 (piccoli), quindi 4, 6, 7 secondo
interesse.
