-- Read queries for the taxonomy tables — the Armory page's filter lists (AOC-009).
--
-- ⭐ WHY THESE ARE QUERIES AND NOT A GO SLICE. Every "kind of thing" in this product is a row
-- (DECISIONS.md, 2026-09-12). A filter list built from a Go constant drifts from the database the
-- first time a currency is added, and the page then offers an option nothing matches — or worse,
-- omits one that does. These read the same rows the items reference.
--
-- Ordered taxonomies come back in sort_order; the rest alphabetically, because that is the order a
-- filter list is read in.

-- name: ListArchetypes :many
SELECT id, slug, name FROM archetypes ORDER BY name;

-- name: ListClasses :many
-- In classes.sort_order (AOC-065): Soldier, Rogue, Priest, Mage (Pierre), the design's order within.
SELECT c.id, c.slug, c.name, c.short_name, c.archetype_id, a.name AS archetype_name, c.max_armour_weight
FROM classes c JOIN archetypes a ON a.id = c.archetype_id
ORDER BY c.sort_order NULLS LAST, c.name;

-- name: ListRarities :many
SELECT id, slug, name, sort_order, colour_token FROM rarities ORDER BY sort_order;

-- name: ListArmourWeights :many
SELECT id, slug, name, sort_order FROM armour_weights ORDER BY sort_order;

-- name: ListTiers :many
SELECT id, slug, name, sort_order FROM tiers ORDER BY sort_order;

-- name: ListItemTypes :many
SELECT id, slug, name FROM item_types ORDER BY name;

-- name: ListItemTypeDefaultSlots :many
-- AOC-054: item type -> the slot its items go in when their tooltip names none. By NAME on both
-- sides, because the importer resolves the snapshot's names; read by internal/items LoadLookups.
SELECT t.name AS item_type, el.name AS slot
FROM item_types t
JOIN equip_locations el ON el.id = t.default_equip_location_id
ORDER BY t.name;

-- name: ListEquipLocations :many
SELECT id, slug, name FROM equip_locations ORDER BY name;

-- name: ListCurrencies :many
SELECT id, slug, name FROM currencies ORDER BY name;

-- name: ListAcquisitionTypes :many
SELECT id, slug, name FROM acquisition_types ORDER BY name;

-- name: ListBindings :many
SELECT id, slug, name FROM bindings ORDER BY name;

-- name: ListFactions :many
SELECT id, slug, name FROM factions ORDER BY name;
