# gx-core engine

This is the BitTorrent engine of **gx-core**, the torrent daemon used by Gextto
and shipped standalone as `gx-torrent`.

It was born from a copy of [`cenkalti/rain`](https://github.com/cenkalti/rain)
v2.4.2 (MIT) and now lives inside the main Gextto module as
`github.com/buzzqw/gextto/internal/gxcore`. It is **our code**, not a fork to be
rebased: the original MIT attribution stays in [`LICENSE`](LICENSE).

The public entry point is the package `torrent`
(`github.com/buzzqw/gextto/internal/gxcore/torrent`); the `internal/`
subpackages are implementation details.

- Inventory of our changes and how they differ from the upstream base:
  [`GEXTTO.md`](GEXTTO.md).
- How the daemon uses it (flags, API, configuration, limits):
  [`docs/gx-torrent.md`](../../docs/gx-torrent.md).
- Looking at upstream fixes by hand:
  [`docs/motore-allineamento.md`](../../docs/motore-allineamento.md).
