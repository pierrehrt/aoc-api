-- +goose Up
-- AOC-054: the necklace slot, and the rule that puts a necklace in it.
--
-- AoC>TV's armour builder has 14 slots: AOC-009's 13 plus Necklace (its "Back" is our cloak).
-- Pierre, 2026-09-29: "always take aoc>tv as truth" (DECISIONS.md). The 13 were derived from the
-- slot lines the tooltips carry, and a necklace's tooltip has none — its line reads `Necklace`
-- where armour's reads `Light Armor - Hands` — so the slot was never in the data to be found.
INSERT INTO equip_locations (slug, name) VALUES ('necklace', 'Necklace')
ON CONFLICT (slug) DO NOTHING;

-- The slot an item of this type goes in when its own tooltip names none. DATA, not a Go literal
-- (reference/content-model.md § 0): the importer reads it and this migration's backfill reads it,
-- so the two apply one rule. Set ONLY where a source says so — Necklace, from AoC>TV's builder.
-- ⛔ Not for the other types: a Crossbow's slot is not a default, and two items typed Crossbow and
-- Polearm are in fact a consumable and a companion (AOC-059) — a default would put them in a hand.
ALTER TABLE item_types ADD COLUMN default_equip_location_id integer NULL REFERENCES equip_locations(id);
UPDATE item_types SET default_equip_location_id = (SELECT id FROM equip_locations WHERE slug = 'necklace')
WHERE slug = 'necklace';

-- Whether an item of this type is worn at all. It exists so a test can say "every piece of
-- equipment has a slot" without deriving equipment FROM the slots — derived that way, a type
-- whose tooltips never name a slot (this bug) is simply not equipment, and the check passes.
-- Seeded: the 23 types whose items carry a slot in the armory (measured on dev, 2026-09-30) plus
-- necklace. The other six — backpack, consumable, generic, mount, pet, potion — carry none.
-- ⚠️ NULLABLE WITH NO DEFAULT, on purpose: a default of false would file every type added later
-- as "not worn" without anyone deciding it — the necklace's blind spot again. Every row is set
-- here, true or false, and TestTheNecklaceSlotAndItsRuleAreSeeded fails on a NULL.
ALTER TABLE item_types ADD COLUMN is_equipment boolean NULL;
UPDATE item_types SET is_equipment = slug IN (
    '1hb', '1he', '2hb', '2he', 'ammunition', 'back', 'belt', 'bow', 'chest', 'crossbow',
    'dagger', 'feet', 'hands', 'head', 'legs', 'necklace', 'polearm', 'ring', 'shield',
    'shoulder', 'staff', 'talisman', 'thrown', 'wrist');

-- The backfill: the items already in the database, so production gets the slot without a
-- re-import (~15 minutes, docs/architecture.md § The import service). A later import applies the
-- same rule itself (internal/items slotFit) and arrives at the same rows.
WITH filled AS (
    INSERT INTO item_equip_locations (item_id, equip_location_id)
    SELECT i.item_id, t.default_equip_location_id
    FROM items i
    JOIN item_types t ON t.id = i.item_type_id
    WHERE t.default_equip_location_id IS NOT NULL
      AND NOT EXISTS (SELECT 1 FROM item_equip_locations e WHERE e.item_id = i.item_id)
    ON CONFLICT DO NOTHING
    RETURNING item_id
)
UPDATE items SET slot_fit_id = (SELECT id FROM slot_fits WHERE slug = 'single')
WHERE item_id IN (SELECT item_id FROM filled);

-- +goose Down
-- An item of a DEFAULTED type left with no slot row after the delete had its fit set by the Up (or
-- by an importer applying the same rule), so it goes back to NULL. Scoped to those types, so a fit
-- the Up never touched survives the rollback — and done before the column that names them goes.
DELETE FROM item_equip_locations
WHERE equip_location_id = (SELECT id FROM equip_locations WHERE slug = 'necklace');
UPDATE items SET slot_fit_id = NULL
WHERE slot_fit_id IS NOT NULL
  AND item_type_id IN (SELECT id FROM item_types WHERE default_equip_location_id IS NOT NULL)
  AND NOT EXISTS (SELECT 1 FROM item_equip_locations e WHERE e.item_id = items.item_id);
ALTER TABLE item_types DROP COLUMN is_equipment;
ALTER TABLE item_types DROP COLUMN default_equip_location_id;
DELETE FROM equip_locations WHERE slug = 'necklace';
