# Gextto access keys

Gextto protects its web interface and HTTP API with access control (optional:
the local network is open by default). Two **shared secrets** let a client that
has no browser session reach the daemon. Both are long random strings: keep them
private and treat them like an administrator password.

The UI generates them for you in *Configuration → Access and services → API
keys*: press **Generate** to replace the value with a fresh random one, then
**Copy** to put it in the clipboard.

## Gextto API key (`auth_api_key`)

What it is for:

- scripts and automations that call Gextto's HTTP API;
- the terminal client (TUI);
- calendar apps that subscribe to the iCal feed.

How to use it:

- header `X-Api-Key: <key>`; or
- query string `?apikey=<key>`;
- the TUI reads it from the environment variable `GEXTTO_API_KEY`.

It only matters while access control is on: with the login off (the default) the
API answers freely. Setting, changing or clearing the key never locks the
browser out — the password still works. The key is a *shared secret*: every
client uses the same one, so rotating it means updating every client (scripts,
the TUI, the calendar subscription).

## gx-torrent token (`gxtorrent_token`)

What it is for:

- the web page and the HTTP API of the gx-torrent daemon (`/api/v1` and the
  qBittorrent-compatible `/api/v2`), when the daemon is reachable on the local
  network.

How to use it: clients send the header `X-Gx-Token: <token>`.

It is only needed when the daemon listens beyond loopback. On the default
`127.0.0.1:8890` it is optional; if you expose the daemon (set
`gxtorrent_listen` to `0.0.0.0`) set a token, otherwise anyone on the network
can drive it. Gextto passes the token to the daemon when it starts it, so **a
new value applies after gx-torrent (or Gextto) restarts**; the interface says so
when it generates one.

## See also

- [Manual — Access and services](MANUAL.en.md#access-and-services)
- [Security](SECURITY.md)
- [HTTP API](API.md)
