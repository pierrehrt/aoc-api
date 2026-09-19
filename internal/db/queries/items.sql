-- Read queries for the Armory (AOC-010). The write side belongs to the importer (AOC-011) and the
-- public endpoints to AOC-012; what is here is the shape both of them read through.
--
-- ⭐ EQUIP LOCATION IS A JOIN. `ListItems` filters slots through item_equip_locations, never
-- through a column on items, because 390 two-handers occupy Main Hand AND Off Hand and 188 rings
-- occupy EITHER finger. A query that filtered a single column would silently drop the 390 —
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
       i.item_level, i.requires_level, i.armor, i.critigation, i.dps, i.damage_range,
       i.set_id, s.name AS set_name,
       f.slug AS faction, i.faction_rank,
       i.pvp_source, i.has_pvp_stats, i.pvp_penalty, i.no_longer_available,
       i.tooltip_image, i.tooltip_source_url,
       c.slug AS confidence, i.source_note, i.open_question
FROM items i
JOIN rarities r ON r.id = i.rarity_id
JOIN item_types it ON it.id = i.item_type_id
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
SELECT st.item_id, st.stat, st.value, st.sign, st.unit, st.damage_type, st.pvp
FROM item_stats st
WHERE st.item_id = $1
ORDER BY st.stat;

-- name: ListItemSpellEffects :many
-- Deliberately its own query against its own table. See the header.
SELECT se.item_id, se.stat, se.value, se.sign, se.unit, se.damage_type, se.pvp
FROM item_spell_effects se
WHERE se.item_id = $1
ORDER BY se.stat;

-- name: ListItemEquipLocations :many
SELECT el.id, el.slug, el.name
FROM item_equip_locations iel
JOIN equip_locations el ON el.id = iel.equip_location_id
WHERE iel.item_id = $1
ORDER BY el.id;

-- name: ListItemClasses :many
SELECT cl.id, cl.slug, cl.name
FROM item_classes ic
JOIN classes cl ON cl.id = ic.class_id
WHERE ic.item_id = $1
ORDER BY cl.name;

-- name: ListItemSources :many
-- An item page's "where does this come from". 422 items have more than one place, so this is a
-- list and never a single row.
SELECT src.id, src.item_id,
       at.slug AS acquisition_type,
       src.place_id, p.name AS place_name,
       src.boss_id, bo.name AS boss_name,
       src.vendor_id, v.name AS vendor_name,
       src.quest_id, q.name AS quest_name,
       src.container_id, ct.name AS container_name,
       src.region_id, rg.name AS region_name,
       src.map_id, mp.name AS map_name,
       src.tier_id, tr.slug AS tier,
       src.is_raid, src.coords, src.section_raw, src.unchained,
       c.slug AS confidence, src.source_note, src.open_question
FROM item_sources src
JOIN confidence_levels c ON c.id = src.confidence_id
LEFT JOIN acquisition_types at ON at.id = src.acquisition_type_id
LEFT JOIN places p ON p.id = src.place_id
LEFT JOIN bosses bo ON bo.id = src.boss_id
LEFT JOIN places v ON v.id = src.vendor_id
LEFT JOIN quests q ON q.id = src.quest_id
LEFT JOIN containers ct ON ct.id = src.container_id
LEFT JOIN regions rg ON rg.id = src.region_id
LEFT JOIN maps mp ON mp.id = src.map_id
LEFT JOIN tiers tr ON tr.id = src.tier_id
WHERE src.item_id = $1
ORDER BY src.id;

-- name: ListItemCosts :many
SELECT ic.item_source_id, cu.slug AS currency, cu.name AS currency_name, ic.amount
FROM item_costs ic
JOIN currencies cu ON cu.id = ic.currency_id
WHERE ic.item_source_id = ANY($1::bigint[])
ORDER BY ic.item_source_id, cu.name;

-- name: ListItems :many
-- The Armory list page. Every filter is optional: a NULL argument means "do not filter on this",
-- which keeps one query behind every combination the page offers rather than building SQL by hand.
--
-- ⭐ The equip-location filter goes through the join (EXISTS), so asking for 'off-hand' returns
-- the 390 two-handers as well as the 141 off-hand-only items. That is the acceptance criterion
-- this whole schema shape exists for.
SELECT i.item_id, i.slug, i.name,
       r.slug AS rarity, r.sort_order AS rarity_sort,
       it.slug AS item_type,
       sf.slug AS slot_fit,
       i.item_level, i.requires_level, i.armor, i.dps,
       i.set_id, i.tooltip_image,
       c.slug AS confidence,
       count(*) OVER () AS total_count
FROM items i
JOIN rarities r ON r.id = i.rarity_id
JOIN item_types it ON it.id = i.item_type_id
JOIN confidence_levels c ON c.id = i.confidence_id
LEFT JOIN slot_fits sf ON sf.id = i.slot_fit_id
WHERE (sqlc.narg('rarity')::varchar IS NULL OR r.slug = sqlc.narg('rarity')::varchar)
  AND (sqlc.narg('item_type')::varchar IS NULL OR it.slug = sqlc.narg('item_type')::varchar)
  AND (sqlc.narg('name_query')::varchar IS NULL OR i.name ILIKE '%' || sqlc.narg('name_query')::varchar || '%')
  AND (sqlc.narg('equip_location')::varchar IS NULL OR EXISTS (
        SELECT 1 FROM item_equip_locations iel
        JOIN equip_locations el ON el.id = iel.equip_location_id
        WHERE iel.item_id = i.item_id AND el.slug = sqlc.narg('equip_location')::varchar))
  AND (sqlc.narg('class')::varchar IS NULL OR EXISTS (
        SELECT 1 FROM item_classes ic
        JOIN classes cl ON cl.id = ic.class_id
        WHERE ic.item_id = i.item_id AND cl.slug = sqlc.narg('class')::varchar))
  AND (sqlc.narg('place')::varchar IS NULL OR EXISTS (
        SELECT 1 FROM item_sources src
        JOIN places p ON p.id = src.place_id
        WHERE src.item_id = i.item_id AND p.slug = sqlc.narg('place')::varchar))
ORDER BY i.name, i.item_id
LIMIT sqlc.arg('page_size')::integer OFFSET sqlc.arg('page_offset')::integer;

-- name: ListItemsByPlace :many
-- The reverse lookup, and the one Pierre asked for first: "what drops here?" on a place page.
SELECT DISTINCT i.item_id, i.slug, i.name, r.slug AS rarity, r.sort_order AS rarity_sort,
       it.slug AS item_type, i.tooltip_image
FROM items i
JOIN rarities r ON r.id = i.rarity_id
JOIN item_types it ON it.id = i.item_type_id
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
JOIN item_types it ON it.id = i.item_type_id
JOIN item_sources src ON src.item_id = i.item_id
JOIN item_costs ic ON ic.item_source_id = src.id
JOIN currencies cu ON cu.id = ic.currency_id
WHERE cu.slug = $1
ORDER BY ic.amount, i.name;

-- name: ListSets :many
SELECT s.id, s.slug, s.name, cl.slug AS class, aw.slug AS set_armour_weight,
       c.slug AS confidence, s.source_note, s.open_question,
       (SELECT count(*) FROM items i WHERE i.set_id = s.id) AS piece_count
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
