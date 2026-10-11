# gx-core engine

This is the BitTorrent engine of **gx-core**, the torrent daemon used by Gextto
and shipped standalone as `gx-torrent`.

It lives inside the main Gextto module as
`github.com/buzzqw/gextto/internal/gxcore` and is **our code**.

The public entry point is the package `torrent`
(`github.com/buzzqw/gextto/internal/gxcore/torrent`); the `internal/`
subpackages are implementation details.

- Engine overview and feature inventory: [`GEXTTO.md`](GEXTTO.md).
- How the daemon uses it (flags, API, configuration, limits):
  [`docs/gx-torrent.md`](../../docs/gx-torrent.md).
