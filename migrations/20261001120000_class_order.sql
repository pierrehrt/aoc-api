-- +goose Up
-- AOC-065: the order classes are listed in becomes DATA, like their short names (AOC-046). An order
-- in code is a taxonomy in code (reference/content-model.md § 0): the day a class is added, the code
-- is the file nobody updates.
--
-- Archetypes in the order Soldier, Rogue, Priest, Mage — Pierre, 2026-10-01, Tier A ("soldier first,
-- rogue second, priest third, and mage last", DECISIONS.md). Within each archetype, the validated
-- design's order (product_management/discovery/design/armory-2026-10-01). Every place that lists
-- classes reads this one column: the filter chips, the list's "Conq/DT/Guard", the item page, /v1.
-- A class added later without an order sorts last (NULLS LAST everywhere), never nowhere.
ALTER TABLE classes ADD COLUMN sort_order integer NULL;
UPDATE classes SET sort_order = v.sort_order
FROM (VALUES
    ('conqueror', 1), ('dark-templar', 2), ('guardian', 3),
    ('barbarian', 4), ('assassin', 5), ('ranger', 6),
    ('priest-of-mitra', 7), ('tempest-of-set', 8), ('bear-shaman', 9),
    ('herald-of-xotli', 10), ('demonologist', 11), ('necromancer', 12)
) AS v(slug, sort_order)
WHERE classes.slug = v.slug;

-- +goose Down
ALTER TABLE classes DROP COLUMN sort_order;
