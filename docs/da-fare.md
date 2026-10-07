# Da fare (backlog)

Punti aperti, in ordine di rilevanza. Aggiornato: 2026-10-07.

## 1. Write-back cache nel fork di rain (gx-torrent)
Far **usare più RAM** a gx-torrent quando lo storage è lento (HDD/NFS).
Serve una vera cache write-back nel fork: tenere i pezzi verificati in memoria
e scriverli in blocco, con **flush su stop/verify/move** e gestione sicura del
resume (un pezzo non scritto non deve risultare completo). Oggi rain è
write-through: `WriteCacheSize` è un tetto sui pezzi *in volo*, quindi la RAM
resta bassa anche alzando i cap (misurato). Cambiamento delicato.

## 2. Cap cache su storage lento (NFS)
La misura (web seed locale veloce → scrittura NFS, cap 2 GB, 8 download) ha
mostrato `write_cache=0`: la coda non si riempie perché storage e download
vanno di pari passo. Decisione: **alzare comunque** il cap come rete di
sicurezza, o **chiudere** il punto (nessun beneficio osservato).

## 3. Verifica live del flusso RAM disk con gx-torrent
Dopo il cambio di staging (`resolveSavePath` condivisa), gx-torrent parte su
RAM disk/temp. Da provare un download reale che **attraversi il RAM disk e lo
spostamento** a disco (copy+remove cross-filesystem), con seed ripreso.
Test mirato proposto con **Big Buck Bunny** (piccolo, entra nella soglia).
Nota: i link di download Blender/Google per BBB hanno dato 404 — serve una
fonte (torrent WebTorrent di BBB, oppure file fornito localmente).

## 4. Script di benchmark RAM riproducibile
Committare in `scripts/` (o `docs/`) lo script usato per la misura di memoria
dei motori (workload da `torrent-done`, web seed locale Range, staging locale vs
NFS), così è ripetibile senza ricrearlo ogni volta.

## 5. Log: unificare "sostituzione" e "aggiunto alla libreria"
Oggi due righe ravvicinate e ridondanti:
- `cleaner.go`: `🗑️ Replaced with a better version: «nuovo»; the old file «vecchio» is in the trash`
- `web_torrent_events.go`: `📁 <serie> <ep> added to the library (X GB): <path>`

Vanno fuse in **una sola riga parlante** che dica: episodio aggiornato, versione
precedente spostata nel cestino, dimensione e percorso finale.

---

### Fatto di recente (per contesto)
- Filtro dei log nella UI: scattava solo al submit perché il trigger `input
  changed` era sul `<form>` (htmx valuta `changed` sul valore dell'elemento).
  Ora usa `from:#v2-log-filter` e ha il pulsante "Filtra".
- Impostazioni gx-torrent "Auto (gestito)": con `gxtorrent_auto` attivo, cache e
  coda si mostrano come gestite (non editabili).
