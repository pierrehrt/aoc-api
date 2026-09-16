# aoc_api — routes

> Every route this service serves. Updated in the same commit as any route change
> (`bin/docs-check api`). Created by AOC-002.

## Conventions

- **Everything product-facing lives under `/v1/`.** A breaking change ships as `/v2/` beside it and
  the old version is marked deprecated here, never changed in place (CLAUDE.md rule 5c).
- **Every response is JSON**, success or failure, including 404, 405 and 500. The one exception is
  outside our reach: `net/http` rejects a malformed request line or illegal header bytes with a
  `400 text/plain` before any of our code runs.
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
| GET | `/assets/{name}.{hash}.{ext}` | Embedded CSS/JS, `Cache-Control: public, max-age=31536000, immutable`. A wrong hash is 404 |

**404 shape follows the path:** `/v1/*` and `/assets/*` are JSON; everything else is a small
HTML page.
