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
- **Every response carries `Cache-Control`, set by `httpx.Cache` and by nothing else** (AOC-026):
  pages `public, max-age=60, s-maxage=3600, stale-while-revalidate=86400`, `/v1/*`
  `public, max-age=60, s-maxage=600`, assets a year and `immutable`, 404/410 a minute, `/health`,
  errors, writes and HTMX `no-store`, and anything with a session, a `Set-Cookie` or an
  `Authorization` header `private, no-store`. Full table: `docs/architecture.md` § Caching.
- **Errors share one body shape**: `{"error": "...", "request_id": "..."}`.
- Every list endpoint is paginated (`GET /v1/items`: `limit`/`offset`, below).

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
| `GET /v1/items` | the armory list, paginated and filtered | `public, max-age=60, s-maxage=600` (the `/v1` policy) |
| `GET /v1/items/{slug}` | one item with stats, sources, costs and set | `public, max-age=60, s-maxage=600`; a 404 `public, max-age=60, s-maxage=60` |
| `GET /v1/taxonomies` | every filter vocabulary in one call | `public, max-age=60, s-maxage=600` (the `/v1` policy) |

**Why one window for all three.** These routes do not choose their own: every `/v1` GET gets the
same policy from `httpx.Cache` (`docs/architecture.md` § Caching). A browser rechecks after a minute;
Cloudflare keeps ten minutes, which is what stops a link from Reddit billing us per view (Railway
charges usage), and a purge makes a correction immediate. The corpus is *preserved* — the source site
is dead, so a row changes only when a human edits it — which is why a short browser window costs
nothing. Until the 0.1.0 release these routes set five minutes (items) and an hour (taxonomies) by
hand; those gave way to the one table (AOC-015).

### `GET /v1/items`

Filters, all optional and all combinable. Every one takes a **slug the taxonomy endpoint returned**,
never a free-text value a client invented — except `q`, which is a name search.

`rarity` · `item_type` · `equip_location` · `armour_weight` · `class` · `region` · `tier` ·
`place` (repeatable) · `pvp` (bool) · `unchained` (bool) · `q` · `sort` · `limit` · `offset` ·
since AOC-049: `ilvl_min` · `ilvl_max` · `reqlvl_min` · `reqlvl_max` · `price` (bool) ·
`currency` · `set` · `facets` (bool) · since AOC-050: `tab` · `source` · `get`

- **`q`** matches the name (case-insensitive substring) — **and, when it is a whole number, the
  item's id exactly** (AOC-047): `q=2183` finds item 2183 as well as any item whose name contains
  "2183".
- **`sort`** is `name` (the default, unchanged since 0.1.0), `ilvl` (item level, highest first,
  items with no level last) or `id` (ascending). Any other value is a 400. The keys are code, not a
  game concept.
- **`ilvl_min` / `ilvl_max`** and **`reqlvl_min` / `reqlvl_max`** (AOC-049) bound the item level and
  the required level, both ends inclusive. Two ranges, because the two differ on 234 items. A bound
  **excludes an item with no level of that kind**: it cannot be shown to be inside the range.
- **`price`** (AOC-049): `true` keeps items a vendor sells on **any** of their sources, `false`
  items no source has a price for. "Any", because the list shows items, not occurrences: an item
  free from a boss and sold by a vendor (689 in the corpus) has a price. `price=1` reads as `true`.
- **`currency`** (currency slugs) keeps items bought with one of those currencies from at least one
  source; **`set`** (set slugs) keeps the items of those sets. Both are lists, like the other facets
  (below).
- **`facets=1`** (AOC-049) adds **`facets`** to the envelope: the counts the Armory's filter rail
  shows. Absent unless asked for, so a caller that does not ask sees the 0.1.0 envelope. Each count
  is **how many items choosing that value would leave under the other filters**: the filter set
  minus the facet's own. It is exactly the `total` this endpoint returns with that value chosen, and
  the same SQL computes both (`docs/architecture.md` § Filtering and facet counts). Every key is the
  parameter it sets:

  ```json
  "facets": {
    "rarity":         { "any": 4646, "values": [ { "slug": "legendary", "name": "Legendary", "colour_token": "rarity-legendary", "count": 0 }, … ] },
    "equip_location": { "any": 4646, "values": [ … ] },
    "armour_weight":  { "any": 4646, "values": [ … ] },
    "class":          { "any": 4646, "values": [ { "slug": "…", "name": "…", "short_name": "…", "count": 0 }, … ] },
    "currency":       { "any": 4646, "values": [ … ] },
    "set":            { "any": 4646, "values": [ … ] },
    "price":          { "any": 4646, "count": 2067 },
    "ilvl":           { "min": 1, "max": 90 },
    "reqlvl":         { "min": 1, "max": 80 }
  }
  ```

  (Counts illustrative.) A group lists **every** value of its vocabulary, read from the lookup
  table, **0 counts included**, in display order: rarities best first, slots head to necklace,
  armour weights heaviest first, classes in `classes.sort_order` (Soldier, Rogue, Priest, Mage — AOC-065), currencies and sets by name. `any` is the
  count with that facet unset. `price.count` is items with a vendor price, `any − count` those with
  none. `ilvl` / `reqlvl` are the lowest and highest level among the items the other filters leave,
  and are **absent** when none of them has that level.
- Each row also carries, additively since AOC-047: **`item_type_name`** (AOC-062 — the item type's
  display name, "Crossbow", beside `item_type`, its slug; absent when the item has no type; the
  list's Type column prints the name, never the slug), `rarity_colour_token` (AOC-046),
  `armour_weight` (`{slug, name}`, armour only), `equip_locations[]` and `classes[]` (`{slug, name, short_name}`), and `price` — the first vendor
  source's costs as one string (`"9 Simple Relic I + 2 Gold"`), absent when no vendor sells it.
  Loaded in one round trip per page each, never per row.

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
  "attribution": "AoC Codex — https://aoc-codex.app/info"
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
| a `place` that **contains places** (a raid and its wings), alone or with others | the place's items **and every place inside it**, at any depth, **one per item**, with `places[]` as context | `true` |

**A place that contains places** (AOC-038, Pierre 2026-09-29). House of Crom's loot is recorded against
its two wings and Warmonk Monastery's against its three, so `place=house-of-crom` used to answer 0
items. It now answers all 152, each once: asking for the raid means everything in it. Naming two
wings of one raid is still two dungeons (the row above). Which places a place contains is
`places.parent_place_id`, read by the service, never a list in code. A `place` value no place has
still matches nothing; it is never dropped, which would remove the filter.

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
represented is refused rather than wrapped). Since AOC-049 also: a level bound that is not a whole
number from 0 to 2,147,483,647 (`ilvl_min=7.5`, `reqlvl_max=-1`), a range whose minimum is above its
maximum (`ilvl_min=80&ilvl_max=70`, which can match nothing by construction), and `price` or
`facets` that is not a boolean. An *unknown value* is not rejected: whether `legendaryy` is a
rarity, or `not-a-set` a set, is a database question, and the database answers it with an empty
page. The same holds for `currency` and `set` (AOC-049).

**Lists (AOC-064).** `rarity`, `equip_location`, `armour_weight`, `class`, `currency`, `set` and
`place` each take **several values**, repeated (`?rarity=epic&rarity=rare`), comma-separated
(`?rarity=epic,rare`) or both. Each value is counted once, and empty elements are dropped. An item
matches **any** value of a filter and **every** filter. `item_type`, `region` and `tier` take one
value (the first). ⚠️ **Changed in 0.5.0:** until then a repeated `rarity` (or slot, weight, class,
currency, set) used the **first** value and ignored the rest. The response shape is unchanged, and
the only answers that change are those to requests that named two values, which now get both
(`DECISIONS.md`, 2026-10-01).

With several values in a group, a facet's count is still "items with this value under the other
filters". For a facet that holds one value per item (rarity, armour weight, set), the total for
several ticked values is exactly the sum of their counts. For slot, class and currency it is
their union.

**The source panel's filter (AOC-050), additive.** Without `tab`, `source` or `get` every answer is
byte-identical to 0.5.0's.

- **`source`** picks a node of the Armory's source panel: **typed levels joined by `.`**, in tree
  order. The kinds are `s:` section, `r:` region, `m:` map, `p:` place, `b:` boss, `v:` vendor,
  `q:` quest giver and `c:` container, each followed by a slug:
  `source=s:pve-tier-3.p:<raid>.b:<boss>`.
  - **A node is ONE source row.** An item is listed when one of its source rows has every level
    named, together. That differs from `tier=` plus `place=`, which may each match a different row
    of the same item and keep their meaning.
  - **Each kind appears once, except places,** which form a chain, each the parent of the next
    (`p:house-of-crom.p:the-vile-nativity`), at most eight. A chain whose places are not each
    other's parents names nothing. The last place is the one matched. It is matched with every
    place inside it when the path ends there (AOC-038), and alone when a boss, vendor, quest giver
    or container follows.
  - **`-` is a level the row does not have:** `r:-` and `m:-` for no region or map, and `p:-` (only
    on its own) for no place before a boss, vendor, quest giver or container.
    `/v1/sources/tree` writes them wherever a row lacks a level, so a branch's `source` lists
    exactly its rows.
  - The values to send come from `/v1/sources/tree`.
- **`tab`** is the panel's tab (`/v1/taxonomies` → `source_tabs`). On its own it filters
  **nothing**: the tab changes what the panel shows, not the list (Pierre, 2026-10-02). With a
  `source`, the node is matched among the rows of that tab's sections. With no `tab`, the first tab
  is used.
- **`get`** picks one half of a node, as an acquisition group's slug: `drop` (the design's
  "loot / drops") or `vendor` ("quest / vendor"). Which acquisition types fall in each is data
  (`acquisition_types.group_id`). **`get` needs a `source`**: alone it is a 400.
- **A malformed `source`, `tab` or `get` is a 400.** That covers an unknown kind, a segment that is
  not `kind:slug`, a level twice, more than eight places, `-` where it cannot stand, a value that is
  not a slug, or `get` without a `source`. The body is the central mapping's
  `{"error":"invalid request"}`, as for a bad `sort`; the reason is logged. An unknown **slug** is
  not malformed: it matches nothing, like an unknown rarity.

### `GET /v1/sources/tree`

The Armory's source panel for one tab (AOC-050): `?tab=<slug>` (absent = the first tab), plus **any
`/v1/items` filter**. Every branch is counted under those filters. **A `source` on the request is
not applied to the counts**, so each branch says what picking it would leave (the rail's rule,
AOC-049). An unknown tab is a **404**.

Its shape (counts inside a branch shown as 0 here, not measured values):

```json
{
  "tab": {"slug": "pve", "name": "PVE", "levels_note": "tier › raid › boss", "groups": ["section"]},
  "total": 965,
  "end_points": 26,
  "nodes": [
    {"kind": "section", "slug": "pve-tier-3", "name": "PvE Tier 3", "source": "s:pve-tier-3",
     "count": 183, "groups": [{"slug": "drop", "name": "loot / drops", "count": 0}],
     "children": [{"kind": "place", "slug": "…", "name": "…", "source": "s:pve-tier-3.p:…",
                   "count": 0, "children": [{"kind": "boss", "…": "…"}]}]}
  ]
}
```

- **The levels are data.** A tab draws its `groups` (from `section`, `region`, `map`), then the
  location: the row's place under every place above it, then its boss, vendor, quest giver or
  container. A row with no place has one of those as its location instead. A level a row does not
  have is never shown as "Unknown": no branch is drawn for it, and its branches hang from the one
  above. Their `source` still names it as `-`. The tabs, the sections in each and their order are rows
  (`source_tabs`, `sections`), seeded with Pierre's assignment of AoC>TV's 39 sections
  (2026-10-02).
- **A branch the filters empty is listed with `count: 0`**, never dropped. The tree's shape is every
  source row of the tab, whatever the filters.
- `count` is distinct items. `groups` is the same count per acquisition group, for every group the
  branch's rows have, **0 included** when the filters empty it. `total` is the distinct items in
  the tab under the filters. `end_points` is how many branches have no branch under them
  (structural, not filtered). `halves` is every acquisition group in its order. `attribution` is
  on this route too.
- `source` is the value `/v1/items?tab=<tab>&source=…` takes to list exactly `count` items.
- `coords` (AOC-068) is where on its map the branch is, AoC>TV's `x,y`, **only when every row of the
  branch has the same coordinates** (Pierre, 2026-10-02). A branch with no coordinates, a row
  without any, or two different points (a tier, a region, the Gilding Vendor's four cities) has
  none. The key is then absent.
- Not paginated: a tab's tree is bounded by the data (the largest, Faction, has 230 branches).

### `GET /v1/items/{slug}`

One item with everything its page shows, in one response: stats, every source (place, boss, region,
map, tier, raid and unchained flags), costs, set, classes and equip locations. **A source's
`unchained` includes its place's** (AOC-039): it is true when the source row is flagged *or* the
place it sits in is an Unchained dungeon — the one expression the list filter uses, so an item found
by `unchained=true` never denies it on its own page. An unknown slug is a
**404 through the central error mapper**, with the standard JSON body — never a bare string.

Each source carries **both a name and a slug** for place, region and map — `"place": "Kyllikki's
Crypt"` beside `"place_slug": "kyllikki-s-crypt"`. The name is what a person reads; the slug is what
the list filters take, so an item page can link *"everything else from here"* straight back into
`/v1/items?place=…`. Without it that link is a dead end: `region=Cimmeria` matches nothing, only
`region=cimmeria` does.

⚠️ **A source's region and map come from its PLACE**, not from the source row's own columns, in
every query that publishes them. 196 source rows disagree with their own place, and honouring the
source row made the item page contradict the list about where the same dungeon is. Which record is
right is a game question (**AOC-037**); until it is answered, both endpoints at least say the same
thing. A source with no place still falls back to its own columns.

**AOC-048, additive:** `spell_effects` (the same line shape as `stats`, from a separate table — a
build calculator sums `stats` and must never reach these) and `set_pieces` (every piece of the
item's set, this one included: `{slug, name}`, plus `rarity_colour_token` **only when the piece's
rarity has a colour of its own** — absent otherwise, as on the list). Both are always arrays, empty
rather than null. ⚠️ A "set" is every item sharing the set's **name**; no set size is published,
because the data's per-item piece count is not one (AOC-060). `set` stays the set's **name, a string** — the pieces are a sibling field, because
turning `set` into an object would retype it.

**`stats` and `spell_effects` are in the tooltip's own order** (AOC-048) — the order the importer
wrote them, `Dexterity` before `Combat Rating` as the game prints them. They were alphabetical until
then; the order was never documented, and no reader of a tooltip would recognise the old one.

**AOC-058: `slot_fit` is `either` on the 389 one-handed weapons**, which carried `both` until then.
The field's meaning is unchanged — how to read the item's `equip_locations` — and so are its three
values; the old value was a wrong fact, corrected (Pierre, 2026-09-30: a one-hander goes in either
hand). `both` remains a valid value, and **no item carries it** now. ⚠️ **`slot_fit` does not say
whether an item blocks the other hand:** a two-hander is `single` in `main-hand`, like a crossbow —
which one takes both hands is `item_types.two_handed`, **not yet in `/v1`** (the gear builder,
AOC-051, is its first reader and exposes it). Do not infer it from `slot_fit`.

### `GET /v1/taxonomies`

Every filter vocabulary in one call: rarities, item types, equip locations, armour weights, classes,
tiers, regions, places, currencies. ⭐ **Read from the database, never hardcoded** — a literal list
of class names in a filter dropdown is the exact bug the content model exists to prevent
(`reference/content-model.md` § 0).

Each term is `{"slug", "name"}` plus, where the row has one (AOC-046, additive):
- classes: `"short_name"` — the abbreviation players use (`Conq`, `DT`, `HoX` …). Since AOC-065 they come
  in `classes.sort_order`: Soldier, Rogue, Priest, Mage (Pierre), the design's order within each —
  the same order the list's class column and the item page use. The order of an array was never
  part of the contract; only its contents are;
- rarities: `"colour_token"` — the name of the CSS custom property that paints it
  (`rarity-epic` → `--color-rarity-epic` in the site's stylesheet). Absent = no colour of its own.

**`source_tabs`** (AOC-050, a new key, additive): the source panel's tabs in Pierre's order, each
`{"slug", "name", "levels_note", "groups"}`: PVE, PVP, Region, Faction, Onslaught, Other.
`levels_note` is the panel's wording for the tab's levels.

### Attribution

Every response on every route carries `"attribution": "AoC Codex — https://aoc-codex.app/info"`.
It names AoC Codex and the Info page and no other site or person (Pierre, 2026-09-29: the site
shows no source and no other creator anywhere; origins are explained once, on `/info`). The field
**stays** — same name, type and meaning as in 0.1.0 — because removing it would be a breaking change
to this contract (CLAUDE.md 5c) for no gain; only its value changed (AOC-055).

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
| GET | `/armory` | **The Armory list** (AOC-047): `q` (name or id), `sort` (`ilvl` default here, `name`, `id`), `p` (1-based, 50 rows), and **every `/v1/items` filter** (AOC-049; since AOC-065 the pane is exactly the validated design's: rarity, slot, armour weight and class as checkboxes and toggle chips, several at once (AOC-064), and the item level as two sliders. Every other filter — required level, vendor price, currency, set, and the rest — rides along as a hidden input and shows as a pill, one per value, worded as the design's (`rarity: Epic`, `class: Conq`, `ilvl 60–90`, `q: …`)). Same parser and service as `/v1/items`, always with the facet counts. Every link on the page (pager, sort, chips, canonical) carries the whole state; the default sort and `p=1` stay out of it. `HX-Request: true` gets `armory_update` — the rows, plus the pane, the pills and both active counts out of band — with `HX-Push-Url` set to the state's canonical URL (`HX-Replace-Url` instead when `HX-Current-URL` names the same state: one history entry per state); both answers send `Vary: HX-Request`. A request whose reader went away (an aborted live request: its own context canceled) is logged as 499, not an error; a database failure stays a 500. `p` past the end is 404; a bad `p`, `sort`, level or boolean, or an empty range, is 400 — as dependency-free HTML (`httpx.RejectHTML`) naming the reason; to an `HX-Request` a malformed filter is a **400 `armory_invalid` fragment** (the reason, for `#results`; `HX-Push-Url: false`). An unknown slug is not rejected: it shows as a chip, over the empty state. **Since AOC-068 the sources panel**: `tab` (the active main category, absent = the first; it filters nothing), `source` and `get` (the picked branch, as `/v1/items` reads them). The tabs, the tree, the selected-source box and its counts come back out of band too, and the pick is a `source: …` pill. An unknown `tab` is a 404 |
| GET | `/armory/{slug}` | **The item page** (AOC-048): one item from `items.Service.Get` — the call `/v1/items/{slug}` makes. Stats as text beside the tooltip image, the set with its other pieces linked, sources grouped by each row's own acquisition type. `<title>` "{name} — AoC Codex", canonical `/armory/{slug}`, `og:image` = the tooltip image with `twitter:card` `summary` (it is portrait), one JSON-LD `Thing`. An unknown slug is a **404** as dependency-free HTML, `s-maxage=60`. **The same bytes for every reader** — nothing is read from the Referer; the back link's "return to your search" happens in the browser. The list's rows link here |
| GET | `/aa`, `/feats`, `/dj-raids`, `/more` | **Coming Soon pages** (Pierre, 2026-10-01): the design's header tabs, shown before their sections exist. Each says only that the section is not built. **`noindex`**, canonical to itself, **not in the sitemap**. A section that ships takes over its URL (or 301s it, rule 5c) |
| GET | `/robots.txt` | **AOC-025.** `text/plain`: `User-agent: *`, `Disallow` for `/_smoke`, `/v1/` and `/health` (one list, `pages.robotsDisallow`), and the absolute `Sitemap:` URL. ⚠️ In production **Cloudflare prepends its managed "content signals" comment block** to it (measured 2026-09-30) — parse the rules, never compare the bytes |
| GET | `/sitemap.xml` | **AOC-025.** A sitemap **index** (sitemaps.org 0.9) listing every chunk, absolute URLs on `PUBLIC_BASE_URL` |
| GET | `/sitemaps/{n}.xml` | **AOC-025.** Chunk `n` (1-based) of one sequence: `/`, every **built** section in the nav (a Coming Soon tab is left out), then every `/armory/{slug}` in item-id order — at most **50,000** URLs a file, built from the database on each request (edge-cached for an hour). **No `<lastmod>`**: no row has a real modification time. `n` out of range is a 404 |
| GET | `/_smoke` | Rendering proof page, HTML, **noindex**. Deleted by a later ticket |
| POST | `/_smoke/echo` | Fragment when `HX-Request: true`, otherwise the full page. Both send `Vary: HX-Request` |
| GET | `/assets/{name}.{hash}.{ext}` | Embedded CSS, JS and images (`.css`, `.js`, `.png`, `.svg`), `Cache-Control: public, max-age=31536000, immutable` (from the policy). A wrong hash is 404, `no-store` |

**Every request under a host that is not `PUBLIC_BASE_URL`'s is a 301 to the same path on it**
(AOC-025, `httpx.WithCanonicalHost`, inside the router): the Railway domain
`aoc-armory-snapshot-production.up.railway.app` served a full 200 copy of the site. 308 for methods
other than GET/HEAD; `/health` is exempt (monitors read it on any host); hosts compare lowercased
without a port. `aoc-codex.app` itself is never redirected — requests reach the origin as
`Host: aoc-codex.app`. **`PUBLIC_BASE_URL` is required when `ENV=production`**, parsed strictly and must be
**https** (`httpx.ResolvePublicBase`); malformed, missing or http, the boot fails. The redirect's
Location takes only the request's path and query, always starting with `/` — a target like
`x:@evil.example/` goes to `https://aoc-codex.app/`, never off the host.

**Rejection shape follows the path**, for **404 and 405 alike**: `/v1/*`, `/health` and
`/assets/*` are JSON; everything else is a small HTML page. `/health` is included because it is
read by Railway and by uptime monitors — never by a person in a browser.

**`HEAD` is answered on every route** (`chi/middleware.GetHead`): it routes an unmatched HEAD to
the GET handler and drops the body. Without it chi replied **405**, which is what monitors, link
checkers and `curl -I` would have seen on a site built to be crawled.

⚠️ **405 responses carry no `Allow` header.** RFC 9110 §15.5.6 requires one; chi does not hand the
handler the matched route's method set, and synthesising one risks a header that lies. Accepted
limit, recorded in `DECISIONS.md` (2026-09-16).
