# Gextto

Gextto è un demone self-hosted che cerca, seleziona, scarica, verifica, rinomina
e archivia serie TV, film e fumetti. L'interfaccia web è il piano di controllo;
il demone continua a funzionare come servizio.

> **English:** [README.md](README.md) · **Guida completa:**
> [manuale italiano](docs/MANUAL.it.md)

## In breve

- Un solo servizio gestisce ciclo di ricerca, database SQLite, coda torrent,
  post-processing e archivio.
- Il motore predefinito è libtorrent integrato. qBittorrent-nox e anacrolix
  opzionale sono alternative, non servizi aggiuntivi necessari.
- La scelta delle release considera qualità, sorgente, codec, audio, HDR, lingue
  e dimensione. `ffprobe` può aggiungere le caratteristiche del file archiviato.
- UI web responsive e TUI mostrano salute, log, backup, manutenzione e
  integrazioni con Trakt, Simkl, Jellyfin e Plex.

## Installazione Linux

L'installer ufficiale è destinato a server Linux 64 bit con systemd. Eseguilo
come root:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

Installa il programma in `/opt/gextto`, conserva i dati del servizio in
`/var/lib/gextto` ed espone la UI sulla porta 5000.

> [!IMPORTANT]
> La UI web è un'interfaccia amministrativa senza autenticazione. Mantienila in
> una rete fidata oppure proteggila con firewall e reverse proxy HTTPS dotato di
> autenticazione. Leggi prima la [politica di sicurezza](docs/SECURITY.md).

### Installazione dal sorgente

Servono Go 1.26+, compilatore C++17 e header di sviluppo libtorrent-rasterbar.
La build normale include la UI web: non è richiesto un build frontend separato.
Consulta il [manuale sviluppatori](docs/DEVELOPERS.md).

```bash
make build
```

`make build` incrementa il numero della build locale e scrive il demone
versionato in `bin/gexttod`. Per ricompilare senza incrementare il numero usa
`make fast`. Verifica binario, versione del prodotto, numero di build e versione
di libtorrent collegata con:

```bash
./bin/gexttod --version
```

## Accessibilità

La UI web include navigazione da tastiera, nomi accessibili per i controlli,
gestione del focus nei dialoghi, tabelle ordinabili semantiche, annunci live per
gli aggiornamenti dinamici, reflow responsive e contrasto migliorato nel tema
chiaro. Le regressioni vengono controllate con axe-core e Playwright:

```bash
cd uiweb/end2end
npm ci
npm run test:a11y
```

La suite automatica attuale copre le sezioni principali della UI con 12 test di
accessibilità. Questo non costituisce da solo una certificazione normativa:
servono ancora test manuali con screen reader, tastiera e tecnologie assistive.
Consulta l'[analisi di accessibilità](accessibility-analysis.md) per ambito e
limitazioni note.

## Primo avvio sicuro

1. Apri `http://<server>:5000` e completa il setup.
2. Configura percorsi, una sorgente e, se necessarie, credenziali TMDB/TVDB.
3. Mantieni il **dry-run**, aggiungi un titolo di prova ed esegui una ricerca o
   un ciclo.
4. Controlla **Salute** e **Log**, inclusi permessi filesystem e risultati della
   sorgente.
5. Abilita la modalità attiva solo quando l'esito è corretto.

Checklist, configurazione NAS e diagnostica sono nel
[manuale utente](docs/MANUAL.it.md).

## Aggiornamento

Per l'installazione ufficiale ripeti l'installer. Se disponibile, verifica il
checksum della release e sostituisce il payload in modo atomico, senza toccare
dati e configurazione.

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

Da un checkout sorgente:

```bash
git pull --ff-only
./scripts/update.sh
curl -fsS http://127.0.0.1:5000/api/health
```

Lo script di aggiornamento esegue la build versionata, riavvia il servizio
rilevato e lascia intatti dati e configurazione. Usa
`./scripts/update.sh --no-restart` se vuoi riavviare manualmente.

## Documentazione

| Esigenza | Documento |
|---|---|
| Usare il servizio | [Manuale italiano](docs/MANUAL.it.md) · [English manual](docs/MANUAL.en.md) |
| NAS, reverse proxy, ripristino | [Guida avanzata](docs/ADVANCED.it.md) |
| Integrazioni HTTP | [Riferimento API](docs/API.md) |
| Migrare un'installazione | [Guida migrazione](docs/MIGRATION.md) |
| Client da terminale | [Riferimento TUI](docs/tui.md) |
| Compilare o contribuire | [Manuale sviluppatori](docs/DEVELOPERS.md) |
| Rete e protezione dati | [Politica di sicurezza](docs/SECURITY.md) |
| Ambito e test accessibilità | [Analisi accessibilità](accessibility-analysis.md) |
| Sostituire Sonarr/Radarr | [Piano di sostituzione in inglese](docs/SONARR_RADARR_REPLACEMENT_PLAN.md) |

Per l'intera struttura documentale parti dall'[indice della documentazione](docs/README.md).

## Licenza

Gextto è distribuito con [EUPL-1.2](LICENSE). I componenti di terze parti sono
indicati in [NOTICE](NOTICE).
