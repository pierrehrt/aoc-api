//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// equipmentWithoutASlot is every worn item the corpus leaves slotless ON PURPOSE, each read off its
// own tooltip at AOC-054's claim (2026-09-30). Keyed by the source site's item id, which never
// changes.
//
// ⚠️ An entry that stops matching FAILS the test — the item gained a slot, changed type, or left
// the corpus — so this list cannot rot into an exemption nobody rechecks (the knownUnresolved rule
// in internal/items/resolve.go). AOC-059 removes the first four.
var equipmentWithoutASlot = map[int]string{
	2190: "tooltip says `Light Armor - Hands`; the OCR missed the line — AOC-059",
	2834: "tooltip says `Medium Armor - Feet`; the OCR missed the line — AOC-059",
	4594: "typed Crossbow, tooltip says `Consumable` — AOC-059",
	4553: "typed Polearm, tooltip says `Companions` — AOC-059",
	2703: "tooltip says `Talisman` and names no slot — blank is what the source says",
	3872: "no tooltip image — nothing to read a slot from",
	1302: "no tooltip image — nothing to read a slot from",
}

// ⭐ AOC-054's prevention: every item of a worn type has a slot. "Worn" is item_types.is_equipment,
// seeded independently of the slots — derived FROM the slots, a type whose tooltips never name
// one (the necklace, 146 items) would simply not count as equipment, and this would pass.
func TestEveryPieceOfEquipmentHasASlot(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `
SELECT i.item_id, i.name, t.slug
FROM items i JOIN item_types t ON t.id = i.item_type_id
WHERE t.is_equipment
  AND NOT EXISTS (SELECT 1 FROM item_equip_locations e WHERE e.item_id = i.item_id)
ORDER BY i.item_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	slotless := map[int]string{}
	for rows.Next() {
		var id int
		var name, typ string
		if err := rows.Scan(&id, &name, &typ); err != nil {
			t.Fatal(err)
		}
		slotless[id] = fmt.Sprintf("%d %s (%s)", id, name, typ)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var unexplained, stale []string
	for id, desc := range slotless {
		if _, ok := equipmentWithoutASlot[id]; !ok {
			unexplained = append(unexplained, desc)
		}
	}
	for id, why := range equipmentWithoutASlot {
		if _, ok := slotless[id]; !ok {
			stale = append(stale, fmt.Sprintf("%d (%s)", id, why))
		}
	}
	sort.Strings(unexplained)
	sort.Strings(stale)
	if len(unexplained) > 0 {
		t.Errorf("%d worn item(s) have no slot and no recorded reason — read each tooltip, then "+
			"fix the data or record why:\n  %s", len(unexplained), strings.Join(unexplained, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d recorded exception(s) no longer match — the item has a slot now, changed type "+
			"or left the corpus; remove them:\n  %s", len(stale), strings.Join(stale, "\n  "))
	}
}
