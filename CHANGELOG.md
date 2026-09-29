# Changelog

All notable changes to `aoc_api`. Every production deploy is a release with a semver bump, a section
here, a git tag and a GitHub release carrying that section (CLAUDE.md rule 15).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project uses
[semantic versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-09-29

The first tagged release: everything built since the repo was created, shipped as one deploy.

### Added

- The application skeleton — one Go binary with chi, `/v1` as a versioned sub-router, `GET /health`
  (version, commit, environment), request-id, logging and panic-recovery middleware, one central
  error mapping with `http.Error` banned, and graceful shutdown (AOC-002)
- CI on every pull request: vet, lint and the tests against a real Postgres, failing when the
  database tests skip instead of passing on nothing (AOC-003)
- A pinned Dockerfile build for Railway — one service, one Postgres, one environment — with
  `/health` reporting which environment answered (AOC-004)
- Migrations (goose) and typed queries (sqlc), always run against local Postgres first (AOC-005)
- The production-access runbook and a rehearsed restore, `docs/runbook-restore.md` (AOC-006)
- The Cloudflare R2 bucket for the tooltip images, public only through `img.aoc-codex.app` (AOC-007)
- The taxonomy tables and the place entities — regions, maps, places, bosses — as migrations and
  seeds (AOC-009)
- The item schema: items, stats, sources, costs, sets and vendors (AOC-010)
- The armory importer, loading the 4,646 items of the preserved AoC>TV snapshot (AOC-011)
- Public read endpoints `GET /v1/items` (filtered, paginated), `GET /v1/items/{slug}` and
  `GET /v1/taxonomies`, each carrying "Data preserved from AoC>TV by Kentarii" (AOC-012)
- `aoc-codex.app` as the one canonical host, with `www` redirecting at the edge (AOC-014)
- The HTML rendering foundation: embedded templates, HTMX, content-hashed assets and a smoke page
  (AOC-024)
- One cache policy for every response, so Cloudflare serves public pages from its edge while
  anything with a session, a cookie or an `Authorization` header is never stored; the zone now
  refuses TLS 1.0 and 1.1 (AOC-026)
- Automated daily off-site backups to R2, with an alarm when the newest one is stale, tiny or
  missing (AOC-030)
- The first production import — the schema and the 4,646 items in the live database (AOC-034)
- The importer runs inside Railway as its own service beside the database, not down an SSH tunnel
  (AOC-040)

### Changed

- The version `/health` reports comes from the repo's `VERSION` file, and the build fails on
  anything but `x.y.z`; this release joins four pull requests into one deploy (AOC-015)

### Fixed

- `img.aoc-codex.app` serves the tooltip images from a Cloudflare Worker reading R2 through a
  binding, instead of R2's custom domain, which stalled on cache misses at the Singapore edge
  (AOC-041)
- A source no longer repeats a geography its place already supplies, so an unchained place and its
  sources can no longer disagree about the region (AOC-037)
- The import refuses a snapshot holding under 90% of the corpus, before it deletes anything
  (AOC-042)
