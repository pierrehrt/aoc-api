# Changelog

All notable changes to `aoc_api`. Every production deploy is a release with a semver bump, a section
here, a git tag and a GitHub release carrying that section (CLAUDE.md rule 15).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project uses
[semantic versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Repo skeleton: Go module, package layout, and a `doc.go` in each `internal/` package saying what
  belongs in it (AOC-002)
- chi router with `/v1` mounted as a sub-router, ready for `/v2` to mount beside it (AOC-002)
- `GET /health` reporting version and commit, deliberately not under `/v1` and deliberately not
  touching the database (AOC-002)
- One central `error → status` mapping in `internal/httpx`; `http.Error` is banned (AOC-002)
- Request-id, panic-recovery and structured-logging middleware (AOC-002)
- Graceful shutdown on SIGTERM and the four HTTP timeouts `net/http` leaves unset (AOC-002)

### Fixed

- `img.aoc-codex.app` serves the armory tooltip images from a Cloudflare Worker reading R2 through
  a binding, instead of R2's custom domain, which stalled on cache misses at the Singapore edge
  (119 of 160 whole against the Worker's 160 of 160 on the same edge) (AOC-041)
- Middleware order: `Log` now wraps `Recover`, so a panicking request still produces an access line
  with its real 500. Under the previous order the panic unwound past `Log` and the request vanished
  from the log entirely (AOC-002)
- chi's 405 goes through the central error mapper instead of writing its own body, so there is
  genuinely one path to an error response and a 405 is logged like any other rejection (AOC-002)
- A panic *after* the response has begun no longer appends a second JSON document to a partial body
  (AOC-002)
- The status-recording `ResponseWriter` wrapper implements `Unwrap`, so `Flush`, `Hijack` and the
  deadline setters remain reachable downstream (AOC-002)
