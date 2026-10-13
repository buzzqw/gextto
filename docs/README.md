# Gextto documentation

This folder holds the documentation maintained together with the code. Start
from the manual in the language you prefer; the other documents cover specific
needs.

| Document | When to use it |
|---|---|
| [Manuale italiano](MANUAL.it.md) | Setup and day-to-day use of the web UI. |
| [English manual](MANUAL.en.md) | English version of the complete user guide. |
| [Guida avanzata (Italiano)](ADVANCED.it.md) | NAS, permissions, proxy, backup and diagnostics. |
| [Advanced guide](ADVANCED.en.md) | English advanced-operation guide. |
| [HTTP API](API.md) | Automations and integrations that use the daemon. |
| [Migration](MIGRATION.md) | A safe move from a previous install. |
| [qBittorrent-nox for Gextto](qbittorrent-nox.md) | How to configure the qBittorrent-nox backend: settings, path mapping, examples and typical cases. |
| [Security](SECURITY.md) | Network exposure, secrets and vulnerability reporting. |
| [TUI](tui.md) | Terminal client and keyboard shortcuts. |
| [Developers](DEVELOPERS.md) | Build, tests and contributor conventions. |
| [Build and test times](tempistiche.md) | What builds and the test suite cost on a real machine, and how long CI takes. |
| [Architecture](ARCHITECTURE.md) | Map of the code folders and files. |
| [Design choices, improvements and evolution](evoluzione.md) | Active backlog, implemented improvements, measurements and strategic direction for Gextto and the gx-torrent / gx-core engine. |
| [gx-torrent engine](gx-torrent.md) | Technical document of the pure-Go torrent engine (gx-core, daemon, adapter). |
| [BEP compliance](bep.md) | Which BitTorrent specifications (BEPs) the engine supports, which are missing and the roadmap. |
| [gx-torrent evolved (gx-nox)](gx-torrent-evoluto.md) | Analysis and decision: gx-torrent as a standalone torrent client (one binary, two modes). |
| [Archive](archive/README.md) | Historical reports and plans, no longer maintained. |

> [!NOTE]
> The paths, ports and commands here are examples. Always check the service
> user, the NAS mount and the permissions in your real install.
