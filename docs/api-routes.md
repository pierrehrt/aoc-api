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

Anonymous, read-only, and cached. No route here requires authentication, and first paint of a public
page depends on no authenticated request.

| Route | Returns | `Cache-Control` |
|---|---|---|
| `GET /v1/items` | the armory list, paginated and filtered | `public, max-age=300` |
| `GET /v1/items/{slug}` | one item with stats, sources, costs and set | `public, max-age=300` |
| `GET /v1/taxonomies` | every filter vocabulary in one call | `public, max-age=3600` |

**Why those windows.** Items are a *preserved* corpus — the source site is dead, so a row changes
only when a human edits it. Five minutes is short enough that a moderation fix is visible while
someone is still looking at the page, and long enough that a link from Reddit does not bill us per
view (Railway charges usage; AOC-026 puts Cloudflare in front of exactly this). Taxonomies change
only when a migration changes them, which is a deploy — an hour is generous and still bounded,
because a stale vocabulary offers filters that return nothing.

### `GET /v1/items`

Filters, all optional and all combinable. Every one takes a **slug the taxonomy endpoint returned**,
never a free-text value a client invented — except `q`, which is a name search.

`rarity` · `item_type` · `equip_location` · `armour_weight` · `class` · `region` · `tier` ·
`place` (repeatable) · `pvp` (bool) · `unchained` (bool) · `q` · `limit` · `offset`

```
GET /v1/items?rarity=epic&armour_weight=heavy&limit=2
```
```json
{
  "items": [
    {
      "id": 2841,
      "slug": "achiton-of-illuminant-conviction",
      "name": "Achiton of Illuminant Conviction",
      "rarity": "epic",
      "item_type": "chest",
      "item_level": 80,
      "tooltip_image": "https://img.aoc-codex.app/armory/achiton_of_illuminant_conviction.jpg",
      "confidence": "unconfirmed",
      "places": [
        { "slug": "kyllikki-s-crypt", "name": "Kyllikki's Crypt", "region": "cimmeria", "tier": "pve-1", "unchained": false }
      ]
    }
  ],
  "total": 39,
  "limit": 2,
  "offset": 0,
  "collapsed": true,
  "attribution": "Data preserved from AoC>TV by Kentarii"
}
```

**`collapsed` is not a parameter, and that is the point.** One dungeon shows its loot as it is;
anything that *contains* several dungeons shows each item **once** (`DECISIONS.md`, 2026-09-13). The
mode therefore follows the filters and cannot be asked for:

| the caller filtered by | rows | `collapsed` |
|---|---|---|
| nothing, or `region` / `tier` — an **aggregate** view | one per item, with `places[]` as context | `true` |
| one `place` | one per item, each carrying `place` | `false` |
| several `place` values | **one per item per named place**, so a shared item appears under each | `false` |

An item that matches the filters but is in **none** of the named places does not appear. It is not
emitted without a place: a row missing from a place view is visible, a place-less row in one is not
(that fallback existed once, and its only effect was to hide a filter that had stopped working).

A client that had to opt in would render a visibly wrong page the first time it forgot, which is why
this is server-side (`CLAUDE.md` rule 5b).

**Paging.** `limit` defaults to 50 and is clamped to 200 — `limit=0` and `limit=10000` are both
answered rather than rejected. Paging past the end returns an empty `items` with the **true**
`total`, so "past the end" stays distinguishable from "nothing matches".

⚠️ **In an expanded view, `limit` and `total` count ITEMS, not rows.** Naming *k* places can
therefore return up to `limit × k` rows, because a shared item appears under each named place — that
is the whole point of the expanded view. `total` is the number of distinct items matching the
filters, which is what a pager needs; counting rows would make the page count change depending on
how many of the selected dungeons happen to share loot. A client rendering rows should page on
`total` and expect more rows than items.

**Empty results are a 200** with `"items": []` and the full envelope, never a 404: *no item matches*
is an answer, not a missing resource.

**Rejections.** A *malformed* parameter is a 400 — `limit=abc`, `pvp=maybe`, `offset=-1`, and an
`offset` above 2,147,483,647 (the query's `OFFSET` is a 32-bit integer, and a value that cannot be
represented is refused rather than wrapped). An *unknown value* is not rejected: whether
`legendaryy` is a rarity is a database question, and the database answers it with an empty page.

⚠️ **Only `place` may be repeated.** `?place=a&place=b` is one selection of two dungeons, and
`?place=a,b` means the same. Every other filter takes a single value: `?rarity=epic&rarity=rare`
uses the **first** and ignores the rest. That is worth knowing precisely because `place` repeats —
the rest are single-valued because no page needs them otherwise, and making each one a list would
be more surface to keep correct for a filter nobody asked to combine.

### `GET /v1/items/{slug}`

One item with everything its page shows, in one response: stats, every source (place, boss, region,
tier, raid and unchained flags), costs, set, classes and equip locations. An unknown slug is a
**404 through the central error mapper**, with the standard JSON body — never a bare string.

### `GET /v1/taxonomies`

Every filter vocabulary in one call: rarities, item types, equip locations, armour weights, classes,
tiers, regions, places, currencies. ⭐ **Read from the database, never hardcoded** — a literal list
of class names in a filter dropdown is the exact bug the content model exists to prevent
(`reference/content-model.md` § 0).

### Attribution

Every response on every route carries
`"attribution": "Data preserved from AoC>TV by Kentarii"`. His release was unconditional, which is
precisely why the credit is in the payload rather than left to a template
(`DECISIONS.md`, 2026-09-13).

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
