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

### UI v2 (SSR + HTMX), sperimentale e isolata

- Accesso: `http://127.0.0.1:5000/v2` (la UI classica su `/` resta invariata).
- Codice: `uiweb_v2.go`, `uiweb_v2_table.go`, `uiweb_v2_sections.go`,
  `uiweb_v2_search.go`, `uiweb_v2_detail.go`, `uiweb_v2_settings_extras.go`,
  `uiweb_v2_maintenance.go`, `uiweb_v2_widgets.go`,
  `uiweb/v2/templates/v2.html`, `uiweb/v2/static/htmx.min.js`.
- Registrazione: una sola riga `registerV2Routes(s, mux)` in `web_router.go`.
- Report completo, copertura, benchmark e prossimi passi: `docs/UI_V2.md`.
- Tutte le 16 voci `/v2?view=…` rendono senza segnaposto di pagina; sono coperti
  anche dettagli Serie/Film, dettaglio torrent e flussi secondari Fumetti. I soli
  residui consapevoli sono documentati in `docs/UI_V2.md`.
