package db_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-050, verify round 2. Two more shapes the corpus lacks, planted on a throwaway database and
// asked of the real service (Tree, ParseSource and List, the real SQL). Every name is fake
// (STEP ZERO); the sections are the migration's own seeded rows, chosen by their tab's groups.
//
//  1. A row with TWO location kinds. The schema forbids only a boss with a vendor
//     (item_sources_boss_and_vendor_not_both); the corpus has 11 rows with a quest giver and a
//     container. The tree draws such a row under its first kind (boss, vendor, quest giver,
//     container), so a branch of a later kind must not list it. 9801 has a quest giver and a
//     container, 9802 the container alone, both with no region and no place; 9803 has a boss and a
//     container at a place, 9804 the container alone at the same place.
//  2. A place more than eight levels down. The tree draws a place under every place above it, at any
//     depth (F2), and the parser takes at most eight places: the branch must still be one that
//     /v1/items accepts and that lists its count. 9805 is nine places down.
func TestEveryBranchListsItsCountWithTwoLocationKindsOrNinePlaces(t *testing.T) {
	_, url := migratedDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}
	exec(`
WITH r AS (SELECT id FROM regions ORDER BY id LIMIT 1),
     c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO places (region_id, slug, name, confidence_id, source_note)
SELECT r.id, v, 'Test Place ' || v, c.id, 'AOC-050 verify round 2 fixture'
FROM r, c, unnest(ARRAY['test-keep-nu', 'test-n1-nu', 'test-n2-nu', 'test-n3-nu', 'test-n4-nu',
                        'test-n5-nu', 'test-n6-nu', 'test-n7-nu', 'test-n8-nu', 'test-n9-nu']) v`)
	exec(`
UPDATE places c SET parent_place_id = p.id
FROM (VALUES ('test-n2-nu', 'test-n1-nu'), ('test-n3-nu', 'test-n2-nu'), ('test-n4-nu', 'test-n3-nu'),
             ('test-n5-nu', 'test-n4-nu'), ('test-n6-nu', 'test-n5-nu'), ('test-n7-nu', 'test-n6-nu'),
             ('test-n8-nu', 'test-n7-nu'), ('test-n9-nu', 'test-n8-nu')) AS v(child, parent)
JOIN places p ON p.slug = v.parent
WHERE c.slug = v.child`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO bosses (slug, name, confidence_id, source_note) SELECT 'test-boss-nu', 'Test Boss Nu', id, 'AOC-050 verify round 2 fixture' FROM c`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO quests (slug, armory_label, confidence_id, source_note) SELECT 'test-quest-nu', 'Test Quest Nu', id, 'AOC-050 verify round 2 fixture' FROM c`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO containers (slug, name, confidence_id, source_note) SELECT 'test-chest-nu', 'Test Chest Nu', id, 'AOC-050 verify round 2 fixture' FROM c`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
     ra AS (SELECT id FROM rarities WHERE slug = 'rare')
INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
SELECT v, 'test-relic-nu-' || v, 'Test Relic Nu ' || v, ra.id, c.id, 'AOC-050 verify round 2 fixture'
FROM c, ra, generate_series(9801, 9805) v`)
	exec(`
WITH sr AS (SELECT sc.id FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
            WHERE t.groups = '{section,region}' ORDER BY t.sort_order, sc.sort_order LIMIT 1),
     ss AS (SELECT sc.id FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
            WHERE t.groups = '{section}' ORDER BY t.sort_order, sc.sort_order LIMIT 1),
     c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
     drop_ AS (SELECT id FROM acquisition_types WHERE slug = 'drop')
INSERT INTO item_sources (item_id, acquisition_type_id, section_id, place_id, boss_id, quest_id, container_id, confidence_id, source_note)
SELECT v.item_id, (SELECT id FROM drop_),
       CASE WHEN v.tab_region THEN (SELECT id FROM sr) ELSE (SELECT id FROM ss) END,
       (SELECT id FROM places WHERE slug = v.place),
       CASE WHEN v.boss THEN (SELECT id FROM bosses WHERE slug = 'test-boss-nu') END,
       CASE WHEN v.quest THEN (SELECT id FROM quests WHERE slug = 'test-quest-nu') END,
       CASE WHEN v.chest THEN (SELECT id FROM containers WHERE slug = 'test-chest-nu') END,
       (SELECT id FROM c), 'AOC-050 verify round 2 fixture'
FROM (VALUES
  -- 1. a quest giver and a container on one row, and the container alone (no region, no place)
  (9801, true,  NULL,           false, true,  true),
  (9802, true,  NULL,           false, false, true),
  -- 1. a boss and a container on one row at a place, and the container alone at the same place
  (9803, false, 'test-keep-nu', true,  false, true),
  (9804, false, 'test-keep-nu', false, false, true),
  -- 2. nine places down
  (9805, false, 'test-n9-nu',   false, false, false)
) AS v(item_id, tab_region, place, boss, quest, chest)`)

	s := items.NewService(sqlcgen.New(pool))
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// check walks every branch of every tab and asks List for its source; keep picks the branches a
	// subtest is about, so that each fails only on its own shape.
	check := func(t *testing.T, keep func(src string) bool) {
		t.Helper()
		var seen int
		for _, tab := range tabs {
			tree, err := s.Tree(ctx, items.Filters{Tab: tab.Slug})
			if err != nil {
				t.Fatal(err)
			}
			var walk func(ns []*items.TreeNode)
			walk = func(ns []*items.TreeNode) {
				for _, n := range ns {
					walk(n.Children)
					if !keep(n.Source) {
						continue
					}
					seen++
					src, err := items.ParseSource(n.Source)
					if err != nil {
						t.Errorf("%s %s: the tree draws this branch and its own source is refused: %v", tab.Slug, n.Source, err)
						continue
					}
					res, err := s.List(ctx, items.Filters{Tab: tab.Slug, Source: src, Limit: items.MaxLimit})
					if err != nil {
						t.Fatal(err)
					}
					if res.Total != n.Count {
						var ids []int32
						for _, it := range res.Items {
							ids = append(ids, it.ID)
						}
						t.Errorf("%s %s: the branch says %d, picking it lists %d %v", tab.Slug, n.Source, n.Count, res.Total, ids)
					}
				}
			}
			walk(tree.Nodes)
		}
		if seen == 0 {
			t.Fatal("the fixture drew no branch for this shape")
		}
	}
	t.Run("a row with two location kinds is listed only under the branch that draws it", func(t *testing.T) {
		check(t, func(src string) bool {
			return strings.Contains(src, "test-chest-nu") || strings.Contains(src, "test-quest-nu") || strings.Contains(src, "test-keep-nu")
		})
	})
	t.Run("a place nine levels down has a branch that /v1/items accepts and that lists its count", func(t *testing.T) {
		check(t, func(src string) bool { return strings.Contains(src, "test-n1-nu") })
	})
}

// AOC-050, verify round 2. A cycle in parent_place_id is not a hierarchy, and the seed's own test
// (TestThePlaceHierarchyIsWellFormed) keeps one out of the real data; but nothing in the schema
// forbids it, and the tree walks every place up to its top (F2). The walk must stop: the service's
// answer is bounded, whatever the counts of a cycle mean. Without the walk's stop on a place already
// on the chain, Tree never returns.
func TestTheTreeWalkStopsOnACycleOfPlaces(t *testing.T) {
	_, url := migratedDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, sql := range []string{`
WITH r AS (SELECT id FROM regions ORDER BY id LIMIT 1),
     c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO places (region_id, slug, name, confidence_id, source_note)
SELECT r.id, v, 'Test Place ' || v, c.id, 'AOC-050 verify round 2 fixture'
FROM r, c, unnest(ARRAY['test-loop-a-nu', 'test-loop-b-nu']) v`, `
UPDATE places c SET parent_place_id = p.id
FROM (VALUES ('test-loop-a-nu', 'test-loop-b-nu'), ('test-loop-b-nu', 'test-loop-a-nu')) AS v(child, parent)
JOIN places p ON p.slug = v.parent
WHERE c.slug = v.child`, `
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
     ra AS (SELECT id FROM rarities WHERE slug = 'rare')
INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
SELECT 9806, 'test-relic-nu-9806', 'Test Relic Nu 9806', ra.id, c.id, 'AOC-050 verify round 2 fixture' FROM c, ra`, `
WITH ss AS (SELECT sc.id FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
            WHERE t.groups = '{section}' ORDER BY t.sort_order, sc.sort_order LIMIT 1),
     c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO item_sources (item_id, acquisition_type_id, section_id, place_id, confidence_id, source_note)
SELECT 9806, (SELECT id FROM acquisition_types WHERE slug = 'drop'), ss.id,
       (SELECT id FROM places WHERE slug = 'test-loop-a-nu'), c.id, 'AOC-050 verify round 2 fixture'
FROM ss, c`} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}
	s := items.NewService(sqlcgen.New(pool))
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for _, tab := range tabs {
			tree, err := s.Tree(ctx, items.Filters{Tab: tab.Slug})
			if err != nil {
				done <- err
				return
			}
			var walk func(ns []*items.TreeNode) error
			walk = func(ns []*items.TreeNode) error {
				for _, n := range ns {
					src, err := items.ParseSource(n.Source)
					if err != nil {
						return err
					}
					if _, err := s.List(ctx, items.Filters{Tab: tab.Slug, Source: src, Limit: 1}); err != nil {
						return err
					}
					if err := walk(n.Children); err != nil {
						return err
					}
				}
				return nil
			}
			if err := walk(tree.Nodes); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Tree or List did not return within 20 s on a cycle of two places")
	}
}
