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
