# Archivio della documentazione

Report di analisi e piani **conclusi**, conservati per riferimento. Il documento
di lavoro unico su migliorie ed evoluzione è
[`../evoluzione.md`](../evoluzione.md); i file che consolida restano qui solo
come **copie locali non tracciate in git** (vedi `.gitignore` e la sua
Appendice A). Non descrivono lo stato attuale del software: per la
documentazione corrente vedi l'indice in [`../README.md`](../README.md).

| Documento | Cos'era | Perché è archiviato |
|---|---|---|
| `gextto-terra.md` | Rapporto tecnico Terra: miglioramenti proposti | interventi fatti o esclusi; consolidato in `../evoluzione.md` §10.1 |
| `pro-terra.md` | Rapporto tecnico Pro Terra: sicurezza, concorrenza, lifecycle | interventi fatti; consolidato in `../evoluzione.md` §10.2 |
| `revisione-2.md` | Revisione tecnica al commit `97142c1` | fotografia datata; consolidata in `../evoluzione.md` §9 |
| `UI_V2.md` | Report dello stato della UI SSR + HTMX (build 1229) | report datato; consolidato in `../evoluzione.md` §11 |
| `revisione-1.md` | Revisione tecnica su client, i18n, job, concorrenza | consolidata in `../evoluzione.md` §8 |
| `proposte-migliorie.md` | Migliorie Gextto proposte e implementate | consolidate in `../evoluzione.md` §2 |
| `da-fare.md` | Backlog dei punti aperti | consolidato in `../evoluzione.md` §1 |
| `lavoro-sessione.md` | Handover tra sessioni di lavoro | consolidato in `../evoluzione.md` §12 |
| `scelte-di-progetto.md` | Posizionamento e direzione strategica | consolidata in `../evoluzione.md` §3 |
| `accessibility-analysis.md` | Analisi di accessibilità della UI web | consolidata in `../evoluzione.md` §13 |
| `holepunch-test-harness.md` | Harness di test holepunch (BEP 55) | consolidato in `../evoluzione.md` §7 |
| `gx-torrent-migliorie.md` | Lacune e piano di miglioramento gx-torrent | consolidati in `../evoluzione.md` §4 |
| `gx-torrent-ottimizzazioni-prestazionali-v2.md` | Piano di ottimizzazioni prestazionali | consolidato in `../evoluzione.md` §5 |
| `gx-torrent-misure-seeding.md` | Misure di choking e seeding | consolidate in `../evoluzione.md` §6 |

## Documenti rimossi in questa pulizia (2026-10-08)

Restano nella storia di git, non nel working tree:

- `gextto-torrentd.md` — proposta di un demone torrent autonomo mai avviata, con
  backend Anacrolix non più incluso;
- `rain-aggiornamento-audit.md` — audit una tantum completato, superato da
  [`../rain-allineamento.md`](../rain-allineamento.md);
- `gx-torrent-ottimizzazioni-prestazionali.md` (v1) — analisi prestazionale
  superata da [`../evoluzione.md`](../evoluzione.md) (§5).

Restano volutamente fuori dall'indice perché locali e non pubblicati (già in
`.gitignore`): `docs/internal/` e i file della tabella sopra (citati da commenti
nel codice solo tramite `../evoluzione.md`). Il vecchio piano di adozione
`SONARR_RADARR_REPLACEMENT_PLAN.md` è stato rimosso: le parti durevoli sono in
[`../evoluzione.md`](../evoluzione.md) (§3).
