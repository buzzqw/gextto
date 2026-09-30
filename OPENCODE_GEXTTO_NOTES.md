# Note operative Gextto

## Progetto

- Repository: `/home/andres/gextto`
- Binario di sviluppo: `/home/andres/gextto/bin/gexttod`
- Web UI/API: `http://127.0.0.1:5000`
- Porta engine: `127.0.0.1:8889`

## Build e test

```bash
cd /home/andres/gextto
go test ./...
make build
```

`make build` usa `scripts/build-daemon.sh`, aggiorna il numero di build e produce
`bin/gexttod`.

## Avvio e riavvio

In questo ambiente Gextto gira come **servizio systemd dell'utente**, non come
servizio systemd di sistema. Il comando corretto è:

```bash
systemctl --user restart gextto.service
systemctl --user status gextto.service --no-pager -l
systemctl --user is-active gextto.service
```

`systemctl restart gextto.service` (senza `--user`) cerca il servizio di sistema
e non è il comando corretto.

Per verificare che la nuova build risponda:

```bash
curl -fsS http://127.0.0.1:5000/api/status
```

Il file unit dell'utente è normalmente:
`~/.config/systemd/user/gextto.service`.

## UI e API

- Template principali: `uiweb/templates/pages.html` e `uiweb/templates/ui.html`.
- JavaScript UI: `uiweb/static/gextto-ui.js`.
- Dettagli serie: `uiweb_detail.go`.
- Route HTTP: `web_router.go`.
- Documentazione API: `docs/API.md`.
- Per le sorgenti di una puntata: `GET /api/episodes/{series}/{season}/{episode}/sources`.
