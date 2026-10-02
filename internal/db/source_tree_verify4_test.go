package db_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-050, verify round 4. The depth rule (DECISIONS.md, 2026-10-02): the tree draws a row's whole
// ancestry, and the parser keeps no cap, so the hierarchy is the only bound. Chains of fake places
// (STEP ZERO) are planted on a throwaway database and asked of the real service. The sections are
// the migration's own seeded rows, chosen by their tab's groups. Round 3's chains stop at 33 and
// round 4's unit test at 40, so a cap of 64 failed nothing; a chain of 100 does.

// plantChain plants a chain of depth fake places, prefix1 the top and each the parent of the next.
// The region changes every ten places, so a deep place may sit in another region than its top.
func plantChain(t *testing.T, exec func(string), prefix string, depth int) {
	t.Helper()
	exec(fmt.Sprintf(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO places (region_id, slug, name, confidence_id, source_note)
SELECT (ARRAY(SELECT id FROM regions ORDER BY id))[((g - 1) / 10) %% 3 + 1],
       '%[1]s' || g, initcap(replace('%[1]s', '-', ' ')) || g, c.id, 'AOC-050 verify round 4 fixture'
FROM c, generate_series(1, %[2]d) g`, prefix, depth))
	exec(fmt.Sprintf(`
UPDATE places c SET parent_place_id = p.id
FROM generate_series(2, %[2]d) g
JOIN places p ON p.slug = '%[1]s' || (g - 1)
WHERE c.slug = '%[1]s' || g`, prefix, depth))
}

// Every branch on a chain of 1, 2, 32, 33, 40 and 100 places lists its count and each half, and
// every place is drawn once, under exactly its own ancestry from the top place. Rows sit at the top,
// the second place, the middle, the last but one and the deepest (one with a boss), in a section of
// a tab that draws {section} and one that draws {region, map}.
func TestEveryBranchListsItsCountOnAChainOfAnyDepth(t *testing.T) {
	for _, depth := range []int{1, 2, 32, 33, 40, 100} {
		t.Run(fmt.Sprintf("a chain of %d places", depth), func(t *testing.T) {
			s, exec := plantedService(t)
			prefix := fmt.Sprintf("test-depth%d-omicron-", depth)
			plantChain(t, exec, prefix, depth)
			exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed')
INSERT INTO bosses (slug, name, confidence_id, source_note) SELECT 'test-boss-omicron', 'Test Boss Omicron', id, 'AOC-050 verify round 4 fixture' FROM c`)
			exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), ra AS (SELECT id FROM rarities WHERE slug = 'rare')
INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
SELECT v, 'test-relic-omicron-' || v, 'Test Relic Omicron ' || v, ra.id, c.id, 'AOC-050 verify round 4 fixture'
FROM c, ra, generate_series(9601, 9612) v`)
			exec(fmt.Sprintf(`
WITH ss AS (SELECT DISTINCT ON (t.groups) sc.id, t.groups FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
            WHERE t.groups IN ('{section}', '{region,map}') ORDER BY t.groups, t.sort_order, sc.sort_order),
     at(item_id, k, boss) AS (VALUES (9601, 1, false), (9602, 2, false), (9603, (%[2]d + 1) / 2, false),
                                     (9604, %[2]d - 1, false), (9605, %[2]d, false), (9606, %[2]d, true))
INSERT INTO item_sources (item_id, acquisition_type_id, section_id, place_id, boss_id, confidence_id, source_note)
SELECT at.item_id + CASE WHEN ss.groups = '{section}' THEN 0 ELSE 6 END,
       (SELECT id FROM acquisition_types WHERE slug = CASE WHEN at.item_id %% 2 = 0 THEN 'drop' ELSE 'vendor' END),
       ss.id, (SELECT id FROM places WHERE slug = '%[1]s' || at.k),
       CASE WHEN at.boss THEN (SELECT id FROM bosses WHERE slug = 'test-boss-omicron') END,
       (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), 'AOC-050 verify round 4 fixture'
FROM ss, at WHERE at.k >= 1`, prefix, depth))
			everyBranchListsItsCount(t, s, func(src string) bool { return strings.Contains(src, prefix) })

			// Drawn once, at the right level: a place's branch names its whole ancestry, top first.
			ctx := context.Background()
			tabs, err := s.Tabs(ctx)
			if err != nil {
				t.Fatal(err)
			}
			places := 0
			for _, tab := range tabs {
				tree, err := s.Tree(ctx, items.Filters{Tab: tab.Slug})
				if err != nil {
					t.Fatal(err)
				}
				drawn := map[string]int{}
				var walk func(ns []*items.TreeNode)
				walk = func(ns []*items.TreeNode) {
					for _, n := range ns {
						walk(n.Children)
						if n.Kind != "place" || !strings.HasPrefix(n.Slug, prefix) {
							continue
						}
						places++
						k, err := strconv.Atoi(strings.TrimPrefix(n.Slug, prefix))
						if err != nil {
							t.Fatalf("%s: not one of the fixture's places: %v", n.Slug, err)
						}
						var want, above []string
						for i := 1; i <= k; i++ {
							want = append(want, "p:"+prefix+fmt.Sprint(i))
						}
						var got []string
						for _, seg := range strings.Split(n.Source, ".") {
							if strings.HasPrefix(seg, "p:") {
								got = append(got, seg)
							} else {
								above = append(above, seg)
							}
						}
						if strings.Join(got, ".") != strings.Join(want, ".") {
							t.Errorf("%s %s: drawn under %d places, want its whole ancestry of %d from the top", tab.Slug, n.Source, len(got), k)
						}
						drawn[strings.Join(above, ".")+" "+n.Slug]++
					}
				}
				walk(tree.Nodes)
				for k, n := range drawn {
					if n > 1 {
						t.Errorf("%s: %s is drawn %d times under one branch", tab.Slug, k, n)
					}
				}
			}
			if places == 0 {
				t.Fatal("the fixture drew no place")
			}
		})
	}
}

// With no cap, the hierarchy is the bound: a path deeper than it, or with any link that is not a
// parent of the next, names nothing, at a depth no fixed cap would reach. Every path below parses.
func TestAPathDeeperThanItsHierarchyOrWithAWrongLinkNamesNothing(t *testing.T) {
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
	const deep, other = "test-deep-omicron-", "test-other-omicron-"
	plantChain(t, exec, deep, 100)
	plantChain(t, exec, other, 3)
	exec(`
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'), ra AS (SELECT id FROM rarities WHERE slug = 'rare')
INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
SELECT 9651, 'test-relic-omicron-9651', 'Test Relic Omicron 9651', ra.id, c.id, 'AOC-050 verify round 4 fixture' FROM c, ra`)
	var section, tab string
	if err := pool.QueryRow(ctx, `
SELECT sc.slug, t.slug FROM sections sc JOIN source_tabs t ON t.id = sc.tab_id
WHERE t.groups = '{section}' ORDER BY t.sort_order, sc.sort_order LIMIT 1`).Scan(&section, &tab); err != nil {
		t.Fatal(err)
	}
	exec(fmt.Sprintf(`
INSERT INTO item_sources (item_id, acquisition_type_id, section_id, place_id, confidence_id, source_note)
SELECT 9651, (SELECT id FROM acquisition_types WHERE slug = 'drop'), (SELECT id FROM sections WHERE slug = '%s'),
       (SELECT id FROM places WHERE slug = '%s100'), (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
       'AOC-050 verify round 4 fixture'`, section, deep))

	s := items.NewService(sqlcgen.New(pool))
	chain := func(prefix string, ks ...int) []string {
		var out []string
		for _, k := range ks {
			out = append(out, "p:"+prefix+fmt.Sprint(k))
		}
		return out
	}
	upTo := func(n int) []int {
		var out []int
		for k := 1; k <= n; k++ {
			out = append(out, k)
		}
		return out
	}
	whole := chain(deep, upTo(100)...)
	swapped := append([]string{}, whole...)
	swapped[59], swapped[60] = swapped[60], swapped[59]
	var reversed []string
	for i := len(whole) - 1; i >= 0; i-- {
		reversed = append(reversed, whole[i])
	}
	cases := []struct {
		name   string
		places []string
		want   int64
	}{
		{"the whole chain of 100", whole, 1},
		{"one place deeper than the hierarchy: its top again", append(append([]string{}, whole...), "p:"+deep+"1"), 0},
		{"one place deeper than the hierarchy: the deepest again", append(append([]string{}, whole...), "p:"+deep+"100"), 0},
		{"one place deeper than the hierarchy: an unknown place", append(append([]string{}, whole...), "p:test-nowhere-omicron"), 0},
		{"an unknown place first", append([]string{"p:test-nowhere-omicron"}, whole...), 0},
		{"two places swapped 60 levels down", swapped, 0},
		{"a link skipped 70 levels down", append(append([]string{}, whole[:69]...), whole[70:]...), 0},
		{"another chain's place 50 levels down", append(append(append([]string{}, whole[:50]...), chain(other, 2)...), whole[50:]...), 0},
		{"reversed", reversed, 0},
	}
	for _, c := range cases {
		path := "s:" + section + "." + strings.Join(c.places, ".")
		src, err := items.ParseSource(path)
		if err != nil {
			t.Errorf("%s: a path of %d places is refused: %v", c.name, len(c.places), err)
			continue
		}
		res, err := s.List(ctx, items.Filters{Tab: tab, Source: src, Limit: items.MaxLimit})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.Total != c.want {
			t.Errorf("%s: lists %d, want %d", c.name, res.Total, c.want)
		}
	}
}
