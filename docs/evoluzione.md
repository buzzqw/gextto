# Migliorie ed evoluzione di Gextto e gx-torrent

Documento unico di lavoro del **2026-10-10**: raccoglie backlog, migliorie
implementate, piani di miglioramento, misure, revisioni tecniche e direzione
strategica di Gextto e del motore gx-torrent.

Sostituisce i seguenti file, spostati in `docs/archive/` come copie locali non
tracciate in git (vedi Appendice A per la mappa):

- `proposte-migliorie.md`, `da-fare.md`, `lavoro-sessione.md`,
  `scelte-di-progetto.md`, `accessibility-analysis.md`,
  `holepunch-test-harness.md`
- `gx-torrent-migliorie.md`, `gx-torrent-ottimizzazioni-prestazionali-v2.md`,
  `gx-torrent-misure-seeding.md`
- `revisione-1.md` (già locale, già in `.gitignore`)
- `archive/revisione-2.md`, `archive/UI_V2.md`, `archive/gextto-terra.md`,
  `archive/pro-terra.md` (già archiviati)

Restano documenti a sé, perché descrivono lo stato corrente e non un piano:
`docs/gx-torrent.md` (riferimento tecnico del motore), `docs/rain-allineamento.md`
(procedura di rebase del fork), manuali, API, architettura, sicurezza,
migrazione, guide avanzate, TUI.

## Indice

1. [Backlog attivo](#1-backlog-attivo)
2. [Migliorie Gextto implementate](#2-migliorie-gextto-implementate)
3. [Direzione strategica](#3-direzione-strategica)
4. [gx-torrent: lacune e piano di miglioramento](#4-gx-torrent-lacune-e-piano-di-miglioramento)
5. [gx-torrent: ottimizzazioni prestazionali](#5-gx-torrent-ottimizzazioni-prestazionali)
6. [gx-torrent: misure di choking e seeding](#6-gx-torrent-misure-di-choking-e-seeding)
7. [Holepunching: harness di test](#7-holepunching-harness-di-test)
8. [Revisione tecnica 1: client, i18n, job, concorrenza](#8-revisione-tecnica-1-client-i18n-job-concorrenza)
9. [Revisione tecnica 2: bug e debito al commit 97142c1](#9-revisione-tecnica-2-bug-e-debito-al-commit-97142c1)
10. [Rapporti Terra e Pro Terra](#10-rapporti-terra-e-pro-terra)
11. [UI SSR + HTMX: report di migrazione](#11-ui-ssr--htmx-report-di-migrazione)
12. [Handover tra sessioni di lavoro](#12-handover-tra-sessioni-di-lavoro)
13. [Accessibilità](#13-accessibilita)
14. [Appendice A: file consolidati](#appendice-a-file-consolidati)

---

## 1. Backlog attivo

Ex `docs/da-fare.md`. Ripristinato l'8-10-2026: era finito per errore in
`docs/archive/` pur essendo un backlog **attivo**.

### 1.1 Osservabilità: metriche nel tempo (aperta)

ID di correlazione e runbook sono fatti (vedi §1.3). Resta la parte metriche:
andamenti salvati nel database e mostrati in *Salute*, senza Prometheus:

- ~~durata delle ricerche e sorgenti che non rispondono, ciclo per ciclo~~ —
  fatto (2026-10-10): `CycleStats` salva durata ed esito per sorgente nel ciclo
  (`cycle_history`), con il pannello **Ricerche** in *Salute* (`uiweb.go`,
  `RecentCycleStats`, `ScrapeAll`);
- ~~avviso oltre una soglia~~ — fatto (2026-10-10): `cycle_monitor.go` avvisa
  (riga WARN + notifica) se una sorgente ha fallito in 3 delle ultime ricerche,
  se l'ultima ricerca supera 3 ore o se non c'è un ciclo senza errori da 3
  intervalli (minimo 2 ore), e segnala il ritorno alla normalità;
- **restano: import falliti al giorno e titoli in coda con il tempo di attesa**;
- la salute degli indexer del manager è già in Sources (`indexer_health.go`).

### 1.2 Opzionale (solo se si riprende il lavoro sulle prestazioni)

- Script di benchmark RAM/throughput dei motori, riproducibile in `scripts/`
  (workload noto, web seed locale con Range, staging locale vs NFS). Non serve
  di per sé: è utile solo per prendere decisioni future con numeri.

### 1.3 Chiusi / decisioni

- **i18n in sei lingue** — fatto (2026-10-10). Pacchetto completo it/en/de/fr/es/pl:
  UI web (chrome, etichette, tooltip, hint, messaggi server-side e stringhe
  generate dal client JS via `window.__v2i18n` + `t()` in `v2-core.js`); errori
  API/HTMX mostrati all'utente localizzati con `uiText`; notifiche
  (Telegram/email/webhook) con `messages.Pick` su catalogo it→{de,fr,es,pl}
  (`internal/messages/catalog.go`); TUI a 6 lingue (`internal/tui/i18n.go`);
  pagina web di gx-torrent tradotta con lo stesso approccio (catalogo +
  `translateHTML`, lingua passata da Gextto con `-lang` e `?lang=xx`).
  Guardia: `TestUIIsTranslatable` fallisce se una stringa UI di template o
  view-model Go (o un letterale di `uiText`) non è chiave dei cataloghi, con
  allowlist esplicito; `TestClientI18nKeysTranslated` copre le chiavi client.
  Restano **in inglese per scelta** i log del daemon; le stringhe tecniche degli
  errori API non mostrate all'utente possono rimanere nella lingua originale.
- **ID di correlazione e storia dei download** — fatto (2026-10-10). Le righe di
  log su un torrent portano `acq: <id>` (ID corto derivato dall'hash, che resta
  nascosto) e finiscono in `acquisition_events`: scheda *Storia* nel dettaglio
  torrent, 📜 per puntata e pannello nella pagina della serie, API
  `/api/acquisitions` (`acquisition.go`).
- **Runbook "un episodio non è arrivato"** — fatto (2026-10-10), in
  *Risoluzione problemi* di `MANUAL.it.md`/`MANUAL.en.md`, costruito sulla storia.
- **Wizard di primo avvio (onboarding)** — fatto e giudicato concluso
  (2026-10-10). Procedura guidata in `uiweb_v2_setup.go` (commit `c8162d8`), poi
  TVDB come alternativa a TMDB e scelta della lingua al primo passo.
- **Pulsante «Predefinito» su ogni impostazione** — fatto. Ogni campo non segreto
  con un default registrato (anche vuoto) mostra ↺ accanto a «Salva»: riempie il
  campo col default e l'utente conferma salvando; il valore del default è mostrato
  accanto se compatto (`Resettable`/`DefaultInline` in `uiweb_v2.go`, `HasDefault`
  in `uiweb_pages.go`). Esclusi: campi disabilitati per il motore attivo, gestiti
  dall'auto-tuning, segreti o senza default.
- **Log di upgrade unificati** — fatto (commit `988d60a`). Una sostituzione
  produce una sola riga: `♻️ FBI S03E01 updated (1.4 GB): … · previous version
  «…» moved to the trash`. L'aggiunta normale resta `📁 … added to the library …`.
- **Pannello "Operazioni in background"** — risolto (commit `65cb9bf`). Resta e
  funziona (polling, annullamento), ma è **nascosto quando non c'è alcun job**.
- **Write-back cache nel fork di rain** — chiuso: la misura mostra
  `write_cache=0` (storage e download di pari passo), quindi non ci sarebbe
  guadagno. Da riaprire solo con uno storage realmente più lento della rete.
- **Campi specialistici nel form di aggiunta** — chiuso: nessuna richiesta
  concreta, sarebbe fuffa preventiva.
- **Benchmark UI come test di performance in CI** — chiuso: il rendering SSR
  costa ~1–3 ms/pagina, non è un problema.
- **Cap cache su storage lento (NFS)** — superato: la cache si autoregola
  (`cmd/gx-torrent/cache.go`, tuner `cacheSizes`).
- **Dashboard: diagnostica "Ultimi trovati nelle sorgenti"** — già fatto
  (route `GET /dashboard/feed`, `uiweb_v2.go`).
- **Verifica live del flusso RAM disk con gx-torrent** — fatto (2026-10-07).

---

## 2. Migliorie Gextto implementate

Ex `docs/proposte-migliorie.md` (stato al 2026-10-07: **tutte approvate e
implementate**). Confronto con Sonarr/Radarr (serie/film), Mylar3/Kapowarr
(fumetti), Bazarr (sottotitoli), Prowlarr (indexer).

| # | Proposta | Decisione | Commit |
|---|---|---|---|
| 1 | Hardlink invece della copia durante il seed | sì, con garanzia anti-perdita | `20b28eb` |
| 2 | Aggiornamento mirato di Jellyfin e Plex | sì | `1bfaad4` |
| 3 | Soglia "upgrade until" | sì, oltre alla differenza minima | `95f6f97` |
| 4 | Autenticazione facoltativa | sì, spenta di default, LAN esente | `7dae752` |
| 5 | Calendario iCal | sì, con date italiane dichiarate | `e78bcc2`, `a919fcd` |
| 6 | `ComicInfo.xml` nei fumetti | sì | `6a7f739` |
| 7 | Anime con numerazione assoluta | sì | `f4a9f8f` |
| 8 | Mancanti: escludere gli episodi in download | sì | `e815180` |
| 9 | Badge dei download attivi nella barra | sì | `893bc94`, `42db4b2` |
| 9.1 | Traduzioni dei nuovi elementi in tutte le lingue | richiesta del proprietario | `698efbf` |
| 10 | Aggiornamento della documentazione | richiesta del proprietario | commit `docs:` finale |

### 2.1 Hardlink invece della copia durante il seed

- **Prima**: un file in seed veniva copiato in libreria (spazio occupato due volte).
- **Ora** (`hardlink.go`, `hardlink_seeding`, default attivo): episodi in cartella,
  season pack ed episodi in modalità copia entrano in libreria come hardlink. Il
  link nasce con nome nascosto e viene rinominato: il file del torrent non viene
  mai toccato. Se il link non è possibile (filesystem diversi, RAM disk,
  condivisioni senza hardlink) si copia come prima, con una riga di log per
  coppia di dischi.
- **Sicurezza**: i due nomi sono alla pari (non come i symlink). Cancellare il
  nome nel download lascia intatto l'archivio (si interrompe solo il seed); i
  dati spariscono solo cancellando entrambi. Verificato con test.
- I film non sono coinvolti: vengono già spostati col torrent (nessuna copia).

### 2.2 Aggiornamento mirato di Jellyfin e Plex

- **Ora** (`media_refresh.go`): dopo un import Jellyfin riceve
  `/Library/Media/Updated` con la cartella e Plex
  `/library/sections/{id}/refresh?path=` per la sezione che la contiene. Le
  richieste durante un aggiornamento vengono unite. Cartella sconosciuta, nessuna
  sezione Plex o rifiuto del server → aggiornamento completo come prima.
- Mappature `jellyfin_path_mappings` / `plex_path_mappings` per server in Docker.
  I pulsanti manuali restano completi.

### 2.3 Soglia "upgrade until"

- **Ora** (`upgrade_until.go`, `upgrade_until_score`, 0 = spenta): con punteggio
  ≥ soglia la copia non viene più sostituita; restano accettati REPACK e PROPER.
  Vale per ciclo, mancanti, season pack e film. «Perché non questa?» indica gli
  scarti per soglia. Riferimenti: 1080p WEB-DL H.264 ≈ 1280, 1080p WEB-DL
  H.265 DD+ ≈ 1480, 2160p ≈ 2480.
- La differenza minima (`upgrade_min_score_diff`) resta (*di quanto* deve
  migliorare vs *fino a dove*). Il blocco per titolo («Niente upgrade») è anche
  nella scheda serie web.

### 2.4 Autenticazione facoltativa

- **Ora** (`auth.go`, *Configurazione → Accesso*): spenta di default. Accesa: chi
  non è in LAN fa login (password bcrypt, cookie firmato 30 giorni, nuova
  password chiude le sessioni) o usa la chiave API (`X-Api-Key` o `?apikey=`; la
  TUI la prende da `GEXTTO_API_KEY`).
- LAN esente di default. Dietro reverse proxy contano gli indirizzi inoltrati
  (`X-Forwarded-For`, `X-Real-IP`, `Forwarded`).
- 5 password sbagliate = blocco IP per 1 minuto. Senza password né chiave il
  controllo resta aperto (avviso nel log). `GEXTTO_AUTH_DISABLE=1` per rientrare.

### 2.5 Calendario iCal

- **Ora** (`calendar_ics.go`, `GET /feed/calendar.ics`, link in Integrazioni).
  Per il pubblico italiano (serie doppiate mesi/anni dopo) distingue:
  - 📥 **arrivati in libreria** negli ultimi 30 giorni (giorno dell'arrivo);
  - 📺 **messa in onda originale** TMDB della stagione in corso (✓ se in libreria);
  - 🎬 **uscita italiana dei film** TMDB (digitale → home video → cinema), con
    ripiego dichiarato sulla data originale.
- Il riquadro "Prossime uscite" della Dashboard dichiara che è la messa in onda
  originale.

### 2.6 Fumetti: `ComicInfo.xml`

- **Ora** (`comicinfo.go`): nei CBZ scaricati direttamente (HTTP, Mega) viene
  aggiunto `ComicInfo.xml` con serie/numero/anno dal titolo GetComics. Non
  toccati: file che lo hanno già, CBR, fumetti via torrent (in seed). Un errore
  non compromette mai il download.

### 2.7 Anime con numerazione assoluta

- **Ora** (`anime.go`, parser, casella *Anime (numerazione assoluta)* in scheda
  serie web/TUI): `[SubsPlease] One Piece - 1071 (1080p)`,
  `One Piece Ep 1071 SUB ITA`, `One.Piece.1071.SUB.ITA` → stagione/episodio via
  stagioni TMDB (senza TMDB: stagione 1). Anche `S01E1071` letto come assoluto. La
  ricerca mancanti cerca anche il numero assoluto, online e in archivio.
- Le serie non marcate anime non sono mai interessate.

### 2.8 Mancanti senza gli episodi già in download

- Gli episodi con torrent attivo (anche aggiunto a mano, da cartella osservata o
  telefono) e le stagioni con season pack in corso non sono più mancanti, né nel
  ciclo né nella scheda serie. Un download in errore li fa tornare mancanti.

### 2.9 Badge dei download in corso

- La voce "Scarico" mostra i download in corso (torrent non finiti + HTTP attivi,
  non i seed), ogni 5 s; sul telefono è un contatore sull'icona.

### 2.10 Escluse perché già presenti o tolte di proposito

Webhook HMAC, event hook, alias titoli, blocklist, ritardi (delay profile), pack
di stagione, sottotitoli nel punteggio, Discover/Popular TMDB: già presenti.
Trakt: rimossa di proposito (`TestTraktIntegrationRoutesRemoved`).

### 2.10 Escluse perché già presenti o tolte di proposito

Webhook HMAC, event hook, alias titoli, blocklist, ritardi (delay profile), pack
di stagione, sottotitoli nel punteggio, Discover/Popular TMDB: già presenti.
Trakt: rimossa di proposito (`TestTraktIntegrationRoutesRemoved`).

---

## 3. Direzione strategica

Ex `docs/scelte-di-progetto.md` (estratto l'8-10-2026 dal piano di adozione
`SONARR_RADARR_REPLACEMENT_PLAN.md`, rimosso: qui le parti durevoli).

### 3.1 Posizionamento

- Gextto è per **installazioni nuove**, non drop-in di Sonarr/Radarr: niente
  import dei loro database, niente migrazione in place, niente rollback.
- Integrazione **verso** i gestori di indexer (Jackett/Prowlarr), non
  duplicandoli dentro Gextto.
- **Non-goal**: compatibilità API `*arr` / `/api/v3`; quality profile e custom
  format; autenticazione multi-utente (l'esposizione remota passa dal reverse
  proxy in `docs/SECURITY.md`). Le differenze si pubblicano, non si nascondono.
- Target: chi ha un server + media server (Jellyfin/Plex) e vuole "trova,
  scarica, rinomina, archivia" con un solo demone; molto probabilmente ha già
  Jackett o Prowlarr.

### 3.2 Gestori di indexer (Jackett/Prowlarr)

Gextto tratta il manager come **un'unica sorgente aggregata** e non importa la
sua lista di indexer: il manager è l'unica fonte di verità per indexer,
categorie, credenziali, FlareSolverr e rate limit.

Misure (stessa libreria, stesso host):

| Scenario | Latenza | Risultati |
|---|---|---|
| Aggregato con un indexer rotto abilitato | 60 s (timeout) | 0 |
| Indexer sano chiamato direttamente | 0,25–2,4 s | 40–100 |
| Aggregato dopo aver disabilitato il rotto nel manager | 2–4 s | 156–240 |

Conseguenze (tutte implementate):

- un prototipo che duplicava la lista indexer è stato costruito, misurato e
  **revertito**: nessun guadagno di stabilità, solo deriva di configurazione;
- le **categorie** sono state rimosse: un aggregato serve serie e film, una
  categoria statica per indexer è inutile o dannosa (query TV con categoria
  "film" → zero risultati);
- il tipo di manager si può **dichiarare** (`manager` in `config.go` e UI)
  invece di dedurlo da URL/porta/nome (resta il fallback);
- la salute del manager (Prowlarr `indexerstatus`) è in Sources: un indexer
  rotto/disabilitato è visibile invece di dare zero risultati in silenzio
  (`indexer_health.go`);
- un **timeout per sorgente** limita una singola richiesta sotto il budget della
  ricerca;
- l'API di ricerca di Prowlarr **ignora** stagione/episodio ed external-id
  (misurato): non vengono inoltrati; anche `type` non ha effetto osservato;
- la lista indexer di **Jackett non è raggiungibile** con la chiave Torznab (la
  sua API risponde con redirect al login): irrilevante, serve solo l'aggregato.

### 3.3 FlareSolverr

- **Sessioni persistenti** per dominio, con TTL breve e distruzione, invece di un
  browser per richiesta; se non supportate → modalità stateless.
- **Mai per i gestori di indexer**: Jackett/Prowlarr gestiscono Cloudflare da
  soli; un 403 o errore di rete lì viene riportato com'è invece di sprecare
  20–30 s mascherando la causa.
- Si rispetta `solution.status`: pagina "risolta ma in errore" non interpretata
  come contenuto.

### 3.4 Politica di selezione

Gextto **non** implementa profili di qualità, custom format o motore regex/score
come `*arr`. Sceglie invece default globali (risoluzione, sorgente, codec,
audio, HDR, lingua, vincoli di dimensione) + **override ed esclusioni
per-titolo** + **spiegabilità** (l'API di spiegazione dice perché un release è
accettato/rifiutato/preferito). Motivo: i profili sono la parte più complessa —
e più configurata male — del modello `*arr`.

### 3.5 Confronto con Sonarr/Radarr (fotografia onesta, 2026-10)

**Dove Sonarr/Radarr sono avanti:**

- **Profili di qualità e custom format**: score per risoluzione/sorgente/tipo/
  release group (anche regex), delay profile, profili diversi per serie e film.
  Gextto ha politica unica + "upgrade until" + override per-titolo: più facile,
  meno potente per regole fini. *Fattibilità:* dare **nomi ai profili sul modello
  Gextto** è un passo medio (la globale diventa "Default", ogni combinazione di
  override un profilo generato, migrazione automatica dai dati per-titolo in
  `series.quality/language/subtitle/exclude` e `movies_config.*`). Riprodurre la
  **semantica Sonarr** (qualità ordinate + cutoff + custom format) è più costoso;
  i custom format (motore regex + UI) vanno valutati a parte.
- **Maturità di import e rinomina**: multi-episodio, specials, anime assoluti,
  daily, import manuale con matching e sostituzione. *Fattibilità:* si studia il
  comportamento e si reimplementa; Sonarr è GPLv3, Gextto EUPL 1.2: **non si
  copia codice né file di test**, ci si costruisce un corpus proprio da nomi
  reali.
- **Ecosistema e API**: `/api/v3` consumata da Overseerr/Jellyseerr; Gextto non
  la espone. *Fattibilità:* la compatibilità completa non è realistica; un
  **sottoinsieme** per Jellyseerr sì (`system/status`, `qualityprofile`,
  `languageprofile`, `rootfolder`, `tag`, `series/lookup` + `POST series`,
  `movie/lookup` + `POST movie`, `queue`) — ma dopo profili nominati, root folder
  e tag, e provato con un Jellyseerr reale.
- **Notifiche**: *fatto* — il webhook parla anche **Discord, Slack, ntfy, Gotify
  e Pushover** via selettore di formato (`notify_webhook_format`); restano i
  provider più esoterici.
- **Import di librerie esistenti**: parziale in Gextto, non prioritario.
- **Community, wiki, Docker, cadenza**: questione di tempo, non di architettura.

**Differenze che sono scelte, non lacune:** indexer via gestore esterno
(separazione più pulita e sicura); **niente usenet** (torrent, HTTP, web seed;
nel contesto italiano l'usenet è marginale).

**Dove Gextto è avanti (o pari):** tutto in uno (serie, film **e fumetti**) in un
solo demone; motore torrent integrato (gx-torrent Go puro, libtorrent oppure
qBittorrent-nox); spiegabilità; ricerca anche da motori web; leggerezza (un
binario Go + SQLite vs .NET); semplicità (health, backup, manutenzione integrati).

**Verdetto:** Sonarr/Radarr restano migliori su selezione (profili/custom
format), casi limite di import ed ecosistema/API. Gextto è migliore su scope
unico, motore integrato, spiegabilità, footprint e semplicità. Posizione
corretta: **alternativa per installazioni nuove**, senza rivendicare parità.

---

## 4. gx-torrent: lacune e piano di miglioramento

Ex `docs/gx-torrent-migliorie.md` (stato al 2026-10-08: lavoro completato,
ricontrollo finale fatto).

### 4.1 Contesto

`gx-torrent` = motore BitTorrent **in puro Go**, alternativo a libtorrent:

- **rain** (`third_party/rain`, fork v2.4.2): trasferimento;
- **demone + adapter** (`cmd/gx-torrent`, `gxtorrent_engine.go`): REST, pagina
  web, coda autogestita, ponte con Gextto (`TorrentEngine`).

Gextto fornisce già per tutti i motori: queue/slot, stalled/retry, seed policy,
RSS, ricerca, categorie/tag, scheduler di banda, post-processing, spostamenti e
RAM disk, filtro IP, proxy. Queste **non sono lacune del motore**.

Stato per capacità: `capabilityLevels` (`torrent_engine.go`), unica fonte di
verità usata dall'UI.

### 4.2 Lacune reali

**vs libtorrent / qBittorrent** (stato post-lavoro):

| Area | Cosa mancava | Esito |
|---|---|---|
| Limiti per-torrent | velocità download/upload | fatto (`SetLimits`) |
| Limiti per-torrent | connessioni e upload slot | fatto (`SetMaxConnections`/`SetMaxUploads`) |
| Modalità | upload/share mode | saltato (si usa seed infinito ratio 0); super-seeding fatto |
| Priorità | per-pezzo e diagnostica pezzi | fatto (API `pieces`, solo incluso/escluso per file) |
| Tracker | rimozione | fatto (`set-trackers`) |
| Web seed | add/remove a caldo | fatto (azione `webseeds`) |
| Sequenziale | toggle a runtime | fatto (`PiecePicker.SetOrder`) |
| Protocollo | IPv6 | no |
| Protocollo | BitTorrent v2-only | wishlist |
| Qualità | algoritmi di seeding/choking | misurato, vedi §6 |

**vs anacrolix/torrent**: mancano streaming con readahead (fatto, vedi sotto),
WebTorrent/WebRTC (wishlist), storage alternativi. gx-torrent ha invece daemon
sorvegliato, UI web, REST, UPnP/NAT-PMP, porta unica, proxy SOCKS/HTTP, filtro
IP multi-formato, killswitch VPN, preallocazione, cache adattiva.

**Non lacune**: RSS, search plugin, categorie/tag, scheduler, ratio,
post-processing, sequenziale come modalità (mancava solo il toggle a caldo),
cifratura, IP filter, proxy, DHT/PEX/LSD, UPnP/NAT-PMP, fast resume, selezione
file.

### 4.3 Decisioni

| # | Miglioria | Decisione |
|---|---|---|
| 1 | Rimozione/sostituzione tracker | **Fatto** — `Torrent.SetTrackers` nel fork + `set-trackers` |
| 2 | Upload/share mode | **Saltato** — seed infinito (ratio 0) |
| 3 | Toggle sequential/first-last a caldo | **Fatto** — `PiecePicker.SetOrder` + `SetSequential` |
| 4 | Web seed add/remove via API | **Fatto** — `AddWebseeds`/`RemoveWebseeds` + azione `webseeds` |
| 5 | Diagnostica pezzi | **Fatto** — API `pieces`, scheda Pezzi in Gextto e nel demone |
| 6 | Limiti velocità per-torrent | **Fatto** — `Limiter.SetParent`/`SetLimitKiB` + `SetSpeedLimits`, riapplicati al riavvio |
| 7 | Streaming HTTP Range + priorità pezzi | **Fatto** — `SetStreamWindow`, `/ui/stream`, redirect Gextto, pulsante ▶ |
| 8 | Limiti connessioni/upload per-torrent | **Fatto** — `SetMaxConnections`/`SetMaxUploads` + `Unchoker.SetNumUnchoked` |
| 9 | IPv6 | **No** |
| 10 | BitTorrent v2-only | **Wishlist** |
| 11 | Super-seeding (BEP 16) | **Fatto** — `SetSuperSeeding` + azione `super-seeding`; capacità `full` |
| 12 | Holepunching (BEP 55) | **Fatto** — `ut_holepunch` nel fork, relè peer, dial uTP, avvio dopo dial fallito; WebTorrent resta wishlist |
| 13 | Qualità seeding/choking | **Misurato (locale)** — choking corretto ed economico; super-seeding senza guadagno su sciami piccoli, opt-in; scaling su sciame reale, vedi §6 |

Semantica limiti: **-1 = eredita il globale, 0 = illimitato** (come libtorrent).

### 4.4 Wishlist residua

- **BitTorrent v2-only**: richiede supporto v2 in rain (grande).
- **WebTorrent/WebRTC**: nicchia, molto lavoro.
- **Qualità seeding/choking**: prima tornata fatta (vedi §6); per lo scaling
  serve campagna su sciame reale (gx-torrent vs libtorrent/qBittorrent, stessa
  politica, mediana su più run).

### 4.5 Criterio di verifica (applicato)

- Per punto: `go test ./cmd/gx-torrent/`, `go test -run GxEngine .`, fork dove
  tocca (`make test-rain`).
- Con effetto visibile: `docs/gx-torrent.md`, `MANUAL.*`, `README`.
- Ogni miglioria visibile in **entrambe le UI** (Gextto `uiweb/v2` + pagina del
  demone `cmd/gx-torrent/ui.go`), mai solo via API.
- Confronti con libtorrent/qBittorrent solo su misura.

### 4.6 Ricontrollo finale (fatto)

- `capabilityLevels`: gx-torrent `full` per `limits`, `trackers`, `sequential`,
  `piece_diagnostics`, `web_seeds`, `super_seeding`, `session_stats`; resta
  `partial` `first_last` (solo all'aggiunta lato Gextto).
- Note/testi aggiornati: `v2DetailCapsFor`, "Limiti noti", `gx-torrent.md`,
  `API.md`, `MANUAL.*`, `README*`.
- Ogni feature visibile in entrambe le UI (tracker, web seed, limiti,
  diagnostica pezzi, streaming, super-seeding).
- Test percorso felice + casi limite (lista vuota, duplicati, not-found, 416,
  completato/fermo); `make test` e `make test-rain` verdi (+ pacchetto
  `unchoker` in `make test-rain`).
- Fork marcato `gextto fork`, inventario in `third_party/rain/GEXTTO.md`;
  `go vet`, `gofmt` verdi; semantica `-1`/`0`/`>0` coerente.
- Nota residua: attesa pezzi nello streaming a polling (200 ms, timeout 2 min),
  senza test end-to-end su sciame reale; coperti dati presenti e mappatura
  byte→pezzi.

---

## 5. gx-torrent: ottimizzazioni prestazionali

Ex `docs/gx-torrent-ottimizzazioni-prestazionali-v2.md`. Sostituisce la v1
(rimossa l'08-10-2026): diagnosi in gran parte corrette come lettura del codice
ma con tre difetti — non distingueva upstream rain v2.4.2 dal fork, presentava
benchmark non riproducibili (nessuna `Benchmark*` esisteva nel repo), ordinava
per priorità interventi irrilevanti o rischiosi. Qui **solo ciò che vale la
pena fare**, con come/rischi/verifica; una voce = un task/commit.

### 5.1 Stato di avanzamento

| # | Voce | Stato | Commit |
|---|---|---|---|
| 1 | LSD senza `d.mu` e senza `t.Stats()` | **fatto** | `d2a4060` |
| 2 | Streaming: `PiecesDone` mirato | **fatto** | `ff2d4bf` |
| 4 | MSE `StreamWriter` in-place | **fatto** | `5af2ee0` |
| 5 | `servedRequests` a finestra limitata | **fatto** | `a8b3e77` |
| 6 | Piece cache: TTL lazy (sharding condizionale, non fatto) | **fatto** | `60b090e` |
| 7 | `Bitfield.Count` con `math/bits` | **fatto** | `8caf6ad` |
| 3 | Rimozione `O_SYNC` + invariante di durabilità | **non da fare** | — |
| 8 | Opzionali (`findLocked` O(1), ETag/304) | **non da fare** | — |

Ordine per **valore atteso = (impatto × certezza) / (sforzo × rischio)**.
Regole per voce (da `AGENTS.md`): modifiche a `third_party/rain` marcate
`// gextto fork` + riga in `GEXTTO.md`; un commit per voce; test mirati
(`./cmd/gx-torrent/`, `GxEngine`, `make test-rain`, `make test`); invarianti
degli script; mai `bin/`/`gx-torrent.build_number`/`data/`; con comportamento
visibile aggiornare README/MANUAL/`gx-torrent.md`.

| # | Intervento | Impatto | Sforzo | Rischio | Codice |
|---|---|---|---|---|---|
| 1 | LSD senza `d.mu` / `t.Stats()` | Alto (latenza demone) | S | S | fork (cmd) |
| 2 | Streaming: check pezzi mirato | Medio (GC/coupling) | S | S | fork (cmd+rain) |
| 3 | Rimozione `O_SYNC` + durabilità | **Molto alto** (disco) | M/L | M | fork (rain) |
| 4 | MSE in-place | Medio (upload) | S | S | fork (rain) |
| 5 | `servedRequests` a finestra | Medio (memoria) | S/M | M | fork (rain) |
| 6 | Piece cache TTL lazy (+ sharding) | Medio (seed) | M | M | fork (rain) |
| 7 | `Bitfield.Count` con `math/bits` | Basso | S | S | fork (rain) |
| 8 | Opzionali: mappa in `findLocked`, ETag/304 | Basso | S | S | fork (cmd) |

`S` ≈ mezza giornata, `M` ≈ 1–2 giorni, `L` ≈ oltre.

### 5.2 Voce 1 — LSD senza `d.mu`, senza interrogare i run loop (fatta)

`dueHashes` (`cmd/gx-torrent/lsd.go`) prendeva `d.mu` e, tenendolo, chiamava
`t.Stats()` su **ogni** torrent — query bloccante senza timeout al run loop. Con
storage lento (mount di rete) il mutex globale congelava tutte le REST.
Riscritta leggendo dallo snapshot lock-free (`snapshotViews`); stati
annunciabili: `downloading`, `downloading_metadata`, `seeding`,
`checking_files`, `moving`. Differenza: un torrent in `error` non viene più
annunciato (corretto). Verifica: test che `paused`/`stalled`/privati non
compaiono, `downloading` pubblico sì, e che `dueHashes` non prende `d.mu`.

### 5.3 Voce 2 — Streaming: check pezzi mirato (fatta)

`piecesPresent` (`stream.go`) chiamava `t.PieceStates()`: allocava un vettore
lungo quanto il torrent e scansionava tutti i pezzi sul run loop ogni 200 ms per
controllarne 1–2. Aggiunto `Torrent.PiecesDone(begin, end)` mirato
(`session_torrent.go`), che scansiona solo l'intervallo (con `len==0 → false`,
come prima). `PieceStates` resta per la mappa pezzi di API/UI. Follow-up non
fatto: notifica a evento invece del polling (non necessaria finché non alloca).

### 5.4 Voce 3 — Rimozione `O_SYNC` + invariante di durabilità (non da fare)

`filestorage.go` apre ogni file con `O_RDWR|O_SYNC` (upstream): ogni `write(2)`
va su storage stabile — su HDD/rete il throughput è limitato dalle IOPS. **Ma
non è un quick win**: rain affida a `O_SYNC` tutta la durabilità, e `storage.File`
non ha `Sync()`; il bitfield è persistito solo a completamento/stop/verifica e
ogni `ResumeWriteInterval` (2 min), non "a ogni pezzo". Il progetto (conservato
come riferimento) richiedeva: estendere l'interfaccia con `Sync()`, togliere
`O_SYNC`, helper `syncPieces` testabile, ordine critico in `writeBitfield`
(**prima snapshot bitfield, poi sync, poi scrittura** — mai il contrario) e
ristrutturazione in due fasi di `session_stats.updateStats` (niente transazione
Bolt aperta durante gli fsync). Invariante: *un bit persistito si riferisce a
dati già fsyncati* (su crash si riscaricano ≤2 min, mai corruzione). Misure
vedi §5.10: su questo host il guadagno stimato era ~1,2x–1,8x su NFS (non il
"+300–800%" della v1).

### 5.5 Voce 4 — MSE `StreamWriter` in-place (fatta)

`cipher.StreamWriter` faceva `make` per ogni blocco cifrato (decine di MiB/s di
spazzatura in upload). Sostituito con writer in-place (`XORKeyStream(p, p)`) in
`internal/mse/mse.go`: i chiamanti non riusano il buffer dopo la scrittura.
Verifica: `mse_test.go` con `AllocsPerRun == 0` + round-trip; pacchetto aggiunto
a `make test-rain`.

### 5.6 Voce 5 — `servedRequests` a finestra limitata (fatta)

Mappa che cresceva indefinitamente coi blocchi serviti (memoria ∝ GB caricati su
seed longevi) e rifiutava per sempre anche re-request legittimi. Sostituita con
finestra mappa+FIFO da N≈1024 (`peerwriter.go`): dedup anti-abuso immediata,
memoria limitata. Test: dedup entro finestra, eviction oltre, nessuna crescita.

### 5.7 Voce 6 — Piece cache: TTL lazy (fatta; sharding solo se serve)

La cache serve **solo** l'upload (`cachedpiece` ← `RequestMessage`), non
download né streaming locale (`stream.go` usa `os.Open`/`ReadAt`). Costi reali:
un `time.Timer` per pezzo con `Reset` a ogni accesso + lock esclusivo anche su
hit. Fatto: TTL lazy (`expireAt` + ticker di sweep ~30 s, `Close()` lo ferma).
Non fatto: sharding in 16 partizioni — solo se il profiling mostra contesa sotto
forte seeding (refactor ampio di file upstream, non "a naso"). Verifica: sweep,
rinnovo su hit, eviction invariata; `internal/piececache` in `make test-rain`.

### 5.8 Voce 7 — `Bitfield.Count` con `math/bits` (fatta)

Somma byte-a-byte su tabella 256 byte → `bits.OnesCount64/8` a blocchi di 8,
tabella rimossa. Micro-ottimizzo non su percorso critico (`Count` usato in
`torrent_stats.go` e `All()`), ma banale e sicuro. Property test vs tabella.

### 5.9 Voce 8 — Opzionali (non da fare)

- **`findLocked` O(1)**: `ListTorrents()` + scan per ogni lookup per hash.
  A 4 torrent la risposta `/api/v1/torrents` è 3.822 byte in 0,5–0,7 ms:
  guadagno nullo (<0,05% CPU). Misurabile solo a centinaia/migliaia di torrent
  (~1 MB di lista): rimandato a quel carico.
- **ETag/304 su `/api/v1/torrents`**: `handleList` serve già lo snapshot
  precompilato, costo = solo `json.Marshal`; durante attività i tassi cambiano a
  ogni tick e il 304 non scatterebbe. Solo con misura che lo giustifichi.

### 5.10 Esclusi, con motivazione (misurati)

- **Rarity buckets / refactor picker**: costo reale a 27.250 pezzi ~0,11
  ms/pick in regime normale (pdqsort adattivo), <1% di un core a decine di
  pick/s. Risparmio ~0,1 ms/pick per refactor pesante (`HandleHave`/`Cancel`/
  `removeHaving`, endgame): non conviene.
- **Cursore watermark in `pickSequential`/`pickFileEdge`**: ~0,10 ms/pick e
  ~0,04 ms per il full scan di `pickFileEdge`: non merita il rischio.
- **Bitmask in `PieceDownloader`**: mappe solo per download concorrenti (≈ n.
  peer); bitmask 64 bit non copre pezzi da 2–4 MiB: trascurabile.
- **`clear(b.Data)` in `bufferpool`**: ~0,8 µs per 16 KiB → <0,1% di un core a
  1000 blocchi/s; pool condiviso, non dimostrato che tutti i percorsi riempiano
  il buffer: solo con audit completo.
- **`FADV_RANDOM`**: la giustificazione v1 (penalizza lo streaming) non vale —
  `stream.go` legge fuori dallo storage di rain. Al più `FADV_SEQUENTIAL` in
  verifica.
- **`saveLocked` fuori da `d.mu`**: non sul polling (solo mutazioni e
  tick-quando-dirty ~1/min); `state.json` 2,5 KB, ~0,1–0,3 ms. E la proposta v1
  azzerava `dirty` prima della scrittura perdendolo sull'errore.
- **`/proc/meminfo` sotto `d.mu`**: decine di µs, irrilevante.
- **`HandleDisconnect` O(N)**: poche disconnessioni/s, decine di µs: irrilevante.

### 5.11 Correzioni sostanziali alla v1 (riferimento)

- 10 voci su ~16 erano **codice upstream invariato**, non lavoro gextto.
- `O_SYNC` non è un quick win; `storage.File` non ha `Sync()`.
- "Bitfield salvato a ogni pezzo" è falso (solo completamento/stop/verifica/
  `ResumeWriteInterval`).
- I moltiplicatori "15.500x/65.000x/600.000x" venivano da microbenchmark
  eterogenei, non end-to-end; nessun `Benchmark*` esisteva nel repo.
- Piece cache sul percorso **upload**, non download/streaming.
- `handleList` serve già uno snapshot: non "clona i torrent".

### 5.12 Misure prima dell'implementazione (Intel N97)

**Voce 3 — `O_SYNC`**: download dir su **NFS4** (1 GbE), alternativa ext4/NVMe.
N MiB in blocchi di B, `O_SYNC` per write vs bufferizzate + fsync finale:

NFS: 16 KiB 35,1 → 81,3 MiB/s (**2,3x**); 64 KiB 60,2 → 89,6 (1,5x);
256 KiB 67,4 → 108,7 (1,6x); 1 MiB 86,8 → 102,4 (1,2x).
NVMe: 16 KiB 7,3 → 862,7 (~118x); 256 KiB 150,5 → 975,5 (6,5x);
1 MiB 371,8 → 962,5 (2,6x).
**Previsione** (pezzi 32 KiB–2 MiB su NFS): **~1,2x–1,8x**, tetto link
~100–110 MiB/s. Guadagno maggiore con pezzi piccoli e disco veloce. Seed in
lettura non toccato.

**Picker** (`picker_bench_test.go`, 27.250 pezzi): `pickRarest` 110 µs steady /
1,11 ms peggiore (rimescolato ogni pick, shuffle incluso); `pickSequential`
~100 µs; `pickFileEdge` full scan 38 µs. La v1 misurava il caso rimescolato
spacciandolo per tipico.

**`saveLocked`**: 2,5 KB, ~0,1–0,3 ms per operazione utente esplicita.
Conclusione: fuori dalle voci fatte (1, 2, 4, 5, 6, 7) **nulla altro della v1
merita** a questo carico.

---

## 6. gx-torrent: misure di choking e seeding

Ex `docs/gx-torrent-misure-seeding.md`. Supporto alla voce §4.3 #13. Regola di
fondo: **misurare prima**, poi decidere se toccare il core di rain.

### 6.1 Vincolo: non forzare l'automazione

Le misure riflettono il sistema **come gira davvero** (coda/slot, seed policy,
scheduler accesi): harness deterministico sul puro algoritmo (nessuna sessione);
sciame locale col demone e automanagement attivi; campagna reale con Gextto
(`AdjustQueue`, seed policy, limiti). Mai "a code spente": sarebbe laboratorio,
non produzione.

### 6.2 Metriche

Choking (slot di upload): chi viene sbloccato e per quanto, fairness a pari
velocità, rotazione optimistic unchoke, comportamento in download (premia i
downloader veloci) e in seed (premia gli uploader veloci).
Seeding (end-to-end): byte caricati dal seed vs corpus (`Nx corpus`),
throughput up/down, tempo di completamento, ratio e tempo a 1:1 in campagna reale.

### 6.3 Banco 1 — harness deterministico dell'unchoker

`third_party/rain/internal/unchoker/sim_test.go` (in `make test-rain`): pilota
l'`Unchoker` reale con peer sintetici e fissa le proprietà (download veloci in
download, upload veloci in seed, `FastUnchoke` immediato, optimistic che non
ruba slot regolari e ruota, budget rispettato, fairness) + `BenchmarkTickUnchokeSeeding`.
Uso: `go test …/internal/unchoker -v` e `-bench TickUnchoke`. Rete di sicurezza
per varianti future dell'algoritmo.

### 6.4 Banco 2 — sciame locale (opt-in)

`cmd/gx-torrent/measure_test.go`, fuori da `make test`, con `GX_MEASURE=1`:

```
make measure-seeding                        # seeder + 3 leecher
GX_MEASURE_SUPERSEED=1 make measure-seeding # confronto super-seeding
GX_MEASURE_MESH=1 make measure-seeding      # leecher collegati tra loro
GX_MEASURE_LEECHERS=2 make measure-seeding  # sciame di 2 peer
```

Seeder + N leecher su loopback con IP `127.0.0.x` distinti (rain rifiuta due
connessioni dallo stesso IP). Default: ogni leecher solo verso il seeder — si
misura l'**upload del seed** (ciò che il super-seeding cambia). Mesh
sperimentale su un host (connessioni multiple su loopback instabili: peer che
non si connettono/non completano): solo fotografia indicativa, pochi peer,
ripetere. Output: `uploaded=Nx corpus`, medie, tempi. Limiti: numeri assoluti
dipendenti dalla macchina (non confrontabili tra run diversi); un peer può
impiegare due tick di retry (10 s) nel super-seeding: l'harness riporta invece
di fallire su un peer lento.

### 6.5 Banco 3 — campagna su sciame reale

Per gx-torrent vs **libtorrent/qBittorrent**: stesso sciame e ruolo, stesse
politiche (slot, banda, n. peer), stesse metriche (byte seed `Nx corpus`,
throughput, tempo a 1:1, seed generati), mediana su più run, automazione attiva.
Unico banco che mostra il beneficio del super-seeding (~1x invece di Nx) e la
qualità del choking con scambio reale.

### 6.6 Risultati (prima tornata, locale; Intel N97, corpus 2 MiB)

Choking deterministico: tutte le proprietà passano; tick con 50 peer ~1,1 µs,
896 B/op, 1 alloc — trascurabile.
Seeding senza mesh (seed → 3 leecher, 5 ripetizioni): normale ~41–42 ms, seed
**3,00x**; super-seeding ~51–52 ms (**~24% più lento**), seed **3,00x**.
Mesh a 2 peer (indicativo): entrambi **1,00x** (i leecher si scambiano i pezzi).
Mesh a 3+ peer su un host non converge.

Lettura onesta: il choking di rain è **corretto e trascurabile come costo**;
su sciami piccoli/veloci il super-seeding **non riduce** l'upload (lo scambio
basta già) e costa throughput/variabilità. Il vantaggio BEP 16 è per l'*initial
seeding* di torrent grandi con molti peer lenti, non riproducibile su un host.
Conseguenza: super-seeding **opt-in**, non default; scaling via campagna reale.
Hardening applicato: un peer interessato ma in stallo (velocità zero) riceve il
pezzo successivo al tick invece di attendere il solo `not-interested`.

### 6.7 Cosa decidere in base ai risultati

- Choking peggiore di libtorrent su sciame reale → si interviene su
  `internal/unchoker` col banco deterministico come rete e lo sciame come verifica.
- Super-seeding più lento in locale ma più leggero su sciame reale → resta
  opzione di *initial seeding* (BEP 16), non default.
- Nessuna differenza misurabile → non si tocca il core (costo rebase non
  giustificato).

---

## 7. Holepunching: harness di test

Ex `docs/holepunch-test-harness.md`. Supporto a
`scripts/holepunch-netns-test.sh` (comportamento motore in `docs/gx-torrent.md`).

**Scopo**: verificare end-to-end che gx-torrent apra un buco (BEP 55,
`ut_holepunch`) tra due client dietro NAT/firewall usando un peer pubblico come
**relay**. Root-only, tutto in network namespace dedicati: la rete host non
viene toccata.

**Topologia**: `gx-lab` (pubblico: tracker `10.0.0.1:13800`, seed, relay
`10.1.0.1:51410`) + due firewall stateful `gx-r1`/`gx-r2` (lasciano uscire,
DROPPano nuove connessioni in entrata, accettano `ESTABLISHED,RELATED`,
rifiutano TCP verso le porte peer così il buco si apre su uTP) + client
`gx-a 10.10.0.2` (leecher) e `gx-b 10.20.0.2` (seed).

**Sequenza**: tracker minimo Python (interval 30 s) → seed con dati → B entra
leecher, scarica, completa restando connesso al relay (leecher interessato) →
solo allora entra A: non raggiunge B, chiede introduzione al relay, `connect` a
entrambi, dial incrociato su uTP. Successo: peer con origine **`holepunch`**
(chi diala) o `incoming` (l'altro lato) in
`/api/v1/torrents/<hash>/peers`.

**Scelte**: firewall stateful invece di NAT (conntrack non emula un NAT
cone port-translating: il dial diretto crea una entry locale che confligge col
SNAT; lo stateful riproduce lo stesso osservabile e rende il test
deterministico); seed e relay su IP diversi (rain deduplica i peer per IP);
`-outgoing-interface` = IP di ascolto (altrimenti announce con IP sbagliato);
relay leecher interessato (un torrent completato non diala e scarta i non
interessati); tracker con interval lungo (un re-annuncio attribuirebbe la
connessione al dial tracker invece che all'holepunch); successo rilevato su
entrambi i lati (corsa del dial).

**Bug storici dello script**: unico NAT condiviso (buco impossibile);
`/peers` senza `source` (campo poi aggiunto all'API); ruoli sbagliati (relay
seed + B seed); `progress` 0–100 letto come 0–1; `grep` su log binari (serve
`-a`).

**Uso**: `make gx-torrent`, poi `sudo scripts/holepunch-netns-test.sh`
(`--keep` per debug, `--size 16` per torrent più grande). Esempio riuscito in
`scripts/risultato.txt`. Debug: `HOLEPUNCH_DEADLINE` (default 90 s),
`HOLEPUNCH_B_DEADLINE` (60 s), `HOLEPUNCH_DIAG=1` (dump conntrack). Su
fallimento stampa le righe `holepunch` dei log (rendezvous/connect).

**Limiti**: emulazione con firewall stateful, non NAT cone reale; esito
`holepunch`/`incoming` secondo chi vince la corsa; dedup peer per IP di rain
(in uno swarm reale i peer sono su IP diversi).

---

## 8. Revisione tecnica 1: client, i18n, job, concorrenza

Ex `docs/revisione-1.md` (già locale/non tracciato; citato da commenti in
`jobs.go`, `web.go`, `internal_translations.yml`). Anomalie e debito emersi da
revisione del codice, con correzioni incrementali senza cambiare il
comportamento stabilizzato. Perimetro: client JS della UI, i18n, HTML dinamico,
attività asincrone/job, concorrenza e test. Fuori perimetro: autenticazione e
sicurezza LAN (LAN trusted per decisione).

Al momento della revisione: `go test ./...` e `go vet` verdi; client UI unico
file IIFE `uiweb/static/gextto-ui.js` (~253 KiB, 5.194 righe); CSS in
`gextto-ui.css`; template server-rendered; polling + partial HTML + dialoghi
dinamici. Nessun errore bloccante: criticità di manutenzione, osservabilità,
coerenza e robustezza futura. Modello i18n effettivo: chiavi = **stringhe
italiane di sorgente**, cataloghi `internal_translations*.yml`
(italiano → lingua) uniti in SQLite all'avvio; client con `translateUINode()` +
`MutationObserver` e fallback inglese.

Priorità: **P1** prima di aggiungere funzioni (regressioni/diagnosi difficili);
**P2** debito compatibile con l'esercizio; **P3** qualità/manutenzione.

### 8.1 Client JavaScript monolitico (P2)

Un solo IIFE con bootstrap, tema, i18n, polling/API, dashboard, torrent, serie/
film/archivio/fumetti, configurazione, manutenzione, log, dialoghi/tabelle/
accessibilità. Su disco è grande ma compresso è piccolo: il problema è la
manutenzione (stato globale condiviso, collisioni future, init mescolato a
codice comune, logica render/rete/eventi nello stesso blocco, test solo su
output HTTP). Proposta: moduli (`ui-core.js` con contratto stabile
`api`/`pollRequest`/`esc`/`storage`/`notify`/dialoghi, poi dialoghi, shell, log/
manutenzione, torrent, libreria/discovery/fumetti), migrazione per estrazioni
senza big bang (prima ES nativi, poi eventuale bundle in build). Accettazione:
nessuna regressione, no-JS con contenuto essenziale, nessun accesso a variabili
private altrui, peso compresso invariato, focus/tastiera/dialoghi invariati.

### 8.2 Internazionalizzazione (P1 controllo cataloghi, P2 migrazione)

Rischi del modello a stringa: una variazione ortografica rompe la traduzione,
stringhe dinamiche con regex restano parzialmente in italiano, cataloghi
server/client divergenti, `MutationObserver` costoso su pagine dinamiche,
fallback non controllabili. Target: chiavi stabili (`dashboard.title`),
cataloghi versionati, fallback deterministico (attiva → base → chiave),
interpolazione parametrica, helper server/client, niente traduzione globale del
DOM. Migrazione graduale: controllo cataloghi → chiavi per nuove funzioni →
shell/dialoghi/toast → pagine una alla volta → `translateUINode` solo legacy →
rimozione observer. Test: ogni chiave nel catalogo base, nessuna traduzione
vuota, snapshot multilingua, stessa lingua per testo/title/aria/placeholder,
plurali e HTML nei valori.

### 8.3 HTML dinamico e sicurezza del rendering (P1 nuovo codice, P2 storico)

`innerHTML` ovunque (tabelle, toast, dialoghi, TMDB/release/fumetti, partial
server): `esc()` usato in molti punti ma ogni interpolazione deve ricordarselo
nel contesto giusto (testo vs attributo vs `data-*` JSON, errori da provider).
Nuovo codice: `textContent`, `setAttribute` validati, `createElement`,
`DocumentFragment`, `innerHTML` solo per markup statico, mai valori da
API/DB/provider/filesystem in stringhe HTML. Storico: `esc()` centralizzato,
`htmlTrusted()` esplicito per frammenti statici, tabelle esposte migrate per
prime (release, provider, blocklist, ricerche), test con payload ostili
(`<script>`, virgolette, `&`, newline, URL speciali). Accettazione: valori
ostili resi come testo, funzioni trusted riconoscibili dal nome, nessun calo di
sort/focus/paginazione/accessibilità.

### 8.4 Job manager (P1 operazioni distruttive/lunghe, P2 altre)

Modello non uniforme: 202 prima dell'esito, stato globale singolo, doppi avvii,
errori tardivi solo nei log, cancellazione non uniforme, retry/idempotenza non
rappresentati, log non correlati, test con sleep/polling. Proposto: `Job`
(`queued/running/succeeded/failed/canceled`, progress, message/error,
**result** strutturato, timestamp, cancel via context) + manager (concorrenza
limitata, dedup per chiave, retention, log con `job_id`, shutdown ordinato) +
endpoint (`GET /api/jobs`, `GET /api/jobs/{id}`, `POST …/cancel`; niente `POST
/api/jobs` generico: i job nascono dalle operazioni reali validate). Prime da
migrare: rinomina massiva/repair, scansioni archivi/librerie, backfill
MediaInfo, backup/restore, manutenzione DB/scoring, diagnostica sorgenti,
pulizie. Accettazione: niente doppi lavori, stato osservabile, errori tardivi
in stato+log, cancellazione senza orfani, refresh che conserva lo stato, niente
goroutine illimitate.

### 8.5 Concorrenza e race (P1 race in CI, P2 bonifica)

`go vet` non è un race detector. Aree: mappe retry/post-seed, cache config,
stato torrent tra polling/callback/shutdown, progressi manutenzione, registri
job/notifiche, contatori/metriche, chiusura sessione libtorrent, stato/DB
durante backup/restore. Strategia: un solo meccanismo per stato (mutex, atomic,
snapshot immutabili, canali, context, mappe sempre col lock documentato). CI:
`go test -race ./...` (se pesante: veloci+vet per push, completi per PR, race
in coda dedicata/giornaliera, motori reali in job separati). Test con timeout,
`Cleanup`, tmpdir e porte casuali, mai dipendenti dal servizio in esecuzione.

### 8.6 Piano raccomandato (Fase 0–4)

0 Baseline (test comportamento, metriche, convenzione log/ID, cataloghi +
race non bloccante). 1 Rischio basso (`ui-core.js`/dialoghi/shell, helper DOM,
test XSS, chiavi nuove stringhe). 2 Coerenza UI (cataloghi, observer solo
legacy, moduli tabelle/log/manutenzione, test multilingua). 3 Job (manager,
rinomina/scansioni, cancel/dedup/retention, `job_id` in UI/log/notifiche).
4 Concorrenza (race ripetuti, mappe/snapshot, shutdown/restart, race bloccanti).
Atteso: client modificabile senza regressioni incrociate, traduzioni verificabili
pre-rilascio, confini dati/markup, operazioni osservabili/cancellabili/correlate,
fiducia sulla concorrenza, consumi invariati. **Non riscrivere tutto**: piccole
estrazioni, test verdi a ogni fase, refactoring separato dalle funzioni.

### 8.7 Stato di attuazione

Fase 0: race non bloccante (`make test-race`, job CI `continue-on-error`) e
controllo cataloghi (`i18n_catalog_test.go`: YAML valido, niente duplicati/
vuoti, insiemi coerenti) fatti; metriche caricamento e convenzione `job_id` da
fare. Fase 1: test XSS end-to-end (`security.spec.ts`: injection, attribute/
tag breakout, `javascript:`) e `esc()` uniforme (anche `'`) fatti; estrazione
moduli, helper DOM e chiavi nuove stringhe rimandati/ricepiti come convenzione.
Fase 2: catalogo inglese completato (1.345 traduzioni mancanti, ora 3.130 chiavi
come le altre; test impone copertura) e test multilingua end-to-end
(`i18n.spec.ts`) fatti; observer e fallback client invariati. Fase 3:
`JobManager` + test completi (ciclo vita, dedup, cancel, concorrenza, retention,
panico→failed, chiusura) + endpoint read-only (in `API.md`) fatti; migrate
**rinomina massiva** (cancel tra serie, `/api/rename-progress` invariato,
`job_id` in risposta), **scansione archivi** (da sincrona bloccante a job,
`202` + `job_id`), **backfill MediaInfo** (report in `Job.Result`); UI dei job
(seguono fino a esito, pulsante Annulla; `jobs.spec.ts`) fatta; `Fail` esplicito
e `SetResult` aggiunti. Restano una alla volta le altre operazioni. Fase 4:
`make test-race ./...` verde (**0 data race**); bonifica e race bloccante dopo
la Fase 3. Test browser obsoleti riallineati senza mascherare regressioni; fix
reale: tabelle scrollabili raggiungibili da tastiera; suite browser 30/30.

Difetti trovati: chiave fantasma solo-spagnola `Nuevo File Archiviato`
(rimossa; ora il controllo l'intercetterebbe); inglese sottoinsieme con 1.345
voci mancanti (generate automaticamente — **richiedono revisione linguistica**
pre-rilascio); `esc()` senza `'` (corretto); test non idempotente sotto race
(reso ripetibile con cleanup).

### 8.8 Osservazioni emerse (ridimensionano il piano)

- **Modularizzazione completa del client: non consigliata così com'è.** 11
  moduli ES = refactor ampio e rischioso (IIFE→moduli, call-site, test browser)
  per beneficio solo manutentivo; il file compresso non è un problema browser.
  Solo quando la manutenzione diventa il collo di bottiglia, con bundle in build.
  Intanto `esc()` + test XSS coprono il rischio reale.
- **Chiavi i18n stabili globali: non necessarie ora.** Il modello a stringa
  italiana funziona; il buco era la copertura inglese (risolta). 3.130 voci × 6
  lingue riscritte = rischio regressioni. Ibrido: stabili solo per nuove
  stringhe dinamiche, legacy invariate, controllo automatico anti-buchi.
- **`POST /api/jobs` omesso di proposito** (validazione nelle operazioni reali).
- **Backup, pulisci trash, vacuum restano sincroni per scelta** (effetti esterni,
  cancellazioni definitive, VACUUM non interrompibile): non migrare.
- Miglioramenti: `Job.Result`, cancellazione ai punti sicuri (mai a metà
  unità), `Fail` esplicito, traduzioni EN automatiche da rileggere.
- Fase 3: nessuna migrazione obbligatoria rimasta. Fase 4: bonifica + race
  bloccante sui componenti stabilizzati. Prossimi passi: revisione linguistica
  EN; UI su `/api/jobs`; shutdown esplicito per operazione migrata.

---

## 9. Revisione tecnica 2: bug e debito al commit 97142c1

Ex `docs/archive/revisione-2.md` (analisi del 2026-10-06; metodo: lettura quasi
integrale di orchestrator/worker/eventi/database/config/postprocess/cleaner/
safety/web+router, poi engine/rss/parser/update/hooks/notifier/httpx/comics/
watcher/libtorrent; `go test` e `go vet` verdi; severità con grado di certezza
*Confermato*/*Da verificare*, nulla riprodotto a runtime).

### 9.1 Bug e criticità (stato: tutti corretti)

| Voce | Problema | Correzione |
|---|---|---|
| B1 | Path non raggiungibile azzera stato episodi (NAS smontata = dir vuota = `ENOENT`) | reset solo se cartella esistente, o primo antenato esistente non vuoto (`archiveFileConfirmedMissing`; `archive_mount_test.go`) |
| B2 | Errore transitorio di completamento = terminale + torrent staccato, nessun retry | retry transitori (EIO/ENOSPC/ESTALE/rete/DB occupato) dopo 1,2,4,8,16,32 min, poi definitivo; identità/file assenti subito definitivi (`isTransientCompletionError`, `completionRetries`; contatori in memoria) |
| B3 | Torrent aggiunto ma non registrato se la scrittura DB fallisce (ciclo abortito, torrent "esterno") | registrazione fallita → torrent ritirato, placeholder annullato, `continue` |
| B4 | `last_comics_check_ts` aggiornato anche se il ciclo fumetti fallisce (+7 giorni di buco) | timestamp solo a successo; errore → retry ~1 h |
| B5 | Errori DB fumetti abortiscono cicli serie/film | impostazioni fumetti lette solo se servono, errori degradati |
| B6 | Stop per "poco spazio" senza salvare statistiche né report | `finishCycleWithoutDownloads`: salva e riporta comunque (controllo globale invariato) |
| B7 | Config ricaricata male nel worker eventi (fallback = config di avvio) | resta l'ultima valida |
| B8 | Rename repair fuori da `cycle_lock` (concorrenza con ciclo/eventi/rinomine) | lock per-serie (`TryAcquireArchiveRename`/`AcquireArchiveImport`), marcatore import con conteggio; repair resta fuori dal lock ma mutuamente esclusivo per serie |

### 9.2 Illogicità e incoerenze (stato: corrette)

| Voce | Problema | Correzione |
|---|---|---|
| L1 | Booleana ridondante nella selezione (`incumbentWins` valutato 2 volte) | riscritta `covers && incumbentWins`, test esistenti |
| L2 | Supersede/remove non speculari → esito dipendente dall'ordine (confermato: episodio prima del pack = entrambi scaricati) | `mergeSeriesCandidate`/`releaseStrictlyBetter`: a parità vince chi copre di più (REMUX preferito); test con 2000 input rimescolati (fallisce con la vecchia regola) |
| L3 | Funzioni `Check*` che scrivono (approva-subito/annulla-poi fragile) | approvazione in due fasi: decisione in sola lettura → add torrent → conferma in scrittura + registrazione; rollback solo per registrazione fallita post-conferma (`approval_phases_test.go`) |
| L4 | `last_deep_gap_fill_ts` scritto prima del lavoro | salvato solo a passaggio completato e non annullato |
| L5 | Soglie gap non coordinate (deep 6 h vs 23 h hard-coded) | `gap_research_hours` mai sotto l'intervallo deep |
| L6 | Doppia notifica `torrent_finished` + `storage_moved` | `torrent_completed` una sola volta (`alreadyAnnounced`, `completionNotified`; `seeding:true` resta distinto) |
| L7 | Lingue mescolate in log e valori di dato (`"Feed RSS"` usato come chiave) | costanti `episodeOrigin*` condivise g2/g7/dettaglio (valori invariati: arrivano ad API/UI) |

### 9.3 Loop logici (stato: corretti)

| Voce | Problema | Correzione |
|---|---|---|
| C1 | Restart worker dopo panic senza backoff (ciclo panic→restart su evento "veleno") | `workerRestartBackoff`: attesa 5 s→5 min, reset dopo 10 min sani |
| C2 | Ricostruzione eventi a ogni tick (750 ms, doppia fonte di verità, mappe in memoria) | spostamenti salvati in DB (`torrent_moves`, ripristino a +60 s) + ricontrollo ogni 30 s invece che ogni giro; eventi restano il canale principale (`torrent_moves_restart_test.go`) |
| C3 | Lavoro lento inline nel loop (notifiche SMTP/HTTP, `SizeOfPath`, ffprobe, refresh Jellyfin/Plex) | notifiche async (`notifier.Async`, coda 256 best-effort; test di prova resta sincrono), `RefreshMediaLibraries` in goroutine con coalescenza |

### 9.4 Inefficienze (stato: corrette o chiuse con motivo)

| Voce | Problema | Esito |
|---|---|---|
| I1 | `DELETE` per ogni torrent a ogni tick su SQLite single-conn | solo per voci realmente persistite |
| I2 | Query per ogni torrent completo a ogni tick | cache `settledTorrents` 60 s (o fino a cambio percorso/evento) |
| I3 | `torrents.List()` ~10 volte per tick | rinviato (cache backend 500 ms mitiga) |
| I4 | `LoadConfig` pesante da molti punti, copie divergenti | convertito il worker che legge senza modificare; `LatestConfig` ha già cache invalidata al salvataggio |
| I5 | Stat di tutto l'archivio per un log di debug, sotto lock | solo con diagnostica attiva |
| I6 | Selezione O(n²) con ricalcoli | non fatto: quadratico solo entro serie+stagione, trascurabile |
| I7 | Una ricerca d'archivio per query | chiuso: non un problema nell'uso reale |
| I8 | `RecordSeenBatch` ignora errori in Debug | portato a WARN |

### 9.5 Secondo passaggio: parser, feed, notifiche, hook, fumetti, update

| Voce | Problema | Correzione |
|---|---|---|
| P1 | Parser su titoli reali (anno da `1917`/`2001`, `S2024E05` non riconosciuto, `Stagione 2 Episodio 3`, `2160p` nel nome, gruppo `dl` da `WEB-DL`, audio 5.1 da `x265.10bit`, `720p` da CAM) | anno = ultimo prima dei tag tecnici; stagioni a 4 cifre; `Stagione N Episodio/Puntata M` e `[Completa]`; risoluzione fuori dal nome; 5.1 isolato; `hd`/`pal` solo come parola; coda `WEB-DL`/`Blu-Ray` mai gruppo (`parser_regression_test.go`). Non errori: giornaliero S2024E133 (legacy voluto), sentinella `EpisodeRange=[0]` per i pack; non gestito: pack multi-stagione `S01-S03` |
| P2 | `FetchFeed`: pagina non scaricabile → tiene le precedenti; non analizzabile → butta tutto, senza log | tiene sempre le pagine lette, con log |
| P3 | Paginazione presunta base-0 (ext.to: `page=1` = prima pagina → early-stop, crawl fermo) | `listingPageNumber` per ext.to (verificato live); Corsaro invariato |
| P4 | Parser scelto da sottostringa sull'intero URL (query inclusa) | scelta da host+percorso (`feedKindSubject`) |
| P5 | Cache magnet indicizzata dal solo titolo (collisioni) | chiave = URL dettaglio; eviction dei più vecchi (non a caso) |
| P6 | Retry Telegram senza attesa iniziale + sleep finale inutile | attesa prima del tentativo |
| P7 | `smtp.SendMail` senza timeout nel thread chiamante (congela il worker) | `sendMailWithTimeout` 30 s (stesso STARTTLS/AUTH) |
| P8 | Token Telegram nei log (URL intero nell'errore) | `redactRequestError` (schema+host) + maschera `/bot<id>:<token>`; contraddiceva `SECURITY.md` |
| P9 | Hook: variabili espanse prima dello split (titoli con spazi/virgolette = injection argomenti) | prima `SplitArgs`, poi `Expand` per argomento (script che contavano sullo split vanno adattati) |
| P10 | Hook: timeout inefficace coi figli, concorrenza illimitata, niente recover | kill del gruppo processi, `WaitDelay` 5 s, max 4 concorrenti |
| P11 | Panic in goroutine fan-out = morte processo (solo 4 `recover`) | `recoverGoroutine` in 20 goroutine (engine/orchestrator/rss/hooks/handler/restart/qBittorrent) |
| P12 | Ciclo fumetti: download sincroni non annullabili sotto `cycle_lock` + Mega senza timeout + "primo file nuovo" ambiguo | watchdog inattività 2 min su HTTP, timeout 2 h + kill gruppo per `megadl`, interrompibile tra titoli (`comics_stall_test.go`) |
| P13 | Due parser HTML (700 righe artigianali + `x/net/html`) | non si corregge (scelta: va provato su pagine reali) |
| P14 | Checksum update che "fallisce in aperto" + tar senza `--no-same-owner` | non corretto (scelta) |
| P15 | Sessione libtorrent letta senza lock in ~40 metodi (abort su handle distrutto) | contatore + `enterSession`/`exitSession`/`drainSession` su 43 metodi: niente nuove chiamate in shutdown, attesa ≤30 s (`libtorrent_session_guard_test.go`) |
| P16 | Watcher: nessun limite profondità/dimensione/stabilità | in parte: profondità max 8, nomi specifici (limite dimensione e doppia osservazione già esistevano nel worker; `ReadDir` non segue symlink) |
| P17 | Naming misto snake_case/camelCase, prefissi di gruppo | non fatto (stile, nessun bug) |

Altre note sicurezza: P8/P14 le più concrete; P9 injection da titoli di feed;
nessuna restrizione SSRF (coerente con LAN, da ricordare se esposto).

Debito strutturale non fatto con motivo (S1–S8: funzioni da 1100/600/380
righe, duplicati g2/g7, nomi route, parsing booleani → però `settingTruthy`
uniforme fatto, S5, con bug `weekly_enabled` trovato; errori `_ =` in 81 punti,
mappe in memoria, traduzioni 3800 righe): rifattorizzazioni senza bug noti, da
fare a parte se si vuole. Cose verificate a posto: niente deadlock con
single-conn (uso di `tx`), copia atomica con fsync (senza checksum, rename
sovrascrive), backoff qBittorrent, fail-closed su blocklist illeggibile, SQL
parametrizzato.

Domande chiuse: B1 controllo aggiunto; B2 retry solo transitori; L6 doppia
notifica = errore; B8 esclusione per serie; P1 parser corretto; P14 checksum
invariato. Verifica complessiva: `go test ./...` e `go test -race .` verdi a
ogni tornata; ancora da fare: I3, I6, P17, S1–S4, S6–S8.

---

## 10. Rapporti Terra e Pro Terra

Ex `docs/archive/gextto-terra.md` e `docs/archive/pro-terra.md` (letture del
sorgente con migliorie concrete; nessuna vulnerabilità in produzione affermata;
nessun cambio di comportamento del daemon nei report stessi).

### 10.1 Terra: priorità e stato

Implementati punti 2, 3, 5 + primo passo del 7 (concorrenza indexer, contesto
radice di shutdown con attesa worker, test parità router/documentazione,
registrazione UI isolata). Punto 1 escluso su richiesta. Punti 4 e 6 restano
proposte progettuali deliberatamente non introdotte (cache archivio senza
invalidazione affidabile; sostituzione API C++ senza matrice versioni): niente
scorciatoie su correttezza decisioni o compatibilità build.

| Priorità | Area | Intervento | Esito |
|---|---|---|---|
| Alta | Esposizione HTTP | Separare/restringere il listener `EngineListen` (stesso `Router` su entrambe le porte, secondo accesso amministrativo senza auth) | escluso su richiesta |
| Alta | Ricerca provider | Budget concorrenza indexer (semaforo 4–6, default prudente) | fatto |
| Media | Arresto | Chiusura deterministica worker prima di libtorrent (contesto radice, registro operazioni, attesa quiescenza) | fatto |
| Media | Scalabilità | Indice archivio incrementale fuori dal ciclo critico (il ciclo rilegge la NAS) | proposta (indice persistito + timestamp, scansione come fallback; decisioni identiche) |
| Media | API | Riferimento API generato e verificato (route qBit mancanti dalla tabella) | fatto (test di parità) |
| Bassa | Bridge C++ | Rimuovere API libtorrent deprecate (`half_open_limit`, cache, priorità, tracker, resume) | proposta (matrice versioni + adapter, warning attesi come lista in CI) |
| Bassa | Router | Modularizzare la registrazione (`registerTorrentRoutes`, …) senza cambiare il contratto | primo passo fatto (registrazione UI isolata) |

Piano consigliato: esposizione listener → concorrenza indexer + test API →
shutdown → indice archivio + compatibilità libtorrent.

### 10.2 Pro Terra: priorità e stato

Seguito di Terra su sicurezza, concorrenza, lifecycle, integrità dati. Correzioni
applicate (LAN fidata: gli hardening puri di rete rinviati): arresto (punto 5 —
`trackOperation()` per `RunNow`/`RenameAll`/repair, attesa prima di distruggere
la sessione: chiude la corsa sull'handle); concorrenza (punto 6 —
`torrent_engine` sotto `engine_mu`, `RenameProgress` protetta, `categoryReady`
sotto `e.mu`, `safeGo` senza doppio report e restart interrompibile, handle
SQLite chiusi); integrità (punti 7–8 — upgrade serie/film transazionali,
niente `DELETE` su hash magnet vuoto); robustezza (punto 9 — errori non più
silenziosi, `gibSetting` saturato, limite dimensione `HTTPDownload`). Punto 1
parziale (causa risolta col tracciamento, disciplina `sessionMu` su ~90 metodi
raccomandata come difesa in profondità — poi fatta in revisione-2 P15).
Punti 2–4 rinviati (allowlist hook, filtro SSRF, redazione chiavi in
`ConfigView`): valide se mai esposto oltre la LAN.

| Priorità | Intervento | Dettaglio |
|---|---|---|
| Critica | Sincronizzare ogni lettura di `LibtorrentClient.session` | solo `listUncached` usava il lock; lettura senza lock + `Shutdown` che azzera = abort Boost su handle distrutto; proposta helper `withSession`/estensione `RLock` + stress test |
| Critica | Vincolare `Program` degli event hook | solo non-vuoto e assoluto: con `/bin/bash -c` = shell arbitraria da porta non autenticata; proposta allowlist o segreto separato + doc in manuale/`SECURITY.md` |
| Alta | Filtrare URL remoti (SSRF) e origini percorsi | `send-magnet`/`archive/add`/`search/add` validano solo `http(s)://`, client segue redirect; proposte helper condiviso (no privati/link-local/metadata, redirect limitati, no de-ref listener locale) + percorsi dentro le radici configurate |
| Alta | Non esporre chiavi in `ConfigView` | `api_key` indexer + `tmdb/tvdb_api_key` in chiaro (altri segreti già booleani); proposta soli flag |
| Alta | Tracciare goroutine dagli handler | `RunNow`/`RenameAll`/repair non in `bgWG`, toccano il motore dopo `ShutdownEmbedded` |
| Media | Stato condiviso | vedi sopra (fatto) |
| Media/Media | Transazioni DB + hash magnet vuoto | vedi sopra (fatto) |
| Bassa | Errori ignorati e limiti | vedi sopra (fatto); più `staticcheck`/`errcheck` in CI |

Piano: race handle + goroutine → hook + segreti → SSRF + percorsi → locking +
transazioni → errori ignorati + race/staticcheck in CI.

---

## 11. UI SSR + HTMX: report di migrazione

Ex `docs/archive/UI_V2.md` (2026-10-01, build 1229, libtorrent 2.0.11.0).
L'interfaccia ufficiale è SSR + HTMX, non dipende più da `gextto-ui.js`: è su
**`/`**, `/v2` reindirizza (301, query preservata), riusa API JSON, view-model
Go e CSS di base; tabelle renderizzate dal server via handler API in-process
(`v2InternalJSON`: stessa validazione, niente rete); i18n server-side con
tokenizer (`v2TranslateHTML`: text node, `title`/`placeholder`/`aria-label`,
fallback inglese, mai `<script>`/`<style>`, con IT byte-per-byte); JS minimale
(HTMX + `v2-core.js`: tema/font, modali accessibili); route UI isolate
(`v2Handle`, fuori dalla tabella route API: `API.md` e test di parità invariati);
vecchia route `/ui` rimossa.

Architettura (`uiweb_v2*.go`, `uiweb/v2/templates/v2.html`,
`uiweb/v2/static/{htmx.min.js,v2-core.js,v2.css}`, 1 riga in `web_router.go`):
shell/navigazione/i18n, renderer tabelle generico, pannelli Manutenzione/
Integrazioni, Esplora, dettagli Serie/Film, editor strutturati (feed, checkbox,
righe, rinomina, traduzioni), widget Manutenzione, widget speciali (duplicati,
DB, RAM disk, rinomina, OAuth/PIN, job, upload, TMDB, traduzioni per chiave,
anteprima rinomina); test in `uiweb_v2_test.go` /
`uiweb_v2_bench_test.go`.

Copertura: tutte le 16 voci di menu + dettagli (Dashboard con Prossime uscite
etichettate; Scarico con badge download ogni 5 s, limiti/storage,
tracker/file/web seed, storico, aggiunta, tag in massa; Serie/Film con hero
TMDB, stagioni on/off, Anime/Niente-upgrade, rinomina; Mancanti; Esplora con
calendario/tendenze/ricerca TMDB; Archivio con spiegazione decisione; Fumetti
con coda HTTP e weekly; Configurazione 4 aree/13 sezioni con Predefinito,
savebar, pannelli esperti/motore; Integrazioni Simkl/Jellyfin/Plex/FlareSolverr/
indexer/iCal/verifica sorgenti; Manutenzione con cestino/duplicati/DB/RAM
disk/rinomina/job; Salute; Log con colori server-side; Blocklist; Manuale e
Licenza server-side; `/login` per accesso protetto).

Residui consapevoli: "Ultimi trovati nei feed" su richiesta (diagnostica, non
operativa); aggiunta manuale avanzata solo campi principali (specialistici solo
via API); calendario via HTMX async; azioni lunghe nel pannello Operazioni in
background senza barra dedicata. Differenze accettate: ordinamento/filtro
server-side; modali con focus-trap/Escape/backdrop; log a intervalli invece di
SSE; job via polling `/api/jobs`.

Test: `go test .` e `./...` verdi (suite v2 completa); script indice
impostazioni (173 voci, 13 sezioni) e installer-selftest OK; vet/gofmt puliti;
live 16/16 voci → 200, 0 segnaposto; IT→EN con traduzioni reali. Benchmark
(N97, Go 1.26, CGO): `V2TranslateHTML` ~0,42 ms; `V2FormatCell` ~2,9 µs;
`V2RenderTableFragment` (500 righe) ~1,05 ms; `V2RenderShell` ~0,30 ms —
rendering ~1–3 ms/pagina, trascurabile su LAN (la traduzione è il costo
maggiore, solo per lingue ≠ IT). Correttezza: contratto API invariato, azioni
solo su path `/api/…`, salvataggio con gli stessi vincoli, editor a righe
compatibili. Promozione a default e rimozione UI classica completate (CSS
embedded in `uiweb/v2/static/`, view-model/handler condivisi mantenuti).
Prossimi passi: barra contestuale per azione lunga; campi specialistici nel
form; benchmark come test di performance in CI (poi chiuso, vedi §1.3).

---

## 12. Handover tra sessioni di lavoro

Ex `docs/lavoro-sessione.md` (aggiornato al 2026-10-07; copre dal commit
`d9e5d5e` del 06/10 sera al `432ce98` + commit `docs:` del 07/10).

### 12.1 Regole di lavoro (vincolanti, in aggiunta ad `AGENTS.md`)

1. Mai riferimenti a Claude/Anthropic in autore/committer/trailer/messaggi/PR
   (prevale su hook o promemoria dell'ambiente).
2. Identità git `buzzqw <azanzani@gmail.com>`, `commit.gpgsign false` in cloud
   (la firma cloud risulterebbe "Unverified").
3. Lavoro e push diretti su `main`; niente branch/PR salvo richiesta; ignorare
   branch `claude/...` assegnati dall'ambiente.
4. Niente force-push su `main`: fetch + merge prima del push, test rilanciati.
5. Italiano nelle risposte; testi UI nei cataloghi (ogni testo nuovo tradotto
   in tutte le lingue; `uiText` fuori template; TUI bilingue; notifiche
   it/en; log daemon solo inglese).
6. Pubblico italiano: mai dare per scontata la disponibilità all'air-date
   (date TMDB = "messa in onda originale").
7. Log leggibili (frasi, no dump), senza doppioni; dettagli in DEBUG; una riga
   INFO al giorno dai controlli che non trovano nulla.
8. Con "sistema"/"procedi in autonomia": decidere, fare, verificare, riferire
   in sintesi.

### 12.2 Lavoro svolto (06–07/10, per area)

- **Log e notifiche** (`d9e5d5e` e seguenti): pulizia completati che elenca i
  rimossi; rimozione manuale con una sola riga (destino cestino); niente
  notifica per pezzi danneggiati (resta WARN); backup: passo non configurato
  non è "non riuscito"; notifiche doppie eliminate (fumetti torrent ×2,
  "in attesa" a ogni ciclo, errori libtorrent 1 volta/6 h per torrent+tipo,
  archiviazione manuale già annunciata); backup Telegram >50 MB in parti da 45
  MiB con istruzioni `cat`.
- **Stalled, ciclo, FlareSolverr** (merge `281f89a`): backoff reale
  (intervallo → 3 h → 6 h → 12 h → 24 h, step salvato in DB, ogni tentativo
  loggato col prossimo orario); "📊 Downloads" solo al cambio numeri; backup una
  riga INFO; FlareSolverr con causa breve ed esito (chiusure appese in INFO coi
  domini); riepilogo ricerca coi motivi di scarto; dedup per hash feed/archivio
  ("releases checked" su release distinte); controllo libreria con una riga per
  rinomina/copia inferiore eliminata + riga giornaliera "all correctly named".
  Nota: titolo del merge con nome branch assegnato dall'ambiente (solo il
  proprietario può correggerlo con force-push).
- **TUI** (`internal/tui`): testi mancanti, lingua interfaccia vs release,
  aiuto 1-9 scorrevole, Salute in italiano (test chiavi); numeri sempre di
  scheda, ←→ in Libreria; avvio ricerca (Invio/s/f/m/c); log con `w` avvisi e ↓
  di una riga; tabella torrent (dimensione, ETA/ratio, seed/peer, niente hash);
  torrent bloccati spiegati + `F` con azzeramento ritardo (anche daemon in
  `MarkTorrentFailed`, `stalled_since`/`next_retry_at` in API); Stato con ultima
  ricerca in frase e migliaia separate; "⚠ N nuovi avvisi"; `/` anche sui
  torrent; aiuto sempre visibile in Stato.
- **Web su telefono** (`uiweb/v2`): layout ≤560 px (striscia stato toccabile,
  barra in basso una riga, menu Altro, Dashboard riordinata, Scarico con lista
  prima e opzioni dietro ⚙, azioni di gruppo solo con selezione); Log compatto
  con "Solo avvisi ed errori" (anche desktop); PWA (manifest, icone, service
  worker minimo, share target `/share`, pull-to-refresh); contatore WARN/ERROR
  (`ProblemCount`); fix template Scarico (div aperto); avvisi CodeQL (host
  validati, `nosniff`, redirect `/share` con `url.URL`, conversioni sicure).
- **Migliorie §2** (dettaglio sopra): hardlink, refresh mirato, upgrade-until,
  auth, iCal, ComicInfo, anime, mancanti, badge, traduzioni, docs.
- **gx-torrent completato**: da scheletro non selezionabile a motore funzionante
  (coda autogestita con slot/tetto/lenti ignorati/rotazione/pin/probe/coda
  dinamica; stalled come park/probe/unpark persistente; symlink, delete sicura,
  rename-o-copia; token + loopback-only senza token, RPC rain spento; avvio e
  sorveglianza con fallback a libtorrent dopo 3 crash/10 min; impostazioni
  token/eseguibile; matrice capacità onesta; test demone + adapter). Smoke test:
  avvio, verify→seed, pausa/ripresa, spostamento con symlink, kill -9 →
  riavvio, SIGTERM allo spegnimento. (Big Buck Bunny in `downloading_metadata`
  nel sandbox senza peer.)
- **Rete, selezione file, v2**: rain vendored in `third_party/rain` (`replace`,
  inventario `GEXTTO.md`); porta unica + DHT sulla stessa UDP + UPnP/NAT-PMP
  ogni 20 min; selezione file (esclusi in `DATA/parts`, progresso sui scelti);
  interfaccia ascolto/uscente (killswitch), proxy SOCKS5/HTTP
  (`gxtorrent_proxy`), cifratura 0/1/2, filtro IP multi-formato; v2-only
  rifiutati con blocklist; session-stats anche per gx-torrent; bug rain (IP mai
  ricontattati, race logger); test reali demone↔demone (porta unica, cifratura,
  selezione, proxy, RAM disk a metà, filtro IP); smoke (flag di rete, porta
  6881, migrazione torrent, messaggio senza UPnP).
- **uTP, LSD, Salute**: socket UDP condiviso col DHT (`third_party/dht`), dial
  parallelo uTP+TCP, LSD multicast BEP 14, pannello Motore torrent in Salute;
  test reali (solo-uTP, LSD); bug chiusura DHT su socket condiviso.
- **Dettaglio torrent + qBittorrent**: scheda con soli comandi supportati dal
  motore + nota lacune, file "Salta/Scarica", errori in scheda, righe
  sciame/motore, colonna Connessione peer; qBit: avanzamento peer (era ×0,01),
  connessione+flag, super-seeding sbloccato, "Salva tracker" che sostituisce;
  gx-torrent: avanzamento/seed da bitfield, tracker corrente, seed contati.
- **Cache automatica + log v2**: cache da RAM (lettura 1/32, scrittura 1/16) o
  da `libtorrent_cache_size`, scadenza + `fallocate`, "Ottimizza" anche per
  gx-torrent, riga in Salute; log v2-only (🔁 alternativa v1 / ⏳ in attesa /
  ⌛ non più tracciato dopo 30 giorni; memoria in RAM).
- **gx-torrent default**: `torrent_backend` mancante/vuoto = `gx-torrent`
  (installazioni esistenti invariate, `anacrolix` → `embedded`, fallback a
  libtorrent se non si attiva); servizio agnostico (`GEXTTO_LIBTORRENT` solo
  override); impostazioni rete/coda/cache condivise non più disabilitate con
  gx-torrent; README + `gx-torrent.md` aggiornati.

### 12.3 Punti aperti al 07/10 (da rivalutare: alcuni potrebbero essere chiusi)

Da decidere: doppia notifica "completato — in seed" in `move_episodes`
(proposto di togliere la prima); backup Telegram come didascalia dell'ultima
parte; tasti TUI con significato per scheda (`d`, `R`, `i`, `e`); tabelle
Serie/Film/Archivio su telefono con dati veri (schede vs tabelle).
Da fare/verificare: PWA/condivisione su HTTPS reale; backoff stalled sul server
reale (ripresa da `notice_step`); indexer mircrew (solo in locale sul server);
titolo merge `281f89a`; «router port N opened» nel log demone (o apertura
manuale TCP+UDP); con VPN impostare interfaccia uscente (killswitch);
procedura gx-torrent (scelta motore, Big Buck Bunny, slot/pause); migliorie §2
(hardlink con `ls -li`, scansione cartella Jellyfin/Plex, login via proxy con
`X-Forwarded-For`, anime reale, calendario con chiave TMDB).

### 12.4 Ambiente e insidie (sessione cloud, riferimento)

Build con `libtorrent-rasterbar-dev` (warning C++ normali); test radice ~60 s.
Dry-run con `GEXTTO_DATA_DIR` dedicato; stop via `kill` sui PID (mai
`pkill -f`, che uccide anche la shell). TUI in `tmux` + `capture-pane`;
pausa tra `Escape` e lettera (Alt+lettera). Web/telefono con Chromium +
Playwright preinstallati (config senza `channel: chrome`, `executablePath`
esplicito, data dir nuovo; 36 test verdi). Insidie: `gofmt` che riallinea
`internal/tui/i18n.go`; `go test` cached; push/force-push bloccati; `go test …
| grep … && git commit` committa anche con test rossi (controllare `$?`);
registro download fumetti globale nei test; `#` a inizio riga nei manuali =
titolo Markdown (lo trova l'e2e); manuale embedded (ricompilare prima degli
e2e). Mappa codice toccato e ripresa: vedi file originale in archivio.

---

## 13. Accessibilità

Ex `docs/accessibility-analysis.md` (analisi del 29-09-2026 su
`uiweb/v2/templates/`, `uiweb/v2/static/`, test in `uiweb/end2end/`; riferimento
WCAG 2.1 AA, pratiche compatibili 2.2).

**Stato**: interfaccia **parzialmente accessibile** con baseline automatizzata
molto migliorata; i controlli automatici passano ma non sono una certificazione
WCAG/EN 301 549/legale. Una conclusione formale richiede test manuali con
tastiera, screen reader, ingrandimento e tecnologie assistive su browser e
dispositivi supportati.

**Metodo**: review statica (HTML/CSS/JS), semantica/nomi/ARIA/focus, layout
responsive/focus visibile/reduced-motion, contrasto temi chiaro/scuro, axe-core
via Playwright, regression test (dialoghi, sort tastiera, polling, viewport
stretti). Suite: **12 test verdi** su dashboard, downloads, settings,
maintenance, health, logs (`cd uiweb/end2end && npm ci && npm run test:a11y`;
rete di sicurezza, non sostituto della valutazione manuale).

**Miglioramenti implementati**: lingua via attributo `lang`; landmark
(skip-nav, `aside`/`nav`/`header`/`main`); `aria-current="page"`;
nomi accessibili (non solo placeholder/tooltip); label su controlli a icona;
dialoghi (focus dentro, trap Tab/Shift+Tab, Escape, ripristino focus);
header ordinabili da tastiera con `aria-sort`; focus preservato sotto polling;
focus visibile; tabelle con caption/scope (anche dinamiche); progressbar con
valore; ARIA per tab/dialoghi/landmark; live region per stati/errori/notifiche/
async; simboli decorativi mai al posto del testo; ricerca impostazioni con
sinonimi (`memo`, `memory`, `RAM`, `cache`); contrasti tema chiaro scuriti dove
serviva; reflow mobile (tabelle con scroll interno); reduced-motion rispettato.

**Validazione residua** (prima di qualsiasi dichiarazione formale): workflow
primari con Tab/Shift+Tab/Invio/Spazio/frecce/Escape; almeno uno screen reader
desktop (NVDA+Firefox o VoiceOver+Safari) e TalkBack dove applicabile; zoom
200%/400% (overflow, dialoghi, tabelle, form); ogni form con screen reader
(riepilogo errori, associazione campi, stati invalidi, recovery); stati async
non solo-colore (loading/successo/warning/fallimento/vuoto/retry); entrambi i
temi, viewport mobili, reduced motion; comportamenti browser-specifici e
contenuti esterni iniettati.

**Criteri di accettazione**: nome+ruolo affidabili per ogni controllo; ogni
azione mouse da tastiera; focus contenuto/ripristinato nei dialoghi; annunci
async senza interruzioni eccessive; contrasti in entrambi i temi; semantica
usabile per tabelle/tab/progress/errori; nessun regresso su desktop e mobile.

---

## Appendice A: file consolidati

| File originale | Ora (copia locale, non tracciata) | Sezione qui |
|---|---|---|
| `docs/proposte-migliorie.md` | `docs/archive/proposte-migliorie.md` | §2 |
| `docs/da-fare.md` | `docs/archive/da-fare.md` | §1 |
| `docs/lavoro-sessione.md` | `docs/archive/lavoro-sessione.md` | §12 |
| `docs/scelte-di-progetto.md` | `docs/archive/scelte-di-progetto.md` | §3 |
| `docs/accessibility-analysis.md` | `docs/archive/accessibility-analysis.md` | §13 |
| `docs/holepunch-test-harness.md` | `docs/archive/holepunch-test-harness.md` | §7 |
| `docs/gx-torrent-migliorie.md` | `docs/archive/gx-torrent-migliorie.md` | §4 |
| `docs/gx-torrent-ottimizzazioni-prestazionali-v2.md` | `docs/archive/gx-torrent-ottimizzazioni-prestazionali-v2.md` | §5 |
| `docs/gx-torrent-misure-seeding.md` | `docs/archive/gx-torrent-misure-seeding.md` | §6 |
| `docs/revisione-1.md` | `docs/archive/revisione-1.md` | §8 |
| `docs/archive/revisione-2.md` | resta in `docs/archive/` (non tracciato) | §9 |
| `docs/archive/UI_V2.md` | resta in `docs/archive/` (non tracciato) | §11 |
| `docs/archive/gextto-terra.md` | resta in `docs/archive/` (non tracciato) | §10.1 |
| `docs/archive/pro-terra.md` | resta in `docs/archive/` (non tracciato) | §10.2 |
