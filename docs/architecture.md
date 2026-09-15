# aoc_api — architecture

> **This file describes the code as it IS, not as it is planned.** Every later api ticket cites it by
> path, so a change to routes, schema or package structure updates it in the same commit
> (`bin/docs-check api` enforces this).
>
> Created by **AOC-002**. Last updated 2026-09-14.

## What this service is

The backend for the Age of Conan Codex: a public read API over a preserved item database, and later
an authenticated write path for community suggestions with moderation. Go 1.23+ (`go.mod` declares `go 1.23` as the language floor; the local toolchain is
newer), chi, pgx, sqlc, goose, Postgres, deployed on Railway.

## Package layout

```
cmd/api/main.go        wiring ONLY — config, router, serve, shut down
internal/httpx/        the HTTP edge: middleware, error mapping, /health
internal/version/      build identity, injected at link time
internal/db/           pgx pool + sqlc output + queries/   (empty until AOC-005)
internal/items/        the Armory bounded area              (empty until AOC-010/012)
migrations/            goose files                          (empty until AOC-005)
docs/                  this file, api-routes.md, database-schema.sql
```

Each `internal/` package carries a `doc.go` saying what belongs in it and what does not. That is the
cheapest defence against the layout eroding into a pile of helpers, and it costs one file per package.

**A domain package owns its handlers, its service and its tests.** It does not own HTTP concerns
beyond a thin handler (those are `httpx`) or raw SQL (that is `internal/db/queries`).

## CI

`.github/workflows/ci.yml` runs on every pull request and on every push to `main`. A red build
blocks the **merge**, not the release (AOC-003).

Two jobs:

| job | steps | why |
|---|---|---|
| `check` | `make fmt-check`, `make lint`, `go build ./...`, `make test` | the same **make targets** a person runs, and the same ones `bin/gate api` runs in the `product_management` repo |
| `lint` | `go install golangci-lint@v2.13.2`, `golangci-lint config verify`, `golangci-lint run ./...` | required, not advisory |

**The contract: CI never duplicates a command list.** It calls `make`, because a copied list of
commands is how CI and the local gate drift until one of them is lying. Adding a check means adding
it to the `Makefile`; both callers get it for free.

**The Go version is read from `go.mod`** (`go-version-file`), never pinned twice.

**`golangci-lint` is installed with `go install` at a pinned version, not via a third-party action.**
The config schema is version-specific — `.golangci.yml` declares `version: "2"` — so the binary that
runs in CI must be the one the config was verified against. An action whose default version moves is
how a config becomes wrong without anyone editing it. `golangci-lint config verify` runs before
`run`, so a malformed config fails as a config error rather than as a lint result.

**Why the linter is required when `bin/gate api` only warns.** Locally the binary is often absent and
forcing every contributor to install it to run the gate is a worse trade than catching the problem
one step later. But it is the **only** thing that catches an aliased import evading the `http.Error`
ban — `nh "net/http"` then `nh.Error(...)` — which the gate's grep for the literal `http.Error(`
cannot see. So: the grep catches the honest mistake locally and instantly; the linter catches the
rest, in CI, where it is free.

**Enabled beyond the defaults:** `bodyclose`, `errcheck`, `errorlint`, `gosec`, `noctx`,
`sqlclosecheck`, `unconvert`, `unparam` — chosen for what this service will actually do: hold a pgx
pool, write JSON errors through one mapper, and grow an authenticated write path in EP-06. Tests are
excluded from `errcheck` and `gosec` only.

## Three structural decisions every later ticket inherits

### 1. `/v1` is a mounted sub-router, not a path prefix

```go
v1 := chi.NewRouter()
r.Mount("/v1", v1)
```

This is what makes CLAUDE.md rule 5c cheap: a `/v2` with different semantics mounts **beside** `/v1`
and both serve at once. That matters even with one client, because a deploy of api and web is never
simultaneous and a browser holds a cached bundle for minutes to hours. Domain routers mount *inside*
`v1`, never on the root.

### 2. `/health` is deliberately NOT under `/v1`

It is operational surface — read by Railway's health check and by a human confirming which build is
live — not part of the product contract. Versioning it would mean forking it at `/v2` for no reason.
There is a test asserting `/v1/health` is a 404.

**It does not check the database, on purpose.** Railway restarts a container whose health check
fails, so a database blip would become a restart loop that takes the api down for a reason the api
cannot fix. Readiness against Postgres is a separate endpoint if and when something needs one
(AOC-004).

### 3. One error mapping, and `http.Error` is banned

`internal/httpx/errors.go` holds the only `error → status` translation in the repo. Services return
sentinels (`ErrNotFound`, `ErrInvalid`, `ErrUnauthorized`, `ErrForbidden`, `ErrConflict`), wrapped
freely with `%w` for context; handlers call `httpx.Fail`.

**What is logged and what is sent are different on purpose.** The log gets the full wrapped error
with the request id; the client gets the *sentinel's* text, or a flat `"internal error"` for anything
unmapped. A wrapped error routinely carries a table name, a column or a query fragment, and that is
exactly what must not appear in a public response body. There are tests for both directions.

`http.Error` writes `text/plain`, skips the request id and spreads status decisions across every
handler until no two 404s look alike. **`bin/gate api` greps for it** — outside `internal/httpx` and
outside tests, any `http.Error(` fails the gate. That grep was added by AOC-002 verify round 1,
which found the rule claimed in two places and enforced in none.

chi's 404 **and 405** both route through `Fail`, so there is genuinely one path to an error
response. The 405 used to write its own body, which made that claim untrue and made a 405 the only
rejection that never appeared in the log.

## Middleware, in order

`RequestID` → `Log` → `Recover`.

The order is load-bearing, and it is the opposite of what it first looks like. `RequestID` is
outermost so everything downstream can log the id. **`Log` then wraps `Recover`, not the other way
round:** with `Recover` outside, a panic unwinds *past* `Log` before it can record anything, so a
panicking request produces **no access line at all** — the one request you most want in the log is
the one that vanishes from it. With `Recover` inside, the panic is caught within `Log`'s call, `Log`
resumes, and the request is logged with its real status of 500.

This was **measured, not reasoned**: AOC-002 verify round 1 captured slog output under both orders
and found the original order — and the comment defending it — backwards. There is now a test that
fails under the old order.

`Recover` routes the panic through the central mapper; chi's default writes a stack trace into the
response body. If the handler had **already started writing**, `Recover` logs and stops rather than
appending an error document: the status line is spent, and a second JSON object concatenated onto a
partial body parses as neither.

An inbound `X-Request-Id` is echoed for correlation but replaced if it is over 64 characters: it is
attacker-controlled and ends up in our logs. Header splitting is not a concern here — `net/http`
rejects control characters in a header value with a 400 before any middleware runs — and the id is
JSON-escaped in both the response body and the log.

The `Log` middleware wraps the `ResponseWriter` to record the status. It implements `Unwrap`, so
`http.ResponseController` still reaches `Flush`, `Hijack` and the deadline setters: wrapping a
writer must not quietly remove capabilities from everything downstream.

## Server lifecycle

`cmd/api/main.go` sets `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` and `IdleTimeout` — all
four are unset by default in `net/http`, and a server without them is eventually held open by slow or
dead clients until it runs out of file descriptors.

It shuts down gracefully on SIGINT/SIGTERM with a 20s drain. **Railway sends SIGTERM on every
deploy**, so without this every deploy cuts off whatever was mid-request.

## Build identity

`internal/version` holds `Version` and `Commit` as `var`s (not `const`s — `-ldflags -X` can only
write to a var). The `Makefile` injects them from `git describe` and `git rev-parse`, and `/health`
reports them, so a running container can always be traced to a commit.

## Not here yet, and which ticket brings it

| Thing | Ticket |
|---|---|
| CI (vet, lint, test on every PR) | AOC-003 |
| Railway projects + Postgres | AOC-004 |
| goose + sqlc toolchain, first migration | AOC-005 |
| taxonomy tables and place entities | AOC-009 |
| item schema — ⚠️ needs `vendor` and `spell_effect` as their own columns, and a label on what `coords` means (AOC-016/017) | AOC-010 |
| the importer | AOC-011 |
| public read endpoints for items | AOC-012 |
