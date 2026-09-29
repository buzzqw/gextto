# Sonarr/Radarr replacement plan

## Goal

Make Gextto a safe, operational replacement for Sonarr and Radarr for a
typical self-hosted TV and movie library, while preserving Gextto's additional
comic support and its single-daemon architecture.

The goal is **functional and operational parity**, not a superficial feature
checklist. A migration must preserve monitored titles, existing files,
download state, quality rules, history and integrations, and it must remain
reversible until the new installation has completed a representative trial.

Gextto should not be marketed as a Sonarr/Radarr replacement until the exit
criteria in this document are met.

## Current baseline

Gextto already provides important building blocks:

- series, movies and comics in one service;
- embedded libtorrent as the default torrent backend;
- qBittorrent and optional alternative backend support;
- feed and Torznab indexer search;
- quality, source, codec, audio, HDR, language and size-based selection;
- missing-title search, upgrades, post-processing, naming and archive scans;
- Trakt, Simkl, Jellyfin and Plex integrations;
- web UI, terminal TUI, health checks, backups and maintenance operations;
- a source migration command that copies Gextto-compatible databases without
  modifying the source installation.

The existing migration command is not yet a Sonarr/Radarr importer. Sonarr and
Radarr databases, quality profiles, custom formats, history, queue state,
indexer configuration and download-client settings need an explicit import
path and validation report.

## Non-negotiable requirements

Before recommending a full cutover, Gextto must provide:

1. **A reversible migration.** The original Sonarr/Radarr data remains
   untouched and rollback is documented and tested.
2. **Correct library identity.** Existing series, seasons, episodes and movies
   are matched by stable provider IDs where possible, not only by filenames.
3. **Predictable download ownership.** One daemon must own each monitored title
   and download job during the trial; duplicate downloads must be prevented.
4. **Equivalent selection policy.** Quality profiles, cutoff behavior, language,
   exclusions, custom scoring and upgrade rules must have an explicit mapping.
5. **Safe failure behavior.** Network failures, indexer failures, incomplete
   files, restarts and unavailable NAS mounts must not cause silent data loss or
   destructive imports.
6. **Secure administration.** The unauthenticated administrative UI must be
   protected by built-in authentication or a documented, tested authenticated
   reverse-proxy deployment before remote use.
7. **Operational evidence.** The system must pass automated tests, migration
   fixtures, failure-injection tests and a real-world pilot.

## Work phases

### Phase 0 — Baseline and compatibility matrix

Create a fixture installation containing representative Sonarr and Radarr
data:

- single episodes, season packs and multi-season series;
- movies with multiple editions, upgrades and alternate titles;
- missing files, rejected files, imported files and failed downloads;
- non-English audio, subtitles, HDR, 4K and different codecs;
- custom formats, quality profiles, cutoff unmet titles and exclusions;
- multiple indexers, download clients, tags and notifications;
- local storage and a NAS mount with realistic permissions.

For every current Sonarr/Radarr behavior, record one of:

- supported directly by Gextto;
- mapped to an equivalent Gextto setting;
- intentionally unsupported with a documented workaround;
- a release blocker.

Deliverable: a versioned compatibility matrix and a list of P0/P1 gaps.

### Phase 1 — Safety, identity and migration foundation (P0)

#### 1.1 Stable media identity

- Store and preserve TVDB, TMDB, IMDb and other provider IDs where available.
- Define canonical identity rules for series, seasons, episodes, movies and
  movie files.
- Handle renamed titles, aliases, year changes, specials and split/merged
  series explicitly.
- Make identity conflicts visible in a migration report instead of guessing.

#### 1.2 Sonarr/Radarr importers

Implement read-only importers for supported Sonarr and Radarr versions. They
should import into a new Gextto data directory and never modify the source:

- monitored series, seasons, episodes and movies;
- existing file paths and media metadata;
- provider IDs, tags, notes and disabled/ignored items;
- quality profiles, cutoff and language preferences;
- indexers and download-client settings, excluding or separately prompting for
  secrets;
- history and failure information where it has operational value;
- queue/download state only where the state can be represented safely.

The importer must support dry-run, structured warnings, an error count and a
machine-readable report. Secrets must never appear in logs or reports.

#### 1.3 Existing library reconciliation

- Scan the destination library independently of the imported database.
- Match files to imported titles using IDs, paths, parsed names and media
  metadata in that order of confidence.
- Report unmatched, ambiguous, duplicate and missing files.
- Make archive and database updates transactional or restart-safe.
- Provide an explicit operator confirmation before destructive cleanup.

#### 1.4 Backup and rollback

- Create a pre-import backup and record checksums of source databases/configuration.
- Add an import manifest with source versions, timestamps, mappings and warnings.
- Test a rollback using only the original Sonarr/Radarr installation.
- Document how to run both systems on separate ports during the trial.

Exit criteria: a clean fixture can be imported with zero unresolved P0 identity
errors, and every warning is actionable.

### Phase 2 — Acquisition and release-selection parity (P0/P1)

#### 2.1 Indexers and download clients

- Import and validate Torznab indexers, feed sources, API keys and timeouts.
- Provide a safe test for every imported indexer before enabling acquisition.
- Support embedded libtorrent and qBittorrent with clear ownership rules.
- Preserve labels/tags, category behavior, save paths, seeding limits and
  incomplete-download paths where applicable.
- Add retry/backoff classifications that distinguish timeout, cancellation,
  authentication, rate limiting, Cloudflare and permanent HTTP errors.

#### 2.2 Quality profiles and custom formats

- Map resolution, source, codec, audio, HDR, language and size constraints.
- Implement explicit cutoff and upgrade semantics for both episodes and movies.
- Provide a documented mapping for Sonarr/Radarr custom formats and scores.
- Add preview tooling: show why a release is accepted, rejected or preferred.
- Preserve per-title overrides and exclusions.

#### 2.3 Missing searches and upgrades

- Match Sonarr/Radarr monitored/missing/cutoff-unmet states.
- Support scheduled and manual missing searches with cancellation and progress.
- Support upgrade searches without repeatedly selecting the same release.
- Make season packs, specials, movie editions and multi-file releases safe.
- Add idempotency tests for repeated cycles and daemon restarts.

Exit criteria: the fixture matrix produces the same accept/reject/upgrade
decisions as the documented mapping, or every difference is deliberate and
visible to the operator.

### Phase 3 — Import, post-processing and library correctness (P0/P1)

- Validate completed files before archive/import: existence, non-empty content,
  media type and expected title/episode/movie identity.
- Make moves/copies atomic or resumable across local and NAS filesystems.
- Preserve subtitles, sidecar files, hardlinks/copies and permissions according
  to an explicit policy.
- Make naming templates cover the common Sonarr/Radarr patterns and document
  unsupported tokens.
- Implement safe replacement of lower-quality files, including trash/rollback.
- Add recovery for interrupted imports and files disappearing during processing.
- Provide a reconciliation command that can rebuild state from the library.

Exit criteria: a restarted service never loses a completed import, creates no
duplicate episode/movie records and never deletes the only known copy without
an explicit policy allowing it.

### Phase 4 — Ecosystem and API compatibility (P1/P2)

#### 4.1 Notifications and media servers

- Verify Trakt/Simkl watchlist and scrobble behavior.
- Verify Jellyfin and Plex refresh behavior after imports and upgrades.
- Add the notification channels commonly used with Sonarr/Radarr, or document
  a stable webhook adapter.
- Include title, event type, quality, source, failure reason and links in
  notifications without exposing secrets.

#### 4.2 API and webhooks

- Define a versioned, documented Gextto API contract for integrations.
- Add event webhooks for grab, download, import, upgrade, failure and health.
- Provide compatibility adapters for important Sonarr/Radarr consumers such as
  request managers, dashboards and automation scripts where practical.
- Add authentication, authorization and token rotation before exposing write
  endpoints outside loopback.

Exit criteria: the selected external integrations work against a clean Gextto
installation without manually editing their databases.

### Phase 5 — Reliability, security and operations (P0)

- Add built-in authentication or publish a tested reverse-proxy reference with
  authentication, CSRF protection and secure cookie/token handling.
- Define permissions for read-only monitoring versus destructive operations.
- Add structured event IDs and correlation IDs across search, download and
  import logs.
- Expose metrics for queue depth, search duration, indexer failures, import
  failures, disk space and last successful cycle.
- Test shutdown and restart during every long-running operation.
- Test database corruption recovery, backup restore, NAS outage and read-only
  filesystems.
- Add clear retention policies for logs, history, trash and backups.
- Verify that API keys, cookies, paths and media names are redacted where
  appropriate.

Exit criteria: an operator can diagnose a failed cycle from Health, Logs and
the documented runbook without inspecting source code.

### Phase 6 — Test and release gate

Build a permanent test gate containing:

- unit tests for import mappings, identity, quality decisions and naming;
- golden Sonarr/Radarr database fixtures for every supported schema version;
- integration tests with fake indexers and fake download clients;
- filesystem tests for local, NAS-like and interrupted operations;
- restart and cancellation tests for searches and imports;
- API/webhook contract tests;
- end-to-end tests for the main UI workflows;
- accessibility tests and manual keyboard/screen-reader sign-off;
- race, leak, timeout and disk-space tests.

Every release candidate must publish its supported import versions, known
limitations, migration checksum and rollback instructions.

### Phase 7 — Staged migration and adoption

1. Select one non-critical library as a pilot.
2. Stop automatic acquisition in Sonarr/Radarr and take verified backups.
3. Import into an isolated Gextto data directory and run in dry-run mode.
4. Reconcile the library and resolve every ambiguous mapping.
5. Run manual searches and one scheduled cycle without enabling downloads.
6. Enable one indexer and one download backend, then observe a full cycle.
7. Enable the remaining sources and integrations gradually.
8. Keep the original installation stopped but intact through the observation
   window.
9. Expand to the full library only after the exit criteria pass.
10. Keep the rollback procedure tested and the original backups available.

## Definition of done

Gextto can be called a practical Sonarr/Radarr replacement only when all of the
following are true:

- supported Sonarr and Radarr data imports are repeatable, read-only and
  reversible;
- monitored state, existing files and provider IDs survive migration;
- quality, cutoff, missing-search and upgrade behavior is understood and
  covered by tests;
- downloads, post-processing, naming and replacements survive failures and
  restarts;
- required integrations and webhooks work without database-level hacks;
- administration is protected and secrets are handled safely;
- health, logs, metrics, backup and restore procedures are documented;
- a real pilot has completed without duplicate downloads, lost files or silent
  state divergence;
- the project publishes the remaining differences instead of claiming
  unqualified compatibility.

## Recommended implementation order

The highest-value sequence is:

1. authentication/security baseline;
2. stable identity and read-only Sonarr/Radarr import report;
3. library reconciliation and rollback;
4. quality/cutoff/custom-format mapping;
5. download/indexer parity and failure handling;
6. post-processing and upgrade hardening;
7. API/webhook and notification compatibility;
8. pilot migration and release gate.

This order prevents spending time on cosmetic parity before the two risks that
can cause irreversible damage—identity mistakes and unsafe file operations—are
controlled.
