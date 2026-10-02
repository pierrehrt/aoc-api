-- Read queries for the Armory (AOC-010). The write side belongs to the importer (AOC-011) and the
-- public endpoints to AOC-012; what is here is the shape both of them read through.
--
-- ⭐ EQUIP LOCATION IS A JOIN. `ListItems` filters slots through item_equip_locations, never
-- through a column on items, because 389 one-handed weapons fit EITHER hand and 187 rings EITHER
-- finger (AOC-058: the first were read as "two-handers occupying both" until Pierre said otherwise).
-- A query that filtered a single column would silently drop the 389 —
-- the reason the join exists at all (see the migration header).
--
-- ⚠️ Every read returns confidence and open_question with the row, like places.sql. A page that
-- cannot show how sure we are cannot honour sourcing-standards § 3, and dropping them from the
-- SELECT is how that rule quietly stops being enforced.
--
-- ⚠️ Stats and spell effects are returned by SEPARATE queries from separate tables. Nothing here
-- unions them, so nothing downstream can sum a mount's -8% Sprinting Stamina Drain into a build.

-- name: GetItem :one
SELECT i.item_id, i.slug, i.name,
       r.slug AS rarity, it.slug AS item_type,
       sf.slug AS slot_fit,
       aw.slug AS armour_weight, b.slug AS binding,
       -- the display names and the rarity's colour, for the item PAGE (AOC-048); /v1 keeps the slugs
       r.name AS rarity_name, r.colour_token AS rarity_colour_token, it.name AS item_type_name,
       aw.name AS armour_weight_name, b.name AS binding_name,
       i.item_level, i.requires_level, i.armor, i.critigation, i.dps, i.damage_range,
       i.set_id, s.name AS set_name, s.declared_piece_count,
       f.slug AS faction, i.faction_rank,
       i.pvp_source, i.has_pvp_stats, i.pvp_penalty, i.no_longer_available,
       i.tooltip_image, i.tooltip_source_url,
       c.slug AS confidence, i.source_note, i.open_question
FROM items i
JOIN rarities r ON r.id = i.rarity_id
LEFT JOIN item_types it ON it.id = i.item_type_id
JOIN confidence_levels c ON c.id = i.confidence_id
LEFT JOIN slot_fits sf ON sf.id = i.slot_fit_id
LEFT JOIN armour_weights aw ON aw.id = i.armour_weight_id
LEFT JOIN bindings b ON b.id = i.binding_id
LEFT JOIN sets s ON s.id = i.set_id
LEFT JOIN factions f ON f.id = i.faction_id
WHERE i.item_id = $1;

-- name: GetItemBySlug :one
SELECT i.item_id FROM items i WHERE i.slug = $1;

-- name: ListItemStats :many
-- In the TOOLTIP's order (AOC-048): the importer writes each item's stats in the order its tooltip
-- prints them, so the id is that order — Dexterity before Combat Rating, as the game shows it.
-- It was alphabetical until then, which no reader of a tooltip would recognise.
SELECT st.item_id, st.stat, st.value, st.sign, st.unit, st.damage_type, st.pvp
FROM item_stats st
WHERE st.item_id = $1
ORDER BY st.id;

-- name: ListItemSpellEffects :many
-- Deliberately its own query against its own table. See the header. In the tooltip's order, like
-- ListItemStats.
SELECT se.item_id, se.stat, se.value, se.sign, se.unit, se.damage_type, se.pvp
FROM item_spell_effects se
WHERE se.item_id = $1
ORDER BY se.id;

-- name: ListItemEquipLocations :many
SELECT el.id, el.slug, el.name
FROM item_equip_locations iel
JOIN equip_locations el ON el.id = iel.equip_location_id
WHERE iel.item_id = $1
ORDER BY el.id;

-- name: ListItemClasses :many
SELECT cl.id, cl.slug, cl.name, cl.short_name
FROM item_classes ic
JOIN classes cl ON cl.id = ic.class_id
WHERE ic.item_id = $1
-- one order for classes everywhere (AOC-065)
ORDER BY cl.sort_order NULLS LAST, cl.name;

-- name: ListItemSources :many
-- An item page's "where does this come from". 237 items have more than one place — 8 with two and
-- 229 with three — so this is a list and never a single row.
SELECT src.id, src.item_id,
       at.slug AS acquisition_type, at.name AS acquisition_type_name,
       src.place_id, p.name AS place_name, p.slug AS place_slug,
       src.boss_id, bo.name AS boss_name,
       src.vendor_id, v.name AS vendor_name,
       src.quest_id, q.name AS quest_name,
       -- what the armory's quest column said: a giver, a hub or a bucket as often as a title, so
       -- the page shows it as listed and never calls it the quest's name (quests.name is NULL)
       q.armory_label AS quest_label,
       src.container_id, ct.name AS container_name,
       src.region_id, rg.name AS region_name, rg.slug AS region_slug,
       src.map_id, mp.name AS map_name, mp.slug AS map_slug,
       src.tier_id, tr.slug AS tier, tr.name AS tier_name,
       src.is_raid, src.coords, src.section_raw,
       -- ⭐ src OR place (AOC-039): the one unchained expression, shared with the list filter and
       -- ListItemPlaces. This was the bare src.unchained -- the third spelling of one rule, which
       -- agreed with the other two only because the importer set both columns from one source.
       (src.unchained OR coalesce(p.unchained, false))::boolean AS unchained,
       c.slug AS confidence, src.source_note, src.open_question
FROM item_sources src
JOIN confidence_levels c ON c.id = src.confidence_id
LEFT JOIN acquisition_types at ON at.id = src.acquisition_type_id
LEFT JOIN places p ON p.id = src.place_id
LEFT JOIN bosses bo ON bo.id = src.boss_id
LEFT JOIN vendors v ON v.id = src.vendor_id
LEFT JOIN quests q ON q.id = src.quest_id
LEFT JOIN containers ct ON ct.id = src.container_id
-- ⚠️ THE PLACE DECIDES, here too. AOC-012 verify round 1 settled that a source's place is the
-- stronger fact than the source's own region, and it was written into ListItems and
-- ListItemPlaces -- but NOT here, so the item page said Cimmeria for a place the list called
-- Stygia. 196 rows across 98 items, region and map alike, and a reader got a different answer
-- depending on which endpoint they landed on (verify round 4).
--
-- A read decision has to name EVERY query that publishes the fact, not the ones in front of you.
LEFT JOIN regions rg ON rg.id = coalesce(p.region_id, src.region_id)
LEFT JOIN maps mp ON mp.id = coalesce(p.map_id, src.map_id)
LEFT JOIN tiers tr ON tr.id = src.tier_id
WHERE src.item_id = $1
ORDER BY src.id;

-- name: ListItemSlugs :many
-- AOC-025: the sitemap's item URLs, one page of them. Ordered by the source site's own id, which
-- never changes, so a chunk holds the same items from one crawl to the next.
SELECT i.slug
FROM items i
ORDER BY i.item_id
LIMIT sqlc.arg('page_size')::int OFFSET sqlc.arg('page_offset')::int;

-- name: ListSetPieces :many
-- AOC-048: every piece of one set, for the item page and /v1's set_pieces. Bounded by the data's
-- own shape — the largest set holds 16 (measured 2026-09-30) — so it is part of one item's detail,
-- not a list endpoint.
SELECT i.item_id, i.slug, i.name, r.colour_token AS rarity_colour_token
FROM items i
JOIN rarities r ON r.id = i.rarity_id
WHERE i.set_id = $1
ORDER BY i.name, i.item_id;

-- name: ListItemCosts :many
SELECT ic.item_source_id, cu.slug AS currency, cu.name AS currency_name, ic.amount
FROM item_costs ic
JOIN currencies cu ON cu.id = ic.currency_id
WHERE ic.item_source_id = ANY($1::bigint[])
ORDER BY ic.item_source_id, cu.name;

-- name: ListItems :many
-- ⭐ THE FILTER RULES ARE WRITTEN ONCE (AOC-049). Every query that filters the armory list starts with
-- the same `filtered` CTE, byte for byte: one row per item, carrying one boolean per filter -- a
-- flag per facet of the rail, and `in_base` for the filters that have none. The list keeps the
-- items whose flags are all true. A facet counts the items whose flags are all true EXCEPT ITS OWN,
-- which is "how many would this choice leave, under the other filters" -- and since the count and
-- the rows read the same flags, they cannot disagree.
--
-- ⛔ Edit the CTE in one query and you must edit it in all three: ListItems, CountItemFacets and
-- ItemFacetTotals. TestTheFilterCTEIsOneDefinition (internal/db) reads this file and fails on any
-- difference -- sqlc has no way to share a fragment between queries, so the identity is a test.
-- Every flag is two-valued (NULL is coalesced to false), so "all but one" never meets a NULL.
--
-- The Armory list page. Every filter is optional: a NULL argument means "do not filter on this",
-- which keeps one query behind every combination the page offers rather than building SQL by hand.
--
-- ⭐ The equip-location filter goes through the join (EXISTS), so asking for 'off-hand' returns
-- the 389 one-handed weapons (they fit either hand) as well as the 141 off-hand-only items — 530,
-- not 141 (measured 2026-09-30). That is the acceptance criterion this whole schema shape exists for,
-- and TestListItemsFindsOneHandersWhenAskedForOffHand exercises THIS query, not a copy of it.
WITH filtered AS (
  SELECT i.item_id, i.rarity_id, i.armour_weight_id, i.set_id, i.item_level, i.requires_level,
         pr.priced,
         -- The filters that have no facet of their own, as ONE flag.
         coalesce(
           (sqlc.narg('item_type')::varchar IS NULL OR it.slug = sqlc.narg('item_type')::varchar)
           -- ESCAPE, because a name query is free text from a URL: '%' alone matched every one of the
           -- 4,646 items and '_' matched any single character. The caller escapes the metacharacters
           -- (escapeLike); this names the escape character so Postgres honours them.
           -- q matches the name, or the item's id exactly when q is a whole number (AOC-047): the
           -- armory site's ids are what people still quote, and a name search for "2183" would
           -- otherwise find nothing. id_query is NULL unless q is numeric.
           AND (sqlc.narg('name_query')::varchar IS NULL
                OR i.name ILIKE '%' || sqlc.narg('name_query')::varchar || '%' ESCAPE '\'
                OR i.item_id = sqlc.narg('id_query')::integer)
           -- A LIST, not one slug. Selecting two dungeons is the case Pierre's rule is about, and a
           -- single-value parameter made the caller's second choice unrepresentable -- so the
           -- service passed NULL and the place predicate silently vanished, returning all 4,646
           -- items (AOC-012 verify round 1).
           -- ⚠️ NULL means "no filter"; an EMPTY array does not -- `p.slug = ANY('{}')` is false for
           -- every row. The caller passes nil rather than an empty slice, and derives that from the
           -- same predicate that decides collapsing, so the two cannot disagree (verify round 2).
           AND (sqlc.narg('place_slugs')::varchar[] IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN places p ON p.id = src.place_id
                 WHERE src.item_id = i.item_id
                   AND p.slug = ANY(sqlc.narg('place_slugs')::varchar[])))
           -- region and tier live on item_sources, not on the item: an item is "in Kheshatta"
           -- because something that drops it is. Both are aggregate views, so they collapse.
           AND (sqlc.narg('region')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 LEFT JOIN places p ON p.id = src.place_id
                 -- The PLACE's region wins. 196 item_sources rows disagree with their own place's
                 -- region (AOC-037), and the place row is the stronger fact: it carries name, map
                 -- and region together, with an invariant test behind it, while the per-source
                 -- region is derived and has none. A source with no place falls back to its own.
                 LEFT JOIN regions r2 ON r2.id = coalesce(p.region_id, src.region_id)
                 WHERE src.item_id = i.item_id AND r2.slug = sqlc.narg('region')::varchar))
           AND (sqlc.narg('tier')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN tiers t ON t.id = src.tier_id
                 WHERE src.item_id = i.item_id AND t.slug = sqlc.narg('tier')::varchar))
           -- ⭐ unchained is true on EITHER the source row or the place it points at (AOC-039: the
           -- one expression, shared with ListItemSources and ListItemPlaces).
           AND (sqlc.narg('unchained')::boolean IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 LEFT JOIN places p ON p.id = src.place_id
                 WHERE src.item_id = i.item_id
                   AND (src.unchained OR coalesce(p.unchained, false)) = sqlc.narg('unchained')::boolean))
           -- pvp is three independent facts on the item, and "pvp=true" means any of them.
           AND (sqlc.narg('pvp')::boolean IS NULL
                OR (i.pvp_source OR i.has_pvp_stats OR i.pvp_penalty) = sqlc.narg('pvp')::boolean)
           -- ⭐ A SOURCE is ONE item_sources row (AOC-050): its tab's section, its region, map, place,
           -- boss, vendor, quest giver, container and acquisition group TOGETHER. tier= and place=
           -- above may each match a different row of the same item; a node of the source tree may
           -- not, or a tier's raid would count an item that is in the tier only somewhere else.
           -- source_tab is NULL unless a node is picked: a tab on its own filters nothing (Pierre,
           -- 2026-10-02). Region and map are the PLACE's when it has one, as everywhere (AOC-037).
           AND (sqlc.narg('source_tab')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN sections sc ON sc.id = src.section_id
                 JOIN source_tabs stb ON stb.id = sc.tab_id
                 LEFT JOIN places p ON p.id = src.place_id
                 LEFT JOIN regions sr ON sr.id = coalesce(p.region_id, src.region_id)
                 LEFT JOIN maps sm ON sm.id = coalesce(p.map_id, src.map_id)
                 LEFT JOIN bosses sb ON sb.id = src.boss_id
                 LEFT JOIN vendors sv ON sv.id = src.vendor_id
                 LEFT JOIN quests sq ON sq.id = src.quest_id
                 LEFT JOIN containers sct ON sct.id = src.container_id
                 LEFT JOIN acquisition_types sat ON sat.id = src.acquisition_type_id
                 LEFT JOIN acquisition_groups sag ON sag.id = sat.group_id
                 WHERE src.item_id = i.item_id
                   AND stb.slug = sqlc.narg('source_tab')::varchar
                   AND (sqlc.narg('source_section')::varchar IS NULL OR sc.slug = sqlc.narg('source_section')::varchar)
                   AND (sqlc.narg('source_region')::varchar IS NULL OR sr.slug = sqlc.narg('source_region')::varchar)
                   AND (sqlc.narg('source_map')::varchar IS NULL OR sm.slug = sqlc.narg('source_map')::varchar)
                   -- The place and every place inside it (AOC-038), expanded by the service.
                   AND (sqlc.narg('source_places')::varchar[] IS NULL OR p.slug = ANY(sqlc.narg('source_places')::varchar[]))
                   AND (sqlc.narg('source_boss')::varchar IS NULL OR sb.slug = sqlc.narg('source_boss')::varchar)
                   AND (sqlc.narg('source_vendor')::varchar IS NULL OR sv.slug = sqlc.narg('source_vendor')::varchar)
                   AND (sqlc.narg('source_quest')::varchar IS NULL OR sq.slug = sqlc.narg('source_quest')::varchar)
                   AND (sqlc.narg('source_container')::varchar IS NULL OR sct.slug = sqlc.narg('source_container')::varchar)
                   AND (sqlc.narg('source_group')::varchar IS NULL OR sag.slug = sqlc.narg('source_group')::varchar))),
           false) AS in_base,
         -- One flag per facet (AOC-049). Each is the whole of that filter's rule. A facet takes a LIST
         -- (AOC-064): any of its values, as `place` always has; NULL is no filter, never '{}'.
         (sqlc.narg('rarities')::varchar[] IS NULL OR r.slug = ANY(sqlc.narg('rarities')::varchar[])) AS in_rarity,
         -- ⭐ The slot goes through the join, so 'off-hand' finds the 389 one-handers (they fit either
         -- hand) as well as the 141 off-hand-only items -- 530, not 141 (AOC-058).
         (sqlc.narg('equip_locations')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_equip_locations iel
           JOIN equip_locations el ON el.id = iel.equip_location_id
           WHERE iel.item_id = i.item_id AND el.slug = ANY(sqlc.narg('equip_locations')::varchar[]))) AS in_slot,
         coalesce(sqlc.narg('armour_weights')::varchar[] IS NULL OR aw.slug = ANY(sqlc.narg('armour_weights')::varchar[]), false) AS in_weight,
         (sqlc.narg('classes')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_classes ic
           JOIN classes cl ON cl.id = ic.class_id
           WHERE ic.item_id = i.item_id AND cl.slug = ANY(sqlc.narg('classes')::varchar[]))) AS in_class,
         -- A bound excludes an item with no level: it cannot be shown to be inside the range.
         coalesce((sqlc.narg('ilvl_min')::integer IS NULL OR i.item_level >= sqlc.narg('ilvl_min')::integer)
              AND (sqlc.narg('ilvl_max')::integer IS NULL OR i.item_level <= sqlc.narg('ilvl_max')::integer), false) AS in_ilvl,
         coalesce((sqlc.narg('reqlvl_min')::integer IS NULL OR i.requires_level >= sqlc.narg('reqlvl_min')::integer)
              AND (sqlc.narg('reqlvl_max')::integer IS NULL OR i.requires_level <= sqlc.narg('reqlvl_max')::integer), false) AS in_reqlvl,
         -- "Has a vendor price" matches ANY occurrence: the list shows items, not occurrences, and an
         -- item free from a boss and sold by a vendor (689 of them) has a price.
         (sqlc.narg('price')::boolean IS NULL OR pr.priced = sqlc.narg('price')::boolean) AS in_price,
         (sqlc.narg('currencies')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_sources src
           JOIN item_costs ico ON ico.item_source_id = src.id
           JOIN currencies cu ON cu.id = ico.currency_id
           WHERE src.item_id = i.item_id AND cu.slug = ANY(sqlc.narg('currencies')::varchar[]))) AS in_currency,
         coalesce(sqlc.narg('sets')::varchar[] IS NULL OR st.slug = ANY(sqlc.narg('sets')::varchar[]), false) AS in_set
  FROM items i
  JOIN rarities r ON r.id = i.rarity_id
  LEFT JOIN item_types it ON it.id = i.item_type_id
  LEFT JOIN armour_weights aw ON aw.id = i.armour_weight_id
  LEFT JOIN sets st ON st.id = i.set_id
  -- One expression for "has a vendor price", read by in_price and by the price facet. A subquery
  -- with no FROM is pulled up into its references, so a query that reads neither never runs it.
  CROSS JOIN LATERAL (SELECT EXISTS (
    SELECT 1 FROM item_sources src
    JOIN item_costs ico ON ico.item_source_id = src.id
    WHERE src.item_id = i.item_id) AS priced) pr
)
SELECT i.item_id, i.slug, i.name,
       r.slug AS rarity, r.sort_order AS rarity_sort, r.colour_token AS rarity_colour_token,
       it.slug AS item_type, it.name AS item_type_name,
       sf.slug AS slot_fit,
       aw.slug AS armour_weight, aw.name AS armour_weight_name,
       i.item_level, i.requires_level, i.armor, i.dps,
       i.set_id, i.tooltip_image,
       c.slug AS confidence,
       count(*) OVER () AS total_count
FROM filtered f
JOIN items i ON i.item_id = f.item_id
JOIN rarities r ON r.id = i.rarity_id
LEFT JOIN item_types it ON it.id = i.item_type_id
JOIN confidence_levels c ON c.id = i.confidence_id
LEFT JOIN slot_fits sf ON sf.id = i.slot_fit_id
LEFT JOIN armour_weights aw ON aw.id = i.armour_weight_id
WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set
-- ⭐ ONE query, three orders (AOC-047). A CASE per sort key keeps every filter above in one
-- place; the keys are code ('name', 'ilvl', 'id' — not a game concept), validated before they
-- get here. ilvl is DESCENDING with NULLs LAST, so the 50 items with no recorded level end the
-- list rather than vanish or lead it; the name and id tie-breakers make every page stable.
ORDER BY
  CASE WHEN sqlc.arg('sort_by')::varchar = 'ilvl' THEN i.item_level END DESC NULLS LAST,
  CASE WHEN sqlc.arg('sort_by')::varchar = 'id' THEN i.item_id END ASC,
  i.name, i.item_id
LIMIT sqlc.arg('page_size')::integer OFFSET sqlc.arg('page_offset')::integer;

-- name: CountItemFacets :many
-- The rail's counts (AOC-049): EVERY value of each facet's vocabulary, with how many items picking
-- it would leave under the other filters -- 0 included, because a value that is hidden teaches
-- nothing and a greyed 0 says why. The values come from their lookup tables, never from Go
-- (reference/content-model.md § 0). count(DISTINCT): an item bought with one currency from two
-- vendors is one item, as it is one row of the list. `ord` is the order the rail shows them in.
WITH filtered AS MATERIALIZED (
  SELECT i.item_id, i.rarity_id, i.armour_weight_id, i.set_id, i.item_level, i.requires_level,
         pr.priced,
         -- The filters that have no facet of their own, as ONE flag.
         coalesce(
           (sqlc.narg('item_type')::varchar IS NULL OR it.slug = sqlc.narg('item_type')::varchar)
           -- ESCAPE, because a name query is free text from a URL: '%' alone matched every one of the
           -- 4,646 items and '_' matched any single character. The caller escapes the metacharacters
           -- (escapeLike); this names the escape character so Postgres honours them.
           -- q matches the name, or the item's id exactly when q is a whole number (AOC-047): the
           -- armory site's ids are what people still quote, and a name search for "2183" would
           -- otherwise find nothing. id_query is NULL unless q is numeric.
           AND (sqlc.narg('name_query')::varchar IS NULL
                OR i.name ILIKE '%' || sqlc.narg('name_query')::varchar || '%' ESCAPE '\'
                OR i.item_id = sqlc.narg('id_query')::integer)
           -- A LIST, not one slug. Selecting two dungeons is the case Pierre's rule is about, and a
           -- single-value parameter made the caller's second choice unrepresentable -- so the
           -- service passed NULL and the place predicate silently vanished, returning all 4,646
           -- items (AOC-012 verify round 1).
           -- ⚠️ NULL means "no filter"; an EMPTY array does not -- `p.slug = ANY('{}')` is false for
           -- every row. The caller passes nil rather than an empty slice, and derives that from the
           -- same predicate that decides collapsing, so the two cannot disagree (verify round 2).
           AND (sqlc.narg('place_slugs')::varchar[] IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN places p ON p.id = src.place_id
                 WHERE src.item_id = i.item_id
                   AND p.slug = ANY(sqlc.narg('place_slugs')::varchar[])))
           -- region and tier live on item_sources, not on the item: an item is "in Kheshatta"
           -- because something that drops it is. Both are aggregate views, so they collapse.
           AND (sqlc.narg('region')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 LEFT JOIN places p ON p.id = src.place_id
                 -- The PLACE's region wins. 196 item_sources rows disagree with their own place's
                 -- region (AOC-037), and the place row is the stronger fact: it carries name, map
                 -- and region together, with an invariant test behind it, while the per-source
                 -- region is derived and has none. A source with no place falls back to its own.
                 LEFT JOIN regions r2 ON r2.id = coalesce(p.region_id, src.region_id)
                 WHERE src.item_id = i.item_id AND r2.slug = sqlc.narg('region')::varchar))
           AND (sqlc.narg('tier')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN tiers t ON t.id = src.tier_id
                 WHERE src.item_id = i.item_id AND t.slug = sqlc.narg('tier')::varchar))
           -- ⭐ unchained is true on EITHER the source row or the place it points at (AOC-039: the
           -- one expression, shared with ListItemSources and ListItemPlaces).
           AND (sqlc.narg('unchained')::boolean IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 LEFT JOIN places p ON p.id = src.place_id
                 WHERE src.item_id = i.item_id
                   AND (src.unchained OR coalesce(p.unchained, false)) = sqlc.narg('unchained')::boolean))
           -- pvp is three independent facts on the item, and "pvp=true" means any of them.
           AND (sqlc.narg('pvp')::boolean IS NULL
                OR (i.pvp_source OR i.has_pvp_stats OR i.pvp_penalty) = sqlc.narg('pvp')::boolean)
           -- ⭐ A SOURCE is ONE item_sources row (AOC-050): its tab's section, its region, map, place,
           -- boss, vendor, quest giver, container and acquisition group TOGETHER. tier= and place=
           -- above may each match a different row of the same item; a node of the source tree may
           -- not, or a tier's raid would count an item that is in the tier only somewhere else.
           -- source_tab is NULL unless a node is picked: a tab on its own filters nothing (Pierre,
           -- 2026-10-02). Region and map are the PLACE's when it has one, as everywhere (AOC-037).
           AND (sqlc.narg('source_tab')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN sections sc ON sc.id = src.section_id
                 JOIN source_tabs stb ON stb.id = sc.tab_id
                 LEFT JOIN places p ON p.id = src.place_id
                 LEFT JOIN regions sr ON sr.id = coalesce(p.region_id, src.region_id)
                 LEFT JOIN maps sm ON sm.id = coalesce(p.map_id, src.map_id)
                 LEFT JOIN bosses sb ON sb.id = src.boss_id
                 LEFT JOIN vendors sv ON sv.id = src.vendor_id
                 LEFT JOIN quests sq ON sq.id = src.quest_id
                 LEFT JOIN containers sct ON sct.id = src.container_id
                 LEFT JOIN acquisition_types sat ON sat.id = src.acquisition_type_id
                 LEFT JOIN acquisition_groups sag ON sag.id = sat.group_id
                 WHERE src.item_id = i.item_id
                   AND stb.slug = sqlc.narg('source_tab')::varchar
                   AND (sqlc.narg('source_section')::varchar IS NULL OR sc.slug = sqlc.narg('source_section')::varchar)
                   AND (sqlc.narg('source_region')::varchar IS NULL OR sr.slug = sqlc.narg('source_region')::varchar)
                   AND (sqlc.narg('source_map')::varchar IS NULL OR sm.slug = sqlc.narg('source_map')::varchar)
                   -- The place and every place inside it (AOC-038), expanded by the service.
                   AND (sqlc.narg('source_places')::varchar[] IS NULL OR p.slug = ANY(sqlc.narg('source_places')::varchar[]))
                   AND (sqlc.narg('source_boss')::varchar IS NULL OR sb.slug = sqlc.narg('source_boss')::varchar)
                   AND (sqlc.narg('source_vendor')::varchar IS NULL OR sv.slug = sqlc.narg('source_vendor')::varchar)
                   AND (sqlc.narg('source_quest')::varchar IS NULL OR sq.slug = sqlc.narg('source_quest')::varchar)
                   AND (sqlc.narg('source_container')::varchar IS NULL OR sct.slug = sqlc.narg('source_container')::varchar)
                   AND (sqlc.narg('source_group')::varchar IS NULL OR sag.slug = sqlc.narg('source_group')::varchar))),
           false) AS in_base,
         -- One flag per facet (AOC-049). Each is the whole of that filter's rule. A facet takes a LIST
         -- (AOC-064): any of its values, as `place` always has; NULL is no filter, never '{}'.
         (sqlc.narg('rarities')::varchar[] IS NULL OR r.slug = ANY(sqlc.narg('rarities')::varchar[])) AS in_rarity,
         -- ⭐ The slot goes through the join, so 'off-hand' finds the 389 one-handers (they fit either
         -- hand) as well as the 141 off-hand-only items -- 530, not 141 (AOC-058).
         (sqlc.narg('equip_locations')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_equip_locations iel
           JOIN equip_locations el ON el.id = iel.equip_location_id
           WHERE iel.item_id = i.item_id AND el.slug = ANY(sqlc.narg('equip_locations')::varchar[]))) AS in_slot,
         coalesce(sqlc.narg('armour_weights')::varchar[] IS NULL OR aw.slug = ANY(sqlc.narg('armour_weights')::varchar[]), false) AS in_weight,
         (sqlc.narg('classes')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_classes ic
           JOIN classes cl ON cl.id = ic.class_id
           WHERE ic.item_id = i.item_id AND cl.slug = ANY(sqlc.narg('classes')::varchar[]))) AS in_class,
         -- A bound excludes an item with no level: it cannot be shown to be inside the range.
         coalesce((sqlc.narg('ilvl_min')::integer IS NULL OR i.item_level >= sqlc.narg('ilvl_min')::integer)
              AND (sqlc.narg('ilvl_max')::integer IS NULL OR i.item_level <= sqlc.narg('ilvl_max')::integer), false) AS in_ilvl,
         coalesce((sqlc.narg('reqlvl_min')::integer IS NULL OR i.requires_level >= sqlc.narg('reqlvl_min')::integer)
              AND (sqlc.narg('reqlvl_max')::integer IS NULL OR i.requires_level <= sqlc.narg('reqlvl_max')::integer), false) AS in_reqlvl,
         -- "Has a vendor price" matches ANY occurrence: the list shows items, not occurrences, and an
         -- item free from a boss and sold by a vendor (689 of them) has a price.
         (sqlc.narg('price')::boolean IS NULL OR pr.priced = sqlc.narg('price')::boolean) AS in_price,
         (sqlc.narg('currencies')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_sources src
           JOIN item_costs ico ON ico.item_source_id = src.id
           JOIN currencies cu ON cu.id = ico.currency_id
           WHERE src.item_id = i.item_id AND cu.slug = ANY(sqlc.narg('currencies')::varchar[]))) AS in_currency,
         coalesce(sqlc.narg('sets')::varchar[] IS NULL OR st.slug = ANY(sqlc.narg('sets')::varchar[]), false) AS in_set
  FROM items i
  JOIN rarities r ON r.id = i.rarity_id
  LEFT JOIN item_types it ON it.id = i.item_type_id
  LEFT JOIN armour_weights aw ON aw.id = i.armour_weight_id
  LEFT JOIN sets st ON st.id = i.set_id
  -- One expression for "has a vendor price", read by in_price and by the price facet. A subquery
  -- with no FROM is pulled up into its references, so a query that reads neither never runs it.
  CROSS JOIN LATERAL (SELECT EXISTS (
    SELECT 1 FROM item_sources src
    JOIN item_costs ico ON ico.item_source_id = src.id
    WHERE src.item_id = i.item_id) AS priced) pr
)
SELECT 'rarity'::varchar AS facet, r.slug, r.name, ''::varchar AS short_name,
       coalesce(r.colour_token, '')::varchar AS colour_token,
       row_number() OVER (ORDER BY r.sort_order DESC) AS ord,
       count(DISTINCT f.item_id) AS items
FROM rarities r
LEFT JOIN filtered f ON f.rarity_id = r.id AND f.in_base AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set
GROUP BY r.id
UNION ALL
SELECT 'equip_location', el.slug, el.name, '', '',
       row_number() OVER (ORDER BY el.id),
       count(DISTINCT f.item_id)
FROM equip_locations el
LEFT JOIN item_equip_locations iel ON iel.equip_location_id = el.id
LEFT JOIN filtered f ON f.item_id = iel.item_id AND f.in_base AND f.in_rarity AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set
GROUP BY el.id
UNION ALL
SELECT 'armour_weight', aw.slug, aw.name, '', '',
       row_number() OVER (ORDER BY aw.sort_order DESC),
       count(DISTINCT f.item_id)
FROM armour_weights aw
LEFT JOIN filtered f ON f.armour_weight_id = aw.id AND f.in_base AND f.in_rarity AND f.in_slot AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set
GROUP BY aw.id
UNION ALL
SELECT 'class', cl.slug, cl.name, coalesce(cl.short_name, ''), '',
       row_number() OVER (ORDER BY cl.sort_order NULLS LAST, cl.name),
       count(DISTINCT f.item_id)
FROM classes cl
LEFT JOIN item_classes ic ON ic.class_id = cl.id
LEFT JOIN filtered f ON f.item_id = ic.item_id AND f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set
GROUP BY cl.id
UNION ALL
SELECT 'currency', cu.slug, cu.name, '', '',
       row_number() OVER (ORDER BY cu.name),
       count(DISTINCT f.item_id)
FROM currencies cu
LEFT JOIN item_costs ico ON ico.currency_id = cu.id
LEFT JOIN item_sources src ON src.id = ico.item_source_id
LEFT JOIN filtered f ON f.item_id = src.item_id AND f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_set
GROUP BY cu.id
UNION ALL
SELECT 'set', st.slug, st.name, '', '',
       row_number() OVER (ORDER BY st.name, st.id),
       count(DISTINCT f.item_id)
FROM sets st
LEFT JOIN filtered f ON f.set_id = st.id AND f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency
GROUP BY st.id
ORDER BY facet, ord;

-- name: ItemFacetTotals :one
-- The rail's other numbers (AOC-049), from the same flags: each group's "Any" (every flag but the
-- group's own), how many items have a vendor price, and the item-level and required-level spans a
-- range control can usefully ask for. A span means something only when its count (ilvl_n, reqlvl_n)
-- is above 0: sqlc types a cast aggregate as non-null, so an empty span is coalesced to 0 and the
-- count says whether to believe it -- never a sentinel level.
WITH filtered AS MATERIALIZED (
  SELECT i.item_id, i.rarity_id, i.armour_weight_id, i.set_id, i.item_level, i.requires_level,
         pr.priced,
         -- The filters that have no facet of their own, as ONE flag.
         coalesce(
           (sqlc.narg('item_type')::varchar IS NULL OR it.slug = sqlc.narg('item_type')::varchar)
           -- ESCAPE, because a name query is free text from a URL: '%' alone matched every one of the
           -- 4,646 items and '_' matched any single character. The caller escapes the metacharacters
           -- (escapeLike); this names the escape character so Postgres honours them.
           -- q matches the name, or the item's id exactly when q is a whole number (AOC-047): the
           -- armory site's ids are what people still quote, and a name search for "2183" would
           -- otherwise find nothing. id_query is NULL unless q is numeric.
           AND (sqlc.narg('name_query')::varchar IS NULL
                OR i.name ILIKE '%' || sqlc.narg('name_query')::varchar || '%' ESCAPE '\'
                OR i.item_id = sqlc.narg('id_query')::integer)
           -- A LIST, not one slug. Selecting two dungeons is the case Pierre's rule is about, and a
           -- single-value parameter made the caller's second choice unrepresentable -- so the
           -- service passed NULL and the place predicate silently vanished, returning all 4,646
           -- items (AOC-012 verify round 1).
           -- ⚠️ NULL means "no filter"; an EMPTY array does not -- `p.slug = ANY('{}')` is false for
           -- every row. The caller passes nil rather than an empty slice, and derives that from the
           -- same predicate that decides collapsing, so the two cannot disagree (verify round 2).
           AND (sqlc.narg('place_slugs')::varchar[] IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN places p ON p.id = src.place_id
                 WHERE src.item_id = i.item_id
                   AND p.slug = ANY(sqlc.narg('place_slugs')::varchar[])))
           -- region and tier live on item_sources, not on the item: an item is "in Kheshatta"
           -- because something that drops it is. Both are aggregate views, so they collapse.
           AND (sqlc.narg('region')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 LEFT JOIN places p ON p.id = src.place_id
                 -- The PLACE's region wins. 196 item_sources rows disagree with their own place's
                 -- region (AOC-037), and the place row is the stronger fact: it carries name, map
                 -- and region together, with an invariant test behind it, while the per-source
                 -- region is derived and has none. A source with no place falls back to its own.
                 LEFT JOIN regions r2 ON r2.id = coalesce(p.region_id, src.region_id)
                 WHERE src.item_id = i.item_id AND r2.slug = sqlc.narg('region')::varchar))
           AND (sqlc.narg('tier')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN tiers t ON t.id = src.tier_id
                 WHERE src.item_id = i.item_id AND t.slug = sqlc.narg('tier')::varchar))
           -- ⭐ unchained is true on EITHER the source row or the place it points at (AOC-039: the
           -- one expression, shared with ListItemSources and ListItemPlaces).
           AND (sqlc.narg('unchained')::boolean IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 LEFT JOIN places p ON p.id = src.place_id
                 WHERE src.item_id = i.item_id
                   AND (src.unchained OR coalesce(p.unchained, false)) = sqlc.narg('unchained')::boolean))
           -- pvp is three independent facts on the item, and "pvp=true" means any of them.
           AND (sqlc.narg('pvp')::boolean IS NULL
                OR (i.pvp_source OR i.has_pvp_stats OR i.pvp_penalty) = sqlc.narg('pvp')::boolean)
           -- ⭐ A SOURCE is ONE item_sources row (AOC-050): its tab's section, its region, map, place,
           -- boss, vendor, quest giver, container and acquisition group TOGETHER. tier= and place=
           -- above may each match a different row of the same item; a node of the source tree may
           -- not, or a tier's raid would count an item that is in the tier only somewhere else.
           -- source_tab is NULL unless a node is picked: a tab on its own filters nothing (Pierre,
           -- 2026-10-02). Region and map are the PLACE's when it has one, as everywhere (AOC-037).
           AND (sqlc.narg('source_tab')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN sections sc ON sc.id = src.section_id
                 JOIN source_tabs stb ON stb.id = sc.tab_id
                 LEFT JOIN places p ON p.id = src.place_id
                 LEFT JOIN regions sr ON sr.id = coalesce(p.region_id, src.region_id)
                 LEFT JOIN maps sm ON sm.id = coalesce(p.map_id, src.map_id)
                 LEFT JOIN bosses sb ON sb.id = src.boss_id
                 LEFT JOIN vendors sv ON sv.id = src.vendor_id
                 LEFT JOIN quests sq ON sq.id = src.quest_id
                 LEFT JOIN containers sct ON sct.id = src.container_id
                 LEFT JOIN acquisition_types sat ON sat.id = src.acquisition_type_id
                 LEFT JOIN acquisition_groups sag ON sag.id = sat.group_id
                 WHERE src.item_id = i.item_id
                   AND stb.slug = sqlc.narg('source_tab')::varchar
                   AND (sqlc.narg('source_section')::varchar IS NULL OR sc.slug = sqlc.narg('source_section')::varchar)
                   AND (sqlc.narg('source_region')::varchar IS NULL OR sr.slug = sqlc.narg('source_region')::varchar)
                   AND (sqlc.narg('source_map')::varchar IS NULL OR sm.slug = sqlc.narg('source_map')::varchar)
                   -- The place and every place inside it (AOC-038), expanded by the service.
                   AND (sqlc.narg('source_places')::varchar[] IS NULL OR p.slug = ANY(sqlc.narg('source_places')::varchar[]))
                   AND (sqlc.narg('source_boss')::varchar IS NULL OR sb.slug = sqlc.narg('source_boss')::varchar)
                   AND (sqlc.narg('source_vendor')::varchar IS NULL OR sv.slug = sqlc.narg('source_vendor')::varchar)
                   AND (sqlc.narg('source_quest')::varchar IS NULL OR sq.slug = sqlc.narg('source_quest')::varchar)
                   AND (sqlc.narg('source_container')::varchar IS NULL OR sct.slug = sqlc.narg('source_container')::varchar)
                   AND (sqlc.narg('source_group')::varchar IS NULL OR sag.slug = sqlc.narg('source_group')::varchar))),
           false) AS in_base,
         -- One flag per facet (AOC-049). Each is the whole of that filter's rule. A facet takes a LIST
         -- (AOC-064): any of its values, as `place` always has; NULL is no filter, never '{}'.
         (sqlc.narg('rarities')::varchar[] IS NULL OR r.slug = ANY(sqlc.narg('rarities')::varchar[])) AS in_rarity,
         -- ⭐ The slot goes through the join, so 'off-hand' finds the 389 one-handers (they fit either
         -- hand) as well as the 141 off-hand-only items -- 530, not 141 (AOC-058).
         (sqlc.narg('equip_locations')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_equip_locations iel
           JOIN equip_locations el ON el.id = iel.equip_location_id
           WHERE iel.item_id = i.item_id AND el.slug = ANY(sqlc.narg('equip_locations')::varchar[]))) AS in_slot,
         coalesce(sqlc.narg('armour_weights')::varchar[] IS NULL OR aw.slug = ANY(sqlc.narg('armour_weights')::varchar[]), false) AS in_weight,
         (sqlc.narg('classes')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_classes ic
           JOIN classes cl ON cl.id = ic.class_id
           WHERE ic.item_id = i.item_id AND cl.slug = ANY(sqlc.narg('classes')::varchar[]))) AS in_class,
         -- A bound excludes an item with no level: it cannot be shown to be inside the range.
         coalesce((sqlc.narg('ilvl_min')::integer IS NULL OR i.item_level >= sqlc.narg('ilvl_min')::integer)
              AND (sqlc.narg('ilvl_max')::integer IS NULL OR i.item_level <= sqlc.narg('ilvl_max')::integer), false) AS in_ilvl,
         coalesce((sqlc.narg('reqlvl_min')::integer IS NULL OR i.requires_level >= sqlc.narg('reqlvl_min')::integer)
              AND (sqlc.narg('reqlvl_max')::integer IS NULL OR i.requires_level <= sqlc.narg('reqlvl_max')::integer), false) AS in_reqlvl,
         -- "Has a vendor price" matches ANY occurrence: the list shows items, not occurrences, and an
         -- item free from a boss and sold by a vendor (689 of them) has a price.
         (sqlc.narg('price')::boolean IS NULL OR pr.priced = sqlc.narg('price')::boolean) AS in_price,
         (sqlc.narg('currencies')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_sources src
           JOIN item_costs ico ON ico.item_source_id = src.id
           JOIN currencies cu ON cu.id = ico.currency_id
           WHERE src.item_id = i.item_id AND cu.slug = ANY(sqlc.narg('currencies')::varchar[]))) AS in_currency,
         coalesce(sqlc.narg('sets')::varchar[] IS NULL OR st.slug = ANY(sqlc.narg('sets')::varchar[]), false) AS in_set
  FROM items i
  JOIN rarities r ON r.id = i.rarity_id
  LEFT JOIN item_types it ON it.id = i.item_type_id
  LEFT JOIN armour_weights aw ON aw.id = i.armour_weight_id
  LEFT JOIN sets st ON st.id = i.set_id
  -- One expression for "has a vendor price", read by in_price and by the price facet. A subquery
  -- with no FROM is pulled up into its references, so a query that reads neither never runs it.
  CROSS JOIN LATERAL (SELECT EXISTS (
    SELECT 1 FROM item_sources src
    JOIN item_costs ico ON ico.item_source_id = src.id
    WHERE src.item_id = i.item_id) AS priced) pr
)
SELECT
  count(*) FILTER (WHERE f.in_base AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set) AS any_rarity,
  count(*) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set) AS any_equip_location,
  count(*) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set) AS any_armour_weight,
  count(*) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set) AS any_class,
  count(*) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_set) AS any_currency,
  count(*) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency) AS any_set,
  count(*) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_currency AND f.in_set) AS any_price,
  count(*) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_currency AND f.in_set AND f.priced) AS priced,
  count(f.item_level) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set) AS ilvl_n,
  coalesce(min(f.item_level) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set), 0)::integer AS ilvl_lo,
  coalesce(max(f.item_level) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set), 0)::integer AS ilvl_hi,
  count(f.requires_level) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_price AND f.in_currency AND f.in_set) AS reqlvl_n,
  coalesce(min(f.requires_level) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_price AND f.in_currency AND f.in_set), 0)::integer AS reqlvl_lo,
  coalesce(max(f.requires_level) FILTER (WHERE f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_price AND f.in_currency AND f.in_set), 0)::integer AS reqlvl_hi
FROM filtered f;

-- name: ListItemPageEquipLocations :many
-- A page's slots in one round trip (AOC-047), the ListItemPlaces pattern: never one query per row.
SELECT iel.item_id, el.slug, el.name
FROM item_equip_locations iel
JOIN equip_locations el ON el.id = iel.equip_location_id
WHERE iel.item_id = ANY(sqlc.arg('item_ids')::integer[])
ORDER BY iel.item_id, el.id;

-- name: ListItemPageClasses :many
-- A page's class restrictions in one round trip, with the short names the row shows (AOC-046).
SELECT ic.item_id, cl.slug, cl.name, cl.short_name
FROM item_classes ic
JOIN classes cl ON cl.id = ic.class_id
WHERE ic.item_id = ANY(sqlc.arg('item_ids')::integer[])
-- one order for classes everywhere (AOC-065)
ORDER BY ic.item_id, cl.sort_order NULLS LAST, cl.id;

-- name: ListItemPageCosts :many
-- A page's vendor prices in one round trip: every cost of every source, grouped by the caller.
SELECT src.item_id, src.id AS item_source_id, cu.name AS currency_name, ic.amount
FROM item_costs ic
JOIN item_sources src ON src.id = ic.item_source_id
JOIN currencies cu ON cu.id = ic.currency_id
WHERE src.item_id = ANY(sqlc.arg('item_ids')::integer[])
ORDER BY src.item_id, src.id, cu.name;

-- name: ItemIDSpan :one
-- The honest empty state (AOC-047): "ids run 1–4692, N absent" is computed, never typed.
SELECT coalesce(min(item_id), 0)::integer AS min_id,
       coalesce(max(item_id), 0)::integer AS max_id,
       count(*)::bigint AS total
FROM items;

-- name: ListItemPlaces :many
-- The per-item place context for a page of results, fetched in ONE round trip for the whole page
-- rather than per row. Used two ways:
--   * an aggregate view (region/map/tier, or no place filter) renders these as "also drops in",
--     and the item still appears ONCE -- Pierre's rule, DECISIONS.md 2026-09-13;
--   * a view that named specific places expands to one row per item per NAMED place, because there
--     the duplication is the information.
-- The choice is made server-side from the filters, never by a client flag.
-- ⚠️ GROUPED BY PLACE, not by source row. An item can have several item_sources in ONE place --
-- two bosses, or a boss and a container -- and without the grouping the same dungeon appeared
-- twice next to the item, which made a two-dungeon selection render four rows instead of two.
-- Measured on the real data before it was written. The unit of Pierre's rule is the PLACE.
-- Every individual source, with its own boss, is still on the item page via ListItemSources.
SELECT src.item_id, p.slug AS place_slug, p.name AS place_name,
       -- coalesce, not a bare cast: min() over all-NULL is NULL, and sqlc types a cast as
       -- non-null, so the scan would fail on exactly the rows that have no region.
       coalesce(min(r.slug), '')::varchar AS region_slug,
       -- ⭐ ONE EXPRESSION PER FACT (AOC-039). The tier is blanked when the place's sources disagree,
       -- exactly like the boss below: min() would state one of two tiers as fact the first time the
       -- corpus held two. And unchained is the SAME expression the list filter and ListItemSources
       -- use -- src OR place, with a NULL place read as false -- so a row cannot be findable by
       -- unchained=true and then deny it on its own page.
       coalesce(CASE WHEN count(DISTINCT t.slug) = 1 THEN min(t.slug) END, '')::varchar AS tier_slug,
       bool_or(src.unchained OR coalesce(p.unchained, false)) AS unchained,
       -- The boss only when it is unambiguous: two bosses in one place would make either name a
       -- lie, and a blank is honest where a guess is not (CLAUDE.md STEP ZERO).
       coalesce(CASE WHEN count(DISTINCT b.name) = 1 THEN min(b.name) END, '')::varchar AS boss_name
FROM item_sources src
JOIN places p ON p.id = src.place_id
LEFT JOIN regions r ON r.id = coalesce(p.region_id, src.region_id)
LEFT JOIN tiers t ON t.id = src.tier_id
LEFT JOIN bosses b ON b.id = src.boss_id
WHERE src.item_id = ANY(sqlc.arg('item_ids')::integer[])
GROUP BY src.item_id, p.slug, p.name
ORDER BY p.name;

-- name: ExpandPlaces :many
-- A place that CONTAINS places (AOC-038): House of Crom's loot is recorded against its two wings,
-- Warmonk Monastery's against its three, so the place itself holds almost none of it. Asking for the
-- place means everything in it (Pierre, 2026-09-29, DECISIONS.md), so the service hands SQL the named
-- places AND every place under them, at any depth, and reads off whether anything named contained
-- something. One row per (named, place inside it), the named place included as its own row.
-- UNION, not UNION ALL: a cycle in parent_place_id would repeat a row, and UNION stops there.
WITH RECURSIVE inside AS (
    SELECT p.slug AS named, p.id, p.slug
    FROM places p
    WHERE p.slug = ANY(sqlc.arg('slugs')::varchar[])
  UNION
    SELECT inside.named, c.id, c.slug
    FROM places c JOIN inside ON c.parent_place_id = inside.id
)
SELECT inside.named::varchar AS named, inside.slug::varchar AS slug
FROM inside
ORDER BY inside.named, inside.slug;

-- name: ListItemsByPlace :many
-- The reverse lookup, and the one Pierre asked for first: "what drops here?" on a place page.
SELECT DISTINCT i.item_id, i.slug, i.name, r.slug AS rarity, r.sort_order AS rarity_sort,
       it.slug AS item_type, i.tooltip_image
FROM items i
JOIN rarities r ON r.id = i.rarity_id
LEFT JOIN item_types it ON it.id = i.item_type_id
JOIN item_sources src ON src.item_id = i.item_id
JOIN places p ON p.id = src.place_id
WHERE p.slug = $1
ORDER BY r.sort_order DESC, i.name;

-- name: ListItemsByCurrency :many
-- A currency page: "what does this token buy?" (DECISIONS.md 2026-09-14 — currency is an entity
-- with its own page, so the fact is stated once instead of on every item bought with it).
SELECT DISTINCT i.item_id, i.slug, i.name, r.slug AS rarity, it.slug AS item_type,
       ic.amount, i.tooltip_image
FROM items i
JOIN rarities r ON r.id = i.rarity_id
LEFT JOIN item_types it ON it.id = i.item_type_id
JOIN item_sources src ON src.item_id = i.item_id
JOIN item_costs ic ON ic.item_source_id = src.id
JOIN currencies cu ON cu.id = ic.currency_id
WHERE cu.slug = $1
ORDER BY ic.amount, i.name;

-- name: ListSets :many
SELECT s.id, s.slug, s.name, cl.slug AS class, aw.slug AS set_armour_weight,
       c.slug AS confidence, s.source_note, s.open_question,
       s.declared_piece_count,
       (SELECT count(*) FROM items i WHERE i.set_id = s.id) AS pieces_held
FROM sets s
JOIN confidence_levels c ON c.id = s.confidence_id
LEFT JOIN classes cl ON cl.id = s.class_id
LEFT JOIN armour_weights aw ON aw.id = s.set_armour_weight_id
ORDER BY s.name;

-- name: ListOpenItemQuestions :many
-- The counterpart of AOC-009's ListOpenQuestions: everything the item data could not answer,
-- in one read, so "what do we still need to ask Pierre" stays a query and not a memory.
SELECT 'item' AS kind, i.item_id AS id, i.name, i.open_question
FROM items i WHERE i.open_question IS NOT NULL
UNION ALL
SELECT 'item_source', src.item_id, i.name, src.open_question
FROM item_sources src JOIN items i ON i.item_id = src.item_id
WHERE src.open_question IS NOT NULL
UNION ALL
SELECT 'set', s.id, s.name, s.open_question
FROM sets s WHERE s.open_question IS NOT NULL
ORDER BY kind, name;

-- name: ListSourceTreeRows :many
-- The source panel's raw material (AOC-050): one row per (item, the levels of ONE of its source rows)
-- in a tab, for the items every OTHER filter leaves. The service builds the tab's tree from these and
-- counts distinct items per node, so a node's count is what choosing it would leave -- the rail's
-- rule (AOC-049) -- and the tree's own selection is left out by calling this with no source_*
-- arguments. Levels are columns of our own tables; which of them a tab draws is source_tabs.groups.
-- '' is "this row has no such level" (sqlc types a coalesced column as non-null).
-- A place's PARENT is the location and the place itself its wing (House of Crom › The Vile Nativity).
WITH filtered AS (
  SELECT i.item_id, i.rarity_id, i.armour_weight_id, i.set_id, i.item_level, i.requires_level,
         pr.priced,
         -- The filters that have no facet of their own, as ONE flag.
         coalesce(
           (sqlc.narg('item_type')::varchar IS NULL OR it.slug = sqlc.narg('item_type')::varchar)
           -- ESCAPE, because a name query is free text from a URL: '%' alone matched every one of the
           -- 4,646 items and '_' matched any single character. The caller escapes the metacharacters
           -- (escapeLike); this names the escape character so Postgres honours them.
           -- q matches the name, or the item's id exactly when q is a whole number (AOC-047): the
           -- armory site's ids are what people still quote, and a name search for "2183" would
           -- otherwise find nothing. id_query is NULL unless q is numeric.
           AND (sqlc.narg('name_query')::varchar IS NULL
                OR i.name ILIKE '%' || sqlc.narg('name_query')::varchar || '%' ESCAPE '\'
                OR i.item_id = sqlc.narg('id_query')::integer)
           -- A LIST, not one slug. Selecting two dungeons is the case Pierre's rule is about, and a
           -- single-value parameter made the caller's second choice unrepresentable -- so the
           -- service passed NULL and the place predicate silently vanished, returning all 4,646
           -- items (AOC-012 verify round 1).
           -- ⚠️ NULL means "no filter"; an EMPTY array does not -- `p.slug = ANY('{}')` is false for
           -- every row. The caller passes nil rather than an empty slice, and derives that from the
           -- same predicate that decides collapsing, so the two cannot disagree (verify round 2).
           AND (sqlc.narg('place_slugs')::varchar[] IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN places p ON p.id = src.place_id
                 WHERE src.item_id = i.item_id
                   AND p.slug = ANY(sqlc.narg('place_slugs')::varchar[])))
           -- region and tier live on item_sources, not on the item: an item is "in Kheshatta"
           -- because something that drops it is. Both are aggregate views, so they collapse.
           AND (sqlc.narg('region')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 LEFT JOIN places p ON p.id = src.place_id
                 -- The PLACE's region wins. 196 item_sources rows disagree with their own place's
                 -- region (AOC-037), and the place row is the stronger fact: it carries name, map
                 -- and region together, with an invariant test behind it, while the per-source
                 -- region is derived and has none. A source with no place falls back to its own.
                 LEFT JOIN regions r2 ON r2.id = coalesce(p.region_id, src.region_id)
                 WHERE src.item_id = i.item_id AND r2.slug = sqlc.narg('region')::varchar))
           AND (sqlc.narg('tier')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN tiers t ON t.id = src.tier_id
                 WHERE src.item_id = i.item_id AND t.slug = sqlc.narg('tier')::varchar))
           -- ⭐ unchained is true on EITHER the source row or the place it points at (AOC-039: the
           -- one expression, shared with ListItemSources and ListItemPlaces).
           AND (sqlc.narg('unchained')::boolean IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 LEFT JOIN places p ON p.id = src.place_id
                 WHERE src.item_id = i.item_id
                   AND (src.unchained OR coalesce(p.unchained, false)) = sqlc.narg('unchained')::boolean))
           -- pvp is three independent facts on the item, and "pvp=true" means any of them.
           AND (sqlc.narg('pvp')::boolean IS NULL
                OR (i.pvp_source OR i.has_pvp_stats OR i.pvp_penalty) = sqlc.narg('pvp')::boolean)
           -- ⭐ A SOURCE is ONE item_sources row (AOC-050): its tab's section, its region, map, place,
           -- boss, vendor, quest giver, container and acquisition group TOGETHER. tier= and place=
           -- above may each match a different row of the same item; a node of the source tree may
           -- not, or a tier's raid would count an item that is in the tier only somewhere else.
           -- source_tab is NULL unless a node is picked: a tab on its own filters nothing (Pierre,
           -- 2026-10-02). Region and map are the PLACE's when it has one, as everywhere (AOC-037).
           AND (sqlc.narg('source_tab')::varchar IS NULL OR EXISTS (
                 SELECT 1 FROM item_sources src
                 JOIN sections sc ON sc.id = src.section_id
                 JOIN source_tabs stb ON stb.id = sc.tab_id
                 LEFT JOIN places p ON p.id = src.place_id
                 LEFT JOIN regions sr ON sr.id = coalesce(p.region_id, src.region_id)
                 LEFT JOIN maps sm ON sm.id = coalesce(p.map_id, src.map_id)
                 LEFT JOIN bosses sb ON sb.id = src.boss_id
                 LEFT JOIN vendors sv ON sv.id = src.vendor_id
                 LEFT JOIN quests sq ON sq.id = src.quest_id
                 LEFT JOIN containers sct ON sct.id = src.container_id
                 LEFT JOIN acquisition_types sat ON sat.id = src.acquisition_type_id
                 LEFT JOIN acquisition_groups sag ON sag.id = sat.group_id
                 WHERE src.item_id = i.item_id
                   AND stb.slug = sqlc.narg('source_tab')::varchar
                   AND (sqlc.narg('source_section')::varchar IS NULL OR sc.slug = sqlc.narg('source_section')::varchar)
                   AND (sqlc.narg('source_region')::varchar IS NULL OR sr.slug = sqlc.narg('source_region')::varchar)
                   AND (sqlc.narg('source_map')::varchar IS NULL OR sm.slug = sqlc.narg('source_map')::varchar)
                   -- The place and every place inside it (AOC-038), expanded by the service.
                   AND (sqlc.narg('source_places')::varchar[] IS NULL OR p.slug = ANY(sqlc.narg('source_places')::varchar[]))
                   AND (sqlc.narg('source_boss')::varchar IS NULL OR sb.slug = sqlc.narg('source_boss')::varchar)
                   AND (sqlc.narg('source_vendor')::varchar IS NULL OR sv.slug = sqlc.narg('source_vendor')::varchar)
                   AND (sqlc.narg('source_quest')::varchar IS NULL OR sq.slug = sqlc.narg('source_quest')::varchar)
                   AND (sqlc.narg('source_container')::varchar IS NULL OR sct.slug = sqlc.narg('source_container')::varchar)
                   AND (sqlc.narg('source_group')::varchar IS NULL OR sag.slug = sqlc.narg('source_group')::varchar))),
           false) AS in_base,
         -- One flag per facet (AOC-049). Each is the whole of that filter's rule. A facet takes a LIST
         -- (AOC-064): any of its values, as `place` always has; NULL is no filter, never '{}'.
         (sqlc.narg('rarities')::varchar[] IS NULL OR r.slug = ANY(sqlc.narg('rarities')::varchar[])) AS in_rarity,
         -- ⭐ The slot goes through the join, so 'off-hand' finds the 389 one-handers (they fit either
         -- hand) as well as the 141 off-hand-only items -- 530, not 141 (AOC-058).
         (sqlc.narg('equip_locations')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_equip_locations iel
           JOIN equip_locations el ON el.id = iel.equip_location_id
           WHERE iel.item_id = i.item_id AND el.slug = ANY(sqlc.narg('equip_locations')::varchar[]))) AS in_slot,
         coalesce(sqlc.narg('armour_weights')::varchar[] IS NULL OR aw.slug = ANY(sqlc.narg('armour_weights')::varchar[]), false) AS in_weight,
         (sqlc.narg('classes')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_classes ic
           JOIN classes cl ON cl.id = ic.class_id
           WHERE ic.item_id = i.item_id AND cl.slug = ANY(sqlc.narg('classes')::varchar[]))) AS in_class,
         -- A bound excludes an item with no level: it cannot be shown to be inside the range.
         coalesce((sqlc.narg('ilvl_min')::integer IS NULL OR i.item_level >= sqlc.narg('ilvl_min')::integer)
              AND (sqlc.narg('ilvl_max')::integer IS NULL OR i.item_level <= sqlc.narg('ilvl_max')::integer), false) AS in_ilvl,
         coalesce((sqlc.narg('reqlvl_min')::integer IS NULL OR i.requires_level >= sqlc.narg('reqlvl_min')::integer)
              AND (sqlc.narg('reqlvl_max')::integer IS NULL OR i.requires_level <= sqlc.narg('reqlvl_max')::integer), false) AS in_reqlvl,
         -- "Has a vendor price" matches ANY occurrence: the list shows items, not occurrences, and an
         -- item free from a boss and sold by a vendor (689 of them) has a price.
         (sqlc.narg('price')::boolean IS NULL OR pr.priced = sqlc.narg('price')::boolean) AS in_price,
         (sqlc.narg('currencies')::varchar[] IS NULL OR EXISTS (
           SELECT 1 FROM item_sources src
           JOIN item_costs ico ON ico.item_source_id = src.id
           JOIN currencies cu ON cu.id = ico.currency_id
           WHERE src.item_id = i.item_id AND cu.slug = ANY(sqlc.narg('currencies')::varchar[]))) AS in_currency,
         coalesce(sqlc.narg('sets')::varchar[] IS NULL OR st.slug = ANY(sqlc.narg('sets')::varchar[]), false) AS in_set
  FROM items i
  JOIN rarities r ON r.id = i.rarity_id
  LEFT JOIN item_types it ON it.id = i.item_type_id
  LEFT JOIN armour_weights aw ON aw.id = i.armour_weight_id
  LEFT JOIN sets st ON st.id = i.set_id
  -- One expression for "has a vendor price", read by in_price and by the price facet. A subquery
  -- with no FROM is pulled up into its references, so a query that reads neither never runs it.
  CROSS JOIN LATERAL (SELECT EXISTS (
    SELECT 1 FROM item_sources src
    JOIN item_costs ico ON ico.item_source_id = src.id
    WHERE src.item_id = i.item_id) AS priced) pr
)
SELECT DISTINCT src.item_id,
       sc.slug AS section_slug, sc.name AS section_name, sc.sort_order AS section_sort,
       coalesce(sr.slug, '')::varchar AS region_slug, coalesce(sr.name, '')::varchar AS region_name,
       coalesce(sr.sort_order, 0)::integer AS region_sort,
       coalesce(sm.slug, '')::varchar AS map_slug, coalesce(sm.name, '')::varchar AS map_name,
       coalesce(pp.slug, p.slug, '')::varchar AS place_slug, coalesce(pp.name, p.name, '')::varchar AS place_name,
       (CASE WHEN pp.id IS NOT NULL THEN p.slug ELSE '' END)::varchar AS wing_slug,
       (CASE WHEN pp.id IS NOT NULL THEN p.name ELSE '' END)::varchar AS wing_name,
       coalesce(sb.slug, '')::varchar AS boss_slug, coalesce(sb.name, '')::varchar AS boss_name,
       coalesce(sv.slug, '')::varchar AS vendor_slug, coalesce(sv.name, '')::varchar AS vendor_name,
       -- quests.name is deliberately NULL (the real name is unknown); the armory's label is what it said.
       coalesce(sq.slug, '')::varchar AS quest_slug, coalesce(sq.name, sq.armory_label, '')::varchar AS quest_name,
       coalesce(sct.slug, '')::varchar AS container_slug, coalesce(sct.name, '')::varchar AS container_name,
       coalesce(sag.slug, '')::varchar AS group_slug
FROM filtered f
JOIN item_sources src ON src.item_id = f.item_id
JOIN sections sc ON sc.id = src.section_id
JOIN source_tabs stb ON stb.id = sc.tab_id
LEFT JOIN places p ON p.id = src.place_id
LEFT JOIN places pp ON pp.id = p.parent_place_id
LEFT JOIN regions sr ON sr.id = coalesce(p.region_id, src.region_id)
LEFT JOIN maps sm ON sm.id = coalesce(p.map_id, src.map_id)
LEFT JOIN bosses sb ON sb.id = src.boss_id
LEFT JOIN vendors sv ON sv.id = src.vendor_id
LEFT JOIN quests sq ON sq.id = src.quest_id
LEFT JOIN containers sct ON sct.id = src.container_id
LEFT JOIN acquisition_types sat ON sat.id = src.acquisition_type_id
LEFT JOIN acquisition_groups sag ON sag.id = sat.group_id
WHERE stb.slug = sqlc.arg('tab')::varchar
  AND f.in_base AND f.in_rarity AND f.in_slot AND f.in_weight AND f.in_class AND f.in_ilvl AND f.in_reqlvl AND f.in_price AND f.in_currency AND f.in_set
ORDER BY src.item_id;

-- name: ListSourceTabs :many
-- The tabs in Pierre's order, with the levels each draws above the location (AOC-050).
SELECT slug, name, sort_order, levels_note, groups::varchar[] AS groups
FROM source_tabs
ORDER BY sort_order;

-- name: ListSectionTabs :many
-- Every section and its tab: what a source path's section must belong to.
SELECT sc.slug, sc.name, t.slug AS tab_slug
FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
ORDER BY t.sort_order, sc.sort_order;

-- name: ListAcquisitionGroups :many
-- The design's two halves of a location, "loot / drops" and "quest / vendor" (AOC-050), in order.
SELECT slug, name FROM acquisition_groups ORDER BY sort_order;
