//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-038. Before it, place=house-of-crom answered 0 items and place=warmonk-monastery 1, because
// their loot is recorded against their wings and the filter matched one slug exactly. Pierre
// (2026-09-29): asking for the raid means everything in it, each item once.
//
// The parents are found by query, never named, so this keeps holding when a snapshot adds one.

// insideCount is the distinct items sourced in a place or any place under it, at any depth, computed
// here independently of the service.
const insideCount = `
	WITH RECURSIVE inside AS (
	    SELECT id FROM places WHERE slug = $1
	  UNION
	    SELECT c.id FROM places c JOIN inside ON c.parent_place_id = inside.id)
	SELECT count(DISTINCT src.item_id) FROM item_sources src WHERE src.place_id IN (SELECT id FROM inside)`

func TestEveryPlaceThatContainsPlacesListsAllItsLootOnce(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))

	rows, err := pool.Query(ctx, `
		SELECT DISTINCT parent.slug FROM places parent JOIN places c ON c.parent_place_id = parent.id
		ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var parents []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		parents = append(parents, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(parents) == 0 {
		t.Fatal("no place contains another in this corpus; the test would prove nothing")
	}

	for _, parent := range parents {
		t.Run(parent, func(t *testing.T) {
			var want int64
			if err := pool.QueryRow(ctx, insideCount, parent).Scan(&want); err != nil {
				t.Fatal(err)
			}
			if want == 0 {
				t.Fatalf("%s and the places in it hold no item", parent)
			}

			seen := map[string]int{}
			var total int64
			for offset := 0; ; offset += items.MaxLimit {
				res, err := s.List(ctx, items.Filters{Places: []string{parent}, Limit: items.MaxLimit, Offset: offset, WithFacets: offset == 0})
				if err != nil {
					t.Fatal(err)
				}
				if !res.Collapsed {
					t.Errorf("collapsed = false for %s; a place that contains places shows each item once", parent)
				}
				if offset == 0 {
					total = res.Total
					// Rarity is one value per item, so its counts add up to the list's total: the
					// rail counts the same places as the rows.
					var sum int64
					for _, v := range res.Facets.Rarity.Values {
						sum += v.Count
					}
					if sum != res.Total {
						t.Errorf("rarity counts sum to %d, the list's total is %d", sum, res.Total)
					}
				}
				for _, it := range res.Items {
					seen[it.Slug]++
					if it.Place != nil {
						t.Errorf("%s came back under one place (%s); a collapsed row carries its places as context", it.Slug, it.Place.Slug)
					}
				}
				if len(res.Items) < items.MaxLimit {
					break
				}
			}
			if total != want {
				t.Errorf("total = %d, want %d (the items in %s and the places inside it)", total, want, parent)
			}
			if int64(len(seen)) != want {
				t.Errorf("%d distinct items listed, want %d", len(seen), want)
			}
			for slug, n := range seen {
				if n != 1 {
					t.Errorf("%s listed %d times; each item appears once", slug, n)
				}
			}
			t.Logf("%s: %d items, each once", parent, want)
		})
	}
}

// The rule AOC-038 must not move: two wings of one complex, named together, are two dungeons, so the
// view stays expanded, one row per item per named wing. (No item is shared by two wings in this
// corpus, so "a shared item under each" is proved on fixtures, in internal/items.)
func TestTwoWingsOfOneComplexStayExpanded(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))

	var w1, w2 string
	if err := pool.QueryRow(ctx, `
		SELECT min(slug), max(slug) FROM places WHERE parent_place_id IS NOT NULL
		GROUP BY parent_place_id HAVING count(*) >= 2 ORDER BY 1 LIMIT 1`).Scan(&w1, &w2); err != nil {
		t.Fatalf("no complex with two wings in this corpus: %v", err)
	}
	var want int64
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT src.item_id) FROM item_sources src JOIN places p ON p.id = src.place_id
		WHERE p.slug = ANY($1::varchar[])`, []string{w1, w2}).Scan(&want); err != nil {
		t.Fatal(err)
	}

	res, err := s.List(ctx, items.Filters{Places: []string{w1, w2}, Limit: items.MaxLimit})
	if err != nil {
		t.Fatal(err)
	}
	if res.Collapsed {
		t.Errorf("collapsed = true for two wings (%s, %s); two dungeons stay expanded", w1, w2)
	}
	if res.Total != want {
		t.Errorf("total = %d, want %d (the items in %s and %s, and nothing else)", res.Total, want, w1, w2)
	}
	for _, it := range res.Items {
		if it.Place == nil || (it.Place.Slug != w1 && it.Place.Slug != w2) {
			t.Errorf("%s came back under %v, want %s or %s", it.Slug, it.Place, w1, w2)
		}
	}
}

// A leaf place is unchanged: it holds only its own loot, and it is not collapsed.
func TestALeafPlaceIsUnchanged(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))

	var leaf string
	var want int64
	err := pool.QueryRow(ctx, `
		SELECT p.slug, count(DISTINCT src.item_id)
		FROM places p JOIN item_sources src ON src.place_id = p.id
		WHERE NOT EXISTS (SELECT 1 FROM places c WHERE c.parent_place_id = p.id)
		GROUP BY p.slug ORDER BY count(DISTINCT src.item_id) DESC, p.slug LIMIT 1`).Scan(&leaf, &want)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.List(ctx, items.Filters{Places: []string{leaf}, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Collapsed || res.Total != want {
		t.Errorf("%s: collapsed=%v total=%d, want expanded and %d", leaf, res.Collapsed, res.Total, want)
	}
}
