package db_test

import (
	"context"
	"testing"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
	"github.com/pierrehrt/aoc-api/internal/items"
)

// AOC-012, verify round 1. The regression test the two-dungeon case was missing.
//
// The existing TestCollapsingAgainstTheRealCorpus asserts that a shared item appears TWICE when two
// dungeons are named — the duplication half of Pierre's rule. It never asserts the other half: that
// the answer contains NOTHING ELSE. Those are different claims, and only the first one held.
//
// Measured on the real corpus: ?place=vistrix-s-lair&place=yakhmar-s-cave answered total=4646 — the
// whole armory — because Service.List only passes a place down to SQL when exactly ONE is named, so
// two named places drop the filter entirely and every unrelated item falls through the
// "matched == 0" fallback as a row with no place.
func TestNamingTwoPlacesFiltersToThoseTwoPlaces(t *testing.T) {
	pool := readPool(t)
	ctx := context.Background()
	s := items.NewService(sqlcgen.New(pool))

	// Two places that genuinely share at least one item, chosen by query so this survives a new
	// snapshot.
	var p1, p2 string
	err := pool.QueryRow(ctx, `
		SELECT min(p.slug), max(p.slug)
		FROM items i
		JOIN item_sources src ON src.item_id = i.item_id
		JOIN places p ON p.id = src.place_id
		GROUP BY i.item_id
		HAVING count(DISTINCT p.slug) >= 2
		ORDER BY min(p.slug)
		LIMIT 1`).Scan(&p1, &p2)
	if err != nil {
		t.Skipf("no item shared by two places in this corpus: %v", err)
	}

	// What the answer must be about: the distinct items sourced in either named place.
	var want int64
	if err := pool.QueryRow(ctx, `
		SELECT count(DISTINCT src.item_id)
		FROM item_sources src JOIN places p ON p.id = src.place_id
		WHERE p.slug = ANY($1::varchar[])`, []string{p1, p2}).Scan(&want); err != nil {
		t.Fatal(err)
	}

	res, err := s.List(ctx, items.Filters{Places: []string{p1, p2}, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}

	if res.Total != want {
		t.Errorf("total = %d for place=%s&place=%s, want %d (the items in those two places); "+
			"a total of the whole corpus means the place filter never reached SQL",
			res.Total, p1, p2, want)
	}
	for _, it := range res.Items {
		if it.Place == nil {
			t.Errorf("row %q came back with no place; a place view must attach the place it matched", it.Slug)
			continue
		}
		if it.Place.Slug != p1 && it.Place.Slug != p2 {
			t.Errorf("row %q came back under %q, want only %q or %q", it.Slug, it.Place.Slug, p1, p2)
		}
	}
}
