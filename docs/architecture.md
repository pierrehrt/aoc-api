# aoc_api — architecture

> **This file describes the code as it IS, not as it is planned.** Every later api ticket cites it by
> path, so a change to routes, schema or package structure updates it in the same commit
> (`bin/docs-check api` enforces this).
>
> Created by **AOC-002**. Last updated 2026-09-14.

## What this service is

The backend for the Age of Conan Codex: a public read API over a preserved item database, and later
an authenticated write path for community suggestions with moderation. Go 1.23+, chi, pgx, sqlc,
goose, Postgres, deployed on Railway.

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
handler until no two 404s look alike. `bin/gate api` greps for it.

## Middleware, in order

`RequestID` → `Recover` → `Log`.

The order is load-bearing: `RequestID` first so everything downstream can log it, and `Recover`
before `Log` so a panic still produces one request line carrying its 500. `Recover` routes the panic
through the central mapper — chi's default writes a stack trace into the response body.

An inbound `X-Request-Id` is echoed for correlation but replaced if it is over 64 characters: it is
attacker-controlled and ends up in our logs.

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
