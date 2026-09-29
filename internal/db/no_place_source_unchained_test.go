package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pierrehrt/aoc-api/internal/db/sqlcgen"
)

// AOC-039 verify round 1 — the edge case the test plan names: a source with NO place (11 in the
// corpus today). The one unchained expression is `src.unchained OR coalesce(p.unchained, false)`,
// and the coalesce is load-bearing only here: with a NULL place, `false OR NULL` is NULL, sqlc
// types the cast as a non-null bool, and the scan fails on exactly the item page that has no
// dungeon to blame. TestEveryQueryPublishesTheSameUnchainedAndBlanksAnAmbiguousTier gives every
// source a place, so a mutant that drops the coalesce survives it; this one kills it.
//
// Two fake items, both without a place: one whose own flag is false, one whose own flag is true.
// The item page must read each flag as written, and the list filter must sort them the same way.
func TestASourceWithoutAPlaceStillPublishesItsOwnUnchainedFlag(t *testing.T) {
	_, url := migratedDB(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	const flagFalse, flagTrue = 9402, 9403
	_, err = pool.Exec(ctx, `
WITH c AS (SELECT id FROM confidence_levels WHERE slug = 'unconfirmed'),
     ra AS (SELECT id FROM rarities WHERE slug = 'rare'),
     i AS (
       INSERT INTO items (item_id, slug, name, rarity_id, confidence_id, source_note)
       SELECT $1::integer, 'test-relic-sigma', 'Test Relic Sigma', ra.id, c.id, 'AOC-039 verify fixture' FROM ra, c
       UNION ALL
       SELECT $2::integer, 'test-relic-tau', 'Test Relic Tau', ra.id, c.id, 'AOC-039 verify fixture' FROM ra, c
       RETURNING item_id)
INSERT INTO item_sources (item_id, acquisition_type_id, place_id, tier_id, unchained, confidence_id, source_note)
SELECT i.item_id, (SELECT id FROM acquisition_types WHERE slug = 'vendor'), NULL, NULL,
       i.item_id = $2::integer, c.id, 'AOC-039 verify fixture: no place'
FROM i, c`, flagFalse, flagTrue)
	if err != nil {
		t.Fatalf("planting the fixture: %v", err)
	}
	q := sqlcgen.New(pool)

	// 1. The item page reads the source's own flag, and does not error on the NULL place.
	for _, tc := range []struct {
		id   int32
		want bool
	}{{flagFalse, false}, {flagTrue, true}} {
		srcs, err := q.ListItemSources(ctx, tc.id)
		if err != nil {
			t.Fatalf("ListItemSources(%d) with a NULL place: %v", tc.id, err)
		}
		if len(srcs) != 1 {
			t.Fatalf("ListItemSources(%d) returned %d rows, want 1", tc.id, len(srcs))
		}
		if srcs[0].Unchained != tc.want {
			t.Errorf("ListItemSources(%d) says unchained=%v for a place-less source whose own flag is %v", tc.id, srcs[0].Unchained, tc.want)
		}
	}

	// 2. The list filter sorts them the same way: unchained=false finds the false one and not the
	// true one, unchained=true the reverse. A NULL place must never hide a row from either side.
	listed := func(flag bool) map[int32]bool {
		rows, err := q.ListItems(ctx, sqlcgen.ListItemsParams{Unchained: &flag, PageSize: 50})
		if err != nil {
			t.Fatalf("ListItems(unchained=%v): %v", flag, err)
		}
		got := map[int32]bool{}
		for _, r := range rows {
			got[r.ItemID] = true
		}
		return got
	}
	if got := listed(false); !got[flagFalse] || got[flagTrue] {
		t.Errorf("unchained=false lists %v; want item %d listed and %d not", got, flagFalse, flagTrue)
	}
	if got := listed(true); got[flagFalse] || !got[flagTrue] {
		t.Errorf("unchained=true lists %v; want item %d listed and %d not", got, flagTrue, flagFalse)
	}

	// 3. No place, nothing to summarise per place: the page still shows the source via
	// ListItemSources, but the "also drops in" summary has no dungeon to name.
	places, err := q.ListItemPlaces(ctx, []int32{flagFalse, flagTrue})
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 0 {
		t.Errorf("ListItemPlaces returned %d rows for place-less sources, want 0", len(places))
	}
}
