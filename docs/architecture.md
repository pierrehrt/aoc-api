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
cmd/import-armory/     the armory importer, run as the `import` service (AOC-011, AOC-040)
internal/httpx/        the HTTP edge: middleware, error mapping, the cache policy, /health
internal/version/      build identity: the VERSION file, the commit (AOC-015)
internal/db/           pgx pool + sqlc output + queries/ (AOC-005)
internal/items/        the Armory bounded area: schema, importer, the /v1 read surface (AOC-010/011/012)
internal/templates/    html/template engine: parse once, View contract, fragments (AOC-024)
internal/pages/        the HTML handlers (AOC-024)
internal/assets/       the committed, content-hashed CSS/JS/images and their handler (AOC-024)
migrations/            goose files (AOC-005 onwards)
scripts/               operational scripts: backup, the backup alarm, cloudflare-cache.sh
web/src/               build INPUTS for the assets (Tailwind CSS, vendored htmx) — not served
docs/                  this file, api-routes.md, database-schema.sql, runbook-restore.md
workers/img/           the Cloudflare Worker serving img.aoc-codex.app (AOC-041) - see Object storage
```

`internal/db`, `internal/httpx` and `internal/items` carry a `doc.go` saying what belongs in each and
what does not — the cheapest defence against the layout eroding into a pile of helpers. The other
packages (`version`, `templates`, `pages`, `assets`) state it in their package comment instead.

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

**A client that went away is not a failure** (`httpx.ClientGone`). It requires that the request's
own context was canceled. net/http cancels it when the reader's connection closes, and when a write
to it fails. The error must then be that cancellation, or the reset or broken pipe (`ECONNRESET`,
`EPIPE`) of the failed write. The answer is 499 ("client closed request", nginx's) with an Info line,
in `httpx.Fail` and in the pages' `fail`; the access line may say 200 if the status had already
gone out. ⚠️ **The context is what makes it the reader's connection.** A reset from the database's
connection leaves the request alive and is a 500 with an ERROR line. Before the context check it was
answered 499 with an empty body and logged as a reader gone, at Info (delta verify 6, N9).

The armory aborts every superseded live request (`hx-sync` replace). The query each was running
failed with "context canceled", or, if it had finished, the write failed. Logged as ERROR and 500,
that put 52 failures nobody saw in one session's log, and it would have filled the +24h watch's
error count (AOC-065 delta verifies 4 and 5). A deadline or a write timeout is still a 500: a
timeout is the server being slow.

**Every request has a deadline** (`httpx.RequestDeadline`, 10 s, set by the router). A database that
hangs without resetting used to hold a request until the reader, or Cloudflare at 100 s, gave up.
That cancellation is a genuine reader gone, so the hang reached the log only as 499s at Info, and
`/health`, which touches no database, stayed 200 (delta verify 7, N11). At the deadline the query
fails with `DeadlineExceeded`, which `ClientGone` does not take: a 500 and an ERROR line. 10 s is far
beyond the slowest page's queries (tens of milliseconds) and well inside Cloudflare's limit.

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

`RequestID` → `Log` → `Cache` → (`canonicalHost`, with `WithCanonicalHost`) → `Recover` → `deadline`
→ `GetHead`.

`deadline` (AOC-065, § 3) gives every request's context `RequestDeadline`. It is mounted with
`r.Use` ahead of every route, so everything a handler starts is bounded.

`Cache` (AOC-026, § Caching below) sits **inside `Log` and outside `Recover`**: the 500 that
`Recover` writes for a panic passes through it and leaves as `no-store`, where the other way round
it would leave with no header at all. Its writer implements `Wrote()`, because `Recover` asks the
writer it holds whether the response has begun — without it, a panic after a partial write would get
a JSON error appended. A test drives the production router through both.

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

## Caching (AOC-026)

Cloudflare's free plan sits in front of the site. The origin decides what it may keep, through one
header on every response, set in **one place**: `internal/httpx/cache.go`.

**Why it matters more than speed:** Railway bills usage. An uncached origin turns a viral Reddit
link into an invoice; a cached page costs the same whether ten people read it or ten thousand.

### The policy

| Response | `Cache-Control` | At the edge |
|---|---|---|
| `/assets/*`, 200 or 304 (content-hashed) | `public, max-age=31536000, immutable` | a year |
| Public pages (`/`, and everything outside `/v1`, `/assets`, `/health`), 200 / 301 / 304 / 308 | `public, max-age=60, s-maxage=3600, stale-while-revalidate=86400` | an hour |
| `/v1/*` GETs, 200 / 301 / 304 / 308 | `public, max-age=60, s-maxage=600` | ten minutes |
| 404 or 410, except under `/assets` | `public, max-age=60, s-maxage=60` | a minute |
| `/health`; any other status (400s, 405, 429, 5xx); any method but GET/HEAD; any `HX-Request: true` request; a 404 under `/assets` | `no-store` | never |
| ⛔ `MarkPrivate` called, a `Set-Cookie` on the response, or `Authorization` on the request | `private, no-store` | **never** |

The last row **wins over every other**, including a value a handler set by hand.

- **Browsers recheck pages after a minute**, so a correction reaches a reader who reloads; the edge
  keeps an hour, and a purge makes a change immediate.
- **A 404 is cached for a minute**, so a burst for a missing URL cannot all reach the origin, and a
  newly published page is visible within a minute. **Not under `/assets`:** there a 404 is a stale
  hash or a deploy racing its own HTML, and caching it would leave a page unstyled at that edge.
- **An error is never cached.** An outage must not be served as though it were the page.
- **An HTMX fragment is never cached** by a shared cache, under any URL. That closes the dangerous
  case — a bare fragment handed to a browser that asked for the page — **whether or not the edge
  honours `Vary`**, which is not assumed. Handlers that branch on HTMX still send
  `Vary: HX-Request`, for browsers. **An HTMX request is exactly `HX-Request: true`** — what htmx
  sends — and it has **one** definition, `httpx.IsHTMX`, which the renderer (`templates.IsHTMX`
  calls it), this policy and the edge rule all use; `TestNoFragmentEverLeavesWithAPublicHeader`
  pins that no spelling yields a fragment with a public header. ⚠️ That closes only the *storing*
  direction. The other one — an
  HTMX request answered with the cached full page — is the edge's to close, because a `HIT` never
  reaches the origin: that is the second Cache Rule below.
- **The request's `Cookie` header is not a trigger.** Cloudflare's bot-management cookies ride on
  ordinary requests; keying on them would switch caching off for everyone.

### ⛔ The hard rule, and how it is enforced

A response that depends on who is asking must never reach a shared cache — that is how one user sees
another's session. Nothing here relies on remembering it:

1. **`httpx.MarkPrivate(r)`** marks the response private. ⚠️ **EP-06:** the session accessor must
   call it, so no handler can read a session without it. It **panics** if the `Cache` middleware is
   not on the route, or if the response has already begun — fail closed, found by the first test.
2. A `Set-Cookie` on the response or an `Authorization` on the request makes it private **with no
   call at all.**
3. **No handler sets the header.** `Cache` replaces whatever a handler set, and
   `TestOnlyCacheGoNamesTheHeader` parses every non-test Go file and fails if any file but
   `cache.go` names `Cache-Control` in a string literal. `TestHeaderScanSeesAPlantedHandler` plants
   offenders to prove the scan can see one. The scan sees **literals** only — a name built by
   concatenation is invisible to it — so it is the tripwire; the runtime override is the guarantee.
4. **A flush cannot get past it.** A flush sends the header map as it stands, so the `Cache` writer
   implements `FlushError` (and `Flush`) and decides the header **before** any flush reaches the
   wire; `ResponseController` asks for `FlushError` before it follows `Unwrap`. Without it, verify
   round 1 measured a handler's own `public` leaving beside its `Set-Cookie`, and a `MarkPrivate`
   response leaving with no header at all. `MarkPrivate` after a flush panics, like after any write.
   ⚠️ A **hijacked** connection is raw bytes the handler writes itself — outside this policy by
   construction.

⚠️ **EP-06 — the session cookie needs its own edge rule.** The request's `Cookie` header is not a
trigger here, and it is not one at the edge either: `Cookie: sid=…` gets a `HIT` (measured
2026-09-29). So once accounts exist, a logged-in reader asking for a cached URL is served the
anonymous copy, whatever the origin would have said. The change that creates the session cookie adds
a bypass rule on that cookie's **name** to `scripts/cloudflare-cache.sh`, in the same PR.

Tests: `internal/httpx/cache_test.go` — 52 policy cases, the planted offenders, the production
router (panic, partial write, 404, 405, `/health`), a real server for 1xx, and flush-first handlers
on the production router over a real server, through both `ResponseController` and `http.Flusher`.
**30 mutants of the policy and the router order, all killed; 5 of the scan, all killed; 4 of the
flush path, all killed** (AOC-026 build).

### The edge (Cloudflare)

Two **Cache Rules** on `aoc-codex.app` (zone ruleset, phase `http_request_cache_settings`), in
this order:

1. **Every request to the apex** —
   - **eligible for cache** — Cloudflare does not cache HTML otherwise;
   - **edge TTL: use `Cache-Control` if present, bypass if not** — so a response without the header
     is never cached. The origin always sends one; this is the fail-closed backstop;
   - **browser TTL: respect origin** — ⚠️ the zone's `browser_cache_ttl` is **14400** (read
     2026-09-28), which would otherwise stretch pages' `max-age=60` for browsers.
2. **A request carrying `HX-Request: true` or any `Authorization`** → **not eligible** (bypass).
   The origin's `no-store` stops such a response being *stored*, but a request that matches a stored
   page never reaches the origin: before this rule an `HX-Request` and an `Authorization` request
   for the cached stylesheet both got `HIT` (verify round 1, measured). Rule 2 comes after rule 1
   because a later matching rule overrides an earlier one's setting. Not on `Cookie` — see EP-06
   above.

The rules and the zone's `min_tls_version` (1.0 → **1.2**) are applied by
**`scripts/cloudflare-cache.sh`** — `check` (read only), `apply --dry-run` (every read, no write),
`apply`, `purge <url>`, `rollback`. Pierre runs the writing modes, because Cloudflare writes are
refused from the assistant. It looks before every write (safe to re-run; stops, unchanged, if the
cache phase holds a rule it did not make) and judges success by HTTP status, not reply shape.
Needs the token's **Zone → Cache Rules → Edit**, **Zone → Zone Settings → Edit** and, for `purge`,
**Zone → Cache Purge** — all three proven by a real write on 2026-09-29.

**Applied 2026-09-29** (Pierre ran `apply`: TLS 1.0 → 1.2, the rule added). Measured after:
TLS 1.0 and 1.1 are refused with the server's `protocol version` alert, 1.2 and 1.3 answer 200; `/`,
`/health` and `/v1/items` answer `cf-cache-status: BYPASS` (was `DYNAMIC`) — eligible, but the
running build sends no `Cache-Control` to cache by, which is the rule seen working before this
policy deploys. The hashed assets were already `MISS` then `HIT` without the rule, because
Cloudflare caches static file extensions by default.

**Rule 2 applied the same day** (Pierre ran `apply` again; one PUT, the free plan accepted the header
condition). Measured on the cached stylesheet, twice, at SIN/HKG/NRT: plain → `HIT`;
`HX-Request: true` → `DYNAMIC`; `Authorization: Bearer …` → `DYNAMIC`; `Cookie: sid=…` → still
`HIT`, as intended; `hx-request: TRUE` → `HIT`, because the rule matches exactly — which is why the
origin's test is exact too. Before it, all four were `HIT`.

### Purging

A content change is visible to readers within the edge window (an hour for pages) — plus, because
pages carry `stale-while-revalidate=86400`, possibly **one more request** after the hour, which may be
handed the old copy while the edge refetches (how Cloudflare treats it is not measured). To make it
immediate, purge the URLs:

```bash
bash scripts/cloudflare-cache.sh purge https://aoc-codex.app/<path>
```

Performed once, on 2026-09-29, on `/assets/app.2ab669de.css`: `HIT` before (cached ~18 h), then
`MISS`, then `HIT` with `age: 1`. The dashboard does the same under *Caching → Configuration →
Purge Cache → Custom Purge*. Either needs the token's **Zone → Cache Purge** permission.

### Bypassing the cache, for debugging

- **The origin directly:** `https://aoc-armory-snapshot-production.up.railway.app` is the `api`
  service's Railway domain — no Cloudflare in front (`server: railway-hikari`, no `cf-ray`).
- **Through Cloudflare, uncached:** add a throwaway query string (`?cb=<random>`); it is part of the
  cache key, so the request goes to the origin.
- `cf-cache-status` on any response says what the edge did (`HIT`, `MISS`, `EXPIRED`, `BYPASS`,
  `DYNAMIC`).

## Server lifecycle

`cmd/api/main.go` sets `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout` and `IdleTimeout` — all
four are unset by default in `net/http`, and a server without them is eventually held open by slow or
dead clients until it runs out of file descriptors.

It shuts down gracefully on SIGINT/SIGTERM with a 20s drain. **Railway sends SIGTERM on every
deploy**, so without this every deploy cuts off whatever was mid-request.

## Build identity

`internal/version` holds `Version` and `Commit` as `var`s (not `const`s — `-ldflags -X` can only
write to a var), and `/health` reports them, so a running container can always be traced to a
commit.

- **Production:** the version is the repo's **`VERSION` file** — one `x.y.z` line, the one place a
  release bumps it (AOC-015). The `Dockerfile` reads it and **fails the build** on anything that is
  not `x.y.z`, so Railway keeps the running deploy rather than shipping a bad value.
  `TestTheVersionFileIsSemver` pins the same rule in CI. Before 0.1.0 a Railway service variable fed
  `0.0.0-dev` through a build arg; that variable is now unused.
- **The commit:** a `COMMIT` build arg if set, else Railway's runtime `RAILWAY_GIT_COMMIT_SHA`
  (`version.Resolve`), else `unknown` — never blank.
- **Local builds:** the `Makefile` injects `git describe` and `git rev-parse`, so `make run` says
  exactly which tree it is (`v0.1.0-3-gabc1234-dirty`).

## Rendering (the HTML surface)

Added by **AOC-024**. This is the pattern every later page copies, so it is worth reading
once before adding a page.

```
web/src/app.css          Tailwind input      ─┐
web/src/htmx.min.js      vendored HTMX        │ make assets
                                              ▼
internal/assets/built/   app.css, htmx.min.js   COMMITTED, embedded, content-hashed
internal/templates/html/ base (the shell) · home · smoke · echo
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
| Both HTMX branches send `Vary: HX-Request`, and an HTMX response is `no-store` (§ Caching) | A cache handing a browser the bare fragment — a blank-looking site, very hard to diagnose |
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

### The shell (AOC-046)

`base.html` renders the same chrome around every page: a header (the logo home, and a
`<nav aria-label="Sections">` of the site's sections), `<main>`, and a footer. **The footer carries
no credit, no notice and no contact** (Pierre, 2026-09-29, `DECISIONS.md`): the site names no
source and no other creator anywhere; where the data came from is said once, on the Info page
(`/info`, AOC-056). AOC-046 shipped the credit and a Funcom notice there; AOC-055 removed them.
`TestEveryPageCarriesTheShell` asserts their **absence**. The nav comes through `View.Nav`, filled by
`pages.(*Handler).view`.

- **The nav lists only routes that exist.** `pages.siteNav` is the list;
  `TestEveryNavLinkIsARegisteredRoute` follows every href through the real router and wants 200.
  A section is added to the nav in the ticket that adds its page, never before.
- **The theme is tokens**, in `web/src/app.css` `@theme static` — `static` because a token reached
  only through a database value (`var(--color-{{.ColourToken}})`) is invisible to Tailwind's scanner
  and plain `@theme` would drop it from the build (AOC-046 verify round 1; `DECISIONS.md`
  2026-09-29). IBM Plex Sans/Mono (Google Fonts, linked
  from `base.html`), `ink` (the page), `paper`/`muted`/`link` (text), `line` (borders), and the
  rarity colours. `TestThemeTextTokensPassAA` reads that block and fails any text token under
  4.5:1 on ink — the design's own note records that the raw game colours fail (Epic 2.0:1, Rare
  3.1:1), so they were lightened along their hue.
- **Rarity colours and class short names are data**, not template literals:
  `rarities.colour_token` names a CSS property (`rarity-epic` → `--color-rarity-epic`; NULL = no
  colour of its own, renders as paper) and `classes.short_name` holds the abbreviation players use
  (Conq, DT, Guard … — Pierre, Tier A). `/v1/taxonomies` carries both, so the JSON surface and
  the pages read one row. **`classes.sort_order`** (AOC-065, migration `20261001120000`) is the one
  order every class list reads: the filter chips, the list's `Conq/DT/Guard`, the item page and
  `/v1`. It runs Soldier, Rogue, Priest, Mage (Pierre, Tier A), with the validated design's order
  within each. A class without one sorts last. `TestClassesAreOrderedSoldierRoguePriestMage` pins
  it. A template paints a rarity with
  `style="color: var(--color-{{.ColourToken}})"` and never names a rarity itself.
- Pages are wide (`max-w-7xl`): the Armory table needs it; prose pages constrain themselves.

### Adding a page

1. A template in `internal/templates/html/` defining `content`.
2. A line in `pageTemplates`.
3. A handler in `internal/pages/` that builds a `View` and calls `Render`.
4. If it is a **section**, its `NavItem` in `pages.siteNav` — in the same commit as its route.
5. If the page reads its own data shape, that shape lives **beside the template**
   (`templates.ArmoryData`) with a **probe value** in `pageProbes` that exercises every branch, on
   obviously fake rows — so a field the template reads that nobody declared fails the boot.

**Fragments** are every `html/*.html` that is neither `base.html` nor a page — derived from the
filesystem, not listed (a fixture FS in a test carries only what it holds). Every page is parsed
with all of them, so a page can `{{template "armory_rows" .Data}}` the same definition its HTMX
answer uses: **one definition of the rows, two renderings** (AOC-047). A fragment's probe goes in
`fragmentProbes`.

**A page's own rejections** (a bad query, a page past the end) go through `httpx.RejectHTML`: the
same dependency-free HTML as the router's 404/405 on the HTML surface, JSON on a machine surface —
never `Fail`, which is JSON-only, and never a template, which may be the thing that broke.

**The Armory list** (`/armory`, AOC-047; Slot and Type in two columns since AOC-062 — Type is the
armour weight when there is one, otherwise the item type's **name**, following the tooltip's own
`Light Armor - Hands` / `Crossbow - Main Hand`; the phone row keeps one line) is the first content
page and the pattern for the rest:
`items.ParseFilters` (the `/v1` parser) reads the query string, `items.Service.List` (the `/v1`
service) answers it, and the handler adds only what a page owns — `p`, the URLs it links, the
`<title>`/description/canonical per state, and the honest empty state whose numbers come from
`items.Service.IDSpan`. Each row links to its item page (AOC-048); before that page existed rows
linked to nothing, because a link to a 404 is a bug — the same rule as the nav.

**The full-window app** (AOC-065, Pierre's validated design; `product_management/reference/ui-guidelines.md`
is the visual source of truth, distilled from `discovery/design/armory-2026-10-01/`). A page that sets
`View.App` is laid out from `lg` up as the design's app, and `base.html` does the rest:
- **The body is the window** (`lg:h-screen lg:overflow-hidden`, a flex column): the 52px header, then
  the page's own fixed bars, then panes that each scroll inside themselves (`min-h-0`, and
  `overflow-auto` for the table, `overflow-y-auto` for the filter pane). **The document never
  scrolls** — measured at 1024, 1440 and 1920 px, with and without JavaScript. ⚠️ Every scrolling pane is `relative`: the visually hidden checkboxes are
  absolutely positioned, and without a positioned ancestor the ones far down a pane counted
  toward the *document's* height (it scrolled 109px with JavaScript off until this was found).
- **Below `lg` it is an ordinary scrolling page** (the canvas's 1b), so a phone is never a squeezed app.
- **Scrollbars are the design's** (9px, `line-control` thumb, `ink-pane` track), drawn with
  `::-webkit-scrollbar` in `app.css`'s base layer. ⚠️ Chrome ignores those rules on any element whose
  standard `scrollbar-width`/`scrollbar-color` is set, so the standard pair applies only where
  `::-webkit-scrollbar` is unsupported (`@supports not selector(…)`, Firefox). The corner between
  two bars is `ink-pane` too — the prototype leaves it the browser's white (a noted deviation). A global
  `scrollbar-width: thin` hid every bar until AOC-065 verify round 2.
- **The phone sheet's bar is the canvas 1b's** — Cancel and a full-width "Show N items", 46px. Cancel
  is a `type="reset"`: it restores every control as the page drew it, the sheet's own unticked
  checkbox included, so the sheet closes with nothing applied and no script. With scripts each change
  has already applied live, so `armory.html` also returns, through htmx, to the URL the sheet was
  opened on. On a wide screen without scripts the bar is Reset and the submit; with them it is hidden.
- **The design's type is `line-height: normal`** (its `font` shorthand resets it), scoped to `.armory`
  and the header; prose pages keep 1.5. With it, and the first cell's own 13.5px (a cell that inherits
  16px sets a taller line box), every row is the design's 37px; it was 43px, then 39px. The table's
  lines are on its cells with separate borders: a collapsed table gives half of each line to the next
  row (a 30.5px header against the design's 31) and its sticky header's line scrolls away.
- **Tokens:** `app.css` carries the guideline's palette; `TestThemeTextTokensPassAA` measures every
  text token against every pane background (the design's two faintest greys are lifted to `faint`,
  `#837d74`, for AA — a noted deviation). Computed styles of 61 elements were checked against the
  guideline in a browser (AOC-065 ticket Log).
- **JavaScript-only controls** (⌘K, Copy link, the level sliders) carry `.js-only` and stay hidden until
  `base.html`'s first script marks `<html class="js">`; what only a script-less reader needs carries
  `.no-js-only`. Each level bound has **two inputs and exactly one submits, in every state**: the
  hidden one ships `disabled` (`data-js-enable`), the number input ships enabled (`data-js-disable`),
  and the page's script flips both at load **and for every pane htmx swaps in** (`htmx:load`). The
  hidden inputs are drawn even when the other filters leave no item with a level and there are no
  sliders; until AOC-065's delta verify (F16) they came with the sliders, and with scripts on such a
  state dropped the bound. ⚠️ Not
  `<noscript>`: htmx parses a swapped fragment with scripting off, so `<noscript>` content became live
  fields after the first update and every bound was sent twice (AOC-065 verify round 1).
  `TestTheLevelSlidersNeverDoubleABound` pins the markup on the page and on a live update, with a
  span and without; the browser half (one value per bound in the submitted URL after a swap) was
  measured.
- **The filter pane is exactly the design's five sections** (Pierre, 2026-10-01: *"I want exactly like it
  is in the design"*): Rarity (checkbox rows with counts), Slot, Armour weight and Class restriction
  (toggle chips, no numbers), Item level (two stacked sliders and the note). Vendor price, required
  level, currency and set left the pane. They still filter by link and by `/v1`, and an active one
  rides along as a hidden input and shows as a pill (`TestTheOtherFiltersStillApplyAsPills`). Its
  parts were measured against the prototype in the same browser: rows 16px on a 21px pitch,
  checkbox 13px/3px, chips' font, padding, border and radius, and the sliders' 2px margin and 15px
  pitch are identical.
- **Not built → not shown**: the design's source tabs and tree (AOC-050), gear builder (AOC-051) and
  account (EP-06) are absent until they ship. **The exception is the header's tabs** (Pierre,
  2026-10-01): AA's, Feats, DJ/Raids and More are shown, and each leads to a "Coming Soon" page.
  `pages.siteNav` marks them `Soon`; that gives each a route, `noindex`, and keeps it out of
  `sitemapStatic`. So every nav link still answers 200 (`TestEveryNavLinkIsARegisteredRoute`), and
  no thin page is offered for indexing (`TestTheHeaderTabsLeadToComingSoonPages`).

**The filter rail** (AOC-049) is the pattern for any page with filters:

- **One `<form method="get">`** holds the search and the rail, so each keeps the other, and the
  page works with JavaScript off (Apply is a submit). The controls are the validated design's and
  nothing else (AOC-065): rarity, slot, armour weight and class as checkbox groups — several ticked
  at once, none ticked meaning any (AOC-064) — and one level range, the item level, as two sliders
  over number inputs for a script-less reader. **No radio group, no `<select>`:** every other filter
  the parser accepts (vendor price, required level, currency, set, region, tier, place…) has no
  control; an active one rides along as a hidden input and shows as a pill. A page built from this
  pattern starts from its own design, not from a list of every filter the API takes.
  Every value and count is `items.Facets`; the handler turns it into controls in
  `pages.buildRail`, and the template only prints. A 0 is muted, never hidden.
- **Every link is built from the whole state** — `items.Filters.Values()`, the parser's inverse,
  pinned by a round-trip test that sets every field. Before it, the page's URL builder knew only `q`
  and `sort`, so `/armory?region=x&p=2` linked to an unfiltered page 3. Filters the rail has no
  control for (`region`, `tier`, `place`, …) ride along as hidden inputs **inside the rail**, and
  show as chips.
- **With HTMX, a change re-renders the rows and the rail in one request.** The rail's scroll box
  carries `hx-get hx-trigger="change" hx-include="closest form" hx-target="#results"`; the answer is
  `armory_update` — the rows, plus the rail, the pills and both "active" counts (the phone
  button's and the collapsed strip's) **out of band**
  (`hx-swap-oob="innerHTML:#…"`), so once an answer has landed (and no press on a slider holds
  its pane back; see below) the counts describe the rows beside them, and the hidden inputs and the
  sort match its state. Inputs keep stable ids, so HTMX hands
  focus back after the swap. `HX-Push-Url` is the state's canonical URL, not the form's raw query with its empty
  fields.
- **The phone sheet is CSS only**: an unnamed checkbox (`#filter-sheet`, never submitted, never in
  the URL) and one `:has()` rule in `app.css` that turns the rail into a full-screen sheet below
  `lg`. From `lg` up the same pane **collapses to a 34px strip** through another unnamed checkbox
  (`#filters-collapsed`), as the design's ›. Not `<details>`: a closed `<details>` hides its content
  at every width, so the desktop rail would need a second copy of the form.
- **The sources pane is the same pattern on the left** (AOC-068; the data is § The source tree).
  - **What it shows.** The main categories (`items.Service.Tabs`) are links under the search. The
    active tab's tree (`items.Service.Tree`, counted under every other filter) goes in a 288px pane
    left of the table. `pages.buildSources` turns the tree into rows, and the templates only print
    them.
  - **Links, not controls.** Every tab and every branch is a link built from the whole state, so the
    pane works without JavaScript. A tab link drops the pick and keeps the filters. The picked branch
    and every branch above it are open: the open branches derive from the pick, never from stored
    state. A picked, open branch links to the branch above it, which is the design's toggle. A set
    of end branches is split under the design's halves ("loot / drops", "quest / vendor"), each
    branch counted for that half and linking with `get=`.
  - **The state rides in the form.** `tab`, `source` and `get` are hidden inputs in the rail
    (`buildRail`), so a filter changed in the pane keeps the pick. The page's script copies a link's
    URL into the form as it goes, the same as for a pill. The pick is a pill ("source: …", the
    design's second, after the search). "Clear all" keeps the tab, because the tab is not a filter.
  - **One answer redraws it.** `armory_update` carries the tabs, the pane's head, the tree, the
    selected-source box and both of its counts out of band. The tree's scroll box is never swapped
    itself, so it keeps its scroll position.
  - **It folds like the filter pane:** an unnamed checkbox (`#sources-collapsed`) folds it to a 34px
    strip on the left. Below `lg` it is a second CSS-only sheet (`#sources-sheet`), opened by
    "Sources · 1" beside "Filters · N" (the design's mobile rule: only filters and the source tree
    are behind a sheet). Its "Show N items" closes it, since every pick has already applied.
  - **AA:** a row under the pointer sits on `ink-tree-hover` (#1a1816, the design's). There `faint`
    is 4.34:1, so the caret and count turn `muted-2` (4.85:1). The theme test measures every pair
    written on it.
  - **The selected-source box** shows the picked branch's path and the list's count. It adds
    "· map (x,y)" when the branch is one point: every row of it has the same coordinates
    (`TreeNode.Coords`; Pierre, 2026-10-02). It shows nothing otherwise.
  - **An unknown `tab` is a 404**: it names no panel, like a page past the end.
- **A live request that the parser rejects says why** (AOC-049 review). htmx discards every 4xx by
  default, so the 400 for an empty range left the rail looking dead. `base.html` carries an
  `htmx-config` meta tag that adds one rule, "a 400 is swapped" (other 4xx/5xx stay unswapped), and
  the handler answers an HTMX request it rejects with `armory_invalid`: the parser's reason, in place
  of the rows, with `HX-Push-Url: false` and **no rail redraw**, so the bad value stays where the
  reader can fix it. `templates.Engine.FragmentStatus` is a fragment with a status. Without
  JavaScript the same reason is on the `RejectHTML` page.
- **Back always reloads the whole page from the server** (AOC-063, AOC-065). On a history miss htmx
  used to re-request the URL *as an HTMX request* and put the answer in `<body>`. A handler that
  serves fragments (today the armory's) answers that with one, so Back left bare rows (live since
  0.2.0 through the pager, and reachable from every filter change once the rail pushed URLs). The
  `htmx-config` tag sets `refreshOnHistoryMiss: true`, so a miss reloads the page normally.
  **`historyCacheSize: 0` makes every Back a miss.** A snapshot is the live DOM at the moment the
  next answer lands, and on a live pane that can hold what a server never drew: a slider's value
  written but not yet sent, or a pane whose redraw was skipped mid-drag. Back restored those
  (AOC-065 delta verify 3, N2 and P1). The server's page for a URL is correct by construction, and
  it is one request. These are two config lines for every htmx page, rather than a full-page branch
  for `HX-History-Restore-Request` in every handler.
- **Only the latest action's answer lands: `hx-sync="this:replace"` on the form.** Every htmx
  request in the form inherits it: the pane's, the pills', sort's and the pager's. A new one aborts
  the one in flight. An answer's pane is drawn from the state its request carried, so a stale answer
  landing over a newer change used to wipe it (a second box ticked, a slider moved). htmx's default
  queue then sent the change *after* that redraw, reading the wiped form. Measured: a second tick
  lost (live since AOC-049's rail), and slider moves lost or read again in a new span (AOC-065 delta
  verifies). With replace, a change is sent at once with the form as the reader has it, and nothing
  older can land after it.
  - **A link's request sets the form to its state as it goes.** A request that does not come from
    the pane (a pill's ×, "clear all", sort, a page, Cancel) names its state in its URL. On
    `htmx:beforeRequest` the page's script copies that URL into the form by parameter name: boxes
    ticked or not, hidden inputs and the search box set, and a hidden input for a parameter that has
    no control (never `p`: a change starts again at page 1). It knows no filter, only names. So a change made before the link's answer lands
    builds on the link's state, not on the page being left. Before this, a pill removed and then a
    box ticked inside the round trip brought the pill back, and the search box, which no redraw
    touches, kept a removed query and sent it again (AOC-065 delta verify 4, P3).
  - **Cancel returns to the form as it was when the sheet opened**, not to the address bar, which a
    link's request in flight has not updated yet. After the form's reset, which alone would restore
    the page as first drawn and bring back a removed search (delta verify 5, N8), the script copies
    that state into the form. If anything changed in the sheet, a request returns the rows to it,
    aborting a change in flight.
  - **A drag.** An answer landing mid-drag would replace the slider under the pointer and cut the
    drag short (F19). While a slider is pressed with the primary button, the pane that answer draws
    is held back (`shouldSwap = false`); the rows, pills and counts still update. When the press
    ends, the held pane is drawn, unless something newer superseded it (below). If the press moved the
    slider, its `change`, which fires in
    the same task as the `pointerup` (measured for mouse and touch), is not sent as it is: that would
    send the pane from before the answer, and undo a link (N4). The moved bound is carried into the
    held pane, and that pane is sent. Whatever is still held after that is drawn, with no request.
    No pane outlives its press (delta verify 5, N7: when the slider's value changed without the
    pointer, the press was taken for one that moved, and a stale pane was drawn much later).
  - **What the reader sees:** every quiet end state equals a direct load of its URL, and that URL is
    what they set. (The search box can also hold an unsent draft: see below.) No value is reinterpreted. A bound beyond the new span applies as set; its legend
    and pill read "≥ 85", never "85–80". This was measured on the real corpus with 40–300 ms of added
    latency, real drags, key presses and touch:
    - two ticks;
    - a slider moved or dragged during a request;
    - a slider taken to its end (no bound) while the span widens;
    - an answer with no span;
    - five arrow presses;
    - a press, tap or swipe while an answer lands;
    - a pill ×, "clear all" or sort, then a press or a held press;
    - a link, then a change inside its round trip, or a change, then a link;
    - Cancel within the first round trip;
    - Back and Forward.
  - **Requests:** one per change; the older ones are aborted (five arrow presses send five, and only
    the last answer lands; nothing lands until the reader pauses). A link aborts a pending change,
    which is right: it goes to the state it names. An aborted request is a 499 in the log, not an
    error (§ 3).
  - **One history entry per new state:** an answer for the state the reader is already on carries
    `HX-Replace-Url` instead of `HX-Push-Url`, so Back never lands on the same page twice. The state
    is `HX-Current-URL` read by the same parser and defaults (`canonicalOf`), so the form's raw query
    after Enter or Apply counts as the state it names.
  - **Pills follow the vocabulary's order** (the pane's), whatever order the request named the values
    in. Slugs the vocabulary lacks, and places, come sorted as the canonical URL sorts them. A live
    answer and a direct load of its URL show the same pills.
  - **A draft in the search box is the reader's, not the state's.** Text typed and not yet sent stays
    in the box, and the next change sends it, as the form always has. A link replaces it with the
    link's own search, as it would without scripts. Cancel restores the state the sheet was opened
    on, with the search that state was asked with, and leaves the draft in the box (delta verify 6,
    B5b: Cancel had applied a search never sent).
  - **A touch the browser takes over is not a move.** This covers a scroll that starts on a slider's
    track (Chrome jumps the thumb under the finger at `touchstart`, then sends `pointercancel`) and a
    touch the system cancels. The slider goes back to where the press found it, with its hidden
    input and legend, and nothing is sent. Chrome has sent no `change` after a put-back in any run;
    should one come when the finger lifts, it is not sent. A link clicked during the press sets what
    the press is put back to (delta verify 8, N12), and a press ends only on its own pointer, so a
    second finger cannot put back the first one's drag. Before, a scroll of the phone sheet applied a level bound (delta verify 7,
    N10), and a cancelled touch left an unsent one shown (delta 6, A1c).
  - **Nothing held is drawn after something newer:** a pane drawn while nothing is pressed, or any
    request sent, discards a held pane. The newer answer draws the pane (R; delta verify 7, R2: a
    link sent during a press was undone by the press's held pane).
  - **Known limit:** a Tab pressed while the mouse holds a slider and an answer is held ends the drag
    early. The end state is consistent (A3c); it takes two inputs at once.
  - **A 400:** its reason replaces the rows. The pane keeps the reader's input to correct, with the
    last drawn counts.
  - **Relies on** the platform firing `change` when a slider's move ends. Mouse, touch, keys and
    assistive technology all do; a script that sets a value and fires only `input` is not a move.
  - It is an enhancement only, like the item page's back link, and moves to a hashed asset the day a
    CSP arrives.

**The item page** (`/armory/{slug}`, AOC-048) is one `items.Service.Get` — the `/v1/items/{slug}` call
— rendered through `templates.NewItemData`, which groups the sources for display and does nothing
else. Four things it added that later pages inherit:

- **`Detail.Display`, a `json:"-"` block**: the names and colours the page shows where the contract
  carries slugs (rarity name and colour token, type, weight, binding, slots, classes with short
  names, DPS), filled from the **same rows** in `hydrate`. ⛔ **Not the set's size**:
  `sets.declared_piece_count` is the last per-item `set_pieces` the importer saw, and inside one set
  those vary (Waning Dusk: 1, 2, 3 across 16 items) — the page lists the items sharing the name and
  states no size (AOC-060). So the page reads
  exactly what `/v1` reads without `/v1` growing a field per page need; exposing any of it is an
  additive decision of its own. `SourceRef` carries three the same way (type name, tier name, the
  quest's `armory_label` — shown *as listed*, never as a quest name, which is unknown).
- **Per-page `og:image` and JSON-LD.** `View.OGImage` is the tooltip image, so a pasted link
  previews the item as the game shows it; `View.JSONLD` is a value `base.html` writes into
  `<script type="application/ld+json">` — html/template marshals it as JSON there and escapes `<`,
  so no field can close the script. `templates.ThingLD` is schema.org **`Thing`, not `Product`**:
  Google reports a Product with no offer, review or rating as an error, and a game item has none.
  `TestItemPageJSONLDParses` parses it back.
- **Groups come from the rows, never from a literal.** Sources group by each row's own acquisition
  type, in the order they first appear; one uniform row prints whatever is present (vendor, quest as
  listed, place — boss, container, and the `raid` / `Unchained` flags — the latter only when the
  place's name does not say it, which "Otherworldly Junction" does not); a group's **cost and tier
  columns exist only when one of its rows has one** — so drops (0 of 3,436 carry a cost) and quests
  (228 of 229 have no tier) get no column of dashes without the page asking which group is which.
  A line identical to one already shown is shown once (42 items carry rows equal in every column but
  the id); `/v1` keeps every row. A row with nothing in it says "No place recorded".
- **One rule, two pages.** `items.Price` is the only spelling of a cost ("9 Simple Relic I + 2
  Gold") — the list row and the item page both call it; `templates.typeRepeatsSlot` decides for both
  when an item type is worth printing beside its slot.
- **A few lines of inline script may ENHANCE a page, never complete it.** The back link is
  `/armory` in the HTML; the browser upgrades it to the reader's own search when `document.referrer`
  is a same-origin `/armory?…`. It is client-side **because** the page is edge-cached for an hour: a
  server that read the Referer would store one reader's search and hand it to everyone.
  `TestItemPageIsTheSameWhoeverAsks` pins that the bytes do not change with the Referer. There is no
  Content-Security-Policy today; the day one is added, this script moves to a hashed asset.

⛔ **Nothing sits below the tooltip image.** Its size is not in the data (90% of tooltips are 208–344
px wide and 337–512 tall, measured on the local archive 2026-09-30), so the browser cannot reserve its
box — and the first build, which put Set and Sources under it, moved them **260–460 px** when it
arrived (CLS 0.08–0.19; AOC-048 verify round 1). So the page is **one block of text, then the
image**: from `lg` up the image has its own fixed `22rem` column beside the text, which is
top-aligned and never moves; below `lg` it comes last. Measured with the image served 2 s late:
headings stay put, CLS ≤ 0.02 on a phone and ≈ 0 on desktop.
`TestNothingOnTheItemPageSitsBelowTheTooltip` pins the order. A wider tooltip (744 of 4,645) scales
to the column, so the image links to itself at full size. It is **not lazy** (on desktop it is the
main above-the-fold image), and it is portrait, so the page sets `View.Card = "summary"` —
`summary_large_image` crops to 2:1 and can cut the item's name off. Storing the dimensions would let
the browser reserve the box anywhere, but needs the snapshot and a re-import.

If the page needs data the startup probe does not supply, the probe **fails** — which is the
point: it should not be possible to add a page whose data nobody declared.

### Being found: robots.txt, the sitemap, one indexed host (AOC-025)

`internal/pages/seo.go`. **The sitemap is built from the database and the nav, never from a list**:
`/`, every built `siteNav` section (it lists only routes that exist; a Coming Soon tab is skipped), then every item slug in item-id
order (`items.Service.Slugs`, paged by the chunk) — so an import that adds items adds their URLs,
and a new section is in the sitemap the day it enters the nav. `/sitemap.xml` is an index; chunks
hold at most the protocol's **50,000** URLs (`sitemapMaxURLs`, boundary-tested; a full chunk of the
longest possible slug is well under 50 MB). XML is written with **`encoding/xml`**, not a template —
html/template escapes for HTML.

- ⛔ **No `<lastmod>`.** No row carries a real modification time and the import is a full replace;
  a stamp would be the last import's, on all 4,646 at once, and a lastmod that is not accurate is
  one Google learns to ignore for the whole site. Revisit when community edits (EP-06) give rows a
  real one.
- **`robots.txt` disallows machinery only** (`/_smoke`, `/v1/`, `/health`) and names the sitemap.
  `/assets` stays open — Google renders with our CSS. ⚠️ Cloudflare's managed robots.txt is on for
  the zone and **prepends its comment block** to ours; the origin's rules follow it.
- **One indexed host, by redirect.** Requests reach the origin as `Host: aoc-codex.app` (Railway
  routes custom domains by Host, measured 2026-09-30); the service's own Railway domain served a
  crawlable copy. `httpx.WithCanonicalHost` — a router option, so the router tests cover it as
  composed, 404 page and `/v1` included — **301s every other host to the same path on
  `PUBLIC_BASE_URL`** (308 for writes), except `/health`. Chosen over `X-Robots-Tag: noindex`
  (the first build): a noindex beside a canonical pointing elsewhere is a mixed signal Google may
  carry to the target, and a redirect is also *loud* — a wrong host rule shows as a broken site at
  the release check, not as a site quietly dropping out of the index. Not robots.txt: disallowing
  the crawl would stop Google seeing any signal at all.
  ⛔ **`PUBLIC_BASE_URL` is required in production, parsed strictly and must be https**
  (`httpx.ResolvePublicBase`, table-tested — the rule was first written in `main`, where dropping it
  failed no test) — unset, malformed or http, the boot fails, where Railway keeps the previous deploy
  serving. The Location is built by `canonicalLocation` from the request's path and query only, the
  path always starting with `/`: the first build's `base + RequestURI()` let `GET x:@evil.example/`
  become `https://aoc-codex.app@evil.example/` (verify round 1). The release checks that the live apex answers 200, not a redirect
  (`workflows/5-release.md` § 4).

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

   **Asking for a place means everything inside it (AOC-038).** The list's `place` filter goes
   through `Service.expandPlaces`: one recursive query (`ExpandPlaces`) adds every place under each
   named one, at any depth, and a named place that contained something makes the view collapsed
   (each item once), like any view of several dungeons. The named slugs are always kept, so a name
   no place has still matches nothing instead of emptying the list into "no filter".

   ⛔ **A source does not repeat a geography its place already supplies (AOC-037).**
   `item_sources.region_id` / `map_id` are written **only when the place cannot supply them** —
   no `place_id` at all, or a place with no map. They exist for the 1,732 sources that have a
   region and no place, not as a second opinion about a place that has one.
   **This is not tidiness, it is a defect that shipped:** 196 rows published `cimmeria` for two
   dungeons that are in Kheshatta, in Stygia. Every one arrived through the container
   `Acheronian Cache`, whose observed sources straddle two regions, so its geography was resolved
   **once — from the first of them —** and stamped onto every item it holds. The rows imported
   and the counts matched, because *the disagreement was the bug and each column was individually
   plausible*. Worse, the wrong value wore the **highest** confidence: it was attributed to
   `reference_geography.json`, so the importer marked it `verified` while the correct `derived`
   value sat in the sibling rows. `TestNoSourceContradictsItsPlacesGeography` now asserts that
   **nothing** disagrees, rather than sampling rows that happen to agree.
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
   `equip_location` values are **compound** — `Main Hand, Off Hand` on **390** items and
   `Left/Right Finger` on **188** — and AOC-009 seeds only the **atomic slots** (**14** since
   AOC-054 added the necklace, which no tooltip names as a slot). Both compounds are an item that
   fits **either** slot: `Main Hand, Off Hand` sits on the one-handed weapons (1HB, 1HE, dagger,
   talisman), which go in either hand — Pierre, 2026-09-30 (**AOC-058**). ⚠️ Until then it was read
   as `both` ("a two-hander occupying both slots at once"), from the value's shape alone; the data
   put it on the one-handers all along. The importer now reads compounds from **`compoundSlots`**,
   one recorded meaning per value, and **refuses an import carrying any other** `,`/`/` value before
   it deletes anything — the shape of a value is never read as its meaning again. Nine one-handers
   are `Main Hand` or `Off Hand` alone, and their own tooltips say so ("Talisman - Off Hand"): the
   game restricts those items; they are `single`. Flattened into one column, *"show me every Off Hand item"*
   silently returns 141 instead of 530. `items.slot_fit_id` (`single` · `either`; `both` stays a
   row, and no item carries it) says how to read an item's rows, and it lives on the **item**
   because it describes the whole set: two join rows could otherwise contradict each other.
   **What takes both hands is a fact about the TYPE**: `item_types.two_handed` — true for 2HB, 2HE,
   staff, bow, polearm, thrown; false for 1HB, 1HE, dagger, talisman, crossbow (Pierre); NULL for
   types with no main-hand item. The gear builder reads it; nothing lists weapon types in code.
   `TestEveryWeaponFollowsPierresHands` (corpus) checks every weapon's fit against its own rows;
   `TestTheWeaponMigrationAndTheImporterAgree` runs the migration's UPDATE, read from the file,
   against what the importer writes.
   `TestListItemsFindsOneHandersWhenAskedForOffHand` exercises the **shipped** `ListItems` query and
   is mutation-tested: break that query and it fails naming the lost one-hander.
   `TestAskingForOffHandItemsReturnsOneHandersToo` pins the same property at the schema level.
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

### The armory importer (AOC-011)

`cmd/import-armory` — the first `cmd/` binary besides the server, and the only writer the item
tables have.

```
DATABASE_URL=… go run ./cmd/import-armory -snapshot ../armory_snapshot/items_clean.json -dry-run
DATABASE_URL=… go run ./cmd/import-armory -snapshot ../armory_snapshot/items_clean.json
DATABASE_URL=… go run ./cmd/import-armory -confirm-host <host:port>     # a non-local target
DATABASE_URL=… go run ./cmd/import-armory -allow-shrink                 # the corpus really did shrink
```

Wiring only, like `cmd/api`: every decision about what the data means lives in `internal/items`.

⚠️ **It says which database it is writing to, before it writes — and a remote one has to be named
back.** The import is a **full replace**: nine tables are `DELETE`d and rebuilt. Its only clue
about the target is `DATABASE_URL`, which is the same variable the Makefile tells you to export to
production for a migration, *in the same shell*. So `db.ConfirmTarget` (`internal/db/target.go`)
runs first, before the snapshot is even read:

| target | what happens |
|---|---|
| local (`localhost`, `127.0.0.1`, `::1`, `postgres`, `db`, `host.docker.internal`) | prints `▶ target: <host>  (local)` and continues |
| anything else, no `-confirm-host` | **refuses**, names the host, says nothing was written |
| anything else, `-confirm-host` naming a *different* host | **refuses** — this is the shell-history near-miss |
| anything else, `-confirm-host` naming it exactly | continues, banner still says `⚠️ NOT LOCAL` |

The flag takes the hostname rather than being a bare `-yes`, because a `-yes` is a flag people
learn to add by reflex while a hostname has to be read off the URL in front of you. `-dry-run` is
refused on a remote target too: it rolls back, but it still takes locks on nine tables of a live
database. The same `HostOf`/`IsLocalHost` pair decides the migration tests' refusal to run
anywhere but a developer machine — **one definition of "is this local?", not two**.
(AOC-005 established the property; AOC-011 verify round 1 found this command bypassing it.)

⛔ **It refuses to shrink the corpus (AOC-042).** The full replace above cannot tell *a smaller
dataset* from *a broken one*. Before this guard existed, a **well-formed 40-item snapshot** would
delete 4,646 items, insert 40, print a tidy report and **exit 0**.

`internal/items.Import` therefore counts `items` **inside the transaction, before the `DELETE`**, and
refuses if the incoming corpus is under **`items.ShrinkFloorPercent` (90%)** of what is already
there. The refusal names both numbers and nothing is deleted.

| situation | what happens |
|---|---|
| the table is **empty** | always allowed — the first import, and every dev run and test |
| incoming ≥ 90% of existing | allowed; the armory moves by single items, not tenths of itself |
| incoming < 90% of existing | **refused**, both counts printed, `-allow-shrink` named as the way out |
| `-allow-shrink` passed | allowed — the legitimate case, typed once by a person |

⚠️ **Truncation was never the danger.** A file cut mid-JSON fails to decode and `DecodeSnapshot`
already refuses an empty one. The dangerous input is a **valid short file** — exactly what a
`--limit N` smoke test produces, which is the same trap AOC-031 describes one layer up.

📌 **The guard lives in `internal/items`, not in `cmd/import-armory`**, so a second caller cannot
route around it (CLAUDE.md rule 5b). `TestTheFloorGuardsImportItselfNotTheCommand` calls
`items.Import` directly to pin exactly that, and the boundary is pinned from both sides because
"roughly 90%" is not a specification.

**It refuses to guess.** Before a single row is written it resolves every name in the snapshot
against AOC-009's seeds. An unrecognised value prints and stops the import, because it means
either the seed is incomplete or the snapshot changed — both need a person, and inventing a row is
what STEP ZERO forbids. Four values are known not to resolve and are listed in
`internal/items/resolve.go` with their reasons; each becomes a **NULL plus an `open_question`**,
never a guess:

| value | rows | what it becomes |
|---|---|---|
| place `World Boss` | 62 | not a location — a spawn category. `place_id` NULL |
| boss `Armsman's Arena` | 12 | already an open question on `places`: Pierre's geography calls it an instance, the armory files it as a boss |
| map `Skull Gate Pass` | 2 | a **place**, not a map — it hangs straight off its region |
| map `Border Range` | 2 | no seeded map matches and nothing says what it is |

⭐ **A recorded decision that stops matching anything also stops the import.** Without that, the
list above would rot into an exemption nobody rechecks — the shape this repo has now found four
times (AOC-019, AOC-020, AOC-032, and the guard inside AOC-032's own fix).

**A slot the tooltip does not name (AOC-054).** A necklace's tooltip line reads `Necklace` where
armour's reads `Light Armor - Hands`, so the OCR had no slot to find and all 146 arrived slotless.
The fix is data on `item_types`, read by the importer, never a literal:

| column | means | set on |
|---|---|---|
| `default_equip_location_id` | the slot an item of this type goes in **when its own record names none** — a fallback, never an override | `necklace` only (AoC>TV's builder, Pierre 2026-09-29). ⛔ Not for other types: two items typed Crossbow and Polearm are really a consumable and a companion (AOC-059), and a default would put them in a hand |
| `is_equipment` | whether an item of this type is worn at all — **nullable, no default**, so a type added later must be classified rather than filed as "not worn" by omission; a NULL fails `TestTheNecklaceSlotAndItsRuleAreSeeded` | `true` on the 23 types that carry a slot in the data plus `necklace`, `false` on the other six |

`slotFit` applies the fallback and the report prints how many items it placed, per type. The rule
runs in **two places that must agree**: the migration `20260930120000_necklace_slot.sql` backfills the
rows already there — so production got the slot **without a 15-minute re-import** — and the
importer applies it on every later run. Measured on a restored production dump: the backfill and a
fresh import produce **byte-identical** `item_equip_locations` and `slot_fit_id` rows — and
`TestTheBackfillAndTheImporterAgree` keeps checking it, replaying the pre-AOC-054 importer, running
the backfill statement read out of the migration file, and comparing with the current importer.
The default is read through sqlc (`ListItemTypeDefaultSlots`), so a column rename breaks the build.

`is_equipment` is deliberately **independent of the slots**: derived from them, a type whose
tooltips never name a slot is simply not equipment, and "every piece of equipment has a slot"
passes on exactly the bug it exists to catch. `TestEveryPieceOfEquipmentHasASlot` (corpus) holds
it, with seven recorded exceptions each read off its own tooltip — and, as with `knownUnresolved`,
**an exception that stops matching fails the test**.

**Join keys that are not names.** Places join on the armory's own pair
`(armory_instance, armory_dungeon)` — `places_armory_key` is UNIQUE on it. Quests join on
`quests.armory_label`, because `quests.name` is **NULL on all 51 rows**: the real quest names are
unknown and AOC-009 refused to invent them. Matching either on a display name looks like it works
and silently resolves nothing.

**Two exclusions, both Pierre's, both counted in the report.** 2 items that can no longer be
obtained never reach the database (the snapshot keeps them — the archive is the preservation).
68 source rows are quarantined: Tier 5 Stygian loot filed under three Tier 1 Cimmerian bosses,
flagged `on_hold` in the generator. Four items are left with no source at all, and that is the
accepted cost of showing the item and blanking the source.

**One transaction, full replace.** The item tables are emptied and rebuilt rather than diffed: the
snapshot is regenerated by every data-quality ticket, so this runs again, and a replace is either
right or it rolls back. Re-running produces identical counts.

⭐ **The count check compares the database against the INPUT, not a constant.** A hardcoded 4,646
would be wrong the first time the snapshot is regenerated, and somebody would fix it by editing
the number. It only ever asks *"did every row I read arrive?"* — which stays true whatever the
snapshot says next. Mutation-tested: dropping nine stat rows fails the import and names the table.

As of 2026-09-20, against dev: **items 4,646 · item_stats 23,063 · item_spell_effects 19 ·
item_sources 6,571 · item_costs 5,956 · item_classes 4,259 · item_equip_locations 4,882 ·
sets 368 · vendors 23.** Since AOC-054 (2026-09-30): **item_equip_locations 5,028** — the 146
necklaces.

### Filtering and facet counts: one definition (AOC-049)

The Armory's filter rail shows, beside every value, **how many items picking it would leave under
the other filters**. That number is only worth showing if it is the total the list then reports, so
the filter rules exist **once**, as per-item flags, and both the rows and the counts read them.

```
WITH filtered AS (             -- byte-identical in ListItems, CountItemFacets, ItemFacetTotals, ListSourceTreeRows
  SELECT i.item_id, …,          -- what the facets group by: rarity_id, set_id, levels, priced
         <q, item_type, place, region, tier, unchained, pvp, source> AS in_base,   -- no facet of their own
         <rarity rule>   AS in_rarity,   <slot rule> AS in_slot,   <weight rule> AS in_weight,
         <class rule>    AS in_class,    <ilvl range> AS in_ilvl,  <reqlvl range> AS in_reqlvl,
         <price rule>    AS in_price,    <currency rule> AS in_currency,   <set rule> AS in_set
  FROM items i …)
ListItems:        … WHERE every flag
a facet value:    count(DISTINCT item) WHERE every flag EXCEPT the facet's own   (0s included)
a group's "Any":  count(*) FILTER (WHERE every flag except its own)
```

- **The four copies are one definition by test** (the fourth is the source tree's, AOC-050). sqlc cannot share a fragment between queries, so
  `TestTheFilterCTEIsOneDefinition` reads `items.sql` and fails on any difference between the CTE
  bodies. Only the header differs, deliberately: the facet queries say **`MATERIALIZED`** (they read
  the flags 6–14 times; inlined, Postgres re-ran every flag's subquery inside each `FILTER` — the
  totals query took **1.0 s** with five filters set, measured), and `ListItems` does not (inlined, its
  filters push down and it stays at 2–4 ms on the corpus, as before).
- **Every flag is two-valued** (NULL coalesced to false), so "all but one" never meets a NULL.
- **"Has a vendor price"** is one expression in the CTE (`pr.priced`, a FROM-less LATERAL subquery,
  which Postgres pulls up into its references, so `ListItems` never runs it unless `price` is set —
  the first build's `LEFT JOIN LATERAL … LIMIT 1` cost the unfiltered list 4 ms → 32 ms).
- **The vocabulary comes from the lookup tables** (`FROM rarities LEFT JOIN filtered …`), so every
  value is listed, 0 included, and none comes from Go (`reference/content-model.md` § 0).
- **The service passes the facet queries the list's own arguments** (`items.facetParams`, and a
  struct conversion for the totals query, which compiles only while sqlc generates the two from the
  same CTE). `TestTheFacetQueriesTakeEveryListFilter` sets every list argument and fails on one not
  carried.
- **Pinned on fake data in CI** — `TestFacetCountsAreTheRowsTheyPromise` checks every count, every
  "Any", the price split and both level spans against `items.Service.List`'s total for the state
  the number promises, under one combination per filter (so a count that ignores any other filter
  differs from its list) — **and on the real corpus** in the gate (`item_facets_corpus_test.go`).
  A mutant sweep dropped each `f.in_*` flag from each condition in the three queries, one at a time
  (190 mutants): the two fixture tests kill all 190. The first two runs did not, and what they found
  is now in the fixture — `OFFSET NULL` is `OFFSET 0` (a "weightless" item silently had the first
  weight), and combinations where a level span vanishes or moves.
- **Several values per facet** (AOC-064, Pierre's validated design). A facet's flag is
  `= ANY(list)`, and a value's count is unchanged in meaning: the items with that value under the
  other facets. For a facet holding one value per item (rarity, armour weight, set), that is exactly
  what ticking it adds. An empty list reaches SQL as **NULL, never `'{}'`** (`items.listArg`, one
  spelling for all seven lists; `x = ANY('{}')` is false for every row). This supersedes AOC-049's
  single-value design (`DECISIONS.md`).

Measured on the dev corpus, without JIT: list 2–4 ms, facets 9–16 ms, totals 4–6 ms.

### The source tree (AOC-050)

The Armory's source panel: main categories under the search (PVE, PVP, Region, Faction, Onslaught,
Other, Pierre 2026-10-02), one active at a time, and a tree of sources for the active one. **It is
Pierre's regrouping of AoC>TV's 39 armory sections**, so the structure is data:

- **`source_tabs`** (the tabs, their order, `levels_note` and `groups`), **`sections`** (AoC>TV's
  section names verbatim, each with its tab) and **`acquisition_groups`** (the design's
  "loot / drops" and "quest / vendor", with `acquisition_types.group_id`).
  `item_sources.section_id` is backfilled from `section_raw` by name; the importer resolves it the
  same way and stops on a name it was never told about. Both new FKs are nullable like every
  lookup: a row with no section is in no tab.
- **A tab's levels.** `groups` is the levels drawn above the location (`section`, `region`, `map`;
  a CHECK holds the vocabulary). Then every tab draws the location: the row's place under every place above it,
  then the boss, or else its vendor, quest giver, container or boss. `items.rowPath` is that rule,
  and the only code that knows level names, which are our own columns, like the sort keys.
  - **Every level is in the path, `-` where the row has none** (verify round 1, F1). A branch
    under a skipped level would otherwise list the rows that do have it too. The panel draws no
    branch for a `-`.
  - **A place is drawn under every place above it**, from `ListPlaceHierarchy`, at any depth, like
    AOC-038's expansion (F2).
  - **A row's location is its first kind** (boss, vendor, quest giver, container), in the tree and
    in the predicate alike (verify round 2, F8). **The place chain is bounded by `maxPlaces`**
    (32), shared by the parser and the walk (F9).
  - **A path's places are checked:** each must be the parent of the next, or the path names
    nothing (F3). The last place is expanded when the path ends there, and exact when a location
    follows, because that is how the tree counts it.
- **A node matches ONE source row** (`Filters.Source`, `items.SourceNode`). Its levels go into the
  shared CTE's `in_base` as one `EXISTS` over `item_sources`, every level on the same row. The
  older `tier=` and `place=` are separate `EXISTS` and may match different rows of one item. That
  is right for them and wrong for a node, where a tier's raid would count an item that is in the
  tier only elsewhere. A place node is expanded to its descendants (AOC-038's `expandPlaces`) when the path ends there.
  **The tab alone filters nothing**: `source_tab` is set only when a node is picked.
- **Counts: one query, shape from every row.** `ListSourceTreeRows` (the fourth copy of the CTE)
  returns every source row of the tab with its levels and a `matches` flag: every other filter, the
  source excluded, because the service calls it with no `source_*` argument (`items.treeParams`;
  `TestTheTreeTakesEveryListFilterButTheSource`). `items.buildTree` builds the tree from all rows
  and counts the distinct matching items per node, so a branch the filters empty is still listed
  at 0. Not `GROUPING SETS`: each tab draws different levels, and one row query plus a Go walk over
  a few thousand rows is simpler. Measured on the corpus: 4–13 ms per tab, filtered or not, and the list unchanged against 0.5.0 (6.6 ms against 7.2 unfiltered, 26.1 against 25.8 with facets).
- **The URL.** `source` is typed segments (`s:`, `r:`, `m:`, `p:`, `b:`, `v:`, `q:`, `c:`) joined
  by `.`, so a level a row lacks cannot shift the others; `tab` and `get` ride beside it. All three
  go through `Filters.Values`, so every link the page prints carries them
  (`TestFiltersRoundTripThroughValues`).
- **Proved on the corpus:** every branch of every tab, unfiltered and under two filtered states,
  counts exactly what `List` gives for its `source`, and each half what `get` gives
  (`TestEveryBranchCountsWhatPickingItLists`). Each tab has the same branches under every filter.
  `TestEverySectionIsInPierresTab` reads the assignment back from the database.

### The pool

Built **once in `main`** — never a package-level global, which cannot be swapped in a test and
hides who depends on it. ⚠️ It is not yet *passed* anywhere: nothing takes a pool until
**AOC-009** adds the first queries. `main` builds it, pings it and closes it, which is what
makes a live 200 proof that the production pool connects. `db.New` **pings**: `pgxpool` is lazy and
succeeds against a completely wrong URL, so without the ping the service boots "successfully"
and fails on the first request a visitor makes. Limits are small on purpose (10 connections):
Railway's Postgres has a fixed limit shared with migrations, `psql` and backups, and
exhausting it presents as the site being down.

**JIT is off on every pooled connection** (`jit=off` as a runtime parameter, AOC-049). Postgres
compiles a plan to machine code once its estimated cost passes `jit_above_cost`, and the facet
queries estimate ~200,000 (hashed subplans inflate the figure) while running in under 16 ms: with
five filters set the facet query took **66 ms with JIT, 10 ms without**, 62 ms of it compiling.
Nothing this service runs is the long analytical query JIT pays off on. Set in `db.New`, so it holds
on any Postgres whatever its default; `TestNewTurnsJITOff` asks the server. ⚠️ It is a **startup
parameter**: production connects straight to Railway's Postgres today. A connection pooler put in
front of it (PgBouncer and the like) must **forward** `jit` (PgBouncer: `track_extra_parameters`),
or the role must carry it instead (`ALTER ROLE … SET jit = off`). An unknown parameter makes the
pooler refuse the connection (the boot's ping fails, loudly); listing it in
`ignore_startup_parameters` would **drop** it, and JIT would silently be back on —
`TestNewTurnsJITOff` connects straight to Postgres and would not see it.

### Testing

`internal/db` tests are **integration** tests against a real Postgres — SQL that has never met
a database is not tested. Each creates a **throwaway database** and drops it, so they cannot
disturb the developer's data or collide with each other. CI runs a `postgres:18-alpine`
service — the same major as production and as `docker-compose.yml`, because **every Postgres in this project tracks production's major** — and then **asserts the tests did not skip**, because a suite that skips its only
integration tests while reporting success is the failure shape this project keeps finding.

**Two kinds, and where each runs (AOC-044):**

| Kind | Data | Build tag | CI | `bin/gate api` |
|---|---|---|---|---|
| Fixture tests (almost all) | obviously-fake rows in a fresh migrated database | none | ✅ | ✅ |
| **Corpus tests** — `item_read_endpoints_test.go`, `item_multiplace_test.go`, `item_source_region_test.go`, `multi_query_facts_test.go` (AOC-012) | the **real imported armory** in the database they are handed | **`corpus`** | ❌ never run (the `lint` job lints them, `.golangci.yml` `build-tags`) | ✅ `-tags corpus` |

The corpus tests are the only ones that meet the real 4,646 rows, which is what caught AOC-012's
defects, so they are not rewritten on fixtures. They cannot run in CI: the corpus comes from the
**private** snapshot repo, and CI's Postgres never holds it — there they could only skip, and CI
fails on a skip. So they are **tagged, not skipped**: CI's tests never compile them (its lint job
still lints them), and the gate always runs them. Where the corpus is missing, `readPool` skips and the gate's skip check exits **2** naming
them — so a machine without the imported armory cannot pass the gate. `make test-corpus` runs
them by hand. A new test that reads the real corpus goes in a `corpus`-tagged file, never in an
untagged one.

## Deploy

> 💾 **Backups and restores have their own runbook: [`runbook-restore.md`](runbook-restore.md).**
> Read it before touching the production database. ⚠️ Railway's scheduled backups are **Pro-only**
> and this project is on **Hobby**, so the dumps that runbook produces are the **only** backups
> that exist. Production is reached over **SSH** (`ssh -L` to the Postgres container's own
> loopback) — there is deliberately **no public database endpoint**. (AOC-006)
>
> ✅ **A backup is scheduled (AOC-030).** A second Railway service dumps to R2 over the private
> network daily, and a GitHub Action goes red when the newest object is stale, tiny or absent —
> see § The backup service. Railway's own scheduled backups are still Pro-only and still unused.

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
| **`aoc-codex.app`** (apex) | Railway, CNAME `nnja20lj.up.railway.app`, **proxied** | **the canonical site** — HTML at `/` **and** `/v1/*` JSON, one origin, one certificate, no CORS |
| `www.aoc-codex.app` | Railway, CNAME `8q2yax25.up.railway.app`, **proxied** | `301` to the apex, answered at Cloudflare's edge |
| `img.aoc-codex.app` | the R2 bucket `aoc-codex-enam`, CNAME `public.r2.dev`, **proxied** | the 4,645 archived tooltip images |

**Railway validates a custom domain with a TXT record, not by reading the CNAME.** This is worth
stating plainly because getting it wrong cost a day and nearly cost the apex permanently.
`railway domain status <host> --service api --json` returns two independent things:

- a **`dnsRecords`** entry with `purpose: DNS_RECORD_PURPOSE_TRAFFIC_ROUTE` — the CNAME that carries
  traffic, and
- a **`verification`** block — `verified`, a `dnsHost` of `_railway-verify[.<label>]`, and the TXT
  value to publish there.

**The certificate waits on `verification`, and nothing else.** Until that TXT exists the status sits
at `CERTIFICATE_STATUS_TYPE_VALIDATING_OWNERSHIP` indefinitely — not "slow", not "stuck", simply
unsatisfied. Both hostnames sat there for a day with perfect CNAMEs. Within **~30 seconds** of the
two TXT records resolving, both went `VALIDATING_OWNERSHIP → ISSUING → VALID`.

⚠️ **The corollary, which reversed a decision.** It was previously recorded here that a zone apex can
*never* be a Railway custom domain, reasoning that Railway reads the CNAME's value and that
Cloudflare must flatten an apex CNAME into A records, leaving nothing to read. The flattening is
real; the conclusion was not. Ownership is proved by a **TXT record, and a TXT at a zone apex is
perfectly legal DNS**. The apex holds a valid Let's Encrypt certificate today. Every symptom
previously offered as proof — *"Application not found"* when proxied, a certificate-name failure when
DNS-only — is simply what **any** host with no certificate looks like, through the two different
paths. None of them was evidence about the apex specifically.

📌 **Each custom domain gets its own CNAME target and its own TXT token.** The apex was issued
`nnja20lj…`, `www` was issued `8q2yax25…`, and the two `_railway-verify` values differ. Reusing one
for the other silently fails verification while looking correct.

📌 **A TXT record is never proxied** — there is no orange cloud to get wrong. Create it with type,
name, content and `ttl: 1`, and leave the neighbouring records alone.

**`www` is a Cloudflare Page Rule, not Go middleware.** `www.aoc-codex.app/*` →
`https://aoc-codex.app/$1`, `301`, path and query preserved. The redirect is answered at the edge and
never reaches the origin: Railway bills usage, so a hostname whose only job is to say "go to the
canonical name" should not cost a container wake-up — the same reasoning as the cache in AOC-026.
⚠️ There is **no Dynamic Redirect permission on this account**, so this is a Page Rule, and the free
plan allows **three**; one is now spent.

**`www` still needs its own Railway custom domain and certificate even though it only redirects.**
Cloudflare presents the requested hostname as SNI to the origin, so a proxied name with no
certificate on the Railway side fails before the Page Rule matters.


**Checking all of this:** `scripts/check-hostnames.sh [canonical-host]` gathers the evidence for
every hostname criterion — resolution, proxy status, TLS, the canonical host's `/health` and HTML,
the 301 from the other name, two real tooltip keys checked against their byte counts in
`armory_snapshot/tooltips_upload_manifest.csv` (one of them URL-encoded, which is the trap), plain
HTTP on all three names, and that the bucket does not list. It only reads: no DNS change, no Railway
call, no credential. Exit 0 means every check passed. Watch mode and AOC-026 re-ask exactly these
questions, which is why it lives in the repo instead of a scratchpad.

📌 **It pins curl to an authoritative address on purpose.** Immediately after the orange cloud is
switched on, this machine's own resolver still answers with the pre-proxy address for the rest of
the old TTL, so an unpinned check reports "not proxied" for a change that was in fact applied —
measured on 2026-09-21, `dig` said `104.21.34.205` while `dscacheutil` still said `69.46.46.106`.
The script reports the disagreement as a note rather than a failure.

⚠️ **`.app` is HSTS-preloaded at the TLD level.** Browsers refuse plain HTTP to *any* `.app` name
before a request is made, so there is no "try it over http first" step and no HTTP fallback to fall
back to. A certificate that has not issued yet does not look like a warning — it looks like the site
is down. Every hostname above must be HTTPS from its first hit, and every asset URL must be `https`
or it is blocked rather than mixed-content-warned.

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
| `VERSION` | ⚠️ **not** a variable any more: the repo's `VERSION` file is the version (§ Build identity, AOC-015). A Railway service variable of that name still exists and is unused — the Dockerfile declares no `ARG VERSION`, so Docker reports it "not consumed" |
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

Steps 3–4 as **one command Pierre runs**: `scripts/release-migrate.sh check` (tunnel, goose status,
close — writes nothing) then `scripts/release-migrate.sh apply` (a verified `pg_dump` to
`~/AoC-backups`, `goose up`, status, close). The password comes from Railway with the project token
and is never printed. ⚠️ A release whose binary reads a new column **migrates first, then merges**:
the old binary ignores an extra column, the new one 500s without it (0.2.0).

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

### The backup service (AOC-030)

**A second Railway service in the same project**, and the first thing here that is not the site.
It shares the repo and the private network with `api` and nothing else: no public domain, no
inbound traffic, no route.

| | |
|---|---|
| Image | `Dockerfile.backup` — `postgres:18.6-alpine` + `rclone`, both pinned |
| Schedule | Railway `cronSchedule`, **UTC**, daily at 03:00 |
| Entry point | `scripts/backup.sh` — dump, check the dump, upload, check the upload |
| Database | `${{Postgres.DATABASE_URL}}` — a **reference**, so no credential is copied by hand or reaches this repo |
| Destination | the **backups** R2 bucket, write-scoped token. ⛔ NOT the tooltip bucket — opposite retention policy, and `backup.sh` refuses by name |
| Restart policy | **never**. A cron service must EXIT; Railway skips the next run while one is still `Active`, so a job that lingers turns into a backup that silently stops |

⚠️ **`postgres:18.6-alpine` is pinned to the EXACT version production runs.** A `pg_dump` older
than the server refuses to dump at all — the trap AOC-006 hit on the laptop — and here it would
fail on a schedule nobody is watching. **Bumping production's Postgres means bumping five things in
one commit:** Railway, `docker-compose.yml`, the Makefile's `POSTGRES_MAJOR`, `ci.yml`'s service
image, and this Dockerfile.

**The script never reports a success it has not looked at.** It uploads only after checking the
dump locally — non-zero size *and* a non-empty `pg_restore -l` table of contents, because a dump of
a database with no tables is a perfectly valid file that restores to nothing (two zeroes are not a
match, AOC-006). Then it reads the object's size back **out of the bucket** and compares it: a 200
is not evidence the bytes arrived. Every failure path exits non-zero with a sentence a person can
act on at 03:00.

⚠️ `RCLONE_S3_NO_CHECK_BUCKET=true` is **required, not an optimisation.** A bucket-scoped token
cannot `CreateBucket`, and rclone tries to ensure the bucket exists before its first `PUT` — so
without it the upload fails at `CreateBucket` with 403 and never reaches `PutObject`. Measured in
AOC-007; see § Object storage.

#### ⚠️ The job's token can DELETE, and that is a limit we accepted knowingly

The ticket asked for a **write-only** credential. **R2 has no write-only permission group** — the
choice is *Object Read & Write* or *Object Read only* — and there is no object lock and no bucket
versioning to fall back on. Criterion 5 also *requires* reading the object back after upload, which
a write-only token could not do. So **Object Read & Write is the least privilege that does the
job**, and it means the backup service's credential could delete the whole archive.

Measured, not assumed: `DELETE` with the job's token returns **204**.

Compensating controls, such as they are: the credential exists only as a Railway variable on one
service that runs for a few seconds a day; the 30-day lifecycle means a deletion is not the only
way objects disappear anyway; and the freshness alarm turns a wiped prefix into a red run within
24 h. **This is recorded rather than fixed because there is nothing to fix it with** — revisit if
R2 ever ships object lock. (AOC-030 verify round 1, Finding B.)

#### ⛔ MEASURED: Railway reports a FAILED run as SUCCESS

**2026-09-22, the first real deployment of this service.** `backup.sh` hit its env guard, printed
`BACKUP FAILED: R2_ACCESS_KEY_ID is not set. Refusing to guess.` and **exited 1**. Railway's API
reported that deployment's status as **`SUCCESS`**, and still did on a later re-query.

Do not design around Railway's deployment status for this service. For a cron job it appears to
describe *"the container was deployed and started"*, not *"the command succeeded"* — so **the
dashboard being green tells you nothing about whether a backup exists.**

This is the strongest argument for the alarm below, and it is the reason the alarm asks the
**bucket** rather than the **scheduler**. The only trustworthy evidence that a backup happened is a
recent, plausible object sitting in R2.

#### The alarm, which is the other half

A backup job fails **quietly**, and the day you find out is the day you needed it. So the
deliverable is not a job that runs — it is a job that runs **plus something that goes red when it
does not**.

`.github/workflows/backup-freshness.yml` lists the bucket daily with a **read-only** credential and
**fails** when the newest object is older than 48 h, implausibly small, or absent. Its failure
notification is the alarm.

**It runs on GitHub, not on Railway, deliberately:** a dead man's switch must not share fate with
the thing it watches. If Railway is the reason the backup stopped, a checker running on Railway
stops with it and nobody hears anything.

The decision itself (`scripts/check_backup_freshness.py`, `judge()`) is a pure function with unit
tests that CI runs on every PR, because a script that decides whether the backup happened is
exactly the code this project does not trust to reasoning alone. **An empty listing is a FAILURE,
not a vacuous pass**, and missing credentials exit **2** rather than 0 — "I could not look" must
never read as "it is fine" (AOC-032).

⚠️⚠️ **The one way the alarm can itself go silent, written down rather than hoped about:** GitHub
**disables scheduled workflows in a repository with no activity for 60 days**, and this project is
explicitly touched in bursts months apart. GitHub emails the owner when it does so, and
`workflow_dispatch` re-arms it, but a reader must not discover this at the same moment they
discover the missing backup. It is repeated in `runbook-restore.md`.

### The import service (AOC-040)

**A third Railway service**, and the second that is not the site. Like the backup job it shares
only the repo and the private network — no public domain, no route, no inbound traffic. Unlike it,
it has **no `cronSchedule`**: it runs when a person asks.

| | |
|---|---|
| Image | `Dockerfile.import` — `golang:1.23-alpine` build, `alpine:3.22` runtime |
| Schedule | **none.** A job, not a timer |
| Entry point | `cmd/import-armory`, with the corpus baked in at `/snapshot/items_clean.json` |
| Database | `${{Postgres.DATABASE_URL}}` — a **reference**, so no credential is copied by hand or reaches this repo |
| Restart policy | **NEVER**, the same rule as the backup job: a job must exit |
| Default command | ⛔ **`-dry-run`.** Committing requires overriding the start command, which is a separate visible act |
| Corpus floor | ⛔ refuses to replace the corpus with under 90% of itself (AOC-042); `-allow-shrink` is the only way past |

#### How it is deployed, and why it is not built from GitHub

`items_clean.json` is **10 MB** and lives in a **separate, private** repo
(`pierrehrt/aoc-armory-snapshot`). Railway builds this repo, which does not contain it. So this one
service is deployed with **`railway up` from an assembled build context** — this repo's tracked
files (`git archive HEAD`) plus the snapshot, from a machine holding both checkouts.

```bash
CTX=$(mktemp -d)
git archive HEAD | tar -x -C "$CTX"
mkdir -p "$CTX/armory_snapshot"
cp ../armory_snapshot/items_clean.json "$CTX/armory_snapshot/"
cd "$CTX" && git init -q && git add -A && git -c user.email=a@b -c user.name=c commit -qm ctx
railway up -d -s import -e production
```

⚠️ **The flip side: it runs the code it was last deployed with.** A release that changes the importer
redeploys this service in the same release (`workflows/5-release.md` § 4) — AOC-054's migration
backfilled 146 necklace slots, and an importer from before it would delete them on its next real
run and exit 0, because its own count check expects no such rows.

⭐ **A safety consequence worth keeping deliberately: a push to `main` cannot rebuild or trigger this
service.** `api` and `backup` both carry `source.repo = pierrehrt/aoc-api`, so a merge redeploys
them; `import` has **no source repo at all**, because it only ever moves when someone runs
`railway up`. Measured 2026-09-24, when merging a PR rebuilt `backup` (`buildOnly: true`, cron
untouched) and left `import` sitting on its previous deployment. It is the fourth independent
safeguard on a service that deletes nine tables — beside `-dry-run` in the start command, no
`cronSchedule`, and `-confirm-host`. **Do not "tidy this up" by connecting the service to the repo.**

⚠️ **`git init` is not decoration.** `railway up` refuses a context that is not git-rooted, with the
unhelpful message **`prefix not found`** (measured 2026-09-23).

⚠️ **`railway add` can create the service and then fail**, reporting `Project not found` as though
nothing happened. It had in fact created `import`; a later create failed with "a service named
import already exists". Check before creating twice.

Rejected alternatives, and why (`DECISIONS.md`, 2026-09-23): **fetching the snapshot from R2** — the
backups bucket expires objects after 30 days so it would silently vanish, and the tooltip bucket is
served publicly at `img.aoc-codex.app` so it would publish the corpus; **cloning the private repo
at build time** — a GitHub token inside a Railway build, a credential in a third place; **committing
the snapshot into this repo** — a second copy of the corpus, free to drift from the canonical one.

#### ⏱ How long it takes, measured, because this decides the deadline

Completed runs, same snapshot, same code:

| Where | Duration |
|---|---|
| dev, over localhost | **4.2 seconds** |
| here, Railway private network | **15.0 minutes** |
| down the SSH tunnel | **never finished** — 418 of 4,648 items in 10 minutes |

`-timeout` is therefore a flag (default 10 minutes, so a dev run still fails fast) and this service
passes **2h**.

⚠️ **Only ~12,900 of the ~49,800 rows are written one statement at a time** — `item_sources`,
`item_costs`, `sets`, `vendors`. The other ~36,900 (`items`, `item_stats`, `item_spell_effects`,
`item_equip_locations`, `item_classes`) **already go through `tx.CopyFrom`**, and have since
AOC-011. The comment on `insertSources` in `internal/items/import_children.go` says so.

⛔ **Which means the 15 minutes is not explained.** 900 s over ~12,900 statements is **~70 ms each**,
against **0.33 ms** on localhost. 70 ms is not an intra-datacenter round trip, so something other
than network latency dominates — and **what, is not known and was not measured.** Anyone optimising
this should start by finding out, not by reaching for `COPY`, which is already there for 74% of the
rows.

⚠️ **Do not size a run from a partial one.** A first attempt here reached item 921 in ten minutes,
implying ~50 minutes, and the run that finished took 15. **Why that attempt was ~5× slower is also
not known and was not measured.**

#### What happens if two runs overlap

Observed 2026-09-23, not assumed: triggering a second deployment **stops the first mid-transaction**
(`Stopping Container`, six seconds after the new one started). The first run's transaction therefore
**rolls back** — safe, but it means a stray redeploy during a real import aborts it rather than
corrupting it. Confirmed afterwards from `pg_stat_user_tables`: `items.n_tup_ins` was exactly
**3 × 4,646** with `n_live_tup 0`, i.e. three runs reached the write phase and all three rolled back.

⚠️ **The start-command override is untested.** `AOC-034` will drop `-dry-run` by overriding the
start command, and no override has ever been run against this image. **Prove it with a dry run
first.** The failure mode is at least loud: Go's `flag` stops at the first non-flag argument, so a
mangled override leaves `-confirm-host` empty and `db.ConfirmTarget` refuses to touch a non-local
host.

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

**Cloudflare R2, two buckets.** `aoc-codex-enam` holds the 4,645 armory tooltip images (AOC-008);
`aoc-codex-backups-enam` holds the scheduled `pg_dump` backups (AOC-030). **Two buckets, not one,
because their retention policies are opposites**: tooltips are kept forever and read by the site,
dumps are private and expire after 30 days. Both are `enam`, both private, each with its own
read-write and read-only token pair scoped to it alone.

| Bucket | Created | Location | Retention | Tokens |
|---|---|---|---|---|
| `aoc-codex-enam` | AOC-007 | ENAM | forever | read-write + read-only |
| `aoc-codex-backups-enam` | AOC-030 | ENAM, no jurisdiction | **30 days** on `prod/`, plus the default 7-day multipart abort | read-write (the job) + read-only (the alarm) |

⚠️ `scripts/backup.sh` **refuses by name** to write into the tooltip bucket, because the two
lifecycle rules would each be wrong for the other's contents. R2 was chosen over S3 and B2 for one reason: **egress is free at any
volume**, so a link on Reddit cannot turn into an invoice on a project with no revenue
(`product_management/DECISIONS.md`, 2026-09-13).

| | |
|---|---|
| Bucket | `aoc-codex-enam`, **private**: public access **off**, `r2.dev` **off**. The one public route is `img.aoc-codex.app` — AOC-014 bound it as an R2 custom domain; AOC-041 moves it onto the `img` Worker (below), after which the bucket has **zero custom domains** again and is reached only through the Worker's binding |
| S3 endpoint | `https://<account-id>.r2.cloudflarestorage.com` |
| Location hint | **`enam`** (Eastern North America) — **read back from the API**: `GET ?location=` → `<LocationConstraint>ENAM</LocationConstraint>` (see *Reading a bucket's location hint*) |
| Jurisdiction | **none**, deliberately — see below |
| Public URL | `https://img.aoc-codex.app/armory/<source filename>` — served by the `img` Worker (AOC-041). Never `r2.dev` |
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

### Serving the images: the `img` Worker (AOC-041)

`img.aoc-codex.app` is a **Cloudflare Worker** (`workers/img/worker.mjs`, script name `aoc-img`)
that reads `aoc-codex-enam` through an **R2 binding** named `BUCKET`. It is the one deployable in
this repo that is not the Go binary, and it exists because the simpler setup was measured broken.

**Why not R2's own custom domain.** From 2026-09-23 the custom domain stalled on cache misses at
Cloudflare's Singapore edge: connections that opened and never answered, and bodies that stopped on
8 KiB boundaries. On 2026-09-25, pinned to that same edge with the same objects, interleaved:

| Path | Whole images |
|---|---|
| R2 custom domain | 119 / 160 |
| Worker through the R2 binding | **160 / 160** |
| R2 S3 API | 160 / 160 |

The custom domain failed 41 times where the Worker delivered, never the reverse. Every other probed
location in the world was served correctly either way. What goes wrong inside the custom-domain path
is not known; the Worker avoids it. Full evidence: `product_management/tickets/AOC-041-*.md`.

**What it does, and nothing else**

- GET and HEAD of keys under `armory/`. Anything else is 404 (path) or 405 (method).
- One edge-cache entry per object via the Cache API, keyed on the **path only**, so a
  `?cb=` cache-buster can neither multiply entries nor force a bucket read.
- `Cache-Control` comes from the object's own metadata — the uploader sets
  `public, max-age=31536000, immutable` — with that same value as the fallback.
- `ETag` from the object; `If-None-Match` → `304`.
- A bucket error is `503` with `no-store`, so an outage is never cached as if it were the image.
- `x-aoc-cache: hit | miss` on every response, for measuring.

**Measured on `workers.dev` after the first deploy (2026-09-25):** a first fetch reports
`x-aoc-cache: miss`, and the same path with a different `?cb=` then reports `hit`, so the edge cache
works there as well as on the real hostname. `Content-Length` survives the cache split, HEAD sends
headers only, a matching `If-None-Match` is `304`, and non-`armory/` paths and POST are refused.

**Tests:** `make worker-test` — Node's built-in runner for the Worker, no packages, plus
`workers/img/scripts.test.sh` for `switch.sh` and `rollback.sh`; run by CI's `worker` job.
`bin/gate api` is Go-only and does not run them. Each guard is pinned by mutation (build, AOC-041).
The script test runs the real scripts against a fake `curl` that plays the Cloudflare API from a
state file, and asserts the exact writes each starting state produces. ⚠️ On a Mac run it as
`PYTHON=/usr/bin/python3 make worker-test`: Pierre's shell resolves Apple's Python **3.9**, and a
3.12-only f-string stopped `switch.sh` halfway in production on 2026-09-28 because every earlier run
had used Homebrew's 3.13. CI's Python is newer, so it cannot catch that class of fault alone.

**Deploying — the Cloudflare API with curl; no Node, no wrangler** (the same reason the stack uses
the standalone Tailwind CLI). All three read `~/.config/aoc-codex/cloudflare.env`:

| Script | What it changes |
|---|---|
| `workers/img/deploy.sh` | uploads `aoc-img` with its binding, enables it on `workers.dev`. Does not touch the hostname |
| `workers/img/switch.sh` | detaches the R2 custom domain from `img.aoc-codex.app`, removes its leftover `public.r2.dev` CNAME, attaches the Worker. Seconds of downtime |
| `workers/img/rollback.sh` | detaches the Worker, waits for its `AAAA 100::` record to go, re-attaches the R2 custom domain |

Each call prints ✅ or the API's errors and stops the script on the first failure. A call succeeds
on an HTTP 2xx whose body, if JSON, does not say `success:false` — not on "JSON with
`success:true`": detaching a Workers domain answers with a body that is not JSON, and reading that
as failure stopped `rollback.sh` halfway in production (2026-09-28). **Both are safe
to re-run:** each step looks before it writes, so a run that stopped halfway is finished by running
it again, and a run with nothing to do writes nothing. **Both refuse before their first write** if the
hostname carries a DNS record they did not make (switch expects R2's CNAME or nothing, rollback the
Worker's AAAA or nothing), and a lookup the API does not answer stops the script rather than read as
"nothing there". The token needs
Workers Scripts edit, Workers Routes edit, R2 edit and DNS edit on this zone.

### Credentials

⛔ **Never in this repo, never in an env var committed anywhere.** Two files on Pierre's machine,
both `chmod 600`:

| File | Holds |
|---|---|
| `~/.config/aoc-codex/r2.env` | account id, bucket, endpoint, and the R2 API token key pairs |

⚠️ **There is no `~/.config/rclone/rclone.conf` any more, and nothing needs one.** This table used
to list it, holding `[r2]` (read-write) and `[r2ro]` (read-only) remotes. That was true while
AOC-007 was proving the round trip; the file and the `rclone` binary are both **gone from the
machine** (checked 2026-09-22, AOC-030). Nothing regressed, because nothing depended on them:
**AOC-008 never used rclone** — `armory_snapshot/upload_tooltips.py` signs SigV4 with the Python
standard library — and the backup job configures rclone **entirely from environment variables**
(`RCLONE_CONFIG_R2_*`), so no config file ever holds a credential. Recreate the remotes only if
you want them interactively; the commands recorded below were run against a config that existed
at the time.

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
3. ⭐ **A third one, found in AOC-030 (2026-09-22): overwriting with IDENTICAL bytes exits 0 and
   proves nothing.** `rclone copyto` compares size and modtime first, so when the source matches
   the object already there it **skips the transfer** and returns success — with a read-only
   token, which never attempted a `PutObject` at all. Read naively that says *"the overwrite was
   allowed"*. **Always overwrite with a DIFFERENT size and content**; then the same token answers
   `403 AccessDenied`, and the object is verifiably unchanged afterwards.

Afterwards the bucket was re-listed with `[r2]`: `forbidden.txt` **absent**, `t1.txt` **unchanged**,
six objects exactly as uploaded. The denied writes left nothing behind.

## Not here yet

Everything this section used to list — CI, Railway, the toolchain, the taxonomy, the item endpoints,
the scheduled backup — shipped in **0.1.0**. What is still to come is tracked in
`product_management/BOARD.md` and `ROADMAP.md`, not here: a list of future work in this file is a
second copy of the plan, and it went stale the moment the first of its tickets shipped.

