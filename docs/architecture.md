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

chi's 404 and 405 route through `Fail` **for the machine surface** (`/v1/*`, `/health`,
`/assets/*`), so there is one path to an error response there.

⚠️ **Amended by AOC-024:** on the HTML surface both now write a small page directly, deliberately —
the renderer cannot be trusted to render the failure that may be the renderer. The log consequence
the original sentence warned about does **not** return: `Log` records every request from
`statusWriter` regardless of who wrote the body, verified in round 2. It is the claim that was
stale, not the behaviour.

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

## Rendering (the HTML surface)

Added by **AOC-024**. This is the pattern every later page copies, so it is worth reading
once before adding a page.

```
web/src/app.css          Tailwind input      ─┐
web/src/htmx.min.js      vendored HTMX        │ make assets
                                              ▼
internal/assets/built/   app.css, htmx.min.js   COMMITTED, embedded, content-hashed
internal/templates/html/ base · home · smoke · echo
internal/templates/      View + Engine (parse once at boot)
internal/pages/          handlers: build a View, render a template
```

**One handler, one query, two renderings.** A route answers a normal request with a full
page and an `HX-Request` with a fragment. That is what makes progressive enhancement real:
the page works with JavaScript off, and HTMX only removes the reload.

### The rules, and what each one prevents

| Rule | What it prevents |
|---|---|
| Templates parsed **once at startup**, with a probe that *executes* every page | A typo reaching a visitor as a 500. It fails the boot instead, so Railway keeps the last good deploy |
| `View` requires title, description and canonical; `Render` refuses without them | A page quietly shipping with no `<head>`, undoing the reason we render HTML at all |
| Title ≤ **60**, description ≤ **155**, truncated on a **word boundary** | A search result cut mid-word. Counts runes — boss names carry accents |
| Assets **content-hashed**, `immutable`, wrong hash ⇒ 404 | A stale stylesheet cached for a year, or a stale URL looking valid forever |
| `Load()` **refuses** empty or absent assets | A build that "succeeded" and produced nothing: a site that serves fine and looks broken |
| Both HTMX branches send `Vary: HX-Request` | A cache handing a browser the bare fragment — a blank-looking site, very hard to diagnose |
| Canonical built from **`PUBLIC_BASE_URL`**, never the request host | The same page declaring two canonicals when reached by two hostnames |

### Rejections have two shapes, chosen by PATH

Applies to **404 and 405 alike**. `/v1/*`, `/health` and `/assets/*` return **JSON** — those
are contracts a machine parses, and `/health` is read by Railway and by uptime monitors,
never by a person. Everything else returns a small **HTML** page.

The path decides, not `Accept`: a path is a fact about which contract was addressed, where
`Accept` is a negotiation a bot or a proxy can get wrong. Both pages are deliberately
**dependency-free** — no template, no asset — because they must work when the renderer is
the thing that broke.

⚠️ 405 was JSON everywhere until AOC-024 verify round 2, so a browser GET on a fragment
route handed a person raw JSON. ⚠️ `HEAD` is answered on every route via
`chi/middleware.GetHead`; without it chi replied 405 to monitors and link checkers.

### Assets

Built by `make assets` with the **Tailwind standalone binary** — no Node, no
`node_modules`, no `package.json`. Pinned by version **and SHA-256** in the `Makefile`; a
checksum mismatch fails the build, because a build tool that changes silently is how a site
starts looking different for reasons nobody can find. The binary downloads to `.tools/`
(gitignored); the **output is committed**, and `bin/gate api` fails if it is missing, empty,
gitignored or stale.

### Adding a page

1. A template in `internal/templates/html/` defining `content`.
2. A line in `pageTemplates`.
3. A handler in `internal/pages/` that builds a `View` and calls `Render`.

If the page needs data the startup probe does not supply, the probe **fails** — which is the
point: it should not be possible to add a page whose data nobody declared.

## Database

Added by **AOC-005**. `goose` for migrations, `sqlc` for typed queries, `pgx` for the pool.

```
migrations/<UTC timestamp>_<name>.sql   goose, Up + Down
internal/db/queries/*.sql               hand-written SQL
internal/db/sqlcgen/                    GENERATED by `make sqlc`, committed
internal/db/pool.go                     the pgx pool, built once in main
docs/database-schema.sql                GENERATED by `make schema-dump`, committed
```

### The guard rail

There is **one hosted environment**, so nothing may quietly default to a database. Every
migration target **requires `DATABASE_URL`** and **echoes the host** before acting:

```
▶ target: localhost:5433  (local)
▶ target: <host>  ⚠️  NOT LOCAL — this is a real database
```

Unset, it refuses and prints both options rather than guessing. The failure being prevented
is running a migration against production while believing it is local — which here means the
only structured copy of the armory data.

| Target | Does |
|---|---|
| `make migrate-up` / `migrate-down` / `migrate-status` | the obvious things |
| `make migrate-redo` | down then up — rehearses the round trip |
| `make migrate-create NAME=add_item_tables` | new timestamped migration |
| `make sqlc` | regenerate `internal/db/sqlcgen/` |
| `make schema-dump` | regenerate `docs/database-schema.sql` from the migrated local DB |

### Conventions every later migration copies

1. **Timestamped filenames, not sequence numbers.** Two branches both picking `003_` merge
   cleanly and then apply in an order nobody chose.
2. **Every `Up` has a `Down` that reverses it.** Production is forward-only; `Down` exists so
   the round trip can be *rehearsed*. A `Down` nobody has run is one that does not work, and
   it is needed exactly when things are already going wrong. `make migrate-redo` and the test
   suite both exercise it.
3. **Seeds are idempotent, via `ON CONFLICT DO NOTHING`** — not "insert if not exists", which
   is two statements with a race between them. EP-02 seeds every taxonomy table this way, and
   a taxonomy row duplicated by a re-run is a filter offering the same option twice. Pinned by
   `TestSeedsAreIdempotent`.
4. **Generated code is committed** — `sqlcgen/` and `database-schema.sql` both. A reviewer sees
   the diff, and the gate fails when it is stale. Same rule as the built assets.
5. ⚠️ **`make schema-dump` strips pg_dump's `\restrict` lines**, which carry a random token and
   would otherwise make the file differ on every run.

### The pool

Built **once in `main`** and passed down — never a package-level global, which cannot be
swapped in a test and hides who depends on it. `db.New` **pings**: `pgxpool` is lazy and
succeeds against a completely wrong URL, so without the ping the service boots "successfully"
and fails on the first request a visitor makes. Limits are small on purpose (10 connections):
Railway's Postgres has a fixed limit shared with migrations, `psql` and backups, and
exhausting it presents as the site being down.

### Testing

`internal/db` tests are **integration** tests against a real Postgres — SQL that has never met
a database is not tested. Each creates a **throwaway database** and drops it, so they cannot
disturb the developer's data or collide with each other. CI runs a `postgres:18-alpine`
service — the same major as production and as `docker-compose.yml`, because **every Postgres in this project tracks production's major** — and then **asserts the tests did not skip**, because a suite that skips its only
integration tests while reporting success is the failure shape this project keeps finding.

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
| `VERSION` | build arg → `/health` |
| `COMMIT` | ⚠️ **not** a build arg. `${{RAILWAY_GIT_COMMIT_SHA}}` resolves to an empty string at build time — Railway injects its git variables into the deployed **container**, not into the set `${{…}}` references resolve against. `version.Resolve()` reads it at runtime instead (PR #4) |
| `PUBLIC_BASE_URL` | the origin canonical URLs and `og:image` are built from |

⛔ **No secret is ever committed.** `.env` is gitignored; `.env.example` holds names and dummies.

### The local container runtime

**Colima, not Docker Desktop** (installed 2026-09-16). Same `docker` and `docker compose`
commands; a headless Linux VM instead of a GUI app.

Chosen because Docker Desktop needs **administrator rights on first launch** to install a
privileged helper, which would make the one blocking step in this project a thing only Pierre
can do. Colima installs from Homebrew with no admin, no GUI and no licensing question, and we
only ever need it to run a Postgres container.

```sh
brew install colima docker docker-compose
colima start --cpu 2 --memory 4 --disk 20      # once; `colima stop` to reclaim the RAM
make db-up                                      # Postgres 17 on localhost:5433
```

⚠️ **Colima does not share this repo's path with the VM.** The working tree lives on an
external volume (`/Volumes/SSD_pierre`), and Colima mounts `$HOME` by default. The VM shows
the directory structure but **no contents**, so any bind mount of a repo path silently
resolves to an empty directory — a failure that looks like a missing file, not a missing
mount. This is why `make db-restore` **streams the dump over stdin** rather than mounting
`tmp/dumps/`. Do not add repo bind mounts to `docker-compose.yml` without checking this.

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

⚠️ **The local major version must be ≥ production's.** Railway runs **Postgres 18.6**
(`ghcr.io/railwayapp-templates/postgres-ssl:18`), so `docker-compose.yml` pins `postgres:18-alpine`,
and `make db-up` **fails** if the running major does not match `POSTGRES_MAJOR`.

⭐ **The reason, measured — and the opposite of what this paragraph used to say.** It claimed
"pg_dump output from a newer server will not restore into an older one". That was never tested and
is false: an 18.6 dump restores into 17.11 fine. What actually bites is one step earlier —

```
pg_dump: error: aborting because of server version mismatch
pg_dump: detail: server version: 18.6; pg_dump version: 17.11
```

`pg_dump` **refuses to read a server newer than itself**, so with a 17 client **step 1 of the loop
cannot run at all**. It appeared to work only because this Mac happens to carry a Homebrew
`pg_dump 18.6` on `$PATH` — undocumented luck, on the one step the entire "no second hosted
environment" trade depends on. **Take the dump with the pinned container's client**
(`docker compose exec -T db pg_dump …`), never whatever `pg_dump` is on `$PATH`, so the version
that matters is the one this repo pins.

⚠️ Bumping the pin is **not** just a number: the 18+ images store data in major-version-specific
subdirectories, so the volume mounts at `/var/lib/postgresql`, not `/var/lib/postgresql/data`, and
an existing volume from an older image must be dropped (`make db-reset`).

### Rollback

**Proved on 2026-09-16, not assumed.** The builder was pointed at a Dockerfile that does not
exist and a real build was triggered by a push. Result: the build reported **FAILED**, and
the live site answered **200 throughout, still serving the previous commit** — zero non-200
responses across the whole test. Railway does not replace a running deploy with one that
failed to build.

⚠️ **`railway redeploy` does NOT rebuild.** It re-runs the existing image — DEPLOYING to
SUCCESS in six seconds, same commit — so it cannot be used to test a build change, and the
first attempt at this proof silently proved nothing. **A real build needs a push.**



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
