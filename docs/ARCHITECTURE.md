# Architettura e mappa del codice

Questo documento spiega **come è organizzato il repository** e **dove mettere le
mani** per i compiti più comuni. È pensato per chi legge il codice, non per
l'utente finale: per l'installazione e l'uso vedi il [manuale](MANUAL.it.md).

> [!NOTE]
> Gextto è un daemon Go. Tutta la logica applicativa vive in **un unico
> package Go** (`github.com/buzzqw/gextto`) nella cartella principale del repo;
> `cmd/gexttod` è solo l'eseguibile. I file della root sono quindi tanti, ma
> **raggruppati per prefisso**: le sezioni qui sotto sono la mappa di quei
> prefissi.

## 1. Panoramica delle cartelle

```text
gextto/
├── cmd/gexttod/            eseguibile (main): smista i comandi e avvia il daemon
├── *.go                    il package gextto: tutta la logica (vedi §3 e §4)
├── *_test.go               test, accanto al codice che verificano
├── internal/               pacchetti condivisi (vedi §5)
├── uiweb/v2/               UI server-side: template HTML e asset statici
│   ├── templates/*.html
│   ├── static/*            CSS, htmx, JS (v2-core.js)
│   └── end2end/            test Playwright (Node, non Go)
├── docs/                   documentazione (questo file, manuali, API...)
├── scripts/                build, update, packaging e service helper
├── systemd/                unit file di esempio per il servizio
├── .github/                CI, CodeQL, dependabot, release
├── go.mod / go.sum         dipendenze Go
├── Makefile                comandi di build/test
├── VERSION, build_number   versione e numero di build (build_number non è committato)
└── bin/                    binario compilato (ignorato da git)
```

Cartelle **generate/locali** da non confondere con il codice: `bin/` (build),
`data/` (database e stato runtime), `uiweb/end2end/node_modules/`,
`gextto-data/` (directory dati del servizio). Sono elencate in `.gitignore`.

## 2. Come nasce il binario

```
cmd/gexttod/main.go  →  gextto.Parse(args)  →  gextto.Run(...)   (cli.go/run)
                                            →  NewAppState(...)  (appstate.go)
                                            →  avvio server web  (web_serve.go)
```

Il **numero di build** è generato dallo script `scripts/build-daemon.sh`
(e `scripts/next-build-number.sh`) e inciso nel binario; non è versionato.

Il **bridge C++** per libtorrent è la coppia nella root:
`libtorrent_bridge.cpp` (compilato da cgo) + `libtorrent_bridge.h` (header).
`libtorrent_cgo.go` è solo il collante Go; la logica è in `libtorrent.go`.

## 3. Package principale: file per area

### Avvio, orchestrazione e servizio
| File | Ruolo |
|---|---|
| `cli.go` | parsing dei comandi (`gexttod --version`, `--update`, ...). |
| `appstate.go` | costruzione dello stato condiviso `AppState` (`NewAppState`). |
| `engine.go` | motore di scrape/ricerca: feed HTML/RSS, Torznab, motori web. |
| `orchestrator.go` | un ciclo completo di acquisizione. |
| `web_serve.go` | avvio del server web e dei worker di background. |
| `service_restart.go` | azioni systemd pianificate (es. riavvio del servizio). |
| `update.go` | auto-aggiornamento del binario. |
| `safety.go` | riavvio controllato dei worker dopo un panic. |
| `watcher.go` | cartelle "watched": i file `.torrent`/`.magnet` vengono aggiunti da soli. |

### Configurazione, stato e database
| File | Ruolo |
|---|---|
| `config.go` | il modello `Config`: lettura/scrittura, generazione, default. |
| `database.go` | store SQLite: serie, film, torrent, storico feed, backoff provider. |
| `sqlite.go` | apertura di un DB SQLite con pragma condivisi. |
| `archive.go` | catalogo durevole delle release archiviate. |
| `migrate.go` | migrazione della directory dati. |
| `backup.go` | backup dei database (manuale e schedulato). |
| `library_alias.go` | helper di persistenza della libreria. |
| `web_inputs_extra.go` | struct di input delle API web. |

### Pipeline media (release → archivio)
| File | Ruolo |
|---|---|
| `parser.go` | parsing dei nomi release e della qualità (anche il numero assoluto degli anime). |
| `anime.go` | serie anime: numero assoluto ↔ stagione/episodio con i dati TMDB, ricerche per numero assoluto. |
| `upgrade_until.go` | soglia «smetti di migliorare» (`upgrade_until_score`) applicata alle decisioni di upgrade. |
| `decision.go` | spiegazione (sola lettura) delle decisioni su una release. |
| `cleaner.go` | indice dell'archivio, rilevamento duplicati inferiori, pulizia. |
| `postprocess.go` | post-processing: spostamento, rinomina, sidecar. |
| `hardlink.go` | import in libreria come hardlink dei file in seed, con ritorno alla copia. |
| `media_guard.go` | guardie sui file video (estensioni, protezione). |
| `mediainfo.go` | ispezione reale dei file via `ffprobe`. |
| `comics.go` | fumetti: scraper, DB, download HTTP, ciclo dedicato. |
| `comicinfo.go` | scrittura di `ComicInfo.xml` nei CBZ scaricati direttamente. |

### Sorgenti, ricerca e metadati
| File | Ruolo |
|---|---|
| `rss.go` | feed RSS/XML delle sorgenti. |
| `websearch.go` | motori di ricerca web e limiter condiviso. |
| `indexer_health.go` | salute degli indexer (Prowlarr). |
| `tmdb.go` / `tvdb.go` | metadati TMDB e TheTVDB. |
| `integrations.go` | URL base delle integrazioni esterne. |
| `media_refresh.go` | aggiornamento mirato (per cartella) di Jellyfin e Plex, con ripiego sull'aggiornamento completo. |
| `calendar_ics.go` | calendario iCal `/feed/calendar.ics`: arrivi, messe in onda originali, uscite locali dei film. |

### Notifiche, hook, i18n
| File | Ruolo |
|---|---|
| `notifier.go` | notifiche via Telegram, webhook HTTP e SMTP. |
| `hooks.go` | hook utente sugli eventi. |
| `i18n.go` | traduzioni incorporate (`internal_translations*.yml`). |

### Motori torrent (piano di trasferimento)
| File | Ruolo |
|---|---|
| `torrent_engine.go` | contratto `TorrentEngine` tra Gextto e il motore. |
| `torrent_engine_select.go` | scelta del backend da `torrent_backend` + preflight. |
| `gxtorrent_engine.go` + `gxtorrent_runtime.go` + `cmd/gx-torrent/` | backend **gx-torrent** (Go puro, predefinito): adapter REST, avvio e sorveglianza del demone, che usa la copia di rain in `third_party/rain`. |
| `libtorrent.go` + `libtorrent_cgo.go` + `libtorrent_bridge.cpp/.h` | backend libtorrent (C++/cgo), fallback automatico. |
| `qbittorrent_engine.go` + `qbittorrent_runtime.go` | backend qBittorrent. |
| `torrent_migration.go` | preparazione della migrazione tra backend. |
| `torrent_removal.go` | registrazione delle rimozioni manuali. |

### Web/API e worker
| File | Ruolo |
|---|---|
| `web.go` | definizioni condivise: `AppState`, helper risposta/query, setup. |
| `web_router.go` | costruzione del mux HTTP e tabella delle rotte. |
| `auth.go` | accesso facoltativo: middleware davanti a tutte le rotte, esenzione rete locale, login, chiave API. |
| `web_handlers_core.go` | handler core, streaming log, middleware. |
| `web_handlers_g0.go` … `web_handlers_g7.go` | gruppi di handler portati dal progetto di riferimento. |
| `web_handlers_compat.go` | endpoint di compatibilità/diagnostica. |
| `web_handlers_jobs.go`, `web_handlers_libtorrent.go`, `web_handlers_torrent_backend.go`, `web_handlers_torrent_diag.go` | handler specifici (job, libtorrent, backend, diagnostica). |
| `web_torrent_events.go` | macchina a eventi del ciclo di vita dei torrent. |
| `web_background.go` | worker di background di lunga durata. |
| `web_workers_media.go` | worker media: backup schedulato, backfill MediaInfo, calendario. |
| `web_maintenance_rename.go` | rinomina cartelle con revisione utente. |
| `httpx.go` | client HTTP condiviso per sorgenti e provider. |
| `jsonutil.go` | normalizzazione fedele delle risposte JSON. |
| `health.go` | report di salute (dashboard/endpoint). |
| `health_monitor.go` | monitor che logga lo stato `degraded` con motivo e recupero. |
| `jobs.go` | gestore dei job di background. |

### UI server-side (`uiweb` v1 classica e v2)
| File | Ruolo |
|---|---|
| `uiweb.go` | view-model e formatter condivisi; embed di manuali e licenza. |
| `uiweb_pages.go` | pagine lista/azione guidate dai dati. |
| `uiweb_sections.go` | sezioni riutilizzabili (tabella, azioni, form, progresso...). |
| `uiweb_detail.go` | pagine di dettaglio serie/film. |
| `uiweb_shell.go` | "chrome" della UI: barra in alto, sidebar, stato. |
| `uiweb_settings.go`, `uiweb_settings_meta.go`, `uiweb_settings_defaults.go`, `uiweb_tooltips.go` | pagina Configurazione: aree, sezioni e indice con il pannello (`Group`) di ogni impostazione; unità, valori speciali, tipo di controllo e interruttore da cui dipende; default; descrizioni. |
| `uiweb_v2.go` | shell v2 (HTMX), routing dei frammenti e rendering. |
| `uiweb_v2_dashboard.go`, `uiweb_v2_downloads.go`, `uiweb_v2_detail.go`, `uiweb_v2_search.go`, `uiweb_v2_comics.go`, `uiweb_v2_maintenance.go` | pagine v2 dedicate. |
| `uiweb_v2_sections.go` | pagine composte da sezioni (Manutenzione, Integrazioni). |
| `uiweb_v2_settings_extras.go` | editor strutturati della Configurazione v2. |
| `uiweb_v2_table.go` | renderer generico di tabelle server-side. |
| `uiweb_v2_widgets.go` | widget migrati (duplicati, database, RAM disk, rinomina, OAuth, job...). |

## 4. Convenzioni di naming dei file

- `web_handlers_g0.go` … `web_handlers_g7.go`: gruppi di handler portati dal
  progetto di riferimento. Il numero **non** indica una priorità: serve solo a
  spezzare l'enorme tabella di handler in file maneggiabili.
- `uiweb_v2_*.go`: tutto ciò che riguarda la **UI v2** (SSR + HTMX). `uiweb_*.go`
  senza `_v2` è il livello condiviso/classico.
- `libtorrent_*`, `gxtorrent_*`, `qbittorrent_*`: i backend torrent.
- `*_test.go`: test accanto al file che verificano.
- Gli helper privati di un gruppo usano il prefisso del gruppo (`gh2_...`,
  `gh3_...`, `bg_...`) per evitare collisioni tra file dello stesso package.

## 5. Pacchetti in `internal/`

| Pacchetto | Contenuto |
|---|---|
| `internal/constants` | costanti condivise (porte, default, versione/build). |
| `internal/logging` | logger del daemon. |
| `internal/models` | tipi condivisi. |
| `internal/rules` | regole di matching. |
| `internal/utils` | utilità generiche. |
| `internal/cache` | cache. |
| `internal/backoff` | backoff esponenziale. |
| `internal/messages` | messaggi/testi. |
| `internal/qbittorrent` | client dell'API qBittorrent. |
| `internal/tui` | client terminale (TUI). |

## 6. Asset incorporati nel binario (`go:embed`)

- `uiweb/v2/templates/*.html`, `uiweb/v2/static/*` — UI web.
- `docs/MANUAL.it.md`, `docs/MANUAL.en.md` — manuale mostrato nella UI.
- `internal_translations*.yml` — traduzioni.
- `LICENSE` — licenza servita dall'app.

Se modifichi questi file **serve ricompilare** il binario per vederli.

## 7. Dove mettere le mani (compiti comuni)

| Voglio... | File da toccare |
|---|---|
| Aggiungere/modificare un endpoint API | `web_router.go` + un `web_handlers_*.go` (+ eventuale struct in `web_inputs_extra.go`) |
| Aggiungere una pagina o un widget UI | `uiweb_v2*.go` + `uiweb/v2/templates/v2.html` (+ CSS/JS in `uiweb/v2/static/`) |
| Aggiungere un worker di background | `web_serve.go` (registrazione) + `web_background.go` o `web_workers_media.go` |
| Cambiare lo schema del database | `database.go` (+ test) e, se serve, `migrate.go` |
| Aggiungere un'impostazione | `config.go` + `uiweb_settings.go` (sezione e `Group`) + `uiweb_settings_meta.go` (unità, valore speciale, dipendenza) + `uiweb_settings_defaults.go` + `uiweb_tooltips.go` + cataloghi `internal_translations*.yml` |
| Toccare la ricerca/sorgenti | `engine.go`, `rss.go`, `websearch.go`, `indexer_health.go` |
| Toccare un motore torrent | `torrent_engine*.go` e il backend specifico |
| Cambiare il bridge C++ | `libtorrent_bridge.cpp` + `libtorrent_bridge.h` (root), `libtorrent_cgo.go` |

## 8. Note di manutenzione

- **Una sola copia del bridge C++.** `libtorrent_bridge.cpp` è l'unico file
  compilato da cgo; l'header deve restare accanto a lui nella root. Non
  ricreare copie "di riferimento" in sottocartelle: cgo **non** compila file
  fuori dalla cartella del package e le copie divergono silenziosamente.
- **Un unico grande package.** Al momento non è diviso in sottopackage: ogni
  refactor in quel senso va fatto per fasi, con build e test verdi a ogni passo.
- **Aggiornare i test.** Ogni modifica al comportamento dovrebbe aggiornare il
  test accanto al file interessato; `go test ./...` è la rete di sicurezza.

Vedi anche [DEVELOPERS.md](DEVELOPERS.md) per build, test e flusso di contribuzione.
