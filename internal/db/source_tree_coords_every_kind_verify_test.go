//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-068 verify round 1. The map point (Pierre, 2026-10-02): a branch says where on its map it is
// only when every row of it has the same coordinates. The build's corpus test checks the PLACE
// branches. This checks every branch of every tab — sections, regions, maps, places, bosses,
// vendors, quest givers, containers — against SQL of its own: the rows the branch's path names.
// The oracle's distinct items must equal the branch's count first, so a coordinates verdict is
// about the same rows the tree counted.
func TestEveryBranchOfEveryTabHasCoordinatesExactlyWhenItIsOnePoint(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// $5 is the place matched (the path's last), $6 whether the branch IS that place (then every
	// place inside it counts) or a location under it (then the row's place is that one).
	const oracle = `
		WITH RECURSIVE inside AS (
		    SELECT id FROM places WHERE slug = $5
		  UNION
		    SELECT c.id FROM places c JOIN inside ON c.parent_place_id = inside.id)
		SELECT count(*) FILTER (WHERE coalesce(s.coords, '') = ''),
		       count(DISTINCT s.coords) FILTER (WHERE coalesce(s.coords, '') <> ''),
		       coalesce(min(s.coords), ''),
		       count(DISTINCT s.item_id)
		FROM item_sources s
		JOIN sections sc ON sc.id = s.section_id
		JOIN source_tabs t ON t.id = sc.tab_id
		LEFT JOIN places p ON p.id = s.place_id
		LEFT JOIN regions r ON r.id = coalesce(p.region_id, s.region_id)
		LEFT JOIN maps m ON m.id = coalesce(p.map_id, s.map_id)
		LEFT JOIN bosses b ON b.id = s.boss_id
		LEFT JOIN vendors v ON v.id = s.vendor_id
		LEFT JOIN quests q ON q.id = s.quest_id
		LEFT JOIN containers c ON c.id = s.container_id
		WHERE t.slug = $1
		  AND ($2 = '' OR sc.slug = $2)
		  AND ($3 = '' OR ($3 = '-' AND r.id IS NULL) OR r.slug = $3)
		  AND ($4 = '' OR ($4 = '-' AND m.id IS NULL) OR m.slug = $4)
		  AND ($5 = '' OR ($5 = '-' AND s.place_id IS NULL)
		       OR ($6 AND s.place_id IN (SELECT id FROM inside)) OR (NOT $6 AND p.slug = $5))
		  AND ($7 = '' OR b.slug = $7)
		  AND ($8 = '' OR (v.slug = $8 AND s.boss_id IS NULL))
		  AND ($9 = '' OR (q.slug = $9 AND s.boss_id IS NULL AND s.vendor_id IS NULL))
		  AND ($10 = '' OR (c.slug = $10 AND s.boss_id IS NULL AND s.vendor_id IS NULL AND s.quest_id IS NULL))`
	kinds := map[string][3]int{} // kind -> one point, none shown, checked
	for _, tab := range tabs {
		tree, err := s.Tree(ctx, items.Filters{Tab: tab.Slug})
		if err != nil {
			t.Fatal(err)
		}
		var walk func(ns []*items.TreeNode)
		walk = func(ns []*items.TreeNode) {
			for _, n := range ns {
				walk(n.Children)
				src, err := items.ParseSource(n.Source)
				if err != nil {
					t.Fatal(err)
				}
				var missing, distinct int
				var one string
				var itemsIn int64
				if err := pool.QueryRow(ctx, oracle, tab.Slug, src.Section, src.Region, src.Map, src.Place(), n.Kind == "place",
					src.Boss, src.Vendor, src.Quest, src.Container).Scan(&missing, &distinct, &one, &itemsIn); err != nil {
					t.Fatal(err)
				}
				if itemsIn != n.Count {
					t.Errorf("%s %s: the oracle's rows hold %d items, the branch counts %d — not the same rows", tab.Slug, n.Source, itemsIn, n.Count)
					continue
				}
				want := ""
				if missing == 0 && distinct == 1 {
					want = one
				}
				k := kinds[n.Kind]
				k[2]++
				if want != "" {
					k[0]++
				} else {
					k[1]++
				}
				kinds[n.Kind] = k
				if n.Coords != want {
					t.Errorf("%s %s: coords %q, want %q (%d rows without, %d distinct)", tab.Slug, n.Source, n.Coords, want, missing, distinct)
				}
			}
		}
		walk(tree.Nodes)
	}
	for _, kind := range []string{"section", "region", "map", "place", "boss", "vendor"} {
		if kinds[kind][2] == 0 {
			t.Errorf("no %s branch checked", kind)
		}
	}
	t.Logf("checked (one point, none shown, total) by kind: %v", kinds)
}
