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

- [x] Dialog Add: limiti DL/UP per-torrent in creazione (`addRequestFromForm`,
      applicati via `torrentMeta`), `autoTMM` per-add (standalone).
      Resta: selettore cookie, content-layout, anteprima nome dopo magnet.
- [x] Colonne mancanti: `Availability, Time active, Last activity, Wasted`
      (dati già in `torrentInfo`/engine: `LastActive` esposto da
      `runtimeInfo.lastActive`; scelta/ordinamento persistiti come le altre).
- [x] General arricchito: availability, time active, last activity, seeding
      time, next announce (minimo sui tracker).
- [x] Filtri e tastiera: ricerca anche su hash/tracker, `Clear filters`,
      empty-state guidato (Add / Search indexers / RSS), dialog shortcut (`?`),
      `Esc` chiude pannello/menu/sbmore.
- [x] Toolbar: `Force start` anche in toolbar, tooltip Start/Stop stile
      qBittorrent 5 su Resume/Pause.
- [x] Tab Content ad albero: `Group by folder` nel Files tab (JS, senza nuove
      API): header per cartella con checkbox tri-state, priorità per cartella
      (ri-usa `/ui/file-priority` multi-`index`), collapse persistente in
      memoria; pulsante Stream con etichetta visibile. Resta: rename cartella.
- [ ] Resta Add dialog: selettore cookie, content-layout, anteprima nome dopo
      magnet.
      → fatto: anteprima magnet (nome dn + hash) live nel dialog, hint cookie
      con link a `Settings → Cookies` (`#cookies`); content-layout rinviato
      (richiede supporto engine: oggi il save path è per-torrent/categoria).

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

- [x] Prima esecuzione guidata: con 0 feed la tab Feeds mostra 3 strade
      (`New feed` / `New rule` da preset / `Import a setup`) invece della sola
      riga vuota.
- [x] Template regole one-click: `TV series`, `Movie`, `Take all`. Compilano
      l'editor, la preview live mostra subito cosa prenderebbero.
- [x] Editor feed: bottone `Test` (fetch via `/ui/feed-items?url=`, senza
      salvare, con titoli di esempio). I campi fallback restano editabili e
      dimmati quando le regole coprono il feed (disabilitarli cancellerebbe i
      valori al salvataggio): avviso con nomi regole già presente.
- [x] Preview regola: aggregato `rejected` con **perché gli altri restano
      fuori** (`rss.Criteria.RejectReason`, chiavi stabili mappate a etichette
      tradotte lato server).
- [x] Cooldown visibile: `ignore_days` mostra chip `Next match <data>` sulla
      riga regola mentre il cooldown è attivo (`RuleLastMatch` persistito).
- [ ] `Download once` e `Take everything`: conferma esplicita nel dialog
      ("aggiungerà tutto ciò che il feed pubblica") + contatore `aggiunti da
      questa regola` sulla riga.
- [ ] Import setup: preview di validazione (N feed, M regole, K indexer,
      conflitti di nome) prima di `Replace`.
- [ ] Parità API RSS qBittorrent: `rss/items` con tree folder/feed,
      `refreshItem`, `markAsRead`, `matchingArticles` già mappati — verificare
      contro VueTorrent e fissare i gap in `qbit_compat_test.go`.

## 3. Facilità d'uso oltre qBittorrent (P1)

- [x] Empty states guidati in lista torrent (`Add` / `Search indexers` /
      `RSS & indexers`) e `Clear filters` quando i filtri nascondono tutto.
      Wanted (`Add title`) e Archive (guida a archivio vuoto) fatti.
- [x] Settings con filtro di ricerca per sezione (JS, il form posta comunque
      tutti i campi).
- [x] Dialog bulk: riepilogo `N torrent` + placeholder `mixed` nei limiti quando
      la selezione ha valori diversi. Resta: undo di 5s per `remove`.
- [ ] Settings cercabile: filtro sopra `/ui/settings` + badge `modified` per
      valori non-default; tab già deep-linkabili (`tab=`).
- [x] Log stile qBittorrent: filtro livelli DEBUG/INFO/WARN/ERROR
      (client-side, classi già esistenti).
- [x] Context-menu bottom-sheet su `<760px` (`theme.css`, vale per tutte le
      pagine gx-core); toolbar già scrollabile.

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
