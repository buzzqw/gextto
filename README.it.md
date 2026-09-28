# Gextto

Gextto è un demone self-hosted per cercare, scaricare e archiviare
automaticamente serie TV, film e fumetti. La gestione avviene dalla UI web.

> English: [`README.md`](README.md) · Manuale completo:
> [`docs/MANUAL.it.md`](docs/MANUAL.it.md)

## Cos'è Gextto

Gextto automatizza il ciclo completo della libreria: cerca le release, sceglie
quella migliore, scarica, controlla il file, lo rinomina e lo archivia nel
percorso configurato.

## Perché scegliere Gextto

- **Un solo servizio**: UI, database, ricerca, coda torrent e archiviazione
  lavorano insieme, senza un orchestratore esterno.
- **Torrent integrati**: libtorrent è incluso e non richiede un servizio
  aggiuntivo; sono disponibili anche qBittorrent-nox e anacrolix.
- **Scelte basate sulla qualità**: risoluzione, sorgente, codec, audio, HDR,
  lingue e dimensione vengono valutati prima del download e degli upgrade.
- **Controllo reale dei file**: `ffprobe`/MediaInfo verificano codec, audio,
  HDR e lingue del file effettivo, non solo il nome della release.
- **Serie, film e fumetti**: metadati TMDB/TVDB, episodi mancanti, calendario,
  ricerca manuale, rinomina e percorsi NAS.
- **Gestione semplice**: interfaccia web responsive in italiano e inglese,
  backup, log, salute del servizio e integrazioni con Trakt, Simkl, Jellyfin e
  Plex.

## Installazione

### Installazione ufficiale su Linux

Su un server Linux 64 bit con systemd, esegui l'installer come **root** (tramite
`sudo` oppure da una shell root):

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

L'installer configura il servizio e conserva dati e configurazione in
`/var/lib/gextto`. La UI sarà disponibile su `http://<server>:5000`.

### Installazione da codice sorgente

Questa modalità è descritta nel [manuale per sviluppatori](docs/DEVELOPERS.md).

## Aggiornamento

### Installazione ufficiale

Ripeti il comando di installazione:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

### Installazione da repository

```bash
cd /percorso/del/gextto
git pull --ff-only
./scripts/update.sh
```

Lo script compila l'ultima versione e riavvia il servizio. Database,
configurazione, download e archivi non vengono cancellati.

Verifica che il servizio sia attivo:

```bash
curl -fsS http://127.0.0.1:5000/api/health
```

## Primo avvio

1. Apri la UI web e completa il setup iniziale.
2. Configura percorsi, sorgenti e chiavi TMDB/TVDB.
3. Lascia attiva la modalità dry-run finché la configurazione non è verificata.
4. Abilita la modalità attiva quando vuoi iniziare i download.

Per la configurazione dettagliata consulta il
[manuale utente](docs/MANUAL.it.md).

## Cosa offre

- gestione di serie TV, film e fumetti;
- ricerca automatica e manuale delle release;
- torrent con libtorrent integrato o backend alternativi;
- rinomina, archiviazione e controllo reale dei file multimediali;
- ricerca episodi mancanti, calendario, backup e manutenzione;
- integrazioni con Trakt, Simkl, Jellyfin, Plex e notifiche.

## Documentazione

- [Manuale utente italiano](docs/MANUAL.it.md)
- [English user manual](docs/MANUAL.en.md)
- [API HTTP](docs/API.md)
- [Migrazione da un'installazione esistente](docs/MIGRATION.md)
- [Sicurezza](docs/SECURITY.md)
- [TUI da terminale](docs/tui.md)
- [Manuale utenti avanzati](docs/ADVANCED.it.md)
- [Manuale sviluppatori](docs/DEVELOPERS.md)

## Licenza

Vedi [LICENSE](LICENSE).
