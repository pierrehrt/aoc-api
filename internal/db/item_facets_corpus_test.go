//go:build corpus

// A corpus test: compiled only with `-tags corpus` (AOC-044) — see item_read_endpoints_test.go.

package db_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-049: every number the rail shows, against the real corpus — the same check as the fixture
// test, under filters picked FROM the data (the busiest value of each facet), never from a name
// typed here, so a re-import cannot make it stale.
func TestFacetCountsAreTheRowsTheyPromiseOnTheRealCorpus(t *testing.T) {
	s := svc(t)
	ctx := context.Background()
	res, err := s.List(ctx, items.Filters{WithFacets: true, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	busiest := func(g items.FacetGroup) string {
		best := items.FacetValue{}
		for _, v := range g.Values {
			if v.Count > best.Count {
				best = v
			}
		}
		if best.Slug == "" {
			t.Fatal("a facet has no value with items")
		}
		return best.Slug
	}
	fc := res.Facets
	yes := true
	top := fc.ItemLevel.Max
	for label, f := range map[string]items.Filters{
		"no filters":                   {},
		"the busiest rarity":           {Rarity: busiest(fc.Rarity)},
		"slot and weight":              {EquipLocation: busiest(fc.EquipLocation), ArmourWeight: busiest(fc.ArmourWeight)},
		"class and currency":           {Class: busiest(fc.Class), Currency: busiest(fc.Currency)},
		"has a price, the busiest set": {Price: &yes, Set: busiest(fc.Set)},
		"the top ten levels":           {ILvlMin: i32(top - 9)},
		"name, required level and pvp": {Query: "of", ReqLvlMax: i32(50), PvP: &yes},
	} {
		n := assertFacetsAreTheirRows(t, s, f, label)
		t.Logf("%s: %d numbers checked", label, n)
	}
}

// "Has a vendor price" matches ANY occurrence: an item sold by a vendor and also free from another
// source is priced. Picked from the data, then asked of the service.
func TestAPricedItemIsPricedEvenWhereItIsAlsoFree(t *testing.T) {
	pool := readPool(t)
	var id int32
	err := pool.QueryRow(context.Background(), `
SELECT src.item_id FROM item_sources src LEFT JOIN item_costs ic ON ic.item_source_id = src.id
GROUP BY src.item_id HAVING bool_or(ic.id IS NOT NULL) AND bool_or(ic.id IS NULL)
ORDER BY src.item_id LIMIT 1`).Scan(&id)
	if err != nil {
		t.Fatalf("no item is both priced and free in this corpus (%v) — the case is gone, look before deleting the test", err)
	}
	yes, no := true, false
	s := svc(t)
	q := strconv.Itoa(int(id))
	for _, c := range []struct {
		price *bool
		want  int64
	}{{&yes, 1}, {&no, 0}} {
		res, err := s.List(context.Background(), items.Filters{Query: q, Price: c.price, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		found := int64(0)
		for _, it := range res.Items {
			if it.ID == id {
				found++
			}
		}
		if found != c.want {
			t.Errorf("item %d (priced at one source, free at another) with price=%v: found %d, want %d", id, *c.price, found, c.want)
		}
	}
}

// Item level and required level are two ranges because they are two facts: the same bounds on each
// leave different items.
func TestTheTwoLevelRangesAreDifferentFilters(t *testing.T) {
	s := svc(t)
	a := total(t, s, items.Filters{ILvlMin: i32(80), ILvlMax: i32(80)})
	b := total(t, s, items.Filters{ReqLvlMin: i32(80), ReqLvlMax: i32(80)})
	if a == 0 || b == 0 || a == b {
		t.Errorf("ilvl 80 leaves %d items, reqlvl 80 leaves %d — they should both be non-empty and differ", a, b)
	}
}
