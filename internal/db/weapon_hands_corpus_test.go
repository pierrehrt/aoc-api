//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"testing"
)

// ⭐ AOC-058's rule over every real item (Pierre, 2026-09-30):
//   - no item carries `both` — the one fit that meant "occupies two slots at once";
//   - an item whose rows are main hand + off hand fits `either`, and is of a one-handed type;
//   - a two-handed type's items sit in the main hand alone;
//   - every type with an item in the main hand is classified — a new weapon type fails until decided.
func TestEveryWeaponFollowsPierresHands(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	for what, q := range map[string]string{
		"items still carrying `both`": `SELECT count(*) FROM items i JOIN slot_fits sf ON sf.id = i.slot_fit_id WHERE sf.slug = 'both'`,
		"main+off-hand items that are not `either` of a one-handed type": `
			SELECT count(*) FROM items i
			JOIN slot_fits sf ON sf.id = i.slot_fit_id
			LEFT JOIN item_types t ON t.id = i.item_type_id
			WHERE (SELECT array_agg(el.slug ORDER BY el.slug) FROM item_equip_locations e
			       JOIN equip_locations el ON el.id = e.equip_location_id WHERE e.item_id = i.item_id)
			      = ARRAY['main-hand', 'off-hand']::varchar[]
			  AND (sf.slug <> 'either' OR t.two_handed IS DISTINCT FROM false)`,
		"two-handed items with a slot other than the main hand": `
			SELECT count(*) FROM items i JOIN item_types t ON t.id = i.item_type_id
			JOIN item_equip_locations e ON e.item_id = i.item_id
			JOIN equip_locations el ON el.id = e.equip_location_id
			WHERE t.two_handed AND el.slug <> 'main-hand'`,
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
