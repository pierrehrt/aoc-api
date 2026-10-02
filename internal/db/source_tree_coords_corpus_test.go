//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-068, Pierre 2026-10-02: the selected-source box says where on its map a branch is only when
// the branch is one point — every row of it has coordinates, and they are the same. Checked for
// every place branch of every tab against SQL of its own: the place's rows (and the rows of every
// place inside it) in the branch's section, region and map.
func TestAPlaceBranchHasCoordinatesExactlyWhenItIsOnePoint(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const oracle = `
		WITH RECURSIVE inside AS (
		    SELECT id FROM places WHERE slug = $5
		  UNION
		    SELECT c.id FROM places c JOIN inside ON c.parent_place_id = inside.id)
		SELECT count(*) FILTER (WHERE coalesce(s.coords, '') = ''),
		       count(DISTINCT s.coords) FILTER (WHERE coalesce(s.coords, '') <> ''),
		       coalesce(min(s.coords), '')
		FROM item_sources s
		JOIN sections sc ON sc.id = s.section_id
		JOIN source_tabs t ON t.id = sc.tab_id
		LEFT JOIN places p ON p.id = s.place_id
		LEFT JOIN regions r ON r.id = coalesce(p.region_id, s.region_id)
		LEFT JOIN maps m ON m.id = coalesce(p.map_id, s.map_id)
		WHERE t.slug = $1
		  AND ($2 = '' OR sc.slug = $2)
		  AND ($3 = '' OR ($3 = '-' AND r.id IS NULL) OR r.slug = $3)
		  AND ($4 = '' OR ($4 = '-' AND m.id IS NULL) OR m.slug = $4)
		  AND s.place_id IN (SELECT id FROM inside)`
	var checked, shown int
	for _, tab := range tabs {
		tree, err := s.Tree(ctx, items.Filters{Tab: tab.Slug})
		if err != nil {
			t.Fatal(err)
		}
		var walk func(ns []*items.TreeNode)
		walk = func(ns []*items.TreeNode) {
			for _, n := range ns {
				walk(n.Children)
				if n.Kind != "place" {
					continue
				}
				src, err := items.ParseSource(n.Source)
				if err != nil {
					t.Fatal(err)
				}
				var missing, distinct int
				var one string
				if err := pool.QueryRow(ctx, oracle, tab.Slug, src.Section, src.Region, src.Map, src.Place()).Scan(&missing, &distinct, &one); err != nil {
					t.Fatal(err)
				}
				want := ""
				if missing == 0 && distinct == 1 {
					want = one
				}
				checked++
				if want != "" {
					shown++
				}
				if n.Coords != want {
					t.Errorf("%s %s: coords %q, want %q (%d rows without, %d distinct)", tab.Slug, n.Source, n.Coords, want, missing, distinct)
				}
			}
		}
		walk(tree.Nodes)
	}
	if checked == 0 {
		t.Fatal("no place branch checked")
	}
	t.Logf("%d place branches checked, %d of them one point", checked, shown)
}
