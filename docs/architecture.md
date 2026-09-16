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
| `check` | `make fmt-check`, `make lint`, `make compile`, `make test` | **only** make targets — the same ones a person runs |
| `lint` | `go install golangci-lint@v2.13.2`, `golangci-lint config verify`, `golangci-lint run ./...` | required, not advisory |

**The contract: CI never spells a command out.** Every step in `check` is a `make` target, because a
copied command list is how CI and a local run drift until one of them is lying. `make check` is the
canonical list; adding a check means adding it there, once.

⚠️ **`bin/gate api` in the `product_management` repo does NOT call `make`** — it runs the equivalent
checks directly, so that it works against a repo whose shape it does not control and can report each
step's own ✅/❌. So there are two lists, and they can drift. The mitigation is that `make check` is
the declared canonical one and `bin/gate`'s steps mirror it; the thing that catches drift in
practice is that both run on the same code and a check missing from one still fails in the other.
An earlier version of this file claimed CI and `bin/gate` ran the same targets. They do not, and
saying so was worse than the duplication.

**The Go version is read from `go.mod`** (`go-version-file`), never pinned twice.

**`golangci-lint` is installed with `go install` at a pinned version, not via a third-party action.**
The config schema is version-specific — `.golangci.yml` declares `version: "2"` — so the binary that
runs in CI must be the one the config was verified against. An action whose default version moves is
how a config becomes wrong without anyone editing it. `golangci-lint config verify` runs before
`run`, so a malformed config fails as a config error rather than as a lint result.

**Why the linter is required when `bin/gate api` only warns.** Locally the binary is often absent and
forcing every contributor to install it to run the gate is a worse trade than catching the problem
one step later. And it is what catches an aliased import evading the `http.Error` ban — `nh "net/http"`
then `nh.Error(...)` — which the gate's grep for the literal `http.Error(` cannot see. So: the grep
catches the honest mistake locally and instantly; the linter catches the rest, in CI, where it is free.

⚠️ **That is only true because `forbidigo` is configured to make it true**, with
`analyze-types: true` so the alias is judged by the package it resolves to rather than by its
spelling. AOC-003 verify round 1 planted exactly that evasion and **the entire gate passed** — the
sentence above had been written as fact while nothing in the enabled linter set could ban an
identifier at all. Proved after the fix: `use of nh.Error forbidden`. `internal/httpx/` is excluded
from the rule, because it *is* the error mapper.

**Enabled beyond the defaults:** `bodyclose`, `errcheck`, `errorlint`, `forbidigo`, `gosec`, `noctx`,
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

## Deploy

**One hosted environment.** No `dev`, no `staging` — Pierre's call, 2026-09-16: no revenue, so no
second Postgres to pay for (`product_management/DECISIONS.md`). The safety that a second
environment used to provide is replaced by the loop below, which costs nothing and tests more.

| | |
|---|---|
| Host | Railway, one project, one service, one Postgres, **private networking** between them |
| Build | this repo's `Dockerfile` (not Railway's Go buildpack — it picks its own Go version) |
| Trigger | push to `main` → Railway builds → health check |
| URL | `aoc-codex.app` (AOC-014) |
| Local DB | `docker-compose.yml`, Postgres on **localhost:5433** |

### Configuration

Set in Railway's variables and nowhere else. `.env.example` lists every name with dummy values.

| Variable | Notes |
|---|---|
| `PORT` | assigned by Railway; the server reads it, 8080 locally |
| `ENV` | `production` on Railway, `local` otherwise |
| `DATABASE_URL` | ⚠️ Railway's **private** hostname. The public proxy URL bills egress and adds latency for nothing |
| `VERSION`, `COMMIT` | build args → `/health`, so a running container traces to a commit |

⛔ **No secret is ever committed.** `.env` is gitignored; `.env.example` holds names and dummies.

### Migrations — the loop that replaces a second environment

**A migration's first execution is never against production data.** With one hosted environment
that is not a slogan, it is these four steps, in order:

```
1.  pg_dump production      →  tmp/dumps/     (gitignored — see below)
2.  make db-restore                            restore it into local Postgres
3.  run the migration locally                  against the REAL data, not an empty schema
4.  back up production, then deploy            forward-only, one migration per deploy
```

Step 2 is the point. An empty hosted dev database never meets the row that breaks the migration;
a restored production dump does. **Step 4's backup is not optional** — AOC-006 automates it.
A failed migration is fixed **forward** from a known backup, never by hand-editing production.

⛔ **Dumps are never committed.** `tmp/dumps/` is gitignored. A dump is the entire researched
dataset — and after EP-06, real accounts. Committed once, it is in every clone forever.
⚠️ Most of that data is **unre-derivable** after the AoC>TV host lapses ~Feb 2027.

⚠️ **`docker-compose.yml` pins Postgres 17; production's major version must be ≤ that.** `pg_dump`
output from a newer server will not restore into an older one, and that failure would land midway
through rehearsing a migration against the only copy of the armory. Reconcile the pin with
Railway's actual version when the project is created.

### Rollback

A failed **build** never replaces the running deploy — Railway keeps serving the previous one.
A build that succeeds and is *wrong* is rolled back from the Railway dashboard by redeploying the
previous deployment. **A deployment row is not a deployment:** check its newest state, and confirm
`/health` reports the expected commit on the live URL before calling anything done.

⚠️ **Rolling back code does not roll back a migration.** That is why they are forward-only and one
per deploy: the previous binary must still work against the new schema, so any schema change that
the old code cannot tolerate ships in two deploys, not one.

### Logs

`railway logs` from the CLI, or the service's Observability tab. Output is JSON on stdout
(`log/slog`), one object per line, carrying the request id echoed in `X-Request-Id`.

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
