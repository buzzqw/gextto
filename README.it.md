# Gextto

Gextto è un demone self-hosted per cercare, scaricare e archiviare
automaticamente serie TV, film e fumetti. La gestione avviene dalla UI web.

> English: [`README.md`](README.md) · Manuale completo:
> [`docs/MANUAL.it.md`](docs/MANUAL.it.md)

## Installazione

### Installazione ufficiale su Linux

Su un server Linux 64 bit con systemd:

```bash
curl -fsSL https://raw.githubusercontent.com/buzzqw/gextto/main/install.sh | sudo bash
```

L'installer configura il servizio e conserva dati e configurazione in
`/var/lib/gextto`. La UI sarà disponibile su `http://<server>:5000`.

### Installazione da codice sorgente

Questa modalità è descritta in [Sviluppo e installazioni locali](docs/DEVELOPMENT.md).

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
- [Sviluppo e installazioni locali](docs/DEVELOPMENT.md)

## Licenza

Vedi [LICENSE](LICENSE).
