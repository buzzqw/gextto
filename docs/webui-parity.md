# Web UI gx-core verso parità qBittorrent — piano di evoluzione

Documento di lavoro del branch `webui-qbit-parity`. Obiettivo: rendere la pagina
di `gx-torrent` (`/ui`, `internal/webui/assets/ui.tmpl` + `cmd/gx-torrent/ui*.go`)
il più possibile simile, facile e user-friendly come la Web UI di qBittorrent
(stock + VueTorrent), senza copiarla (licenze diverse: codice nostro,
comportamento ispirato).

Riferimento qBittorrent: `qBittorrent/qBittorrent/src/webui` e WebUI API v2
(`sync/maindata?rid=`, `torrents/*`, `transfer/*`, `rss/*`, `search/*`).
Il repo `userdocs/qbittorrent-nox-static` citato in origine contiene solo gli
script di build statica di `qbittorrent-nox`, non la UI: non è un riferimento UI.

Stato di partenza (già buono, da non regredire):

- Layout topbar + sidebar filtri + toolbar + tabella + drawer dettaglio +
  statusbar, come qBittorrent (`docs/gx-torrent.md`, *Web interface*).
- Sidebar con status/categorie/tag/tracker host + conteggi coerenti
  (`uiPageData`/`matchStatus`), grafo 2 minuti, sezioni collassabili.
- Tabella 20 colonne (scelta/larghezza/ordinamento persistiti), context-menu
  completo, card su mobile, `select-all` mai perso al refresh.
- Drawer dettaglio sotto la lista, resizable, tab
  General/Files/Peers/Trackers/Pieces con refresh live.
- i18n it/en/de/fr/es/pl, temi chiaro/scuro/auto, a11y verificata con axe-core.
- `/api/v2` compatibile già ampia (`api_qbit*.go`) + VueTorrent opzionale con
  fallback sempre su `/ui`.

## 1. Parità lista torrent e dettaglio (P0)

- [ ] Dialog Add stile qBittorrent: limiti DL/UP per-torrent in creazione,
      `autoTMM`, selettore cookie (`app/cookies`), content-layout, anteprima
      nome dopo magnet. File: `ui.tmpl` (`dlg-add`), `ui.go` (`uiBuildAddRequest`).
- [ ] Tab Content ad albero: raggruppa per `/`, checkbox tri-state, priorità bulk
      su cartella, rename file/folder inline, pulsante stream più visibile.
      Ri-usa `setFilePriorities` (`ui.go:handleUIFilePriority`).
- [ ] Colonne mancanti: `Availability, Time active, Last activity, Wasted`.
      Dati già in `torrentInfo`/engine, solo `uiFillOptionalColumns` + `__gxCols`.
- [ ] General arricchito: `availability, wasted, time active, last activity,
      next announce, share limits effettivi`. Nessuna nuova API engine.
- [ ] Ricerca anche su hash/tracker, bottone reset filtri evidente, `Esc` chiude
      drawer/menu, `?` apre help shortcut (`N/P/Del/Enter//`, Space, frecce).
- [ ] Toolbar: tooltip `Start/Stop (qBittorrent 5)` su Resume/Pause,
      `Force start` visibile in toolbar quando la selezione è tutta/mista forzata.

## 2. RSS, regole e logica di scarico (P0 — il punto che mancava)

Pipeline da rendere ovvia in pagina (oggi è corretta ma si capisce solo
leggendola):

```text
Feed (RSS / listing HTML / Torznab search / DHT BEP46)
  → Archivio (ogni item letto, dedup persistita)
    → Regole ordinate (prima che matcha decide; Skip scarta, Add aggiunge)
      → Wanted (titoli seguiti, conta come "had" ciò che c'è già nel client)
        → client (add con savepath/categoria/tag/flag della regola)
```

Cosa c'è già (`ui_feeds.go`, `ui_rss_edit.go`, `rss.html`, `feed*.go`):
fallback per-feed, Skip/Add ordinate, `require/include/exclude`,
`episode_filter` + `smart_episode`, regex, limiti size/seeders/età,
`ignore_days` (cooldown reale), `enabled/disabled`, preview live con verdetti,
"cosa decide" per feed (`feed-covered`), export/import JSON, test indexer e
FlareSolverr, poll singolo/all con `409` se già in corso.

Migliorie proposte (usabilità, non motore):

- [ ] Prima esecuzione guidata: con 0 feed la tab Feeds mostra 3 strade
      (`Aggiungi feed` / `Scegli un template regola` / `Importa setup`) invece
      della lista vuota.
- [ ] Template regole one-click: `Serie TV 1080p ITA`, `Film 2160p`,
      `Tutto il feed in una cartella`, `Scarta CAM/TS`. Compilano l'editor,
      l'utente salva dopo aver visto la preview.
- [ ] Editor feed: bottone `Prova` (fetch sincrono di N item + mostra titoli)
      prima di salvare; se regole coprono il feed, i campi fallback restano
      visibili ma disabilitati con il nome delle regole (oggi c'è l'avviso,
      manca il disable + il test).
- [ ] Preview regola: oltre a `Taken/Would be taken`, mostrare **perché un item
      è scartato** (primo filtro che lo ferma: `exclude cam`, `min_seeders`,
      `stagione non voluta`…), come già fa Wanted con i verdetti. Stesso
      endpoint `/ui/rss-rule-preview`, più campo `rejected_by`.
- [ ] Riga feed: oltre a `items/new/added/last_error`, mostrare `prossimo
      controllo`, `ultima durata`, `regole che lo guardano` (click → apre la
      regola), `torrent portati in archivio` (oggi solo count, senza link).
- [ ] Errori azionabili: `last_error` con bottone `Riprova` + hint specifico
      (chiave API Jackett/Prowlarr errata, Cloudflare → FlareSolverr, HTML senza
      link torrent), non solo testo.
- [ ] Cooldown visibile: `ignore_days` mostra `prossimo match possibile il …`
      per regola, così un episodio non preso non sembra un bug.
- [ ] `Download once` e `Take everything`: conferma esplicita nel dialog
      ("aggiungerà tutto ciò che il feed pubblica") + contatore `aggiunti da
      questa regola` sulla riga.
- [ ] Import setup: preview di validazione (N feed, M regole, K indexer,
      conflitti di nome) prima di `Replace`.
- [ ] Parità API RSS qBittorrent: `rss/items` con tree folder/feed,
      `refreshItem`, `markAsRead`, `matchingArticles` già mappati — verificare
      contro VueTorrent e fissare i gap in `qbit_compat_test.go`.

## 3. Facilità d'uso oltre qBittorrent (P1)

- [ ] Empty states guidati in lista torrent (`Aggiungi / Cerca indexer /
      Importa RSS`) e in Wanted/Archive.
- [ ] Dialog bulk (`Set location / Limits / Category`) con riepilogo
      `N torrent, X con valori misti`; toast con undo di 5s per `remove`
      (solo lista, non file).
- [ ] Settings cercabile: filtro sopra `/ui/settings` + badge `modified` per
      valori non-default; tab già deep-linkabili (`tab=`).
- [ ] Log stile qBittorrent: filtro livelli `normal/info/warning/critical` su
      `log/main` + tab `peer log` (dati `PeerErrors` già disponibili).
- [ ] Context-menu bottom-sheet su `<760px`; toolbar già scrollabile.

## 4. Scala ed ecosistema (P2)

- [ ] Refresh incrementale stile `sync/maindata rid`: righe cambiate soltanto
      (oggi fragment HTML intero ogni 2s; basi pronte: `snapshotViews()` + SSE
      `/ui/events`). Misurare prima/dopo in `docs/measurements.md`.
- [ ] Virtualizzazione oltre ~1000 righe (IntersectionObserver) al posto di
      `?rows=300` + `Show more`.
- [ ] Preferenze UI (colonne/filtri/tema) salvate anche lato server
      (`settings.json`) per multi-device, non solo `localStorage`.
- [ ] Chiudere i gap `/api/v2` che rompono VueTorrent/*arr/mobile
      (`transfer/*`, `app/preferences` mancanti, `search/*`); gate
      `qbit_compat_test.go` con client `internal/qbittorrent`.

## Ordine di lavoro

`P0 §1 + §2` (una release) → `P1 §3` → `P2 §4`. Ogni voce chiusa aggiorna
`docs/gx-torrent.md` (*Web interface*), `docs/evoluzione.md` (stato, mai
cronache), `docs/API.md` (se tocca rotte) e `docs/measurements.md` (se tocca
performance), più `README.md`/`README.it.md` e manuali quando cambia il
comportamento visibile.
