package db_test

import (
	"context"
	"strings"
	"testing"
)

// AOC-065: classes are listed in classes.sort_order — archetypes Soldier, Rogue, Priest, Mage (Pierre,
// 2026-10-01, Tier A: "soldier first, rogue second, priest third, and mage last"), the validated
// design's order within each. One column every class list reads; this pins what the migration wrote:
// every class ordered, no two alike, each archetype one unbroken run, in Pierre's order.
func TestClassesAreOrderedSoldierRoguePriestMage(t *testing.T) {
	d, _ := migratedDB(t)
	rows, err := d.QueryContext(context.Background(), `
SELECT a.slug, c.sort_order FROM classes c JOIN archetypes a ON a.id = c.archetype_id
ORDER BY c.sort_order NULLS LAST, c.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var runs []string
	seen := map[int]bool{}
	n := 0
	for rows.Next() {
		var arch string
		var order *int
		if err := rows.Scan(&arch, &order); err != nil {
			t.Fatal(err)
		}
		n++
		if order == nil {
			t.Errorf("a %s class has no sort_order", arch)
			continue
		}
		if seen[*order] {
			t.Errorf("sort_order %d is used twice", *order)
		}
		seen[*order] = true
		if len(runs) == 0 || runs[len(runs)-1] != arch {
			runs = append(runs, arch)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no classes seeded")
	}
	if got := strings.Join(runs, " "); got != "soldier rogue priest mage" {
		t.Errorf("archetypes run %q, want each one unbroken run in Pierre's order: soldier rogue priest mage", got)
	}
}
