# Gextto security policy

Gextto controls downloads and can rename, move or remove files as its service
user. Treat its web interface, API token and backups as administrative assets.

## Supported versions

Only the latest released (or continuous) build is supported. Fixes are published
in the next build; there are no long-term maintenance branches.

## Reporting a vulnerability

Please report security issues privately, not in a public issue. Open a
[GitHub security advisory](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities)
for this repository (Security → Report a vulnerability) with:

- the affected build (`gexttod --version` prints version and build number);
- a description and, when possible, a proof of concept;
- the impact you expect.

You will get an acknowledgement as soon as possible. Please allow a reasonable
delay before any public disclosure.

## Network exposure

Gextto is meant for a trusted LAN and its access control is **off by default**:
anyone who reaches the web port can use it. Treat the port as an administrative
interface.

- **Optional login** (*Configuration → Access and services*): when enabled, clients outside
  the local network must log in (bcrypt password, signed 30-day session cookie;
  a new password ends every session) or send the API key (`X-Api-Key` header or
  `?apikey=`). Five wrong passwords from one address block further attempts for
  a minute and every failure is logged. Access control stays open, with a
  warning, until a password or a key is set; `GEXTTO_AUTH_DISABLE=1` turns it
  off to recover from a lock-out.
- **Local network exemption** (on by default): loopback, private (RFC 1918, IPv6
  ULA) and link-local clients need no login. Behind a reverse proxy every
  address in `X-Forwarded-For`, `X-Real-IP` and `Forwarded` must be local too, so
  Internet clients that come through a proxy on the same host still have to
  authenticate. Turn the exemption off if your LAN is not trusted.
- **Use TLS for remote access**: Gextto serves plain HTTP. Put a reverse proxy
  with a certificate (or a VPN) in front of it, otherwise passwords and keys
  travel in clear text.
- **Restrict network access** with a firewall when the UI must be reachable from
  another machine.

> [!WARNING]
> Do not expose port 5000 directly to the public Internet, with or without the
> login.

## What the daemon deliberately does

- **No implicit shell.** The daemon's own invocations of `megadl`, `ffprobe`,
  `systemctl` and `apt-cache` use `exec.Command` with explicit arguments and no
  shell. **Event hooks are the exception**: a hook runs any absolute-path
  executable you configure, so editing hooks is equivalent to granting command
  execution as the service user. Only define hooks you trust, and treat the
  ability to change them as root-equivalent.
- **Parameterised SQL.** Every user value is bound as a query parameter; dynamic
  SQL is limited to internal literals and clamped integers.
- **No unverified TLS skipping.** The daemon never sets
  `InsecureSkipVerify`; certificates are validated normally.
- **No secrets in logs.** Tokens, passwords and query-string keys are redacted
  (`utils.RedactURLSecrets`) before logging.
- **Bounded requests.** The HTTP server has header/read/idle timeouts and a body
  size cap; the probe of a media file runs `ffprobe` with a timeout and a
  local-only protocol whitelist, and refuses paths that start with `-`.
- **Atomic self-update.** `gexttod --update` downloads the release archive,
  verifies the published `.sha256` when present, stages the payload and swaps it
  with renames; a failure leaves the running installation untouched.

## Operational responsibilities

- **Gextto can write to the filesystem as the user that runs it.** Paths you
  configure (library/NAS root, download/temp folders, tag-directory rules,
  archive paths) are used verbatim for renaming and moving files. Only configure
  paths you trust, and run the daemon as a dedicated unprivileged account.
- **External URLs are fetched by the server.** Archive download links, comic
  pages, FlareSolverr requests and manually added torrent URLs (`send-magnet`,
  `archive/add`, `search/add`) are retrieved by the daemon. Because Jackett
  and Prowlarr commonly run on the same host or LAN, private/loopback URLs are
  *not* blocked by default: protect the token and the network instead of
  relying on an SSRF filter.
- **The configuration API returns integration keys in cleartext.** `GET
  /api/config` includes indexer, TMDB and TVDB keys. Anyone who can call that
  endpoint can read them; protect the port as described above. The login
  password is stored only as a bcrypt hash and is never returned; the API key
  is stored as configured.
- **Event hooks and integrations receive your data.** A webhook payload, a
  Telegram message or a hook script contains titles, paths and hashes. Review
  the destinations you configure.
- **Backups contain configuration and databases**, including stored tokens for
  the integrations you enabled. Store backup archives accordingly.
- **`ffprobe` is optional**; when present it runs as the daemon user on local
  media files only.
