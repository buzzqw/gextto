# Archivio della documentazione

Report di analisi e piani **conclusi**, conservati per riferimento. Non
descrivono lo stato attuale del software: per la documentazione corrente vedi
l'indice in [`../README.md`](../README.md).

| Documento | Cos'era | Perché è archiviato |
|---|---|---|
| `gextto-terra.md` | Rapporto tecnico Terra: miglioramenti proposti | interventi fatti o esclusi |
| `pro-terra.md` | Rapporto tecnico Pro Terra: sicurezza, concorrenza, lifecycle | interventi fatti |
| `revisione-2.md` | Revisione tecnica al commit `97142c1` | fotografia datata |
| `UI_V2.md` | Report dello stato della UI SSR + HTMX (build 1229) | report datato |
| `da-fare.md` | Backlog di spunti e desiderata | non è stato del progetto |

## Documenti rimossi in questa pulizia (2026-10-08)

Restano nella storia di git, non nel working tree:

- `gextto-torrentd.md` — proposta di un demone torrent autonomo mai avviata, con
  backend Anacrolix non più incluso;
- `rain-aggiornamento-audit.md` — audit una tantum completato, superato da
  [`../rain-allineamento.md`](../rain-allineamento.md);
- `gx-torrent-ottimizzazioni-prestazionali.md` (v1) — analisi prestazionale
  superata da
  [`../gx-torrent-ottimizzazioni-prestazionali-v2.md`](../gx-torrent-ottimizzazioni-prestazionali-v2.md).

Restano volutamente fuori dall'indice perché locali e non pubblicati (già in
`.gitignore`): `docs/internal/`, `docs/revisione-1.md` (citato da commenti nel
codice) e `docs/SONARR_RADARR_REPLACEMENT_PLAN.md`.
