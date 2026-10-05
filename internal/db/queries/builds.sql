-- The gear builder (AOC-051). Every query takes the whole build at once — never one per item, the
-- ListItemPlaces pattern.

-- name: ListBuildSlots :many
-- The builder's rows: every equipment slot, in the seed's order (the id). AoC>TV's builder has the
-- same 14 (Pierre, 2026-09-29; Necklace since AOC-054).
SELECT id, slug, name FROM equip_locations ORDER BY id;

-- name: ListBuildHands :many
-- The hands: the slots a ONE-handed weapon fits (item_types.two_handed = false). A two-hander takes
-- both, so it locks every one of these but its own. Derived from the items rather than named, so no
-- slot slug is written in code (reference/content-model.md § 0).
SELECT DISTINCT iel.equip_location_id
FROM item_equip_locations iel
JOIN items i ON i.item_id = iel.item_id
JOIN item_types it ON it.id = i.item_type_id
WHERE it.two_handed = false
ORDER BY iel.equip_location_id;

-- name: ListBuildClasses :many
-- The class picker, in classes.sort_order (AOC-065), with each class's armour ceiling when one is
-- recorded (none is yet: it is Pierre's to supply).
SELECT c.slug, c.name, c.short_name, a.name AS archetype_name,
       aw.sort_order AS max_armour_weight_order
FROM classes c
JOIN archetypes a ON a.id = c.archetype_id
LEFT JOIN armour_weights aw ON aw.id = c.max_armour_weight
ORDER BY c.sort_order NULLS LAST, c.name;

-- name: ListBuildArmourWeights :many
-- Only read when the picked class has a ceiling: a list row names its weight by slug.
SELECT slug, sort_order FROM armour_weights ORDER BY sort_order;

-- name: ListBuildItems :many
-- A build's items, with what the rules read: the type's hands (two_handed, and what a two-hander
-- still allows in the other hand), the armour weight's place in the order, and the base lines.
SELECT i.item_id, i.slug, i.name, r.colour_token AS rarity_colour_token,
       i.item_type_id, COALESCE(it.two_handed, false)::boolean AS two_handed,
       it.other_hand_type_id,
       aw.sort_order AS armour_weight_order,
       i.armor, i.critigation
FROM items i
JOIN rarities r ON r.id = i.rarity_id
LEFT JOIN item_types it ON it.id = i.item_type_id
LEFT JOIN armour_weights aw ON aw.id = i.armour_weight_id
WHERE i.item_id = ANY(sqlc.arg('item_ids')::integer[]);

-- name: ListBuildItemSlots :many
SELECT iel.item_id, el.slug
FROM item_equip_locations iel
JOIN equip_locations el ON el.id = iel.equip_location_id
WHERE iel.item_id = ANY(sqlc.arg('item_ids')::integer[])
ORDER BY iel.item_id, el.id;

-- name: ListBuildItemClasses :many
SELECT ic.item_id, cl.slug
FROM item_classes ic
JOIN classes cl ON cl.id = ic.class_id
WHERE ic.item_id = ANY(sqlc.arg('item_ids')::integer[])
ORDER BY ic.item_id, cl.sort_order NULLS LAST, cl.id;

-- name: ListBuildItemStats :many
-- In the tooltip's order (the id, as ListItemStats), each value as signed HUNDREDTHS: the column is
-- numeric(8,2), so ×100 is a whole number, and the sum is exact integer arithmetic — never a float.
-- Stats only: spell effects are another table on purpose, and a build never sums them.
SELECT st.item_id, st.stat, st.unit, st.damage_type, st.pvp,
       (st.sign * st.value * 100)::bigint AS centi
FROM item_stats st
WHERE st.item_id = ANY(sqlc.arg('item_ids')::integer[])
ORDER BY st.item_id, st.id;
