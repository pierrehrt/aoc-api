//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-050: the Armory's source panel on the real corpus.

// ⭐ The tab of every one of AoC>TV's 39 sections, as Pierre assigned them on 2026-10-02
// (DECISIONS.md). This is the spec, read back from the database.
var pierresTabs = map[string]string{
	"PvE Tier 1": "pve", "PvE Tier 2": "pve", "PvE Tier 3": "pve", "PvE Tier 3.5": "pve",
	"PvE Tier 4": "pve", "PvE Tier 5": "pve", "PvE Tier 6": "pve",
	"PvP Tier 1": "pvp", "PvP Tier 2": "pvp", "PvP Tier 3": "pvp",
	"Aquilonia": "region", "Cimmeria": "region", "Stygia": "region", "Khitai": "region", "Turan": "region",
	"House of Crom": "region", "Dragon's Spine": "region",
	"Brittle Blade": "faction", "Children of Yag-kosha": "faction", "Clan Vigdis": "faction",
	"Hyrkanians": "faction", "Jiang Shi": "faction", "Last Legion": "faction", "Scarlet Circle": "faction",
	"Scholars of Cheng-Ho": "faction", "Shadows of Jade": "faction", "Tamarin's Tigers": "faction",
	"Wolves of the Steppes": "faction", "Yellow Priests of Yun": "faction",
	"Skull Gate Pass": "onslaught", "Kutchemes Temple": "onslaught",
	"Raidfinder": "other", "Item Store": "other", "Bags": "other", "Unsorted Items": "other",
	"World Boss": "other", "Unchained": "other", "Consumable Books": "other", "Recipes": "other",
}

func TestEverySectionIsInPierresTab(t *testing.T) {
	pool := readPool(t)
	rows, err := sqlcgen.New(pool).ListSectionTabs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range rows {
		got[r.Name] = r.TabSlug
	}
	for name, tab := range pierresTabs {
		if got[name] != tab {
			t.Errorf("%s is in tab %q, Pierre put it in %q", name, got[name], tab)
		}
	}
	if len(got) != len(pierresTabs) {
		t.Errorf("%d sections in the database, %d in Pierre's assignment", len(got), len(pierresTabs))
	}
	var orphans int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM item_sources s LEFT JOIN sections sc ON sc.name = s.section_raw WHERE s.section_id IS NULL OR sc.id IS NULL OR sc.id <> s.section_id`).Scan(&orphans); err != nil {
		t.Fatal(err)
	}
	if orphans != 0 {
		t.Errorf("%d source rows have no section, or one other than their own section_raw", orphans)
	}
}

// ⭐ THE CRITERION: every branch of every tab counts exactly what picking it lists — and each half
// of it, what picking that half lists — unfiltered and under filters.
func TestEveryBranchCountsWhatPickingItLists(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) == 0 {
		t.Fatal("no tabs")
	}

	// A class picked by position, so this names no real class.
	var class string
	if err := pool.QueryRow(ctx, `SELECT slug FROM classes ORDER BY sort_order NULLS LAST, slug LIMIT 1`).Scan(&class); err != nil {
		t.Fatal(err)
	}
	states := []struct {
		name string
		f    items.Filters
	}{
		{"unfiltered", items.Filters{}},
		{"epic", items.Filters{Rarities: []string{"epic"}}},
		{"a class", items.Filters{Classes: []string{class}}},
	}

	for _, tab := range tabs {
		shape := -1 // a tab's branches, unfiltered: every state must list the same ones
		for _, st := range states {
			t.Run(tab.Slug+"/"+st.name, func(t *testing.T) {
				f := st.f
				f.Tab = tab.Slug
				tree, err := s.Tree(ctx, f)
				if err != nil {
					t.Fatal(err)
				}
				var nodes int
				var walk func(ns []*items.TreeNode)
				walk = func(ns []*items.TreeNode) {
					for _, n := range ns {
						nodes++
						src, err := items.ParseSource(n.Source)
						if err != nil {
							t.Fatalf("%s: the tree wrote a source the parser refuses: %v", n.Source, err)
						}
						g := f
						g.Source, g.Limit = src, 1
						res, err := s.List(ctx, g)
						if err != nil {
							t.Fatal(err)
						}
						if res.Total != n.Count {
							t.Errorf("%s: the branch says %d, picking it lists %d", n.Source, n.Count, res.Total)
						}
						// The halves only unfiltered: the same rule, and the corpus run stays short.
						if st.name == "unfiltered" {
							for _, gc := range n.Groups {
								h := g
								h.Source.Group = gc.Slug
								if res, err := s.List(ctx, h); err != nil {
									t.Fatal(err)
								} else if res.Total != gc.Count {
									t.Errorf("%s get=%s: the half says %d, picking it lists %d", n.Source, gc.Slug, gc.Count, res.Total)
								}
							}
						}
						walk(n.Children)
					}
				}
				walk(tree.Nodes)
				if st.name == "unfiltered" {
					if nodes == 0 {
						t.Errorf("tab %s has no branch", tab.Slug)
					}
					shape = nodes
				} else if nodes != shape {
					t.Errorf("%s lists %d branches under %s, %d unfiltered: a branch the filters empty must show its 0", tab.Slug, nodes, st.name, shape)
				}
				t.Logf("%s %s: %d branches, %d items", tab.Slug, st.name, nodes, tree.Total)
			})
		}
	}
}

// ⭐ A node is ONE source row. Found by query: an item that is in a section on one row and at a place
// on another, with no row that has both. tier=/place= style matching would list it under the
// section's place branch; the source filter must not.
func TestASourceNodeMatchesOneRowNotTwo(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))

	var tab, section, place string
	err := pool.QueryRow(ctx, `
		SELECT t.slug, sc.slug, p.slug
		FROM item_sources a
		JOIN sections sc ON sc.id = a.section_id
		JOIN source_tabs t ON t.id = sc.tab_id
		JOIN item_sources b ON b.item_id = a.item_id AND b.id <> a.id
		JOIN places p ON p.id = b.place_id
		WHERE 'section' = ANY(t.groups)
		  AND EXISTS (SELECT 1 FROM item_sources c WHERE c.section_id = sc.id AND c.place_id = p.id)
		  AND NOT EXISTS (SELECT 1 FROM item_sources d WHERE d.item_id = a.item_id AND d.section_id = sc.id AND d.place_id = p.id)
		ORDER BY 1, 2, 3 LIMIT 1`).Scan(&tab, &section, &place)
	if err != nil {
		t.Skipf("no item is in a section on one row and at a place of that section on another: %v", err)
	}
	src, _ := items.ParseSource("s:" + section + ".p:" + place)
	var want int64
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT s.item_id) FROM item_sources s JOIN sections sc ON sc.id = s.section_id
		JOIN places p ON p.id = s.place_id WHERE sc.slug = $1 AND p.slug = $2`, section, place).Scan(&want); err != nil {
		t.Fatal(err)
	}
	res, err := s.List(ctx, items.Filters{Tab: tab, Source: src, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != want {
		t.Errorf("%s: %d items, want %d — the rows that have both, not the items that have each", src, res.Total, want)
	}
}

// A place with places inside it is, in the panel too, everything inside it (AOC-038).
func TestAComplexBranchIsItsWingsLoot(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))
	var parent string
	if err := pool.QueryRow(ctx, `
		SELECT DISTINCT parent.slug FROM places parent JOIN places c ON c.parent_place_id = parent.id
		ORDER BY 1 LIMIT 1`).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	tabs, err := s.Tabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tab := range tabs {
		tree, err := s.Tree(ctx, items.Filters{Tab: tab.Slug})
		if err != nil {
			t.Fatal(err)
		}
		var walk func(ns []*items.TreeNode)
		walk = func(ns []*items.TreeNode) {
			for _, n := range ns {
				if n.Kind == "place" && n.Slug == parent {
					found = true
					if len(n.Children) == 0 {
						t.Errorf("%s in %s has no wing under it", parent, tab.Slug)
					}
					var want int64
					if err := pool.QueryRow(ctx, insideCount, parent).Scan(&want); err != nil {
						t.Fatal(err)
					}
					if n.Count > want {
						t.Errorf("%s in %s counts %d, more than everything inside it (%d)", parent, tab.Slug, n.Count, want)
					}
					t.Logf("%s in %s: %d (all of it: %d)", parent, tab.Slug, n.Count, want)
				}
				walk(n.Children)
			}
		}
		walk(tree.Nodes)
	}
	if !found {
		t.Errorf("%s is in no tab's tree", parent)
	}
}
