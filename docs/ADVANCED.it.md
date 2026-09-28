# Manuale per utenti avanzati

Questa guida raccoglie le configurazioni utili quando Gextto viene eseguito su
un NAS, su più macchine o dietro un reverse proxy. Per l'uso normale consulta il
[manuale utente](MANUAL.it.md).

## Percorsi e permessi

Il servizio deve poter leggere e scrivere download, archivio, cestino e
directory dati. Verifica sempre i percorsi dal pannello **Salute** prima di
abilitare i download. Un percorso visto dal client ma non dal demone non è
utilizzabile.

Con un NAS controlla UID/GID, mount all'avvio e permessi di lettura/scrittura.
Dopo aver spostato una libreria usa **Manutenzione → Scansiona archivi** prima
di avviare upgrade o ricerca episodi mancanti.

## Rete e sicurezza

Gextto non include autenticazione utente. Se la UI è raggiungibile da un'altra
macchina, usa firewall o reverse proxy con HTTPS e autenticazione. Per accesso
locale preferisci `127.0.0.1:5000`.

Non esporre direttamente la porta amministrativa su Internet. Per i dettagli
vedi [Sicurezza](SECURITY.md).

## Backend torrent

- **libtorrent integrato**: predefinito, senza servizio esterno;
- **qBittorrent-nox**: utile quando il motore torrent deve vivere separato;
- **anacrolix**: backend Go alternativo, da usare solo dopo aver verificato
  compatibilità e prestazioni.

Un torrent appartiene a un solo backend alla volta. Prima di cambiare backend
metti in pausa i download e verifica i percorsi condivisi o le mappature NAS.

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

## Diagnostica rapida

Controlla nell'ordine:

1. `curl -fsS http://127.0.0.1:5000/api/health`;
2. stato e permessi dei percorsi nel pannello **Salute**;
3. sorgenti e indexer con il pulsante **Verifica**;
4. log filtrati per `ERROR`, `WARN`, `torrent` o `ffprobe`;
5. stato del servizio con `systemctl` o `systemctl --user`.

Non cancellare database o torrent state per risolvere un errore senza prima
fare un backup.
