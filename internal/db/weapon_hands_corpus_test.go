//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"testing"
)

// ⭐ AOC-058's rule over every real weapon (Pierre, 2026-09-30), each fit checked against the item's
// own slot rows:
//   - no item carries `both` — the one fit that meant "occupies two slots at once";
//   - an item in the main hand AND the off hand fits `either`, and is of a one-handed type;
//   - an item of a one-handed type is either that, or `single` in the one hand its tooltip names —
//     nine are (measured 2026-09-30), and their tooltips say so: "One-Handed Edged - Off Hand",
//     "Talisman - Off Hand". "Either hand" is what "Main Hand, Off Hand" means, not a rule for all;
//   - a two-handed type's item sits in the main hand alone, `single`;
//   - every type with an item in the main hand is classified — a new weapon type fails until decided.
//
// Fits are LEFT-joined: an item with rows and no fit at all is a violation, not a row to drop. An item
// with NO slot is TestEveryPieceOfEquipmentHasASlot's to judge (AOC-054, which records the four
// weapon-typed ones: two mistyped, AOC-059; two whose tooltip names no slot) — not counted twice here.
func TestEveryWeaponFollowsPierresHands(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	const hands = `(SELECT array_agg(el.slug ORDER BY el.slug) FROM item_equip_locations e
	                JOIN equip_locations el ON el.id = e.equip_location_id WHERE e.item_id = i.item_id)`
	for what, q := range map[string]string{
		"items still carrying `both`": `SELECT count(*) FROM items i JOIN slot_fits sf ON sf.id = i.slot_fit_id WHERE sf.slug = 'both'`,
		"main+off-hand items that are not `either` of a one-handed type": `
			SELECT count(*) FROM items i
			LEFT JOIN slot_fits sf ON sf.id = i.slot_fit_id
			LEFT JOIN item_types t ON t.id = i.item_type_id
			WHERE ` + hands + ` = ARRAY['main-hand', 'off-hand']::varchar[]
			  AND (sf.slug IS DISTINCT FROM 'either' OR t.two_handed IS DISTINCT FROM false)`,
		"one-handed-type items in neither shape": `
			SELECT count(*) FROM items i
			JOIN item_types t ON t.id = i.item_type_id
			LEFT JOIN slot_fits sf ON sf.id = i.slot_fit_id
			WHERE t.two_handed = false AND ` + hands + ` IS NOT NULL
			  AND NOT (` + hands + ` = ARRAY['main-hand', 'off-hand']::varchar[] AND sf.slug IS NOT DISTINCT FROM 'either')
			  AND NOT (` + hands + ` IN (ARRAY['main-hand']::varchar[], ARRAY['off-hand']::varchar[]) AND sf.slug IS NOT DISTINCT FROM 'single')`,
		"two-handed items not `single` in the main hand alone": `
			SELECT count(*) FROM items i
			JOIN item_types t ON t.id = i.item_type_id
			LEFT JOIN slot_fits sf ON sf.id = i.slot_fit_id
			WHERE t.two_handed AND ` + hands + ` IS NOT NULL
			  AND (` + hands + ` IS DISTINCT FROM ARRAY['main-hand']::varchar[] OR sf.slug IS DISTINCT FROM 'single')`,
		"types with a main-hand item and no two_handed decision": `
			SELECT count(DISTINCT t.id) FROM items i JOIN item_types t ON t.id = i.item_type_id
			JOIN item_equip_locations e ON e.item_id = i.item_id
			JOIN equip_locations el ON el.id = e.equip_location_id
			WHERE el.slug = 'main-hand' AND t.two_handed IS NULL`,
	} {
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		if n != 0 {
			t.Errorf("%d %s", n, what)
		}
	}
}
