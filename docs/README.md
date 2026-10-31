# Gextto documentation

This folder holds the documentation maintained together with the code. Start
from the manual in the language you prefer; the other documents cover specific
needs.

These same files are published as a site at
<https://buzzqw.github.io/gextto/>: the pages are built from `docs/` at every
change, so this index and the site are never two different texts.

**Quick links:** [Manuale italiano](MANUAL.it.md) ·
[English manual](MANUAL.en.md) · [Visita guidata](TOUR.it.md) ·
[Guided tour](TOUR.en.md) · [HTTP API](API.md) ·
[Changelog](https://github.com/buzzqw/gextto/blob/main/changelog.md) ·
[changelog.en.md](https://github.com/buzzqw/gextto/blob/main/changelog.en.md)

| Document | When to use it |
|---|---|
| [Manuale italiano](MANUAL.it.md) | Setup and day-to-day use of the web UI. |
| [English manual](MANUAL.en.md) | English version of the complete user guide. |
| [Manuale gx-core (Italiano)](GXCORE-MANUAL.it.md) | gx-core senza Gextto: avvio Docker, torrent, Serie e film, profili di qualità, RSS, statistiche e integrazioni, con schermate reali. |
| [gx-core manual](GXCORE-MANUAL.en.md) | English version: gx-core without Gextto, Docker start, torrents, Series & movies, quality profiles, RSS, statistics and integrations, with real screenshots. |
| [Visita guidata](TOUR.it.md) | Screenshots of the main screens, generated with `make screenshots`. |
| [Guided tour](TOUR.en.md) | English version of the screenshot tour. |
| [Changelog (italiano)](https://github.com/buzzqw/gextto/blob/main/changelog.md) | What changed for users, day by day. |
| [Docker: stack completo (Italiano)](DOCKER.it.md) | Gextto, gx-torrent, Jackett, Prowlarr, FlareSolverr e aMule in un solo `docker compose`; avvio, configurazione, aggiornamento e problemi comuni. |
| [Docker: full stack](DOCKER.en.md) | English version: Gextto, gx-torrent, Jackett, Prowlarr, FlareSolverr and aMule in one `docker compose`; start, configuration, updating and troubleshooting. |
| [Changelog (English)](https://github.com/buzzqw/gextto/blob/main/changelog.en.md) | English version of the user-facing changelog. |
| [Chiavi di accesso](api-keys.it.md) | The Gextto API key and the gx-torrent token: what they are, how to generate and use them. |
| [Access keys](api-keys.md) | English version: the Gextto API key and the gx-torrent token. |
| [Guida avanzata (Italiano)](ADVANCED.it.md) | NAS, permissions, proxy, backup and diagnostics. |
| [Advanced guide](ADVANCED.en.md) | English advanced-operation guide. |
| [HTTP API](API.md) | The route tables of the Gextto daemon and of the gx-torrent daemon (`/api/v1` and `/api/v2`) for automations and integrations. |
| [Migration](MIGRATION.md) | A safe move from a previous install. |
| [qBittorrent-nox for Gextto](qbittorrent-nox.md) | How to install and configure the qBittorrent-nox backend: Web UI, settings, path mapping, what Gextto drives, managed mode, switching, troubleshooting and the capability gaps. |
| [Security](SECURITY.md) | Network exposure, secrets and vulnerability reporting. |
| [Accessibility](ACCESSIBILITY.md) | Keyboard, screen reader, automated axe tests and the manual checklist for the web UI, the gx-torrent page and the TUI. |
| [TUI](tui.md) | Terminal client and keyboard shortcuts. |
| [Developers](DEVELOPERS.md) | Build, tests and contributor conventions. |
| [Build and test times](tempistiche.md) | What builds and the test suite cost on a real machine, and how long CI takes. |
| [Architecture](ARCHITECTURE.md) | Map of the code folders and files. |
| [State and backlog](evoluzione.md) | What Gextto and the gx-torrent / gx-core engine have today and what is planned next (positioning, capabilities, engine, open items). |
| [Measurements and verification](measurements.md) | How each number is taken: harnesses, benchmarks and footprint, swarm, DHT, merge on real data, accessibility verification. |
| [gx-torrent engine](gx-torrent.md) | Technical document of the pure-Go torrent engine (gx-core, daemon, adapter). |
| [eD2k e Kad con aMule (Italiano)](amule.it.md) | Italian version: install and configure aMule, enable it in Gextto, use eD2k/Kad, troubleshooting and settings. |
| [eD2k and Kad through aMule](amule.md) | How to install and configure aMule, enable it in Gextto and use eD2k/Kad: search, links, aMule's queue in Downloads, completion, shared limits, troubleshooting, settings and tests. |
| [BEP compliance](bep.md) | Which BitTorrent specifications (BEPs) the engine supports, which are missing and the roadmap. |
| [gx-core](gx-core.md) | The standalone torrent client (the `gx-torrent` binary used without Gextto): modes, security, packaging, what is still open. |
| [SQL maintenance scripts](https://github.com/buzzqw/gextto/blob/main/scripts/sql/README.md) | Ready-to-run queries and the Maintenance tab for the release archive: old/language-filtered releases, diagnostics and their manual deletion. |

> [!NOTE]
> The paths, ports and commands here are examples. Always check the service
> user, the NAS mount and the permissions in your real install.
