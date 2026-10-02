package db_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-050, verify round 3. Shapes the corpus lacks, planted on a throwaway database and asked of the
// real service (Tree, ParseSource and List, the real SQL). Every name is fake (STEP ZERO); the
// sections are the migration's own seeded rows, chosen by their tab's groups.

// plantedService is a migrated throwaway database with exec, and the real service over it.
func plantedService(t *testing.T) (*items.Service, func(string)) {
	t.Helper()
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
	return items.NewService(sqlcgen.New(pool)), exec
}

// everyBranchListsItsCount walks every branch of every tab whose source keep picks, and asks List
// for it, and for each of its halves with `get`. It fails when the tree says one number and picking
// the branch lists another.
func everyBranchListsItsCount(t *testing.T, s *items.Service, keep func(src string) bool) {
	t.Helper()
	ctx := context.Background()
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
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
					t.Errorf("%s %s: the branch says %d, picking it lists %d", tab.Slug, n.Source, n.Count, res.Total)
				}
				for _, g := range n.Groups {
					half := src
					half.Group = g.Slug
					res, err := s.List(ctx, items.Filters{Tab: tab.Slug, Source: half, Limit: items.MaxLimit})
					if err != nil {
						t.Fatal(err)
					}
					if res.Total != g.Count {
						t.Errorf("%s %s get=%s: the half says %d, picking it lists %d", tab.Slug, n.Source, g.Slug, g.Count, res.Total)
					}
				}
			}
		}
		walk(tree.Nodes)
	}
	if seen == 0 {
		t.Fatal("the fixture drew no branch for this shape")
	}
}

// Round 2's F8 fix: a row's location is its first kind (boss, vendor, quest giver, container), in the
// tree and in the predicate. Round 2's fixture planted two pairs (quest giver + container, boss +
// container). The schema forbids only a boss with a vendor, so every other pair and triple can come
// from a re-import: boss + quest giver, vendor + quest giver, vendor + container, and the triples.
// Each is planted with each kind alone beside it, at a place and with none, in a section of every
// shape of tab, so every exclusion in the predicate is needed by some branch.
func TestEveryBranchListsItsCountForEveryMixOfLocationKinds(t *testing.T) {
	s, exec := plantedService(t)
	exec(`
WITH r AS (SELECT id FROM regions ORDER BY id LIMIT 1), m AS (SELECT id FROM maps ORDER BY id LIMIT 1),
     c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO places (region_id, map_id, slug, name, confidence_id, source_note)
SELECT r.id, m.id, 'test-keep-xi', 'Test Keep Xi', c.id, 'AOC-050 verify round 3 fixture' FROM r, m, c`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO bosses (slug, name, confidence_id, source_note) SELECT 'test-boss-xi', 'Test Boss Xi', id, 'AOC-050 verify round 3 fixture' FROM c`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO vendors (slug, name, confidence_id, source_note) SELECT 'test-vendor-xi', 'Test Vendor Xi', id, 'AOC-050 verify round 3 fixture' FROM c`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO quests (slug, armory_label, confidence_id, source_note) SELECT 'test-quest-xi', 'Test Quest Xi', id, 'AOC-050 verify round 3 fixture' FROM c`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO containers (slug, name, confidence_id, source_note) SELECT 'test-chest-xi', 'Test Chest Xi', id, 'AOC-050 verify round 3 fixture' FROM c`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), ra AS (SELECT id FROM rarities WHERE slug = 'rare')
INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
SELECT v, 'test-relic-xi-' || v, 'Test Relic Xi ' || v, ra.id, c.id, 'AOC-050 verify round 3 fixture'
FROM c, ra, generate_series(9901, 9988) v`)
	// 4 tab shapes x 11 mixes x (at the keep, no place) = 88 rows, one item each. The halves
	// alternate, and a third of the rows with no place carry a region, so r:- and r:<slug> both occur.
	exec(`
WITH secs AS (
  SELECT DISTINCT ON (t.groups) sc.id, t.groups
  FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id ORDER BY t.groups, t.sort_order, sc.sort_order
), mixes(name, b, v, q, ct) AS (VALUES
  ('b', true, false, false, false), ('v', false, true, false, false), ('q', false, false, true, false), ('c', false, false, false, true),
  ('bq', true, false, true, false), ('bc', true, false, false, true), ('bqc', true, false, true, true),
  ('vq', false, true, true, false), ('vc', false, true, false, true), ('vqc', false, true, true, true), ('qc', false, false, true, true)
), at(pl) AS (VALUES ('test-keep-xi'), (NULL)),
grid AS (SELECT secs.id AS sec, mixes.*, at.pl,
                row_number() OVER (ORDER BY secs.groups, mixes.name, at.pl NULLS LAST) AS n
         FROM secs, mixes, at)
INSERT INTO item_sources (item_id, acquisition_type_id, section_id, place_id, region_id, boss_id, vendor_id, quest_id, container_id, confidence_id, source_note)
SELECT 9900 + g.n,
       (SELECT id FROM acquisition_types WHERE slug = CASE WHEN g.n % 2 = 0 THEN 'drop' ELSE 'vendor' END),
       g.sec,
       (SELECT id FROM places WHERE slug = g.pl),
       CASE WHEN g.pl IS NULL AND g.n % 3 = 0 THEN (SELECT id FROM regions ORDER BY id LIMIT 1) END,
       CASE WHEN g.b THEN (SELECT id FROM bosses WHERE slug = 'test-boss-xi') END,
       CASE WHEN g.v THEN (SELECT id FROM vendors WHERE slug = 'test-vendor-xi') END,
       CASE WHEN g.q THEN (SELECT id FROM quests WHERE slug = 'test-quest-xi') END,
       CASE WHEN g.ct THEN (SELECT id FROM containers WHERE slug = 'test-chest-xi') END,
       (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), 'AOC-050 verify round 3 fixture'
FROM grid g`)
	everyBranchListsItsCount(t, s, func(src string) bool { return strings.Contains(src, "-xi") })
}

// Round 2's F9 fix: one bound, maxPlaces (32), for the parser and the tree's walk; the walk keeps at
// most 32 places above a row, "and counts the same, since a partial chain is valid". A chain of
// exactly 32 does. One deeper does not: a row 33 places down is drawn under a chain cut at its
// second place, so that place appears again at the top of the tab, counting only the rows whose
// chain was cut there, while its source expands to every place inside it (AOC-038), at any depth.
// And the top place's own branch stops counting at depth 32 while its source lists the row below.
// Expected: every branch the tree draws lists its count, at whatever depth (round 2's F9 expected).
func TestEveryBranchListsItsCountAroundThePlaceBound(t *testing.T) {
	for _, depth := range []int{32, 33} {
		t.Run(fmt.Sprintf("a chain of %d places", depth), func(t *testing.T) {
			s, exec := plantedService(t)
			prefix := fmt.Sprintf("test-chain%d-xi-", depth)
			exec(fmt.Sprintf(`
WITH r AS (SELECT id FROM regions ORDER BY id LIMIT 1), c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO places (region_id, slug, name, confidence_id, source_note)
SELECT r.id, '%[1]s' || g, 'Test Chain %[2]d Xi ' || g, c.id, 'AOC-050 verify round 3 fixture'
FROM r, c, generate_series(1, %[2]d) g`, prefix, depth))
			exec(fmt.Sprintf(`
UPDATE places c SET parent_place_id = p.id
FROM generate_series(2, %[2]d) g
JOIN places p ON p.slug = '%[1]s' || (g - 1)
WHERE c.slug = '%[1]s' || g`, prefix, depth))
			exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO bosses (slug, name, confidence_id, source_note) SELECT 'test-boss-xi', 'Test Boss Xi', id, 'AOC-050 verify round 3 fixture' FROM c`)
			exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), ra AS (SELECT id FROM rarities WHERE slug = 'rare')
INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
SELECT v, 'test-relic-xi-' || v, 'Test Relic Xi ' || v, ra.id, c.id, 'AOC-050 verify round 3 fixture'
FROM c, ra, generate_series(9701, 9704) v`)
			// A row at the top place, one at the second, and two at the deepest (one with a boss).
			exec(fmt.Sprintf(`
WITH ss AS (SELECT sc.id FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
            WHERE t.groups = '{section}' ORDER BY t.sort_order, sc.sort_order LIMIT 1)
INSERT INTO item_sources (item_id, acquisition_type_id, section_id, place_id, boss_id, confidence_id, source_note)
SELECT v.item_id, (SELECT id FROM acquisition_types WHERE slug = 'drop'), ss.id,
       (SELECT id FROM places WHERE slug = '%[1]s' || v.at),
       CASE WHEN v.boss THEN (SELECT id FROM bosses WHERE slug = 'test-boss-xi') END,
       (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), 'AOC-050 verify round 3 fixture'
FROM ss, (VALUES (9701, 1, false), (9702, 2, false), (9703, %[2]d, false), (9704, %[2]d, true)) AS v(item_id, at, boss)`, prefix, depth))
			everyBranchListsItsCount(t, s, func(src string) bool { return strings.Contains(src, prefix) })
		})
	}
}
