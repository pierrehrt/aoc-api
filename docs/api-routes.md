# aoc_api — routes

> Every route this service serves. Updated in the same commit as any route change
> (`bin/docs-check api`). Created by AOC-002.

## Conventions

- **Everything product-facing lives under `/v1/`.** A breaking change ships as `/v2/` beside it and
  the old version is marked deprecated here, never changed in place (CLAUDE.md rule 5c).
- **Every `/v1/*`, `/health` and `/assets/*` response is JSON**, success or failure, including
  404, 405 and 500 — those are contracts a machine parses.
  ⚠️ **Since AOC-024 the site surface is not.** A rejection on an HTML path (anything outside
  those three) returns a small **HTML** page, so a person who mistypes a URL or follows a stale
  link is not handed `{"error":"not found"}` in their browser. The shape follows the **path**,
  because a path is a fact about which contract was addressed where `Accept` is a negotiation a
  proxy can get wrong. See `docs/architecture.md` § *Rejections have two shapes*.
- **Every response carries `X-Request-Id`**, echoed from the request if supplied and ≤ 64 chars.
- **Errors share one body shape**: `{"error": "...", "request_id": "..."}`.
- Every list endpoint will be paginated (none exist yet).

## Operational

| Method | Path | Auth | Response |
|---|---|---|---|
| `GET` | `/health` | none | `200 {"status":"ok","version":"…","commit":"…"}` |

**`/health` is not under `/v1` by design** — it is operational, not product surface, so it does not
fork when `/v2` arrives. It reports process liveness only and deliberately does not touch the
database (`docs/architecture.md`).

## `/v1`

The sub-router is mounted and **empty**. Any path under it returns a 404 in the standard error shape.

First real routes arrive with **AOC-012** (public item reads).

## Error statuses

| Sentinel | Status | Body `error` |
|---|---|---|
| `ErrInvalid` | 400 | `invalid request` |
| `ErrUnauthorized` | 401 | `unauthorized` |
| `ErrForbidden` | 403 | `forbidden` |
| `ErrNotFound` | 404 | `not found` |
| `ErrConflict` | 409 | `conflict` |
| `ErrMethodNotAllowed` | 405 | `method not allowed` |
| *(anything unmapped)* | 500 | `internal error` |

A 500's body is always that flat string. The detail goes to the log with the request id — a wrapped
error can carry a table name or a query fragment and must not reach a client.

## HTML surface (AOC-024)

Not versioned: the HTML and its handlers deploy together in one binary, so the cached-client
problem that `/v1` exists for does not apply. ⚠️ Public **URLs** are still a contract — a
changed slug on an indexed page throws away its ranking and breaks every link ever pasted
(`CLAUDE.md` rule 5c).

| Method | Path | Returns |
|---|---|---|
| GET | `/` | Home page, HTML |
| GET | `/_smoke` | Rendering proof page, HTML, **noindex**. Deleted by a later ticket |
| POST | `/_smoke/echo` | Fragment when `HX-Request: true`, otherwise the full page. Both send `Vary: HX-Request` |
| GET | `/assets/{name}.{hash}.{ext}` | Embedded CSS, JS and images (`.css`, `.js`, `.png`, `.svg`), `Cache-Control: public, max-age=31536000, immutable`. A wrong hash is 404 |

**Rejection shape follows the path**, for **404 and 405 alike**: `/v1/*`, `/health` and
`/assets/*` are JSON; everything else is a small HTML page. `/health` is included because it is
read by Railway and by uptime monitors — never by a person in a browser.

**`HEAD` is answered on every route** (`chi/middleware.GetHead`): it routes an unmatched HEAD to
the GET handler and drops the body. Without it chi replied **405**, which is what monitors, link
checkers and `curl -I` would have seen on a site built to be crawled.

⚠️ **405 responses carry no `Allow` header.** RFC 9110 §15.5.6 requires one; chi does not hand the
handler the matched route's method set, and synthesising one risks a header that lies. Accepted
limit, recorded in `DECISIONS.md` (2026-09-16).
