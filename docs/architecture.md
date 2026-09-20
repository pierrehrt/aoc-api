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

### Tool versions are pinned, and the pin is tested

`goose` and `sqlc` are pinned to exact versions in the **Makefile** (`GOOSE_VERSION`,
`SQLC_VERSION`) and invoked as `go run <pkg>@<version>` — never from `PATH`. The same pattern
as `golangci-lint@v2.13.2` in CI and Tailwind's version + SHA-256.

**What went wrong without it** (AOC-005 verify round 1): the CLI on `PATH` was Homebrew goose
**v3.28.0** while `go.mod` pinned the goose *library* at **v3.24.1** — so `make migrate-up` and
`go test` applied the same migrations with two different versions of goose — and `sqlc` was in
neither `go.mod` nor `go.sum`, so the gate's staleness check judged committed generated code
against whatever `brew upgrade` last installed.

⚠️ **Not a `tools.go`.** That was tried and reverted: a build tool must not get a vote on the
deployed binary's toolchain. `sqlc` v1.31.1 declares `go 1.26.0`, so importing it pushed this
module's own `go.mod` from `go 1.23` to `go 1.26.0` — silently invalidating the Dockerfile's
`golang:1.23-alpine` pin, against the rule written in the Dockerfile itself — dragged `pgx`,
the production driver, from v5.7.2 to v5.9.2, and grew `go.sum` from 66 lines to 476.
`go run pkg@version` resolves *outside* this module: exact version, verified against the
checksum database, zero effect on what we ship.

Two tests keep the pins honest, because a comment asking people to keep two numbers in sync is
not a mechanism:
- `TestTheGooseCLIMatchesTheGooseLibrary` — `GOOSE_VERSION` must equal the goose version in
  `go.mod`. The CLI applies migrations in production; the library applies them in the tests.
- `TestTheGateHasAPinnedSQLCToRun` — `SQLC_VERSION` is pinned and reachable via `make sqlc-cmd`.

**`bin/gate api` uses the pinned `sqlc`, not a `PATH` one**, by running what `make sqlc-cmd`
prints. It prints rather than runs because the gate needs *sqlc's own* exit code — 1 is "the
committed code is stale", 2+ is "sqlc itself broke", and those must never be confused — and
`make` collapses both to 2 when a recipe fails.

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
| `make sqlc-cmd` | print the pinned `sqlc` invocation — `bin/gate api` runs what it prints |
| `make schema-dump` | regenerate `docs/database-schema.sql` — ⭐ **from a FRESH database** (`make db-reset && make migrate-up && make schema-dump`). That is the state CI and every new clone are in, so it is what the committed artifact must match. A restored database now produces the identical file, but fresh is the reference |

### Conventions every later migration copies

1. **Timestamped filenames, not sequence numbers.** Two branches both picking `003_` merge
   cleanly and then apply in an order nobody chose.
2. **Every `Up` has a `Down` that reverses it.** Production is forward-only; `Down` exists so
   the round trip can be *rehearsed*. A `Down` nobody has run is one that does not work, and
   it is needed exactly when things are already going wrong. `make migrate-redo` and the test
   suite both exercise it.
3. **Seeds are idempotent, via `ON CONFLICT DO NOTHING`** — not "insert if not exists", which
   is two statements with a race between them. EP-02 seeds every taxonomy table this way, and
   a taxonomy row duplicated by a re-run is a filter offering the same option twice.

   Pinned by **four** tests, because two of them turned out to pin less than they appeared to:
   - `TestEverySeedInsertIsIdempotent` — a STATIC scan of `migrations/`, so it covers migrations
     nobody has written a test for. It splits Up from Down on the **raw** text (`-- +goose Down`
     is itself a `--` comment, so stripping first erases the marker and the Down section gets
     scanned as if it seeded), then reads each statement through `sqlStatements`.
   - `sqlStatements` splits on the semicolons that are outside **both comments and string
     literals**, and decides on `code` — the statement with its comments removed and its
     literals blanked — while keeping `text` verbatim for messages and re-execution. Both
     cheaper readings have now failed on a real migration: matching raw text matched the
     *comment* that explains the clause (AOC-005), and splitting on every `;` chopped AOC-009's
     seeds mid-sentence, because their `source_note` values contain semicolons and apostrophes,
     reporting 4 of 8 correct statements as offenders.
   - `TestSQLStatementsSeparatesCodeFromProse` and `TestTheSeedCheckReadsSQLNotProse` — fixture
     migrations where the prose and the SQL **disagree**. Needed because every real migration
     here has both a real `ON CONFLICT` clause and a comment explaining it, so the check passes
     either way on the repo's own files: when the comment-stripping was disabled, nothing went
     red. The literal cases cover the mirror image — a seed whose *text* says "ON CONFLICT"
     cannot vouch for itself.
   - `TestTheMigrationsOwnSeedIsIdempotent` — re-executes the migration's own statement, lifted
     verbatim from the file, against a real database and counts the rows.
   - `TestEverySeedReExecutedChangesNothing` — the same thing for **every** seed in every
     migration, against an already-seeded database. That is the state a re-applied migration
     actually meets.

   ⚠️ **`TestSeedsAreIdempotent` does NOT pin this**, though its name suggests it: its `down`
   drops the table, so the following `up` can never meet a duplicate.
4. **Generated code is committed** — `sqlcgen/` and `database-schema.sql` both. A reviewer sees
   the diff, and the gate fails when it is stale. Same rule as the built assets.
5. ⚠️ **`make schema-dump` strips pg_dump's `\restrict` lines**, which carry a random token and
   would otherwise make the file differ on every run.

### The content model, and why its seeds are generated

**AOC-009** seeds the taxonomies (`archetypes`, `classes`, `rarities`, `item_types`,
`equip_locations`, `currencies`, `acquisition_types`, `tiers`, `bindings`, `factions`,
`armour_weights`) and the place entities (`regions` → `maps` → `places` → `bosses`, plus
`quests` and `containers`).

```
scripts/gen_taxonomy_seed.py <snapshot>   the taxonomy migration
scripts/gen_places_seed.py   <snapshot>   the place-entity migration
```

**The migrations are GENERATED, not hand-typed**, from `armory_snapshot/` — a separate
repository holding our own OCR capture of AoC>TV (`items_clean.json`) and Pierre's geography
(`reference_geography.json`). Re-run a generator against a newer snapshot and diff: an empty
diff means the data has not moved. Nobody has to trust a number written in a ticket six weeks
ago, and no fact is typed by a human or a model on the way in.

Both generators **fail rather than guess**. A class with no archetype, a place name the
geography does not know, a parenthetical that is neither a known complex nor `Unchained` — each
one exits non-zero and says what to ask Pierre. That is the schema-level expression of
`CLAUDE.md` STEP ZERO: an empty field is a feature, a confident guess is a bug.

Three structural facts the rest of the app inherits:

1. **Every "kind of thing" is a row, never a Postgres `ENUM` or a Go constant** — so adding a
   class or a currency later is an `INSERT`, not a migration plus a deploy.
2. **Places are a hierarchy, not three flat levels.** `places.parent_place_id` is
   self-referencing (House of Crom contains two dungeons; Warmonk Monastery three) and
   `places.map_id` is **nullable** (Skull Gate Pass and Kuthchemes hang straight off a region).
   `region_id` is `NOT NULL` on every place and is deliberately redundant with the map's region
   so that "everything in Stygia" is one join; `TestEveryPlacesRegionMatchesItsMap` is what makes
   that redundancy safe. An Unchained dungeon is **its own row**, not a flag on its twin — the
   two share no loot at all.
3. **Every content row carries its provenance**: `confidence_id` →`confidence_levels`
   (`verified` / `corroborated` / `unconfirmed` / `disputed`, the vocabulary in
   `product_management/reference/sourcing-standards.md` § 3), `source_note` naming which source
   it came from, and `open_question` holding what is still unknown. Conflicts between the two
   sources are seeded `disputed` with both versions in the question rather than silently
   resolved; `ListOpenQuestions` returns all of them in one read.

### The item schema (AOC-010)

`items` · `item_equip_locations` · `item_stats` · `item_spell_effects` · `item_sources` ·
`item_costs` · `item_classes` · `sets` · `slot_fits`. Empty until **AOC-011** imports; the reads
live in `internal/db/queries/items.sql` and the service layer that wraps them arrives with
**AOC-012** (`internal/items/` is a documented empty package until then).

Four shapes that are not obvious, each of which a simpler schema would have got confidently wrong:

1. **Equip location is a JOIN, not a column on `items`.** Two of the snapshot's 16
   `equip_location` values are **compound** — `Main Hand, Off Hand` on **390** items (a two-hander
   occupying both slots at once) and `Left/Right Finger` on **188** (a ring occupying either) —
   and AOC-009 seeds only the **13 atomic slots**. Flattened into one column, *"show me every Off
   Hand item"* silently returns 141 instead of 531. `items.slot_fit_id`
   (`single` · `both` · `either`) says how to read an item's rows, and it lives on the **item**
   because it describes the whole set: two join rows could otherwise contradict each other.
   `TestListItemsFindsTwoHandersWhenAskedForOffHand` exercises the **shipped** `ListItems` query and
   is mutation-tested: break that query and it fails naming the lost two-hander.
   `TestAskingForOffHandItemsReturnsTwoHandersToo` pins the same property at the schema level.
   ⚠️ The distinction matters — an earlier version of this paragraph cited only the schema-level
   test, which passes even when the shipped query is wrong.
2. **Stat values are `numeric(8,2)`, not `integer`.** The 2026-09-13 decision said *"integers"*
   meaning arithmetic-not-text, and **480 rows disprove the literal reading**: Natural Mana Regen
   (314), Natural Stamina Regen (131) and Natural Health Regen (35) carry values like `4.5`, `1.6`,
   `2.4`. An `integer` column truncates 4.5 to 4 and quietly wrongs every regen number on the site.
   `numeric` and not `float` because the armoury **sums** these. `items.dps` is fractional for the
   same reason (16.5 … 157.1).
3. **Spell effects are their own table**, identical in shape to `item_stats` and deliberately not
   a flag on it. A build calculator sums `item_stats` and can never reach `item_spell_effects`, so
   eight mounts cannot hand every wearer `-8% Sprinting Stamina Drain` (AOC-016). Structure rather
   than a rule someone has to remember — the same argument as `sets.set_armour_weight_id`, which is
   what a set's **pieces** weigh and is not a class ceiling (`classes.max_armour_weight` is).
4. **`item_sources.boss_id` and `vendor_id` are separate, with a `CHECK` that at most one is set,
   and `vendor_id` points at its own `vendors` table.** The snapshot's single `boss_or_npc` field
   conflated real bosses with vendor names and with `Unchained`, which is a difficulty and not an
   NPC. AOC-017 split them at the source; the constraint stops an importer re-merging them.
   ⚠️ **A vendor is not a place**: 23 distinct vendors across 1,869 source rows, **zero** matching
   any of the 86 seeded places, and they are not even one kind of thing — `Nikulas the Smith` is an
   NPC, `Minigames` and `Loyalty Rewards` are systems, `Temple of Yun` is a location. `vendor_id`
   pointed at `places(id)` until verify round 1, which would have put all 23 in the browse tree,
   forced an invented `NOT NULL` region onto each, and made "what drops here" answer with vendor
   stock.

5. **`items.item_type_id` is nullable, and one row is the reason.** Item 4532
   *Mini-Pet: The Devourer* has no `item_type` in the snapshot and `item_types` has no catch-all, so
   `NOT NULL` would have forced the importer to choose one — the invention STEP ZERO forbids.
   `TestNothingIsNotNullByAccident` pins the whole `NOT NULL` set, because a list of columns that
   *should* be nullable cannot catch the one nobody thought about, and that is the only direction
   that blocks an import.

`items.item_id` is the **source site's own id** and has no default — after the origin host lapses
(~Feb 2027) it is the only key our data and the original still share, so it is never re-numbered.
`pg_trgm` backs the name search; production's Postgres runs as `postgres`, so the
`CREATE EXTENSION` was checked to be permitted before the index was designed around it.

### The pool

Built **once in `main`** — never a package-level global, which cannot be swapped in a test and
hides who depends on it. ⚠️ It is not yet *passed* anywhere: nothing takes a pool until
**AOC-009** adds the first queries. `main` builds it, pings it and closes it, which is what
makes a live 200 proof that the production pool connects. `db.New` **pings**: `pgxpool` is lazy and
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

> 💾 **Backups and restores have their own runbook: [`runbook-restore.md`](runbook-restore.md).**
> Read it before touching the production database. ⚠️ Railway's scheduled backups are **Pro-only**
> and this project is on **Hobby**, so the dumps that runbook produces are the **only** backups
> that exist. Production is reached over **SSH** (`ssh -L` to the Postgres container's own
> loopback) — there is deliberately **no public database endpoint**. (AOC-006)
>
> ⚠️ **Nothing schedules a backup today.** Every dump that exists was taken by hand. **AOC-030**
> adds a cron service that dumps to R2 over the private network, with an alarm when it stops.

**One hosted environment.** No `dev`, no `staging` — Pierre's call, 2026-09-16: no revenue, so no
second Postgres to pay for (`product_management/DECISIONS.md`). The safety that a second
environment used to provide is replaced by the loop below, which costs nothing and tests more.

| | |
|---|---|
| Host | Railway, one project, one service, one Postgres, **private networking** between them |
| Build | this repo's `Dockerfile` (not Railway's Go buildpack — it picks its own Go version) |
| Trigger | push to `main` → Railway builds → health check |
| URL | `aoc-codex.app` — see § Hostnames below (AOC-014) |
| Local DB | `docker-compose.yml`, Postgres on **localhost:5433** |

### Hostnames

Three names, one of which is not a service at all.

| hostname | points at | serves |
|---|---|---|
| `aoc-codex.app` | Railway, CNAME → `nnja20lj.up.railway.app` | the site (HTML at `/`) **and** `/v1/*` JSON — one origin, one certificate, no CORS |
| `www.aoc-codex.app` | **nothing** — answered at Cloudflare's edge | `301` to the apex, always |
| `img.aoc-codex.app` | the R2 bucket `aoc-codex-enam` | the 4,645 archived tooltip images |

⚠️ **`.app` is HSTS-preloaded at the TLD level.** Browsers refuse plain HTTP to *any* `.app` name
before a request is made, so there is no "try it over http first" step and no HTTP fallback to fall
back to. A certificate that has not issued yet does not look like a warning — it looks like the site
is down. Every hostname above must be HTTPS from its first hit, and every asset URL must be `https`
or it is blocked rather than mixed-content-warned.

**`www` is a Cloudflare Redirect Rule, not a Railway domain.** It is a proxied DNS record with a
rule in front of it, so the request is answered at the edge and never reaches the origin. Railway
bills usage, so a hostname whose only job is to say "go to the apex" should not cost a container
wake-up — the same reasoning as the cache in AOC-026. It also means Railway issues one certificate
instead of two, for one name instead of a name and its alias.

**Why there is no `api.aoc-codex.app`.** One binary serves both surfaces, so a second hostname would
be a second name for the same service. Keeping `/v1/*` on the site's own origin means no CORS
configuration, no preflight round-trip on the critical path, and one certificate.
(Decided 2026-09-16; `product_management/DECISIONS.md`.)

**The images were named before they were reachable.** `tooltip_image` in the database has held
`https://img.aoc-codex.app/armory/…` since AOC-008 rewrote the URLs **in the generator** — 4,644 of
4,646 rows, the other two being AOC-008's two 404s. Those URLs were imported on 2026-09-19 and
pointed at a hostname that did not resolve until this ticket. `tooltip_source_url` still holds all
4,646 original `is-better-than.tv` URLs: that column is provenance and never moves.

### Configuration

Set in Railway's variables and nowhere else. `.env.example` lists every name with dummy values.

| Variable | Notes |
|---|---|
| `PORT` | assigned by Railway; the server reads it, 8080 locally |
| `ENV` | `production` on Railway, `local` otherwise — **reported by `/health` as `env`**. Defaults to `local` when unset, so nothing ever claims to be production by accident |
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
make db-up                                      # Postgres 18 on localhost:5433
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

## Object storage

**Cloudflare R2, one bucket: `aoc-codex-enam`.** It holds the things that must not live in Postgres and
must not live on one SSD — the 4,645 armory tooltip images (AOC-008), and later the scheduled
`pg_dump` backups (AOC-030). R2 was chosen over S3 and B2 for one reason: **egress is free at any
volume**, so a link on Reddit cannot turn into an invoice on a project with no revenue
(`product_management/DECISIONS.md`, 2026-09-13).

| | |
|---|---|
| Bucket | `aoc-codex-enam`, **private** — public access **off** and **zero custom domains**, confirmed off the dashboard 2026-09-19. Nothing is world-readable |
| S3 endpoint | `https://<account-id>.r2.cloudflarestorage.com` |
| Location hint | **`enam`** (Eastern North America) — **read back from the API**: `GET ?location=` → `<LocationConstraint>ENAM</LocationConstraint>` (see *Reading a bucket's location hint*) |
| Jurisdiction | **none**, deliberately — see below |
| Public URL | ⏳ **none yet.** `img.aoc-codex.app` is the *planned* custom domain and **AOC-014 binds it**; today the bucket has no public route at all. Never `r2.dev` |
| Second copy | Pierre's local disk. ⚠️ Manual, unchecked, and not a backup system. The Backblaze B2 mirror was dropped 2026-09-14 |
| Free tier | 10 GB-month. The planned payload is ~175 MB — **1.7%** |

⚠️ **Three fields on the create-bucket form are permanent, and the default on one of them is a
trap.** All three must be set deliberately, because none can be edited afterwards — changing any of
them means deleting the bucket and making a new one.

| Field | What it does | The trap |
|---|---|---|
| **Name** | permanent identity | — |
| **Location** | placement preference: `wnam eeur enam weur apac oc` | ⛔ **defaults to `Automatic`, which picks from where the create request comes from.** Pierre is in Bangkok, so Automatic means **APAC** — the wrong end of the planet for a US/EU audience |
| **Jurisdiction** | data-residency guarantee (`eu`, `us`, `fedramp`) | changes the S3 endpoint to `https://<account-id>.<jurisdiction>.r2.cloudflarestorage.com`, forces every API token to be scoped to that jurisdiction, and **stops Logpush interacting with the bucket at all** |

**This bucket has `Location = enam`, explicitly chosen, and no jurisdiction.** Getting there took
**four buckets, three of them destroyed** — each while still empty, which is the only moment it is
free (AOC-007). All three mistakes were the same mistake: **an instruction that named one permanent
field and left the one beside it to its default.**

#### Reading a bucket's location hint

**One signed S3 call reports it exactly**, and this is the authoritative check:

```
GET https://<account-id>.r2.cloudflarestorage.com/<bucket>?location=
  → 200  <LocationConstraint>ENAM</LocationConstraint>
```

No `aws` CLI is needed — ~40 lines of Python **stdlib** (`hmac`, `hashlib`, `urllib`) signing SigV4
with the key pair from `r2.env`. ⚠️ **The canonical query string must be `location=`, with the empty
value.** Signing the bare flag `location` yields `SignatureDoesNotMatch`, which reads exactly like a
bad secret and is how this call gets wrongly written off as unsupported. `rclone` exposes no
equivalent, which is a limit of `rclone`, not of R2.

**The A/B latency method is history, kept for its one real use.** Before the call above was tried,
the region was established by writing ten sequential objects to two buckets from the same machine
with the same token, interleaved: the APAC bucket ran **6.60 / 6.70 / 7.16 s**, the ENAM bucket
**19.76 / 18.75 / 19.83 s** from Bangkok — 2.85× apart with no overlap. Note what that can and
cannot do. **Absolute latency establishes nothing** (an R2 write is dominated by its durability
commit, not by round trips), and even the A/B only separates **near from far** — it rules out
`apac` from Bangkok but could never tell `enam` from `weur`. Use it only when there is no
credential to sign with; otherwise ask the API.

#### Why the name carries the region

`aoc-codex-enam` is uglier than `aoc-codex` and is kept for a plain reason: **renaming is not a
rename.** The name is permanent, so changing it means creating a fifth bucket and reissuing both
tokens — real clicks from Pierre for a cosmetic gain (rule 16). The suffix is accurate and cannot
become a lie, because the property it names cannot change while the bucket exists.

⚠️ It is **not** kept because the region is otherwise unreadable — an earlier version of this
section claimed exactly that, and it was false. The name is a convenience, not the system of record.
The name is never public either way: the images will be served from `img.aoc-codex.app` (AOC-014).

### Credentials

⛔ **Never in this repo, never in an env var committed anywhere.** Two files on Pierre's machine,
both `chmod 600`:

| File | Holds |
|---|---|
| `~/.config/aoc-codex/r2.env` | account id, bucket, endpoint, and the R2 API token key pairs |
| `~/.config/rclone/rclone.conf` | two remotes, `[r2]` (read-write) and `[r2ro]` (read-only), both `type = s3`, `provider = Cloudflare` |

The **Access Key ID** (32 hex) and **Secret Access Key** (64 hex) come from *R2 → Manage R2 API
Tokens → Create API token*, and the secret is displayed **once**. The S3 endpoint URL shown on that
same page is **not** a credential and is not interchangeable with one.

### Verified round trip (AOC-007, 2026-09-19)

Six files (five text, one 200 KB of random bytes) against the real bucket:

```
rclone copy  <dir> r2:aoc-codex-enam/_aoc-007-roundtrip   # 6 files up
rclone check <dir> r2:aoc-codex-enam/_aoc-007-roundtrip   # 0 differences, 6 matching files
rclone sync  <dir> r2:aoc-codex-enam/_aoc-007-roundtrip   # re-run: Transferred 0 B, Checks 6/6
rclone copy  r2:aoc-codex-enam/_aoc-007-roundtrip <dir>-back && diff -r   # byte-identical
rclone deletefile r2:aoc-codex-enam/_aoc-007-roundtrip/<f>   # per object, back to 0
```

### The two tokens, and what was actually proved about each (AOC-007, 2026-09-19)

Two R2 API tokens, **both scoped to the single bucket `aoc-codex-enam`** — and that scoping is
measured, not assumed, because an earlier version of this section asserted it and was **wrong**:

| rclone remote | Token permission | `ListBuckets` | Used by |
|---|---|---|---|
| `[r2]` | **Object Read & Write** | `403` | the tooltip upload (AOC-008), the backup writer (AOC-030) |
| `[r2ro]` | **Object Read only** | `403` | restore-side reads, and anything that must not be able to damage the bucket |

📌 **`ListBuckets` returning `403` is the evidence available over the S3 API**, and it is the only
one. A token left on *Apply to all buckets* lists them happily. The first read-write token issued
for this project was account-wide exactly that way — it reached a bucket created minutes earlier
that it had never been named against — and it was replaced rather than documented around.

⛔ **What is NOT evidence: a `403` on some other bucket name.** R2 answers
`ListObjectsV2 403 AccessDenied` for a bucket that **has never existed** — measured against
`zzz-never-existed-4b8e21` — so that test cannot separate *scoped out of it* from *it is not there*.
An earlier version of this table presented it as a second, independent column. It was not one.

📌 **Scoping is not invisible in general** — the R2 dashboard's *Manage API Tokens* page states each
token's scope outright. It is invisible only to a program holding nothing but the key pair.

#### Proving a read-only credential is read-only

Three writes denied, each `403 AccessDenied` on the operation named:

```
rclone copy       forbidden.txt r2ro:<bucket>/ --s3-no-check-bucket   # PutObject    403
rclone copy       <overwrite>   r2ro:<bucket>/ --s3-no-check-bucket   # PutObject    403
rclone deletefile r2ro:<bucket>/t1.txt                                # DeleteObject 403
```

⚠️ **Two traps make that evidence worthless if you skip them**, and both were hit here first:

1. **A broken credential also fails to write.** The same token must first be seen to *succeed* at
   reading: `rclone lsf r2ro:<bucket>/...` listed all six objects and the 200 KB random file came
   back with a **matching sha256**. Only then does a denied write mean *scoping* rather than a typo
   in the secret.
2. **`rclone copy` never reaches `PutObject` on the first try.** It fails earlier at `CreateBucket`,
   because it tries to ensure the bucket exists and a scoped token cannot see it.
   `--s3-no-check-bucket` skips that. A `CreateBucket 403` looks like a passing test and proves
   nothing about writing objects.

Afterwards the bucket was re-listed with `[r2]`: `forbidden.txt` **absent**, `t1.txt` **unchanged**,
six objects exactly as uploaded. The denied writes left nothing behind.

## Not here yet, and which ticket brings it

| Thing | Ticket |
|---|---|
| CI (vet, lint, test on every PR) | AOC-003 |
| Railway projects + Postgres | AOC-004 |
| goose + sqlc toolchain, first migration | AOC-005 |
| taxonomy tables and place entities | AOC-009 |
| the importer — 4,648 item rows into the tables AOC-010 created empty | AOC-011 |
| public read endpoints for items | AOC-012 |
| scheduled `pg_dump` to R2 + dead man's switch | AOC-030 |
