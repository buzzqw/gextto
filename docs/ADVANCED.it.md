# Operatività avanzata di Gextto

Usa questa guida per NAS, installazioni su più macchine, reverse proxy e
ripristino. Per l'uso quotidiano consulta il [manuale utente](MANUAL.it.md).

> [!IMPORTANT]
> Prima di modificare storage o backend torrent, sospendi i cicli automatici
> (oppure usa il dry-run), crea un backup, verifica i percorsi con l'utente del
> servizio e prova una modifica alla volta.

## Percorsi e permessi

Il servizio deve poter leggere e scrivere download, archivio, cestino e
directory dati. Verifica sempre i percorsi dal pannello **Salute** prima di
abilitare i download. Un percorso visto dal client ma non dal demone non è
utilizzabile.

Con un NAS controlla UID/GID, mount all'avvio e permessi di lettura/scrittura.
Dopo aver spostato una libreria usa **Manutenzione → Scansiona archivi** prima
di avviare upgrade o ricerca episodi mancanti. La scansione gira in background:
puoi proseguire, l'esito appare quando finisce.

**Hardlink e spazio.** Un file che resta in seed entra in libreria come
hardlink solo se la cartella dei download e la libreria stanno sullo stesso
filesystem (stesso disco o stessa condivisione NAS). Con i download sul disco
locale e la libreria sul NAS Gextto copia, e lo scrive una volta nel log: se vuoi
risparmiare spazio metti la cartella dei download sulla stessa condivisione della
libreria. Se Jellyfin o Plex girano in Docker e vedono la libreria sotto un altro
percorso, compila *Jellyfin/Plex — mappatura percorsi* (`percorso_gextto=percorso_server`)
perché possano aggiornare solo la cartella cambiata.

## Rete e sicurezza

Gextto è pensato per una LAN fidata e per impostazione predefinita è aperto.
Quando possibile ascolta su `127.0.0.1:5000` o solo sulla rete di casa. Per
l'accesso da fuori:

1. metti davanti un reverse proxy HTTPS (Caddy, nginx, Traefik) o usa una VPN
   come Tailscale o WireGuard;
2. attiva *Configurazione → Accesso e servizi → Richiedi l'accesso (login)* e imposta una
   password (e, se ti serve per script o calendario, una chiave API);
3. lascia attivo «Nessun login dalla rete locale»: dalla LAN non cambia nulla.

Il proxy deve passare l'indirizzo del client in `X-Forwarded-For` (nginx:
`proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`, Caddy lo fa da
sé): Gextto lo usa per distinguere chi arriva da Internet. Se resti chiuso fuori,
avvia il servizio con `GEXTTO_AUTH_DISABLE=1`.

Non esporre direttamente la porta amministrativa su Internet. Per i dettagli
vedi [Sicurezza](SECURITY.md).

## Backend torrent

- **libtorrent integrato**: predefinito, senza servizio esterno;
- **qBittorrent-nox**: utile quando il motore torrent deve vivere separato;
  compatibilità e prestazioni.

Un torrent appartiene a un solo backend alla volta. Prima di cambiarlo metti in
pausa i download, esegui il preflight e verifica percorsi condivisi o mappature
NAS.

## MediaInfo e ffprobe

`ffprobe` è opzionale ma consigliato. Gextto analizza solo file video realmente
presenti e regolari; i riferimenti obsoleti nel database vengono ignorati. I
dati rilevati (codec, audio, HDR, lingue e risoluzione) vengono usati nei
confronti di qualità e upgrade.

Se il backfill segnala file mancanti, controlla il percorso archiviato, il mount
NAS e i permessi del servizio. Non creare file vuoti per soddisfare il database:
il probe deve lavorare sul media reale.

## Backup e ripristino

Un backup contiene database e configurazione, non i video né lo stato completo
della sessione torrent. Prima di ripristinare:

1. ferma o metti in pausa i cicli;
2. conserva una copia del database corrente;
3. verifica che i percorsi della libreria siano disponibili;
4. ripristina il backup;
5. esegui una scansione archivio e controlla **Salute**.

## Aggiornamento da sorgente

```bash
git pull --ff-only
./scripts/update.sh
```

Lo script ricompila e riavvia il servizio senza modificare i dati. Per
installazioni ufficiali usa invece l'installer descritto nel README.

## Diagnosi rapida

Controlla nell'ordine:

1. `curl -fsS http://127.0.0.1:5000/api/health`;
2. `curl -fsS http://127.0.0.1:5000/api/status`;
3. stato e permessi dei percorsi nel pannello **Salute**;
4. sorgenti e indexer con il pulsante **Verifica**;
5. log filtrati per `ERROR`, `WARN`, `torrent` o `ffprobe`;
6. stato del servizio con `systemctl` o `systemctl --user`.

Non cancellare database o torrent state per risolvere un errore senza prima
fare un backup.
