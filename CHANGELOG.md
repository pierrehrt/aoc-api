# Changelog

All notable changes to `aoc_api`. Every production deploy is a release with a semver bump, a section
here, a git tag and a GitHub release carrying that section (CLAUDE.md rule 15).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); this project uses
[semantic versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.6.0] - 2026-10-02

The Armory's sources panel. The main categories sit under the search, and on the left a tree
shows where items come from, each branch with its count.

### Added

- The Armory's sources panel (AOC-068):
  - **Tabs:** the main categories PVE, PVP, Region, Faction, Onslaught and Other, under the
    search, one active at a time.
  - **Tree:** on the left, the active category's AoC>TV sections, zones, places, bosses and
    vendors. Each branch shows how many items picking it would leave, with a 0 listed, never
    hidden. End branches split into "loot / drops" and "quest / vendor".
  - **Picking a branch** narrows the list and shows as a "source: …" pill.
  - **The selected-source box** shows the branch's path and count, plus "· map (x,y)" when the
    branch is one point.
  - **Layout:** the pane folds to a strip. On a phone, a Sources sheet holds it. With JavaScript
    off, every tab and branch is a link.
- AoC>TV's 39 armory sections as data, each in one of the six categories (AOC-050).
- `/v1/items` takes `tab`, `source` and `get`, which pick a node of the panel, matched on one source
  row. Without them, the answer is unchanged (AOC-050).
- `/v1/sources/tree?tab=`: a category's tree, each branch counted under every other filter, with
  its halves and, when it is one point, its coordinates (AOC-050, AOC-068).
- `/v1/taxonomies` gains `source_tabs` (AOC-050).

### Changed

- Asking for a place that contains places (a raid and its wings) lists everything inside it, each
  item once. `place=house-of-crom` lists its 152 items instead of none, and Warmonk Monastery 175
  instead of 1 (AOC-038).

### Migration

- `20261002120000_source_sections`: `source_tabs`, `sections` (AoC>TV's 39, each in its category),
  `acquisition_groups`, `acquisition_types.group_id`, and `item_sources.section_id` backfilled
  from each row's own section. It is run against production **before** this deploy with
  `scripts/release-migrate.sh`, and 0.5.0 runs unchanged on it (AOC-050).
- The `import` service is redeployed in this release. The importer now resolves each row's section
  and refuses a section it does not know; an importer from before it would write source rows with
  no section (AOC-050).

## [0.5.0] - 2026-10-02

The Armory list in the validated design: a full-window app whose table and filter panes scroll on
their own, and filters that take several values, so Epic and Legendary, or Head and Shoulder, show in
one list.

### Added

- Several values per filter group: rarity is a list of checkboxes, and slot, armour weight and class
  are toggle chips. Within a group a row matches any of them, across groups all of them; each value
  gets its own pill that removes only that value, and every state is a URL. `/v1/items` takes several
  values for `rarity`, `equip_location`, `armour_weight`, `class`, `currency` and `set`, repeated or
  comma-separated, as `place` always has (AOC-064)
- The header's AA's, Feats, DJ/Raids and More tabs, each leading to a "Coming Soon" page that is
  `noindex` and kept out of the sitemap (AOC-065)

### Changed

- The Armory list follows the validated design: its colours, fonts, spacing and header on every page;
  from desktop widths a full-window app with no page scroll, where the table (sticky header, pager
  bar at the bottom) and the filter pane each scroll on their own and the pane folds to a strip; a
  search block with ⌘K, Copy link and the active filters as pills with "clear all"; on a phone, the
  filter sheet with Cancel and "Show N items" (AOC-065)
- The filter pane shows the design's five groups: rarity, slot, armour weight, class restriction and
  item level, the level as two sliders (number inputs without JavaScript). Vendor price, required
  level, currency and set have no control there any more; they still apply from a link and show as
  pills (AOC-065)
- Classes are listed Soldier, Rogue, Priest, Mage, in the design's order within each, on the list,
  the item page and in `/v1`: `/v1/taxonomies`, the class facet of `/v1/items?facets=1` and an item's
  `classes` in `/v1/items/{slug}` (AOC-065)
- On the Armory list the search shows as a pill like the filters, and "clear all" clears it too (it
  used to keep the search); one sort button steps through the orders, and the Item and iLvl headers
  sort; Back and Forward reload the state from the server (AOC-065)
- ⚠️ `/v1/items`: a repeated value of those six filters used to take the first and ignore the rest,
  and a comma in a value matched nothing; both now mean any of the values. The response shape is
  unchanged (AOC-064)
- Every request has a 10-second deadline: a database that hangs gives an error and an ERROR log line,
  where a page used to wait until the reader or Cloudflare gave up. A request the reader abandons,
  including an Armory update a newer change replaced, is logged as 499 at Info, not as a server error
  (AOC-065)

### Fixed

- A filter changed while the Armory list was still updating, such as a second choice made on a slow
  connection, is no longer lost (AOC-065)
- A filter changed while a link's update was in flight (a pill's ×, a sort, "clear all") no longer
  drops the link (AOC-065)

### Migration

- `20261001120000_class_order`: adds `classes.sort_order` and fills it with the design's order — run
  against production **before** this deploy with `scripts/release-migrate.sh`. The 0.4.0 binary reads
  no such column, so it runs unchanged on the migrated schema. The `import` service is not
  redeployed: it never writes `classes` (AOC-065)

## [0.4.0] - 2026-10-01

The Armory list gets its filter rail, and beside every choice is the number of items it would
leave, so a reader sees an empty result coming before clicking into it.

### Added

- The Armory's filter rail: rarity, slot, armour weight, class restriction, vendor price, item level,
  required level, currency and set. Each choice shows how many items it would leave under the other
  filters; a choice with none stays listed, greyed (or marked "(0)" in the currency and set lists),
  never hidden; the two level ranges show the lowest and highest level the other filters leave.
  Active filters show as chips that each remove one filter, with "clear all". One form
  works without JavaScript; with JavaScript a change re-renders the rows and the counts in one
  request. On a phone the rail opens as a full-screen sheet (AOC-049)
- `/v1/items` takes `ilvl_min`, `ilvl_max`, `reqlvl_min`, `reqlvl_max`, `price`, `currency` and `set`.
  On `facets=1` it adds a `facets` object with the counts the page shows; without it the response
  is unchanged (AOC-049)

### Changed

- Every link on the Armory list (pager, sort, chips, canonical) carries the whole filter state. A
  filtered list's pager used to drop every filter but the search (AOC-049)
- The service's database connections run with JIT off: the facet queries ran more than six times
  slower with it (66 ms against 10 ms on the dev corpus, five filters set), almost all of it
  compiling (AOC-049)
- A rejected Armory search (a malformed level, a range whose minimum is above its maximum, a bad
  sort) now says why, on the page and in place of the rows when the filters change live (AOC-049)

### Fixed

- Pressing Back to an Armory state the browser kept no copy of brings back the whole page; it used
  to leave the bare rows, with no header, search or filters (AOC-063)
- The Armory's meta description for a one-item search reads "1 item", not "1 items" (AOC-049)

## [0.3.0] - 2026-09-30

Every item gets its own page, the list gains separate Slot and Type columns, and the site becomes
findable: a sitemap, and one indexed host.

### Added

- The item page at `/armory/{slug}`: stats as the tooltip prints them beside the tooltip image, the
  items sharing its set linked, sources grouped by their own acquisition type with the costs of each,
  honest empty states, a tooltip link preview and JSON-LD; the list's rows link to it; `/v1/items/{slug}`
  gains `spell_effects` and `set_pieces` (AOC-048)
- `/robots.txt` and a sitemap (`/sitemap.xml` index, `/sitemaps/{n}.xml` chunks) built from the
  database and the nav, so every item page can be found (AOC-025)
- `/v1/items` rows gain `item_type_name`, the type's display name beside its slug (AOC-062)

### Changed

- The Armory list shows **Slot** and **Type** in two columns — Type is the armour weight, otherwise
  the item type's name, never its slug (AOC-062)
- `/v1/items/{slug}`: `stats` and `spell_effects` now come in the tooltip's own order instead of
  alphabetically (AOC-048)
- Any host but aoc-codex.app — the service's Railway address — now answers with a 301 to the same
  path on aoc-codex.app (308 for writes; `/health` excepted), and production refuses to start unless
  `PUBLIC_BASE_URL` is exactly an https origin (AOC-025)

### Fixed

- The 146 necklaces have the Necklace slot — the slot list gains the necklace, and a type can name
  the slot its items take when their tooltip names none (AOC-054)
- One-handed weapons whose tooltip reads `Main Hand, Off Hand` fit either hand: `/v1` `slot_fit` is
  `either` instead of `both` on those 389 items (nine others stay one hand, as their own tooltips
  say); which types take both hands is recorded as data for the gear builder; the importer refuses a
  compound slot value nobody has decided about (AOC-058)

### Migration

- `20260930120000_necklace_slot`: the `necklace` slot, `item_types.default_equip_location_id` and
  `item_types.is_equipment`, and the 146 necklaces backfilled — run against production **before**
  this deploy with `scripts/release-migrate.sh` (AOC-054)
- `20260930130000_weapon_hands`: `slot_fit` `both` → `either` on the 389 one-handers, and
  `item_types.two_handed` — run in the same step, after the one above (AOC-058)
- The `import` service is redeployed in this release: an importer from before it would undo both
  data fixes on its next real run (AOC-054, AOC-058)

## [0.2.0] - 2026-09-29

The Armory list goes live: the first content page, on the site's shell.

### Added

- The site shell: header with the section nav and a footer, the dark theme as tested tokens
  (IBM Plex, AA contrast); rarity colours and class short names are database rows, served by
  `/v1/taxonomies` (AOC-046)

- The Armory list page at `/armory`: search by name or id, sort by item level, name or id, 50 rows
  a page with a shareable URL for every state, an honest empty state, the rows fragment for HTMX,
  and the phone row; `/v1/items` gains `sort=`, an id match on `q`, and per-row slots, classes with
  short names, price and the rarity colour token (AOC-047)

### Changed

- The footer names no source and no other creator, and `/v1`'s `attribution` field now reads
  "AoC Codex — https://aoc-codex.app/info" (same field, same type); where the data came from is
  said once, on the Info page (AOC-055)

### Fixed

- `unchained` is one expression in every query that publishes it (source OR its place), so an item
  found by `unchained=true` never denies it on its own page; a place whose sources disagree about
  the tier is summarised blank instead of with `min()` (AOC-039)

### Migration

- `20260929120000_shell_rarity_colours_class_short_names`: two nullable columns with seeds (`rarities.colour_token`, `classes.short_name`), run against production **before** this deploy with `scripts/release-migrate.sh` (AOC-046)

## [0.1.0] - 2026-09-29

The first tagged release: everything built since the repo was created, shipped as one deploy.

### Added

- The application skeleton — one Go binary with chi, `/v1` as a versioned sub-router, `GET /health`
  (version and commit), request-id, logging and panic-recovery middleware, one central error
  mapping with `http.Error` banned, and graceful shutdown (AOC-002)
- CI on every pull request: gofmt, vet, build, golangci-lint and the tests (AOC-003)
- A pinned Dockerfile build for Railway — one service, one Postgres, one environment — with
  `/health` reporting which environment answered (AOC-004)
- Migrations (goose) and typed queries (sqlc), run against local Postgres first, and a real Postgres
  in CI with a check that the database tests did not skip (AOC-005)
- The production-access runbook and a rehearsed restore, `docs/runbook-restore.md` (AOC-006)
- The Cloudflare R2 bucket for the tooltip images (AOC-007)
- The taxonomy tables and the place entities — regions, maps, places, bosses — as migrations and
  seeds (AOC-009)
- The item schema: items, stats, sources, costs, sets and vendors (AOC-010)
- The armory importer, loading 4,646 of the preserved AoC>TV snapshot's 4,648 items, skipping 2
  (AOC-011)
- Public read endpoints `GET /v1/items` (filtered, paginated), `GET /v1/items/{slug}` and
  `GET /v1/taxonomies`, each carrying "Data preserved from AoC>TV by Kentarii" (AOC-012)
- `aoc-codex.app` as the one canonical host, `www` redirecting at the edge, and `img.aoc-codex.app`
  as the tooltip bucket's only public name (AOC-014)
- The HTML rendering foundation: embedded templates, HTMX, content-hashed assets and a smoke page
  (AOC-024)
- One cache policy for every response, so Cloudflare can serve public pages from its edge while
  anything with a session, a response that sets a cookie, or a request with an `Authorization`
  header is never stored; the zone now refuses TLS 1.0 and 1.1 (AOC-026)
- Automated daily off-site backups to R2, with an alarm when the newest one is stale, tiny or
  missing (AOC-030)
- The importer runs inside Railway as its own service beside the database, not down an SSH tunnel
  (AOC-040)

### Changed

- The version `/health` reports comes from the repo's `VERSION` file, and the build fails on
  anything but `x.y.z`; this release joins four pull requests into one deploy (AOC-015)

### Fixed

- `img.aoc-codex.app` serves the tooltip images from a Cloudflare Worker reading R2 through a
  binding, instead of R2's custom domain, which stalled on cache misses at the Singapore edge
  (AOC-041)
- A source no longer repeats a geography its place already supplies, so an unchained place and its
  sources can no longer disagree about the region (AOC-037)
- The import refuses a snapshot holding under 90% of the corpus, before it deletes anything
  (AOC-042)
- The tests that read the real imported armory are tagged `corpus`, so CI no longer runs them —
  they skipped there, which kept CI red on the item endpoints since they were written — and the
  gate always does (AOC-044)
