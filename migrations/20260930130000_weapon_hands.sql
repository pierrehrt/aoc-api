-- +goose Up
-- AOC-058: which weapons take one hand and which take both. Pierre, 2026-09-30 (DECISIONS.md), in
-- answer to direct questions: a one-handed weapon goes in EITHER hand; a two-handed one takes BOTH;
-- crossbow and ammunition take one hand; thrown takes both.
--
-- (1) The compound slot value `Main Hand, Off Hand` sits on the ONE-handers — 1HB 68, 1HE 105,
-- dagger 127, talisman 89, measured 2026-09-30 — and means the item fits either hand. (Nine other
-- one-handers are `Main Hand` or `Off Hand` alone, and their own tooltips say so — "One-Handed Edged -
-- Off Hand", "Talisman - Off Hand": the game restricts those items, and they stay `single`.) AOC-009 read it
-- as "occupies both at once" from its shape alone. The slot rows are right as they are (the item is
-- found under Main Hand AND under Off Hand, because it can go in either); only the fit was wrong.
-- Scoped to exactly the items the Down moves back — rows main hand + off hand — so the round trip is
-- exact by construction, not only on today's data. A `both` of any other shape stays `both`, and
-- TestEveryWeaponFollowsPierresHands names it.
UPDATE items SET slot_fit_id = (SELECT id FROM slot_fits WHERE slug = 'either')
WHERE slot_fit_id = (SELECT id FROM slot_fits WHERE slug = 'both')
  AND (SELECT array_agg(el.slug ORDER BY el.slug)
       FROM item_equip_locations e JOIN equip_locations el ON el.id = e.equip_location_id
       WHERE e.item_id = items.item_id) = ARRAY['main-hand', 'off-hand']::varchar[];

-- (2) "Takes both hands" is a fact about the item TYPE, and the gear builder (AOC-051) needs it: a
-- two-hander in the main hand empties and locks the off hand. Data, never a list of weapon types in
-- code (reference/content-model.md § 0).
--
-- Set for exactly the 11 types whose items sit in the main hand — the ones Pierre's answers cover.
-- NULL for every other type: the question is what a main-hand item does to the other hand, and shield
-- and ammunition sit in the off hand alone. NULLABLE WITH NO DEFAULT, like is_equipment: a new weapon
-- type must be decided, not filed as one-handed by omission — TestEveryWeaponFollowsPierresHands.
ALTER TABLE item_types ADD COLUMN two_handed boolean NULL;
UPDATE item_types SET two_handed = true  WHERE slug IN ('2hb', '2he', 'staff', 'bow', 'polearm', 'thrown');
UPDATE item_types SET two_handed = false WHERE slug IN ('1hb', '1he', 'dagger', 'talisman', 'crossbow');

-- +goose Down
-- Only the items the Up moved: `either` on an item whose rows are exactly main hand + off hand. A ring
-- (left finger + right finger) was `either` before and stays `either`.
UPDATE items SET slot_fit_id = (SELECT id FROM slot_fits WHERE slug = 'both')
WHERE slot_fit_id = (SELECT id FROM slot_fits WHERE slug = 'either')
  AND (SELECT array_agg(el.slug ORDER BY el.slug)
       FROM item_equip_locations e JOIN equip_locations el ON el.id = e.equip_location_id
       WHERE e.item_id = items.item_id) = ARRAY['main-hand', 'off-hand']::varchar[];
ALTER TABLE item_types DROP COLUMN two_handed;
