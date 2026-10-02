package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-050, verify round 1. What the corpus cannot show: the corpus has none of the shapes below, so
// TestEveryBranchCountsWhatPickingItLists passes on it whether or not the rule holds in general.
// These plant them on a throwaway database and ask the real service (Tree and List, the real SQL).
// Every name is fake (STEP ZERO); the sections are the migration's own seeded rows.
//
//  1. A level the tree SKIPS (a row with no region, a row with no place) must not let the branch's
//     list take rows the branch does not hold. Item 9601 has the vendor in a region, item 9602 has
//     it with no region: the tree draws two vendor branches, one item each.
//  2. The same with a place: 9603 has the boss in a place, 9604 has it with no place.
//  3. A place two levels down (AOC-038 expands at any depth): test-keep-kappa › test-hall-kappa ›
//     test-vault-kappa. 9605 is in the vault, 9606 in the keep itself.
//  4. A source path names a place AND its wing; a "wing" of the wrong place names nothing.
//  5. A half ("loot / drops", "quest / vendor") the filters empty stays listed at 0.
func TestEveryBranchCountsWhatPickingItListsOnShapesTheCorpusLacks(t *testing.T) {
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
SELECT r.id, v.slug, v.name, c.id, 'AOC-050 verify fixture'
FROM r, c, (VALUES ('test-keep-kappa', 'Test Keep Kappa'), ('test-hall-kappa', 'Test Hall Kappa'),
                   ('test-vault-kappa', 'Test Vault Kappa'), ('test-other-kappa', 'Test Other Kappa')) AS v(slug, name)`)
	exec(`
UPDATE places c SET parent_place_id = p.id
FROM (VALUES ('test-hall-kappa', 'test-keep-kappa'), ('test-vault-kappa', 'test-hall-kappa')) AS v(child, parent)
JOIN places p ON p.slug = v.parent
WHERE c.slug = v.child`)
	exec(`
INSERT INTO vendors (slug, name, confidence_id, source_note)
SELECT 'test-vendor-kappa', 'Test Vendor Kappa', id, 'AOC-050 verify fixture' FROM confidence_levels WHERE slug = 'unconfirmed'`)
	exec(`
INSERT INTO bosses (slug, name, confidence_id, source_note)
SELECT 'test-boss-kappa', 'Test Boss Kappa', id, 'AOC-050 verify fixture' FROM confidence_levels WHERE slug = 'unconfirmed'`)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
     ra AS (SELECT id FROM rarities WHERE slug = 'rare')
INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
SELECT v.id, 'test-relic-kappa-' || v.id, 'Test Relic Kappa ' || v.id, ra.id, c.id, 'AOC-050 verify fixture'
FROM c, ra, (VALUES (9601), (9602), (9603), (9604), (9605), (9606)) AS v(id)`)
	// Which seeded sections: one of a tab that draws {section, region}, one of a tab that draws
	// {section} — read from source_tabs, so this names no section of its own.
	exec(`
WITH sr AS (SELECT sc.id FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
            WHERE t.groups = '{section,region}' ORDER BY t.sort_order, sc.sort_order LIMIT 1),
     ss AS (SELECT sc.id FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
            WHERE t.groups = '{section}' ORDER BY t.sort_order, sc.sort_order LIMIT 1),
     r AS (SELECT id FROM regions ORDER BY id LIMIT 1),
     c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
     drop_ AS (SELECT id FROM acquisition_types WHERE slug = 'drop'),
     vend AS (SELECT id FROM acquisition_types WHERE slug = 'vendor')
INSERT INTO item_sources (item_id, acquisition_type_id, section_id, region_id, place_id, boss_id, vendor_id, confidence_id, source_note)
SELECT v.item_id,
       CASE WHEN v.vendor THEN (SELECT id FROM vend) ELSE (SELECT id FROM drop_) END,
       CASE WHEN v.tab_region THEN (SELECT id FROM sr) ELSE (SELECT id FROM ss) END,
       CASE WHEN v.region THEN (SELECT id FROM r) END,
       (SELECT id FROM places WHERE slug = v.place),
       CASE WHEN v.boss THEN (SELECT id FROM bosses WHERE slug = 'test-boss-kappa') END,
       CASE WHEN v.vendor THEN (SELECT id FROM vendors WHERE slug = 'test-vendor-kappa') END,
       (SELECT id FROM c), 'AOC-050 verify fixture'
FROM (VALUES
  -- 1. the vendor in a region, and the same vendor with no region
  (9601, true,  true,  NULL,               false, true),
  (9602, true,  false, NULL,               false, true),
  -- 2. the boss in a place, and the same boss with no place
  (9603, false, false, 'test-other-kappa', true,  false),
  (9604, false, false, NULL,               true,  false),
  -- 3. a place two levels down, and the top place itself
  (9605, false, false, 'test-vault-kappa', false, false),
  (9606, false, false, 'test-keep-kappa',  false, false)
) AS v(item_id, tab_region, region, place, boss, vendor)`)

	s := items.NewService(sqlcgen.New(pool))
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("every branch of every tab lists exactly its count", func(t *testing.T) {
		var nodes int
		for _, tab := range tabs {
			tree, err := s.Tree(ctx, items.Filters{Tab: tab.Slug})
			if err != nil {
				t.Fatal(err)
			}
			var walk func(ns []*items.TreeNode)
			walk = func(ns []*items.TreeNode) {
				for _, n := range ns {
					nodes++
					src, err := items.ParseSource(n.Source)
					if err != nil {
						t.Fatalf("%s: %v", n.Source, err)
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
					walk(n.Children)
				}
			}
			walk(tree.Nodes)
		}
		if nodes == 0 {
			t.Fatal("the fixture drew no branch at all")
		}
	})

	// 5. A half of a branch the filters empty is listed with its 0, like the branch itself: the
	// tree's shape, halves included, is the same under every filter. Every fixture item is rare.
	t.Run("a half the filters empty is listed with 0", func(t *testing.T) {
		halves := func(f items.Filters) map[string]map[string]int64 {
			out := map[string]map[string]int64{}
			for _, tab := range tabs {
				f.Tab = tab.Slug
				tree, err := s.Tree(ctx, f)
				if err != nil {
					t.Fatal(err)
				}
				var walk func(ns []*items.TreeNode)
				walk = func(ns []*items.TreeNode) {
					for _, n := range ns {
						m := map[string]int64{}
						for _, g := range n.Groups {
							m[g.Slug] = g.Count
						}
						out[tab.Slug+" "+n.Source] = m
						walk(n.Children)
					}
				}
				walk(tree.Nodes)
			}
			return out
		}
		all, epic := halves(items.Filters{}), halves(items.Filters{Rarities: []string{"epic"}})
		for node, hs := range all {
			for g := range hs {
				c, ok := epic[node][g]
				if !ok {
					t.Errorf("%s: the %s half is listed unfiltered and gone under rarity=epic; want it at 0", node, g)
				} else if c != 0 {
					t.Errorf("%s: the %s half counts %d under rarity=epic, and no fixture item is epic", node, g, c)
				}
			}
		}
	})

	t.Run("a place's wing must be inside it (unknown or wrong first place matches nothing)", func(t *testing.T) {
		for raw, want := range map[string]int64{
			"p:test-hall-kappa.p:test-vault-kappa":    1, // its own wing
			"p:test-other-kappa.p:test-vault-kappa":   0, // a place it is not inside
			"p:test-nowhere-kappa.p:test-vault-kappa": 0, // a place no row has: an unknown slug matches nothing
			"p:test-vault-kappa.p:test-keep-kappa":    0, // reversed: the keep is not inside the vault
		} {
			src, err := items.ParseSource(raw)
			if err != nil {
				t.Fatalf("%s: %v", raw, err)
			}
			for _, tab := range tabs {
				if tab.Groups[0] != "section" || len(tab.Groups) != 1 {
					continue
				}
				res, err := s.List(ctx, items.Filters{Tab: tab.Slug, Source: src, Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				if res.Total != want {
					t.Errorf("tab %s source=%s: %d items, want %d — every level named, on the same row", tab.Slug, raw, res.Total, want)
				}
				break
			}
		}
	})
}

// AOC-050 criterion 2, through the importer's own harness: every row lands with the section its
// section_raw names, and a re-import (delete and re-insert) gives every row the same section_id.
// The unit test covers the unknown name; nothing else read section_id back after a real import.
func TestTheImporterLandsEveryRowWithItsSectionTwice(t *testing.T) {
	pool, _ := importTarget(t)
	ctx := context.Background()
	sections := func() map[string]int32 {
		t.Helper()
		rows, err := pool.Query(ctx, `
SELECT s.item_id::text || ' ' || coalesce(s.section_raw, '') || ' ' || s.acquisition_type_id::text, s.section_id
FROM item_sources s ORDER BY s.id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]int32{}
		for rows.Next() {
			var k string
			var id *int32
			if err := rows.Scan(&k, &id); err != nil {
				t.Fatal(err)
			}
			if id == nil {
				t.Errorf("%s: no section_id after the import", k)
				continue
			}
			out[k] = *id
		}
		return out
	}
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("import: %v", r.err)
	}
	var wrong int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM item_sources s LEFT JOIN sections sc ON sc.id = s.section_id
WHERE s.section_raw IS NOT NULL AND sc.name IS DISTINCT FROM s.section_raw`).Scan(&wrong); err != nil {
		t.Fatal(err)
	}
	if wrong != 0 {
		t.Errorf("%d imported rows point at a section other than their own section_raw", wrong)
	}
	first := sections()
	if len(first) == 0 {
		t.Fatal("the fixture imported no source row")
	}
	if r := runImport(t, pool, fixtureJSON); r.err != nil {
		t.Fatalf("re-import: %v", r.err)
	}
	second := sections()
	if len(second) != len(first) {
		t.Fatalf("%d rows after the re-import, %d before", len(second), len(first))
	}
	for k, id := range first {
		if second[k] != id {
			t.Errorf("%s: section_id %d, then %d after the re-import", k, id, second[k])
		}
	}
}
