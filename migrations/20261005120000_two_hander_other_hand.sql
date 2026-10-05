-- +goose Up
-- AOC-051: what a two-handed weapon still lets into the other hand. Pierre, 2026-10-05 (DECISIONS.md),
-- in answer to a direct question: "with bow yes need amo … and throw weapon no amo". A two-hander
-- takes both hands (AOC-058, item_types.two_handed), so the gear builder empties and locks the other
-- hand when one is equipped — except for the one item type named here, which a two-hander of this
-- type still allows there. Data, never a pair of slugs in code (reference/content-model.md § 0).
--
-- Set for the bow alone. Every other two-hander (2HB, 2HE, staff, polearm, thrown) allows nothing:
-- NULL. The constraint keeps the column on two-handers only, where the question means something —
-- `IS TRUE`, because two_handed is NULL on a type that is no weapon, and a CHECK that comes out NULL
-- passes (measured: it let a shield take the column, TestOnlyTheBowLetsSomethingIntoTheOtherHand).
-- Pierre also thinks bows and crossbows take different ammunition; the data has one `ammunition`
-- type, so nothing here can say which, and the builder does not try (inbox.md).
ALTER TABLE item_types ADD COLUMN other_hand_type_id integer NULL REFERENCES item_types(id);
ALTER TABLE item_types ADD CONSTRAINT item_types_other_hand_only_two_handed
    CHECK (other_hand_type_id IS NULL OR two_handed IS TRUE);
UPDATE item_types SET other_hand_type_id = (SELECT id FROM item_types WHERE slug = 'ammunition')
WHERE slug = 'bow';

-- +goose Down
ALTER TABLE item_types DROP COLUMN other_hand_type_id;
